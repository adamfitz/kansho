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
)

// chapterImage holds URL and order
type chapterImage struct {
	Order int
	URL   string
}

// Image URL regex patterns - ordered by priority
// Pattern 1 works for older chapters with numeric prefixes (e.g., "00-optimized.webp")
// Pattern 2 works for newer chapters with alphanumeric IDs and JSON order fields
var asuraImageRegexPatterns = []*regexp.Regexp{
	// Pattern 1: Numeric prefix pattern (e.g., chapter 60 and earlier)
	regexp.MustCompile(`https://gg\.asuracomic\.net/storage/media/[0-9]+/conversions/(\d{1,3})-optimized\.(webp|jpg|png)`),

	// Pattern 2: JSON order + URL pattern (e.g., chapter 61+)
	// Matches: {"order":1,"url":"https://...optimized.webp"}
	regexp.MustCompile(`\\"order\\":\s*(\d+),\\"url\\":\\"(https://gg\.asuracomic\.net/storage/media/[0-9]+/conversions/[0-9A-Z]+-optimized\.(?:webp|jpg|png))`),
}

// AsuraDownloadChapters downloads manga chapters from asuracomic.net website
// progressCallback is called with status updates during download
// Parameters: status string, progress (0.0-1.0), actual chapter number, current download, total chapters
func AsuraDownloadChapters(ctx context.Context, manga *config.Bookmarks, progressCallback func(string, float64, int, int, int)) error {

	// Step 1: Extract chapter URLs from the series page with retry
	chapterUrls, err := asuraChapterUrlsWithBackoff(ctx, manga.Url)
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

		// Get sorted chapter images with retry using backoff
		chapterImages, err := asuraSortedChapterImagesWithBackoff(ctx, chapterURL, manga.Shortname, cbzName)
		if err != nil {
			log.Printf("[%s:%s] ✗ Failed to get chapter images after retries: %v", manga.Shortname, cbzName, err)
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
			err := asuraDownloadChapterImageWithBackoff(ctx, img, chapterDir, manga.Shortname, cbzName)
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

// asuraChapterUrlsWithBackoff fetches chapter URLs using exponential backoff retry logic
func asuraChapterUrlsWithBackoff(ctx context.Context, seriesURL string) ([]string, error) {
	// Configure backoff for chapter list fetching
	config := parser.DefaultBackoffConfig()
	config.MaxRetries = 5
	config.BaseDelay = 2 * time.Second
	config.MaxDelay = 32 * time.Second
	config.InitialTimeout = 10 * time.Second
	config.TimeoutMultiplier = 1.5
	config.MaxTimeout = 30 * time.Second

	result, err := parser.RetryWithBackoff(ctx, config, "asura-chapter-list", func(ctx context.Context, attempt int) (interface{}, error) {
		// Calculate timeout for this specific attempt
		timeout := config.InitialTimeout + (time.Duration(attempt) * 5 * time.Second)
		if timeout > config.MaxTimeout {
			timeout = config.MaxTimeout
		}

		log.Printf("<asura> Fetching series page: %s (timeout: %v, attempt: %d)", seriesURL, timeout, attempt+1)

		chapters, err := asuraChapterUrls(seriesURL, timeout)

		// Check if it's a CF challenge - return it directly without wrapping
		// The error type itself indicates it's non-retryable
		if _, isCfErr := err.(*cf.CfChallengeError); isCfErr {
			log.Printf("<asura> CF challenge detected - returning error to caller")
			return nil, err
		}

		return chapters, err
	})

	if err != nil {
		return nil, err
	}

	return result.([]string), nil
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

		// If stored data failed, drop it
		if hasStoredData {
			log.Printf("<asura> Stored bypass invalid – deleting domain data")
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

// asuraSortedChapterImagesWithBackoff gets chapter images using exponential backoff
// asuraSortedChapterImagesWithBackoff gets chapter images using exponential backoff
func asuraSortedChapterImagesWithBackoff(ctx context.Context, chapterURL, shortname, cbzName string) ([]chapterImage, error) {
	backoffConfig := parser.DefaultBackoffConfig()
	backoffConfig.MaxRetries = 3
	backoffConfig.BaseDelay = 5 * time.Second
	backoffConfig.MaxDelay = 30 * time.Second

	// USE ASURA-SPECIFIC CONFIG WITH SCROLLING ENABLED
	waitConfig := parser.AsuraChromedpWaitConfig()

	operationName := fmt.Sprintf("asura-images[%s:%s]", shortname, cbzName)

	result, err := parser.RetryWithChromedpWaits(
		ctx,
		backoffConfig,
		waitConfig,
		operationName,
		chapterURL,
		func(html string) (interface{}, error) {
			return asuraExtractAndParseImages(html)
		},
	)

	if err != nil {
		return nil, err
	}

	return result.([]chapterImage), nil
}

// asuraExtractAndParseImages extracts image URLs from HTML
// asuraExtractAndParseImages extracts image URLs from HTML
func asuraExtractAndParseImages(html string) (interface{}, error) {
	// SAVE THE ENTIRE HTML TO FILE FOR DEBUGGING
	debugFile := "/tmp/asura_debug_page.html"
	if err := os.WriteFile(debugFile, []byte(html), 0644); err == nil {
		log.Printf("<asura> 🔍 DEBUG: Saved full HTML to %s (%d bytes)", debugFile, len(html))
	}

	scripts := asuraExtractScriptsFromHTML(html)
	log.Printf("<asura> 🔍 DEBUG: Extracted %d script tags", len(scripts))

	// Save all scripts to separate files for inspection
	for i, script := range scripts {
		scriptFile := fmt.Sprintf("/tmp/asura_script_%d.js", i)
		os.WriteFile(scriptFile, []byte(script), 0644)

		// Show first 200 chars of each script
		preview := script
		if len(script) > 200 {
			preview = script[:200]
		}
		log.Printf("<asura> 🔍 Script %d: %d bytes, starts with: %s", i, len(script), preview)
	}

	for patternIdx, pattern := range asuraImageRegexPatterns {
		log.Printf("<asura> 🔍 Trying pattern %d: %s", patternIdx+1, pattern.String())

		maxMatches := 0
		bestScriptIdx := -1
		var bestImages []chapterImage

		for scriptIdx, script := range scripts {
			images := asuraExtractImagesWithPattern(script, pattern, patternIdx)
			matchCount := len(images)

			log.Printf("<asura> 🔍 Pattern %d on script %d: found %d matches", patternIdx+1, scriptIdx, matchCount)

			if matchCount > maxMatches {
				maxMatches = matchCount
				bestScriptIdx = scriptIdx
				bestImages = images
			}
		}

		if maxMatches > 0 {
			log.Printf("<asura> ✓ Pattern %d: Found %d images in script %d", patternIdx+1, maxMatches, bestScriptIdx)

			// Log first few image URLs
			for i, img := range bestImages {
				if i < 5 {
					log.Printf("<asura> 🔍 Image %d: order=%d url=%s", i, img.Order, img.URL)
				}
			}

			seen := make(map[string]bool)
			var deduped []chapterImage
			for _, img := range bestImages {
				if !seen[img.URL] {
					seen[img.URL] = true
					deduped = append(deduped, img)
				}
			}

			log.Printf("<asura> ✓ Returning %d images after dedup", len(deduped))
			return deduped, nil
		}

		log.Printf("<asura> ✗ Pattern %d: No matches", patternIdx+1)
	}

	// ULTIMATE DEBUG: Search for ANY image URL in the entire HTML
	log.Printf("<asura> 🔍 FINAL DEBUG: Searching entire HTML for ANY image references...")

	// Look for the storage/media pattern
	mediaRe := regexp.MustCompile(`storage/media/[0-9]+`)
	mediaMatches := mediaRe.FindAllString(html, -1)
	log.Printf("<asura> 🔍 Found %d 'storage/media/' references in HTML", len(mediaMatches))

	// Look for optimized.webp
	webpRe := regexp.MustCompile(`optimized\.webp`)
	webpMatches := webpRe.FindAllString(html, -1)
	log.Printf("<asura> 🔍 Found %d 'optimized.webp' references in HTML", len(webpMatches))

	// Look for order field
	orderRe := regexp.MustCompile(`"order":\s*\d+`)
	orderMatches := orderRe.FindAllString(html, -1)
	log.Printf("<asura> 🔍 Found %d '\"order\":' references in HTML", len(orderMatches))
	if len(orderMatches) > 0 {
		log.Printf("<asura> 🔍 First few order matches: %v", orderMatches[:min(5, len(orderMatches))])
	}

	return nil, fmt.Errorf("no image URLs found with any pattern - check /tmp/asura_debug_page.html and /tmp/asura_script_*.js")
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
	} else if patternIdx == 1 {
		// Pattern 2: JSON with order field (newer chapters)
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

// asuraDownloadChapterImageWithBackoff downloads and converts a chapterImage using exponential backoff
func asuraDownloadChapterImageWithBackoff(ctx context.Context, img chapterImage, targetDir, shortname, cbzName string) error {
	// Configure backoff for image downloads (more aggressive retries for network issues)
	config := parser.DefaultBackoffConfig()
	config.MaxRetries = 5 // Try up to 6 times
	config.BaseDelay = 1 * time.Second
	config.MaxDelay = 16 * time.Second
	config.InitialTimeout = 45 * time.Second // Longer timeout for slow downloads
	config.TimeoutMultiplier = 1.0           // Keep timeout constant
	config.MaxTimeout = 45 * time.Second
	config.Jitter = true

	operationName := fmt.Sprintf("asura-img[%s:%s:%d]", shortname, cbzName, img.Order)

	_, err := parser.RetryWithBackoff(ctx, config, operationName, func(ctx context.Context, attempt int) (interface{}, error) {
		return nil, asuraDownloadChapterImage(img, targetDir)
	})

	return err
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
