package sites

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	url2 "net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"kansho/cf"
	"kansho/config"
	"kansho/parser"

	"github.com/PuerkitoBio/goquery"
	//"github.com/chai2010/webp"
	"github.com/chromedp/chromedp"
)

// chapterImage holds URL and order
type chapterImage struct {
	Order int
	URL   string
}

// AsuraDownloadChapters downloads manga chapters from asuracomic.net website
// progressCallback is called with status updates during download
// Parameters: status string, progress (0.0-1.0), actual chapter number, current download, total chapters
func AsuraDownloadChapters(ctx context.Context, manga *config.Bookmarks, progressCallback func(string, float64, int, int, int)) error {

	// Step 1: Extract chapter URLs from the series page with retry
	chapterUrls, err := asuraChapterUrlsWithRetry(manga.Url)
	if err != nil {
		return err
	}

	log.Printf("<%s> Found %d total chapters on site", manga.Site, len(chapterUrls))

	// Step 2: Create the chapter map with filename as key and url as value
	chapterMap := asuraChapterMap(chapterUrls)
	log.Printf("<%s> Mapped %d chapters to filenames", manga.Site, len(chapterMap))

	// Step 3: Get existing CBZ files so their download can be skipped
	downloadedChapters, err := parser.LocalChapterList(manga.Location)
	if err != nil {
		return fmt.Errorf("failed to list files in %s: %v", manga.Location, err)
	}
	log.Printf("<%s> Found %d already downloaded chapters", manga.Site, len(downloadedChapters))

	// Store total chapters BEFORE filtering
	totalChaptersFound := len(chapterMap)

	// Step 4: Remove already-downloaded chapters
	for _, chapter := range downloadedChapters {
		delete(chapterMap, chapter)
	}

	newChaptersToDownload := len(chapterMap)
	if newChaptersToDownload == 0 {
		log.Printf("<%s> No new chapters to download [%s]", manga.Site, manga.Title)
		if progressCallback != nil {
			progressCallback("No new chapters to download", 1.0, 0, 0, totalChaptersFound)
		}
		return nil
	}

	log.Printf("<%s> %d new chapters to download [%s]", manga.Site, newChaptersToDownload, manga.Title)
	if progressCallback != nil {
		progressCallback(fmt.Sprintf("Found %d new chapters to download", newChaptersToDownload), 0, 0, 0, totalChaptersFound)
	}

	// Step 5: Sort chapter keys
	sortedChapters, sortError := parser.SortKeys(chapterMap)
	if sortError != nil {
		return fmt.Errorf("failed to sort chapter map keys: %v", sortError)
	}

	// Step 6: Download each chapter
	for idx, cbzName := range sortedChapters {

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		chapterURL := chapterMap[cbzName]

		// Extract the actual chapter number from the filename
		actualChapterNum := extractChapterNumber(cbzName)

		currentDownload := idx + 1
		progress := float64(currentDownload) / float64(newChaptersToDownload)

		if progressCallback != nil {
			progressCallback(
				fmt.Sprintf("Downloading chapter %d of %d", actualChapterNum, totalChaptersFound),
				progress,
				actualChapterNum,
				currentDownload,
				totalChaptersFound,
			)
		}

		log.Printf("[%s:%s] Starting download from: %s", manga.Shortname, cbzName, chapterURL)

		// Get sorted chapter images with retry
		chapterImages, err := asuraSortedChapterImagesWithRetry(chapterURL, manga.Shortname, cbzName)
		if err != nil {
			log.Printf("[%s:%s] ✖ Failed to get chapter images after retries: %v", manga.Shortname, cbzName, err)
			continue
		}

		if len(chapterImages) == 0 {
			log.Printf("[%s:%s] ⚠️ WARNING: No images found for chapter", manga.Shortname, cbzName)
			continue
		}

		log.Printf("[%s:%s] Found %d images to download", manga.Shortname, cbzName, len(chapterImages))

		// Create temp directory
		chapterDir := filepath.Join("/tmp", manga.Shortname, strings.TrimSuffix(cbzName, ".cbz"))
		err = os.MkdirAll(chapterDir, 0755)
		if err != nil {
			log.Printf("[%s:%s] Failed to create temporary directory %s: %v", manga.Shortname, cbzName, chapterDir, err)
			continue
		}

		successCount := 0
		rateLimiter := parser.NewRateLimiter(1500 * time.Millisecond)
		defer rateLimiter.Stop()

		// Download images
		for imgIdx, img := range chapterImages {

			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			rateLimiter.Wait()

			if progressCallback != nil {
				imgProgress := progress + (float64(imgIdx) / float64(len(chapterImages)) / float64(newChaptersToDownload))
				progressCallback(
					fmt.Sprintf("Chapter %d/%d: Downloading image %d/%d", actualChapterNum, totalChaptersFound, imgIdx+1, len(chapterImages)),
					imgProgress,
					actualChapterNum,
					currentDownload,
					totalChaptersFound,
				)
			}

			log.Printf("[%s:%s] Downloading image %d/%d: %s", manga.Shortname, cbzName, imgIdx+1, len(chapterImages), img.URL)
			err := asuraDownloadChapterImageWithRetry(img, chapterDir, manga.Shortname, cbzName)
			if err != nil {
				log.Printf("[%s:%s] ⚠️ Failed to download/convert image %s: %v", manga.Shortname, cbzName, img.URL, err)
			} else {
				successCount++
				log.Printf("[%s:%s] ✓ Successfully downloaded and converted image %d/%d", manga.Shortname, cbzName, imgIdx+1, len(chapterImages))
			}
		}

		log.Printf("[%s:%s] Download complete: %d/%d images successful", manga.Shortname, cbzName, successCount, len(chapterImages))

		if successCount == 0 {
			log.Printf("[%s:%s] ⚠️ Skipping CBZ creation - no images downloaded", manga.Shortname, cbzName)
			os.RemoveAll(chapterDir)
			continue
		}

		// Create CBZ
		if progressCallback != nil {
			progressCallback(
				fmt.Sprintf("Chapter %d/%d: Creating CBZ file...", actualChapterNum, totalChaptersFound),
				progress,
				actualChapterNum,
				currentDownload,
				totalChaptersFound,
			)
		}

		cbzPath := filepath.Join(manga.Location, cbzName)
		err = parser.CreateCbzFromDir(chapterDir, cbzPath)
		if err != nil {
			log.Printf("[%s:%s] Failed to create CBZ %s: %v", manga.Shortname, cbzName, cbzPath, err)
		} else {
			log.Printf("[%s] ✓ Created CBZ: %s (%d images)\n", manga.Title, cbzName, successCount)
		}

		err = os.RemoveAll(chapterDir)
		if err != nil {
			log.Printf("[%s:%s] Failed to remove temp directory %s: %v", manga.Shortname, cbzName, chapterDir, err)
		}
	}

	log.Printf("<%s> Download complete [%s]", manga.Site, manga.Title)
	if progressCallback != nil {
		progressCallback(
			fmt.Sprintf("Download complete! Downloaded %d chapters", newChaptersToDownload),
			1.0,
			0,
			newChaptersToDownload,
			totalChaptersFound,
		)
	}

	return nil
}

// asuraChapterUrlsWithRetry fetches chapter URLs with retry logic
func asuraChapterUrlsWithRetry(seriesURL string) ([]string, error) {
	maxRetries := 5
	baseTimeout := 10 * time.Second

	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		timeout := baseTimeout + (time.Duration(attempt) * 5 * time.Second)

		if attempt > 0 {
			log.Printf("<asura> Retry attempt %d/%d with timeout %v for: %s",
				attempt+1, maxRetries, timeout, seriesURL)
		} else {
			log.Printf("<asura> Fetching series page: %s (timeout: %v)", seriesURL, timeout)
		}

		chapters, err := asuraChapterUrls(seriesURL, timeout)

		// Success!
		if err == nil {
			if attempt > 0 {
				log.Printf("<asura> ✓ Success after %d retries", attempt+1)
			}
			return chapters, nil
		}

		// Check if it's a timeout error
		isTimeout := strings.Contains(err.Error(), "context deadline exceeded") ||
			strings.Contains(err.Error(), "Client.Timeout exceeded")

		// If it's a CF challenge, don't retry - return immediately
		if _, isCfErr := err.(*cf.CfChallengeError); isCfErr {
			log.Printf("<asura> CF challenge detected, not retrying")
			return nil, err
		}

		lastErr = err

		// If it's not a timeout, don't retry
		if !isTimeout {
			log.Printf("<asura> Non-timeout error, not retrying: %v", err)
			return nil, err
		}

		// Log the timeout and prepare to retry
		log.Printf("<asura> ⚠️ Timeout on attempt %d/%d: %v", attempt+1, maxRetries, err)

		// Don't sleep on the last attempt
		if attempt < maxRetries-1 {
			sleepTime := 2 * time.Second
			log.Printf("<asura> Waiting %v before retry...", sleepTime)
			time.Sleep(sleepTime)
		}
	}

	log.Printf("<asura> ✖ Failed after %d attempts with timeout errors", maxRetries)
	return nil, fmt.Errorf("failed after %d retries: %w", maxRetries, lastErr)
}

// asuraChapterUrls fetches the series page and returns all valid chapter URLs
func asuraChapterUrls(seriesURL string, timeout time.Duration) ([]string, error) {
	parsedURL, _ := url2.Parse(seriesURL)
	domain := parsedURL.Hostname()

	bypassData, err := cf.LoadFromFile(domain)
	hasStoredData := (err == nil)

	client := &http.Client{Timeout: timeout}

	if hasStoredData {
		log.Printf("<asura> Found stored bypass data for %s (type: %s)", domain, bypassData.Type)

		// Check if cookies are expired
		if bypassData.HasCookies() {
			if bypassData.IsExpired(24 * time.Hour) {
				log.Printf("<asura> Stored Cloudflare cookies are expired")
				hasStoredData = false
			}
		}
	} else {
		log.Printf("<asura> No stored bypass data found for %s", domain)
	}

	req, err := http.NewRequest("GET", seriesURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set User-Agent and headers
	if hasStoredData {
		req.Header.Set("User-Agent", bypassData.Entropy.UserAgent)

		// Set browser-like headers to match the captured session
		if bypassData.Headers["acceptLanguage"] != "" {
			req.Header.Set("Accept-Language", bypassData.Headers["acceptLanguage"])
		}
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
		req.Header.Set("Accept-Encoding", "gzip, deflate, br")
		req.Header.Set("Connection", "keep-alive")
		req.Header.Set("Upgrade-Insecure-Requests", "1")
		req.Header.Set("Sec-Fetch-Dest", "document")
		req.Header.Set("Sec-Fetch-Mode", "navigate")
		req.Header.Set("Sec-Fetch-Site", "none")
		req.Header.Set("Sec-Fetch-User", "?1")

		// Chrome-specific headers
		if strings.Contains(bypassData.Entropy.UserAgent, "Chrome") {
			req.Header.Set("sec-ch-ua", `"Chromium";v="142", "Not_A Brand";v="99"`)
			req.Header.Set("sec-ch-ua-mobile", "?0")
			req.Header.Set("sec-ch-ua-platform", fmt.Sprintf(`"%s"`, bypassData.Entropy.Platform))
		}
	} else {
		req.Header.Set("User-Agent",
			"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/114")
	}

	// Apply cookies if we have stored data
	cookiesAdded := 0
	if hasStoredData && bypassData.HasCookies() {
		// CRITICAL: Add cf_clearance first from CfClearanceStruct
		if bypassData.CfClearanceStruct != nil {
			req.AddCookie(&http.Cookie{
				Name:   bypassData.CfClearanceStruct.Name,
				Value:  bypassData.CfClearanceStruct.Value,
				Domain: bypassData.CfClearanceStruct.Domain,
				Path:   bypassData.CfClearanceStruct.Path,
			})
			cookiesAdded++
			log.Printf("<asura>   ✓ Added cf_clearance cookie (domain: %s)", bypassData.CfClearanceStruct.Domain)
		}

		// Add remaining cookies from AllCookies
		for _, ck := range bypassData.AllCookies {
			// Skip cf_clearance (already added) and empty cookies
			if ck.Name == "cf_clearance" || ck.Name == "" {
				continue
			}

			req.AddCookie(&http.Cookie{
				Name:   ck.Name,
				Value:  ck.Value,
				Domain: ck.Domain,
				Path:   ck.Path,
			})
			cookiesAdded++
		}
		log.Printf("<asura> ✓ Applied stored Cloudflare cookies (%d)", cookiesAdded)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch series page: %w", err)
	}
	defer resp.Body.Close()

	log.Printf("<asura> HTTP status code: %d", resp.StatusCode)

	// Read and decompress response body
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	log.Printf("<asura> Response body length: %d bytes (compressed)", len(bodyBytes))

	// Decompress the response using cf package's DecompressResponseBody
	contentEncoding := resp.Header.Get("Content-Encoding")
	decompressed, wasCompressed, err := cf.DecompressResponseBody(bodyBytes, contentEncoding)
	if err != nil {
		return nil, fmt.Errorf("failed to decompress response: %w", err)
	}

	if wasCompressed {
		log.Printf("<asura> ✓ Decompressed response: %d bytes → %d bytes", len(bodyBytes), len(decompressed))
		bodyBytes = decompressed
	}

	log.Printf("<asura> First 200 chars of body: %s", string(bodyBytes[:min(200, len(bodyBytes))]))

	// Reconstruct response for CF detection
	resp.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	// Check for Cloudflare challenge
	isCF, cfInfo, err := cf.Detectcf(resp)
	if err != nil {
		return nil, fmt.Errorf("detectcf error: %w", err)
	}

	if isCF {
		log.Printf("<asura> ⚠️ Cloudflare challenge detected!")
		log.Printf("<asura> CF Detection details:")
		log.Printf("<asura>   Status: %d", cfInfo.StatusCode)
		log.Printf("<asura>   Reason: %s", cfInfo.Reason)
		log.Printf("<asura>   Indicators: %v", cfInfo.Indicators)
		log.Printf("<asura>   Ray ID: %s", cfInfo.RayID)
		log.Printf("<asura>   Body excerpt: %s", string(bodyBytes[:min(1000, len(bodyBytes))]))

		// If stored data failed, drop it
		if hasStoredData {
			log.Printf("<asura> Stored bypass invalid — deleting domain data")
			cf.DeleteDomain(domain)
		}

		challengeURL := cf.GetChallengeURL(cfInfo, seriesURL)

		log.Printf("<asura> Opening browser for challenge: %s", challengeURL)
		if err := cf.OpenInBrowser(challengeURL); err != nil {
			return nil, fmt.Errorf("cloudflare detected; failed to open browser: %w", err)
		}

		return nil, &cf.CfChallengeError{
			URL:        challengeURL,
			StatusCode: cfInfo.StatusCode,
			Indicators: cfInfo.Indicators,
		}
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("failed to fetch series page: status code %d", resp.StatusCode)
	}

	// Parse the decompressed HTML
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	chapterURLs := make(map[string]struct{})

	doc.Find("a").Each(func(i int, s *goquery.Selection) {
		href, ok := s.Attr("href")
		if !ok || href == "" {
			return
		}
		href = strings.TrimSpace(href)

		if strings.Contains(href, "/chapter/") {
			if !strings.HasPrefix(href, "http") {
				href = "https://asuracomic.net/series/" + strings.TrimPrefix(href, "/")
			}
			chapterURLs[href] = struct{}{}
		}
	})

	var urls []string
	for u := range chapterURLs {
		urls = append(urls, u)
	}

	sort.Slice(urls, func(i, j int) bool { return urls[i] > urls[j] })

	log.Printf("<asura> Successfully found %d chapters", len(urls))
	return urls, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// asuraChapterMap normalizes chapter URLs into consistent filenames
func asuraChapterMap(urls []string) map[string]string {
	result := make(map[string]string)

	// Regex to extract chapter number with optional subchapter (dot or dash)
	re := regexp.MustCompile(`chapter/([\d]+(?:[.-]\d+)?)`)

	for _, u := range urls {
		matches := re.FindStringSubmatch(u)
		if len(matches) < 2 {
			log.Printf("<asura> WARNING: Could not parse chapter number from URL: %s", u)
			continue
		}

		chNum := matches[1] // e.g., "43", "43.4", "54-4"

		mainNum := chNum
		part := ""

		// Normalize any subchapter/part to use dot
		if strings.ContainsAny(chNum, ".-") {
			if strings.Contains(chNum, ".") {
				parts := strings.SplitN(chNum, ".", 2)
				mainNum = parts[0]
				part = "." + parts[1]
			} else if strings.Contains(chNum, "-") {
				parts := strings.SplitN(chNum, "-", 2)
				mainNum = parts[0]
				part = "." + parts[1]
			}
		}

		// Pad main number to 3 digits
		filename := fmt.Sprintf("ch%03s%s.cbz", mainNum, part)
		result[filename] = u
		log.Printf("<asura> Mapped: %s → %s", filename, u)
	}

	return result
}

// asuraSortedChapterImagesWithRetry gets chapter images with retry logic
func asuraSortedChapterImagesWithRetry(chapterURL, shortname, cbzName string) ([]chapterImage, error) {
	maxRetries := 3
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			log.Printf("[%s:%s] Retry attempt %d/%d for chapter images", shortname, cbzName, attempt, maxRetries)
		}

		images, err := asuraSortedChapterImages(chapterURL)
		if err == nil {
			if attempt > 0 {
				log.Printf("[%s:%s] ✓ Success after %d retries", shortname, cbzName, attempt)
			}
			return images, nil
		}

		lastErr = err
		log.Printf("[%s:%s] Failed to get chapter images (attempt %d/%d): %v", shortname, cbzName, attempt+1, maxRetries+1, err)

		// Don't sleep on the last attempt
		if attempt < maxRetries {
			sleepTime := 2 * time.Second
			log.Printf("[%s:%s] Waiting %v before retry...", shortname, cbzName, sleepTime)
			time.Sleep(sleepTime)
		}
	}

	return nil, fmt.Errorf("failed after %d retries: %w", maxRetries+1, lastErr)
}

// asuraRawChapterImageUrls fetches all image URLs from a chapter page (unsorted)
func asuraRawChapterImageUrls(chapterURL string) ([]string, error) {
	log.Printf("<asura> Starting fetch for chapter images: %s", chapterURL)

	ctx, cancel := chromedp.NewContext(context.Background())
	defer cancel()

	var html string

	startNav := time.Now()
	if err := chromedp.Run(ctx,
		chromedp.Navigate(chapterURL),
		chromedp.WaitReady("body"),
		chromedp.OuterHTML("html", &html),
	); err != nil {
		return nil, fmt.Errorf("navigation failed: %w", err)
	}
	elapsedNav := time.Since(startNav)
	log.Printf("<asura> Navigation complete in %s. HTML length: %d", elapsedNav, len(html))

	scripts := asuraExtractScriptsFromHTML(html)
	log.Printf("<asura> Total <script> tags found: %d", len(scripts))

	var urls []string
	for i, script := range scripts {
		matches := asuraExtractImageURLsFromScript(script)
		log.Printf("<asura> Script %d matches found: %d", i, len(matches))
		urls = append(urls, matches...)
	}

	log.Printf("<asura> Total raw image URLs extracted: %d", len(urls))
	return urls, nil
}

// asuraFilterImageURLs filters URLs to only those with filenames like xx-optimized.webp
func asuraFilterImageURLs(urls []string) []string {
	var filtered []string
	re := regexp.MustCompile(`(\d{1,3})-optimized\.webp$`)

	for _, u := range urls {
		if re.MatchString(u) {
			filtered = append(filtered, u)
		}
	}

	log.Printf("<asura> Filtered image URLs count: %d", len(filtered))
	return filtered
}

// asuraBuildChapterImages converts filtered URLs into a sorted slice of chapterImage
func asuraBuildChapterImages(urls []string) []chapterImage {
	type temp struct {
		order int
		url   string
	}

	var tmpList []temp
	re := regexp.MustCompile(`(\d{1,3})-optimized\.webp$`)

	for _, u := range urls {
		m := re.FindStringSubmatch(u)
		if len(m) == 2 {
			num, err := strconv.Atoi(m[1])
			if err != nil {
				log.Printf("<asura> Failed to parse number from URL %s: %v", u, err)
				continue
			}
			tmpList = append(tmpList, temp{order: num, url: u})
		}
	}

	// Sort by the numeric prefix
	sort.Slice(tmpList, func(i, j int) bool {
		return tmpList[i].order < tmpList[j].order
	})

	// Build final slice
	images := make([]chapterImage, len(tmpList))
	for i, t := range tmpList {
		images[i] = chapterImage{
			Order: i + 1,
			URL:   t.url,
		}
	}

	log.Printf("<asura> Total chapter images built: %d", len(images))
	return images
}

// asuraSortedChapterImages deduplicates and sorts the chapter URLs
func asuraSortedChapterImages(chapterURL string) ([]chapterImage, error) {
	rawURLs, err := asuraRawChapterImageUrls(chapterURL)
	if err != nil {
		return nil, err
	}

	filtered := asuraFilterImageURLs(rawURLs)
	deduped := asuraDeduplicateURLs(filtered)
	chapterImages := asuraBuildChapterImages(deduped)

	log.Printf("<asura> Total sorted chapter images after deduplication: %d", len(chapterImages))
	return chapterImages, nil
}

// asuraDeduplicateURLs removes duplicate URLs from a slice while preserving order
func asuraDeduplicateURLs(urls []string) []string {
	seen := make(map[string]struct{})
	var deduped []string

	for _, u := range urls {
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}
		deduped = append(deduped, u)
	}

	log.Printf("<asura> Deduplication complete. Remaining URLs: %d", len(deduped))
	return deduped
}

// asuraExtractScriptsFromHTML returns the content of all <script> tags in the HTML
func asuraExtractScriptsFromHTML(html string) []string {
	var scripts []string
	re := regexp.MustCompile(`(?s)<script[^>]*>(.*?)</script>`)
	matches := re.FindAllStringSubmatch(html, -1)
	for _, m := range matches {
		scripts = append(scripts, m[1])
	}
	return scripts
}

// asuraExtractImageURLsFromScript parses a single script block for image URLs
func asuraExtractImageURLsFromScript(script string) []string {
	// Look for the Asura CDN images ending in optimized.webp or .jpg/.png
	re := regexp.MustCompile(`https://gg\.asuracomic\.net/storage/media/[0-9]+/conversions/[0-9a-fA-F-]+-optimized\.(webp|jpg|png)`)
	return re.FindAllString(script, -1)
}

// asuraDownloadChapterImageWithRetry downloads and converts a chapterImage with retry logic
func asuraDownloadChapterImageWithRetry(img chapterImage, targetDir, shortname, cbzName string) error {
	maxRetries := 3
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			log.Printf("[%s:%s] Retry attempt %d/%d for image %d", shortname, cbzName, attempt, maxRetries, img.Order)
		}

		err := asuraDownloadChapterImage(img, targetDir)
		if err == nil {
			return nil
		}

		lastErr = err
		log.Printf("[%s:%s] Failed to download image (attempt %d/%d): %v", shortname, cbzName, attempt+1, maxRetries+1, err)
	}

	return lastErr
}

// asuraDownloadChapterImage downloads and converts a chapterImage into a JPG file
func asuraDownloadChapterImage(img chapterImage, targetDir string) error {
	resp, err := http.Get(img.URL)
	if err != nil {
		return fmt.Errorf("failed to download image (Order %d): %w", img.Order, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("bad response status for %s: %s", img.URL, resp.Status)
	}

	imgBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read image data for %s: %w", img.URL, err)
	}

	// Generate filename from the Order field
	paddedFileName := fmt.Sprintf("%03d.jpg", img.Order)
	outputFile := filepath.Join(targetDir, paddedFileName)

	// Use the shared ConvertImageToJPEG function
	err = parser.ConvertImageToJPEG(imgBytes, outputFile)
	if err != nil {
		return fmt.Errorf("failed to convert image for %s: %w", img.URL, err)
	}

	log.Printf("<asura> Wrote %s (Order %d)", outputFile, img.Order)
	return nil
}
