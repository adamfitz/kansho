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
	"github.com/chromedp/chromedp"
)

// chapterImage holds URL and order
type chapterImage struct {
	Order int
	URL   string
}

// Image URL regex patterns - ordered by priority
// Pattern 1 works for older chapters with numeric prefixes (e.g., "00-optimized.webp")
// Pattern 2 works for newer chapters with alphanumeric IDs and JSON order fields (escaped quotes)
// Pattern 3 works for chapters with triple-escaped JSON (e.g., chapter 51)
var asuraImageRegexPatterns = []*regexp.Regexp{
	// Pattern 1: Numeric prefix pattern (e.g., chapter 60 and earlier)
	regexp.MustCompile(`https://gg\.asuracomic\.net/storage/media/[0-9]+/conversions/(\d{1,3})-optimized\.(webp|jpg|png)`),

	// Pattern 2: JSON order + URL pattern with escaped quotes (e.g., chapter 61+)
	// Matches: {\"order\":1,\"url\":\"https://...optimized.webp\"}
	regexp.MustCompile(`\\"order\\":\s*(\d+),\\"url\\":\\"(https://gg\.asuracomic\.net/storage/media/[0-9]+/conversions/[0-9A-Z]+-optimized\.(?:webp|jpg|png))`),

	// Pattern 3: JSON order + URL pattern with double-backslash-escaped quotes (e.g., chapter 51)
	// Matches: {\\\"order\\\":1,\\\"url\\\":\\\"https://...optimized.webp\\\"}
	// Pattern 3: Same as Pattern 2 but with flexible filename matching
	// Matches: {\"order\":1,\"url\":\"https://...optimized.webp\"}
	regexp.MustCompile(`\\"order\\":\s*(\d+),\s*\\"url\\":\s*\\"(https://gg\.asuracomic\.net/storage/media/[0-9]+/conversions/[0-9A-Za-z-]+-optimized\.(?:webp|jpg|png))\\"`),
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

		log.Printf("[%s:%s] Starting download from: %s", manga.Site, cbzName, chapterURL)

		// Get sorted chapter images with retry
		chapterImages, err := asuraSortedChapterImagesWithRetry(chapterURL, manga.Site, cbzName)
		if err != nil {
			log.Printf("[%s:%s] ✖ Failed to get chapter images after retries: %v", manga.Site, cbzName, err)
			continue
		}

		if len(chapterImages) == 0 {
			log.Printf("[%s:%s] ⚠️ WARNING: No images found for chapter", manga.Site, cbzName)
			continue
		}

		log.Printf("[%s:%s] Found %d images to download", manga.Site, cbzName, len(chapterImages))

		// Create temp directory
		chapterDir := filepath.Join("/tmp", manga.Site, strings.TrimSuffix(cbzName, ".cbz"))
		err = os.MkdirAll(chapterDir, 0755)
		if err != nil {
			log.Printf("[%s:%s] Failed to create temporary directory %s: %v", manga.Site, cbzName, chapterDir, err)
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

			log.Printf("[%s:%s] Downloading image %d/%d: %s", manga.Site, cbzName, imgIdx+1, len(chapterImages), img.URL)
			err := asuraDownloadChapterImageWithRetry(img, chapterDir, manga.Site, cbzName)
			if err != nil {
				log.Printf("[%s:%s] ⚠️ Failed to download/convert image %s: %v", manga.Site, cbzName, img.URL, err)
			} else {
				successCount++
				log.Printf("[%s:%s] ✓ Successfully downloaded and converted image %d/%d", manga.Site, cbzName, imgIdx+1, len(chapterImages))
			}
		}

		log.Printf("[%s:%s] Download complete: %d/%d images successful", manga.Site, cbzName, successCount, len(chapterImages))

		if successCount == 0 {
			log.Printf("[%s:%s] ⚠️ Skipping CBZ creation - no images downloaded", manga.Site, cbzName)
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
			log.Printf("[%s:%s] Failed to create CBZ %s: %v", manga.Site, cbzName, cbzPath, err)
		} else {
			log.Printf("[%s] ✓ Created CBZ: %s (%d images)\n", manga.Title, cbzName, successCount)
		}

		err = os.RemoveAll(chapterDir)
		if err != nil {
			log.Printf("[%s:%s] Failed to remove temp directory %s: %v", manga.Site, cbzName, chapterDir, err)
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
// Replace these functions in your asura.go file

// asuraChapterUrls fetches the series page and returns all valid chapter URLs
// asuraChapterUrls fetches the series page and returns all valid chapter URLs
func asuraChapterUrls(seriesURL string, timeout time.Duration) ([]string, error) {
	parsedURL, _ := url2.Parse(seriesURL)
	domain := parsedURL.Hostname()

	bypassData, err := cf.LoadFromFile(domain)
	hasStoredData := (err == nil)

	if !hasStoredData {
		log.Printf("<asura> No stored bypass data found for %s", domain)
		cf.LogCFError("asuraChapterUrls", domain, fmt.Errorf("no stored bypass data found"))

		// Open browser for manual solve
		if err := cf.OpenInBrowser(seriesURL); err != nil {
			return nil, fmt.Errorf("failed to open browser: %w", err)
		}

		return nil, &cf.CfChallengeError{
			URL:        seriesURL,
			StatusCode: 0,
			Indicators: []string{"No stored cookies - manual solve required"},
		}
	}

	log.Printf("<asura> Found stored bypass data for %s (type: %s)", domain, bypassData.Type)
	// CF Debug logging happens automatically in LoadFromFile

	client := &http.Client{Timeout: timeout}

	// Check if cookies are expired
	if bypassData.HasCookies() {
		if bypassData.IsExpired(24 * time.Hour) {
			log.Printf("<asura> Stored Cloudflare cookies are expired")
			cf.LogCFError("asuraChapterUrls", domain, fmt.Errorf("cookies expired"))
			hasStoredData = false
		}
	}

	req, err := http.NewRequest("GET", seriesURL, nil)
	if err != nil {
		cf.LogCFError("asuraChapterUrls", domain, fmt.Errorf("failed to create request: %w", err))
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
	var cookieStrings []string
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
			cookieStrings = append(cookieStrings, fmt.Sprintf("cf_clearance=%s",
				bypassData.CfClearanceStruct.Value[:min(20, len(bypassData.CfClearanceStruct.Value))]))
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
			cookieStrings = append(cookieStrings, fmt.Sprintf("%s=%s",
				ck.Name, ck.Value[:min(20, len(ck.Value))]))
		}
		log.Printf("<asura> ✓ Applied stored Cloudflare cookies (%d)", cookiesAdded)

		// CF Debug: Log the request
		cf.LogCFRequest(domain, seriesURL, bypassData.Entropy.UserAgent, cookieStrings)
	}

	resp, err := client.Do(req)
	if err != nil {
		cf.LogCFError("asuraChapterUrls HTTP request", domain, err)
		return nil, fmt.Errorf("failed to fetch series page: %w", err)
	}
	defer resp.Body.Close()

	log.Printf("<asura> HTTP status code: %d", resp.StatusCode)

	// Read and decompress response body
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		cf.LogCFError("asuraChapterUrls read body", domain, err)
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	log.Printf("<asura> Response body length: %d bytes (compressed)", len(bodyBytes))

	// Decompress the response using cf package's DecompressResponseBody
	contentEncoding := resp.Header.Get("Content-Encoding")
	decompressed, wasCompressed, err := cf.DecompressResponseBody(bodyBytes, contentEncoding)
	if err != nil {
		cf.LogCFError("asuraChapterUrls decompress", domain, err)
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
		cf.LogCFError("asuraChapterUrls CF detection", domain, err)
		return nil, fmt.Errorf("detectcf error: %w", err)
	}

	if isCF {
		log.Printf("<asura> ⚠️  Cloudflare challenge detected!")
		cf.LogCFError("HTTP client challenge detected", domain,
			fmt.Errorf("challenge indicators: %v", cfInfo.Indicators))

		// If stored data failed, drop it
		if hasStoredData {
			log.Printf("<asura> Stored bypass invalid — deleting domain data")
			cf.MarkCookieAsFailed(domain)
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
		cf.LogCFError("asuraChapterUrls non-200 status", domain,
			fmt.Errorf("status code %d", resp.StatusCode))
		return nil, fmt.Errorf("failed to fetch series page: status code %d", resp.StatusCode)
	}

	// Parse the decompressed HTML
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(bodyBytes))
	if err != nil {
		cf.LogCFError("asuraChapterUrls HTML parse", domain, err)
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

// asuraRawChapterImageUrls fetches all image URLs from a chapter page
// Returns URLs and the script tag content with the most matches
func asuraRawChapterImageUrls(chapterURL string) ([]chapterImage, error) {
	log.Printf("<asura> Starting fetch for chapter images: %s", chapterURL)

	parsedURL, _ := url2.Parse(chapterURL)
	domain := parsedURL.Hostname()

	// Load stored cookies
	bypassData, err := cf.LoadFromFile(domain)
	if err != nil {
		log.Printf("<asura> No stored bypass data for chapter fetch, using browser without cookies")
		cf.LogCFError("asuraRawChapterImageUrls", domain, fmt.Errorf("no bypass data: %v", err))
		bypassData = nil // Will fetch without cookies
	}

	ctx, cancel := chromedp.NewContext(context.Background())
	defer cancel()

	var html string

	// CF Debug: Log the browser action we're about to take
	if bypassData != nil {
		var cookieStrings []string
		if bypassData.CfClearanceStruct != nil {
			cookieStrings = append(cookieStrings, "cf_clearance=...")
		}
		for _, cookie := range bypassData.AllCookies {
			if cookie.Name != "" && cookie.Name != "cf_clearance" {
				cookieStrings = append(cookieStrings, fmt.Sprintf("%s=...", cookie.Name))
			}
		}
		cf.LogCFRequest(domain, chapterURL, bypassData.Entropy.UserAgent, cookieStrings)
		cf.LogCFBrowserAction("Fetch chapter images (start)", chapterURL, len(cookieStrings), false, nil)
	}

	startNav := time.Now()
	if err := chromedp.Run(ctx,
		chromedp.Navigate(chapterURL),
		chromedp.WaitReady("body"),
		chromedp.OuterHTML("html", &html),
	); err != nil {
		cf.LogCFBrowserAction("Fetch chapter images", chapterURL, 0, false, err)
		cf.LogCFError("Chapter image fetch - chromedp", domain, err)
		return nil, fmt.Errorf("navigation failed: %w", err)
	}
	elapsedNav := time.Since(startNav)
	log.Printf("<asura> Navigation complete in %s. HTML length: %d", elapsedNav, len(html))

	// CF Debug: Log successful browser action
	if bypassData != nil {
		cf.LogCFBrowserAction("Fetch chapter images (success)", chapterURL, 0, true, nil)
	}

	scripts := asuraExtractScriptsFromHTML(html)
	log.Printf("<asura> Total <script> tags found: %d", len(scripts))

	// Try each regex pattern until we find one that works
	for patternIdx, pattern := range asuraImageRegexPatterns {
		log.Printf("<asura> Trying pattern %d...", patternIdx+1)

		// Find the script tag with the MOST matches for this pattern
		maxMatches := 0
		bestScriptIdx := -1
		var bestImages []chapterImage

		for scriptIdx, script := range scripts {
			images := asuraExtractImagesWithPattern(script, pattern, patternIdx)
			matchCount := len(images)

			if matchCount > maxMatches {
				maxMatches = matchCount
				bestScriptIdx = scriptIdx
				bestImages = images
			}
		}

		if maxMatches > 0 {
			log.Printf("<asura> Pattern %d: Found script tag %d with %d matches", patternIdx+1, bestScriptIdx, maxMatches)
			return bestImages, nil
		}

		log.Printf("<asura> Pattern %d: No matches found", patternIdx+1)
	}

	err = fmt.Errorf("no image URLs found with any pattern")
	cf.LogCFError("Chapter image extraction", domain, err)
	return nil, err
}

// asuraExtractImagesWithPattern extracts images using a specific regex pattern
func asuraExtractImagesWithPattern(script string, pattern *regexp.Regexp, patternIdx int) []chapterImage {
	matches := pattern.FindAllStringSubmatch(script, -1)

	if len(matches) == 0 {
		return []chapterImage{}
	}

	var images []chapterImage

	// Pattern 1: Numeric prefix (older chapters)
	if patternIdx == 0 {
		for _, match := range matches {
			if len(match) >= 3 {
				url := match[0]
				numStr := match[1]
				num, err := strconv.Atoi(numStr)
				if err != nil {
					continue
				}
				images = append(images, chapterImage{
					Order: num,
					URL:   url,
				})
			}
		}
	} else if patternIdx == 1 || patternIdx == 2 {
		// Pattern 2 & 3: JSON with order field (escaped or unescaped quotes)
		for _, match := range matches {
			if len(match) >= 3 {
				orderStr := match[1]
				url := match[2]
				order, err := strconv.Atoi(orderStr)
				if err != nil {
					continue
				}
				images = append(images, chapterImage{
					Order: order,
					URL:   url,
				})
			}
		}
	}

	// Sort by order
	sort.Slice(images, func(i, j int) bool {
		return images[i].Order < images[j].Order
	})

	return images
}

// asuraSortedChapterImages gets and sorts chapter image URLs
func asuraSortedChapterImages(chapterURL string) ([]chapterImage, error) {
	images, err := asuraRawChapterImageUrls(chapterURL)
	if err != nil {
		return nil, err
	}

	// Deduplicate by URL while preserving order
	seen := make(map[string]bool)
	var deduped []chapterImage

	for _, img := range images {
		if !seen[img.URL] {
			seen[img.URL] = true
			deduped = append(deduped, img)
		}
	}

	log.Printf("<asura> Total sorted chapter images after deduplication: %d", len(deduped))
	return deduped, nil
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
