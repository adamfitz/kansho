package sites

import (
	"context"
	"fmt"
	"log"
	url2 "net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"kansho/cf"
	"kansho/config"
	"kansho/parser"

	"github.com/PuerkitoBio/goquery"
	"github.com/gocolly/colly"
)

// MangakatanaDownloadChapters downloads manga chapters from mangakatana website.
// This function follows the same pattern as XbatoDownloadChapters for consistency.
//
// Parameters:
//   - manga: Pointer to the manga bookmark containing URL, location, and metadata
//   - progressCallback: Optional callback function for progress updates
//     Called with: status string, progress (0.0-1.0), current chapter number, total chapters
//
// Returns:
//   - error: Any error encountered during the download process, nil on success
//
// The function performs these steps:
// 1. Validates manga data
// 2. Fetches all chapter URLs from the manga page
// 3. Builds a map of chapters to download
// 4. Filters out already downloaded chapters
// 5. Downloads each new chapter by scraping image URLs
// 6. Creates CBZ files from downloaded images
func MangakatanaDownloadChapters(ctx context.Context, manga *config.Bookmarks, progressCallback func(string, float64, int, int, int)) error {
	// Validate input manga data
	if manga == nil {
		return fmt.Errorf("no manga provided")
	}

	// Ensure required fields are present
	if manga.Url == "" {
		return fmt.Errorf("manga url is empty")
	}
	if manga.Location == "" {
		return fmt.Errorf("manga location is empty")
	}

	log.Printf("<%s> Starting download [%s]", manga.Site, manga.Title)
	if progressCallback != nil {
		progressCallback(fmt.Sprintf("Fetching chapter list for %s...", manga.Title), 0, 0, 0, 0)
	}

	// Step 1: use the provided url (for the chapter list)
	mangaUrl := manga.Url

	// Get all chapter entries with retry logic
	chapterEntries, err := mangakatanaChapterUrls(mangaUrl)
	if err != nil {
		return err
	}

	log.Printf("<%s> Found %d total chapters on site", manga.Site, len(chapterEntries))

	// Step 2: Build chapter map
	chapterMap := mangakatanaChapterMap(chapterEntries)
	log.Printf("<%s> Mapped %d chapters to filenames", manga.Site, len(chapterMap))

	// Step 3: Get list of already downloaded chapters
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

	// Step 6: Download each chapter with retry logic
	for idx, cbzName := range sortedChapters {
		// Check for cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		chapterURL := chapterMap[cbzName]

		// Extract the actual chapter number
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

		// Download chapter with retry logic
		imgURLs, err := downloadMangakatanaChapterWithRetry(chapterURL, manga, cbzName)
		if err != nil {
			log.Printf("[%s:%s] ❌ Failed to download chapter after retries: %v", manga.Site, cbzName, err)
			continue
		}

		log.Printf("[%s:%s] ✓ Successfully fetched %d image URLs", manga.Site, cbzName, len(imgURLs))

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

		sortedImgIndices, err := parser.SortKeysNumeric(imgURLs)
		if err != nil {
			log.Printf("[%s:%s] Failed to sort image indices: %v", manga.Site, cbzName, err)
			continue
		}

		// Download images
		for _, imgIdx := range sortedImgIndices {
			imgURL := imgURLs[imgIdx]

			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			rateLimiter.Wait()

			imgNum, err := strconv.ParseInt(imgIdx, 10, 64)
			if err != nil {
				log.Printf("Invalid image index %s: %v", imgIdx, err)
				continue
			}

			if progressCallback != nil {
				imgProgress := progress + (float64(imgNum) / float64(len(imgURLs)) / float64(newChaptersToDownload))
				progressCallback(
					fmt.Sprintf("Chapter %d/%d: Downloading image %d/%d", actualChapterNum, totalChaptersFound, imgNum+1, len(imgURLs)),
					imgProgress,
					actualChapterNum,
					currentDownload,
					totalChaptersFound,
				)
			}

			log.Printf("[%s:%s] Downloading image %d/%d: %s", manga.Site, cbzName, imgNum+1, len(imgURLs), imgURL)
			imgConvertErr := parser.DownloadConvertToJPGRename(imgIdx, imgURL, chapterDir)
			if imgConvertErr != nil {
				log.Printf("[%s:%s] ⚠️ Failed to download/convert image %s: %v", manga.Site, cbzName, imgURL, imgConvertErr)
			} else {
				successCount++
				log.Printf("[%s:%s] ✓ Successfully downloaded and converted image %d/%d", manga.Site, cbzName, imgNum+1, len(imgURLs))
			}
		}

		log.Printf("[%s:%s] Download complete: %d/%d images successful", manga.Site, cbzName, successCount, len(imgURLs))

		if successCount == 0 {
			log.Printf("[%s:%s] ⚠️ Skipping CBZ creation - no images downloaded", manga.Site, cbzName)
			os.RemoveAll(chapterDir)
			continue
		}

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

// mangakatanaChapterUrls retrieves all chapter URLs from a Mangakatana manga page with retry logic.
// Retries up to 5 times with increasing timeout (10s, 15s, 20s, 25s, 30s)
func mangakatanaChapterUrls(url string) ([]string, error) {
	maxRetries := 5
	baseTimeout := 10 * time.Second

	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		timeout := baseTimeout + (time.Duration(attempt) * 5 * time.Second)

		if attempt > 0 {
			log.Printf("<mangakatana> Retry attempt %d/%d with timeout %v for: %s",
				attempt+1, maxRetries, timeout, url)
		} else {
			log.Printf("<mangakatana> Fetching chapter list from: %s (timeout: %v)", url, timeout)
		}

		chapters, err := mangakatanaChapterUrlsAttempt(url, timeout)

		// Success!
		if err == nil {
			if attempt > 0 {
				log.Printf("<mangakatana> ✓ Success after %d retries", attempt+1)
			}
			return chapters, nil
		}

		// Check if it's a timeout error
		isTimeout := strings.Contains(err.Error(), "context deadline exceeded") ||
			strings.Contains(err.Error(), "Client.Timeout exceeded")

		// If it's a CF challenge, don't retry - return immediately
		if _, isCfErr := err.(*cf.CfChallengeError); isCfErr {
			log.Printf("<mangakatana> CF challenge detected, not retrying")
			return nil, err
		}

		lastErr = err

		// If it's not a timeout, don't retry
		if !isTimeout {
			log.Printf("<mangakatana> Non-timeout error, not retrying: %v", err)
			return nil, err
		}

		// Log the timeout and prepare to retry
		log.Printf("<mangakatana> ⚠️ Timeout on attempt %d/%d: %v", attempt+1, maxRetries, err)

		// Don't sleep on the last attempt
		if attempt < maxRetries-1 {
			sleepTime := 2 * time.Second
			log.Printf("<mangakatana> Waiting %v before retry...", sleepTime)
			time.Sleep(sleepTime)
		}
	}

	log.Printf("<mangakatana> ❌ Failed after %d attempts with timeout errors", maxRetries)
	return nil, fmt.Errorf("failed after %d retries: %w", maxRetries, lastErr)
}

// mangakatanaChapterUrlsAttempt performs a single attempt to fetch chapter URLs with the given timeout
// mangakatanaChapterUrlsAttempt performs a single attempt to fetch chapter URLs with the given timeout
func mangakatanaChapterUrlsAttempt(url string, timeout time.Duration) ([]string, error) {
	var chapters []string

	// Create a new Colly collector with custom timeout
	c := colly.NewCollector(
		colly.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/114.0.0.0 Safari/537.36"),
		colly.AllowURLRevisit(),
	)

	// Set custom timeout
	c.SetRequestTimeout(timeout)

	// Check for stored cf data
	parsedURL, _ := url2.Parse(url)
	domain := parsedURL.Hostname()

	bypassData, err := cf.LoadFromFile(domain)
	hasStoredData := (err == nil)

	if hasStoredData {
		log.Printf("<mangakatana> Found stored bypass data for %s (type: %s)", domain, bypassData.Type)

		// Check if cf_clearance exists
		if bypassData.CfClearanceStruct != nil {
			log.Printf("<mangakatana> cf_clearance found, expires: %v", bypassData.CfClearanceStruct.Expires)

			// Check expiration
			if bypassData.CfClearanceStruct.Expires != nil && time.Now().After(*bypassData.CfClearanceStruct.Expires) {
				log.Printf("<mangakatana> ⚠️ cf_clearance has EXPIRED!")
				hasStoredData = false
			}
		}

		if hasStoredData {
			// Apply the stored data
			if err := cf.ApplyToCollector(c, url); err != nil {
				log.Printf("<mangakatana> Failed to apply bypass data: %v", err)
				hasStoredData = false
			} else {
				log.Printf("<mangakatana> ✓ Applied stored cf_clearance cookie")
			}
		}
	} else {
		log.Printf("<mangakatana> No stored bypass data found for %s", domain)
	}

	var cfDetected bool
	var cfInfo *cf.CfInfo
	var scrapeErr error

	c.OnResponse(func(r *colly.Response) {
		// Automatically decompress the response (handles gzip and Brotli)
		if decompressed, err := cf.DecompressResponse(r, "<mangakatana>"); err != nil {
			log.Printf("<mangakatana> ERROR: Failed to decompress response: %v", err)
			return
		} else if decompressed {
			log.Printf("<mangakatana> Response successfully decompressed")
		}

		log.Printf("<mangakatana> Chapter list response: status=%d, size=%d bytes", r.StatusCode, len(r.Body))

		isCF, info, _ := cf.DetectFromColly(r)
		if isCF {
			cfDetected = true
			cfInfo = info
			log.Printf("<mangakatana> ⚠️ cf challenge detected despite using stored cookie!")
			log.Printf("<mangakatana> Indicators that triggered detection: %v", info.Indicators)
		}
	})

	// Extract the base manga URL to filter chapters
	// e.g., from "https://mangakatana.com/manga/mikoto-chan-doesnt-want-to-be-hated.27569"
	// we want to match chapters that start with this base URL
	baseURL := strings.TrimSuffix(url, "/")

	// Mangakatana stores chapter links in <div class="chapter"><a> inside table rows
	c.OnHTML("div.chapter a", func(e *colly.HTMLElement) {
		href := e.Attr("href")
		chapterText := strings.TrimSpace(e.Text)

		if href != "" {
			fullURL := e.Request.AbsoluteURL(href)

			// FILTER: Only include chapters that belong to this manga
			// The chapter URL should start with the base manga URL
			if strings.HasPrefix(fullURL, baseURL+"/") {
				chapterEntry := fmt.Sprintf("%s|%s", chapterText, fullURL)
				chapters = append(chapters, chapterEntry)
				log.Printf("<mangakatana> Added chapter entry: '%s' -> %s", chapterText, fullURL)
			} else {
				log.Printf("<mangakatana> Skipped unrelated chapter: '%s' -> %s", chapterText, fullURL)
			}
		}
	})

	// Capture any scraping errors
	c.OnError(func(r *colly.Response, err error) {
		log.Printf("<mangakatana> ERROR: %v, Status: %d", err, r.StatusCode)

		isCF, info, _ := cf.DetectFromColly(r)
		if isCF {
			cfDetected = true
			cfInfo = info
			log.Printf("<mangakatana> cf block detected: %v", info.Indicators)
		}
		scrapeErr = err
	})

	// Make the request
	visitErr := c.Visit(url)
	if visitErr != nil {
		log.Printf("<mangakatana> Visit error: %v", visitErr)
	}

	// Handle cf detection
	if cfDetected {
		if hasStoredData {
			log.Printf("<mangakatana> ⚠️ Stored cf_clearance failed validation - cookie is expired/invalid")
			log.Printf("<mangakatana> Deleting invalid data and requesting fresh challenge")
			cf.DeleteDomain(domain)
		}

		log.Printf("<mangakatana> Opening browser for cf challenge...")
		challengeURL := cf.GetChallengeURL(cfInfo, url)

		if err := cf.OpenInBrowser(challengeURL); err != nil {
			return nil, fmt.Errorf("cf detected but failed to open browser: %w", err)
		}

		return nil, &cf.CfChallengeError{
			URL:        challengeURL,
			StatusCode: cfInfo.StatusCode,
			Indicators: cfInfo.Indicators,
		}
	}

	if scrapeErr != nil {
		return nil, fmt.Errorf("scrape error: %w", scrapeErr)
	}

	if visitErr != nil {
		return nil, visitErr
	}

	log.Printf("<mangakatana> Successfully scraped %d chapter URLs", len(chapters))

	if len(chapters) == 0 {
		log.Printf("<mangakatana> WARNING: No chapters found at %s", url)
	}

	return chapters, nil
}

// mangakatanaChapterMap takes a slice of Mangakatana chapter entries and returns a normalized map.
//
// Parameters:
//   - entries: Slice of chapter entries from mangakatana.com in format "Chapter X: Title|URL"
//
// Returns:
//   - map[string]string: Map where key = normalized filename (ch###.cbz or ch###.part.cbz)
//     and value = full chapter URL
//
// Example input:
//   - "Chapter 1: Prologue|https://mangakatana.com/manga/title.123/c1" -> ch001.cbz
//   - "Chapter 1.5: Extra|https://mangakatana.com/manga/title.123/c1.5" -> ch001.5.cbz
func mangakatanaChapterMap(entries []string) map[string]string {
	chapterMap := make(map[string]string)

	log.Printf("<mangakatana> Processing %d chapter entries", len(entries))

	// Regex to extract chapter numbers from text like "Chapter 1:", "Chapter 1.5:", "Chapter 123:"
	re := regexp.MustCompile(`Chapter\s+(\d+)(?:\.(\d+))?`)

	for _, entry := range entries {
		// Split the entry into text and URL
		parts := strings.Split(entry, "|")
		if len(parts) != 2 {
			log.Printf("<mangakatana> WARNING: Invalid entry format: %s", entry)
			continue
		}

		chapterText := parts[0]
		url := parts[1]

		// Extract chapter number from text
		matches := re.FindStringSubmatch(chapterText)
		if len(matches) > 0 {
			mainNum := matches[1] // Main chapter number (e.g., "1", "10", "123")
			partNum := ""
			if len(matches) > 2 && matches[2] != "" {
				partNum = matches[2] // Decimal part (e.g., "5" from "1.5")
			}

			// Build final filename: pad main number to 3 digits
			filename := fmt.Sprintf("ch%03s", mainNum)
			if partNum != "" {
				filename += "." + partNum
			}
			filename += ".cbz"

			chapterMap[filename] = url
			log.Printf("<mangakatana> Mapped: %s → %s", filename, url)
		} else {
			log.Printf("<mangakatana> WARNING: Could not parse chapter number from text: %s", chapterText)
		}
	}

	log.Printf("<mangakatana> Created chapter map with %d entries", len(chapterMap))

	return chapterMap
}

// extractMangakatanaImageUrls parses HTML body to extract image URLs from thzq JavaScript array
func extractMangakatanaImageUrls(body []byte) (map[string]string, error) {
	// Parse HTML using goquery
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	// Find <script> containing "var thzq"
	var scriptText string
	doc.Find("script").Each(func(i int, s *goquery.Selection) {
		if strings.Contains(s.Text(), "var thzq") {
			scriptText = s.Text()
		}
	})

	if scriptText == "" {
		return nil, fmt.Errorf("thzq script block not found")
	}

	// Extract the array contents - looking for: var thzq=['url1','url2',...];
	re := regexp.MustCompile(`var\s+thzq\s*=\s*\[(.*?)\];`)
	match := re.FindStringSubmatch(scriptText)
	if len(match) < 2 {
		return nil, fmt.Errorf("thzq array not found inside script")
	}
	arrayText := match[1]

	// Extract URLs from quotes (single quotes in this case)
	urlRe := regexp.MustCompile(`'([^']+)'`)
	matches := urlRe.FindAllStringSubmatch(arrayText, -1)

	if len(matches) == 0 {
		return nil, fmt.Errorf("no image URLs found")
	}

	// Build the map with zero-based index as string
	urlMap := make(map[string]string, len(matches))
	for i, m := range matches {
		urlMap[fmt.Sprintf("%d", i)] = m[1]
	}

	return urlMap, nil
}

// downloadMangakatanaChapterWithRetry attempts to download a single chapter with retries
func downloadMangakatanaChapterWithRetry(chapterURL string, manga *config.Bookmarks, cbzName string) (map[string]string, error) {
	maxRetries := 5
	baseTimeout := 10 * time.Second

	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		timeout := baseTimeout + (time.Duration(attempt) * 5 * time.Second)

		if attempt > 0 {
			log.Printf("[%s:%s] Retry attempt %d/%d with timeout %v",
				manga.Site, cbzName, attempt+1, maxRetries, timeout)
		}

		imgURLs, err := downloadMangakatanaChapterAttempt(chapterURL, manga, cbzName, timeout)

		// Success!
		if err == nil {
			if attempt > 0 {
				log.Printf("[%s:%s] ✓ Success after %d retries", manga.Site, cbzName, attempt+1)
			}
			return imgURLs, nil
		}

		// Check if it's a timeout error
		isTimeout := strings.Contains(err.Error(), "context deadline exceeded") ||
			strings.Contains(err.Error(), "Client.Timeout exceeded")

		lastErr = err

		// If it's not a timeout, don't retry
		if !isTimeout {
			log.Printf("[%s:%s] Non-timeout error, not retrying: %v", manga.Site, cbzName, err)
			return nil, err
		}

		// Log the timeout and prepare to retry
		log.Printf("[%s:%s] ⚠️ Timeout on attempt %d/%d: %v",
			manga.Site, cbzName, attempt+1, maxRetries, err)

		// Don't sleep on the last attempt
		if attempt < maxRetries-1 {
			sleepTime := 2 * time.Second
			log.Printf("[%s:%s] Waiting %v before retry...", manga.Site, cbzName, sleepTime)
			time.Sleep(sleepTime)
		}
	}

	log.Printf("[%s:%s] ❌ Failed after %d attempts with timeout errors",
		manga.Site, cbzName, maxRetries)
	return nil, fmt.Errorf("failed after %d retries: %w", maxRetries, lastErr)
}

// downloadMangakatanaChapterAttempt performs a single attempt to download chapter images
func downloadMangakatanaChapterAttempt(chapterURL string, manga *config.Bookmarks, cbzName string, timeout time.Duration) (map[string]string, error) {
	// Create a NEW Colly collector for this chapter
	c := colly.NewCollector(
		colly.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/114.0.0.0 Safari/537.36"),
	)

	// Set custom timeout
	c.SetRequestTimeout(timeout)

	if applyErr := cf.ApplyToCollector(c, chapterURL); applyErr != nil {
		log.Printf("[%s:%s] WARNING: Failed to apply bypass data: %v", manga.Site, cbzName, applyErr)
	} else {
		log.Printf("[%s:%s] ✓ cf bypass applied to chapter collector", manga.Site, cbzName)
	}

	// Scrape image URLs from the chapter page
	var imgURLs map[string]string
	var scrapeErr error

	c.OnResponse(func(r *colly.Response) {
		if decompressed, err := cf.DecompressResponse(r, fmt.Sprintf("[%s]", cbzName)); err != nil {
			log.Printf("[%s:%s] ERROR: Failed to decompress: %v", manga.Site, cbzName, err)
			return
		} else if decompressed {
			log.Printf("[%s:%s] ✓ Chapter page decompressed", manga.Site, cbzName)
		}

		log.Printf("[%s:%s] Chapter page response: status=%d, size=%d bytes",
			manga.Site, cbzName, r.StatusCode, len(r.Body))

		imgURLs, scrapeErr = extractMangakatanaImageUrls(r.Body)
		if scrapeErr != nil {
			log.Printf("[%s:%s] ERROR parsing image URLs: %v", manga.Site, cbzName, scrapeErr)
		} else {
			log.Printf("[%s:%s] Found %d images to download", manga.Site, cbzName, len(imgURLs))
		}
	})

	c.OnError(func(r *colly.Response, err error) {
		log.Printf("[%s:%s] ERROR fetching chapter page %s: %v (status: %d)",
			manga.Site, cbzName, chapterURL, err, r.StatusCode)

		isCF, cfInfo, _ := cf.DetectFromColly(r)
		if isCF {
			log.Printf("[%s:%s] ⚠️ cf challenge detected on chapter page!", manga.Site, cbzName)
			log.Printf("[%s:%s] Indicators: %v", manga.Site, cbzName, cfInfo.Indicators)
		}
		scrapeErr = err
	})

	err := c.Visit(chapterURL)
	if err != nil {
		return nil, fmt.Errorf("failed to visit: %w", err)
	}

	if scrapeErr != nil {
		return nil, fmt.Errorf("scrape error: %w", scrapeErr)
	}

	if len(imgURLs) == 0 {
		return nil, fmt.Errorf("no images found")
	}

	return imgURLs, nil
}
