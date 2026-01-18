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

// XbatoDownloadChapters downloads manga chapters from xbato website.
// This function follows the same pattern as MgekoDownloadChapters for consistency.
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
func XbatoDownloadChapters(ctx context.Context, manga *config.Bookmarks, progressCallback func(string, float64, int, int, int)) error {
	// Validate input manga data
	if manga == nil {
		return fmt.Errorf("no manga provided")
	}

	// Ensure required fields are present
	if manga.Shortname == "" {
		return fmt.Errorf("manga shortname is empty")
	}
	if manga.Location == "" {
		return fmt.Errorf("manga location is empty")
	}

	log.Printf("<%s> Starting download [%s]", manga.Site, manga.Title)
	if progressCallback != nil {
		progressCallback(fmt.Sprintf("Fetching chapter list for %s...", manga.Title), 0, 0, 0, 0)
	}

	// Step 1: Build the manga URL from shortname
	mangaUrl := fmt.Sprintf("https://xbato.com/series/%s", manga.Shortname)

	// Get all chapter entries with retry logic
	chapterEntries, err := xbatoChapterUrls(mangaUrl)
	if err != nil {
		return err
	}

	log.Printf("<%s> Found %d total chapters on site", manga.Site, len(chapterEntries))

	// Step 2: Build chapter map
	chapterMap := xbatoChapterMap(chapterEntries)
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

		log.Printf("[%s:%s] Starting download from: %s", manga.Shortname, cbzName, chapterURL)

		// Download chapter with retry logic
		imgURLs, err := downloadXbatoChapterWithRetry(chapterURL, manga, cbzName)
		if err != nil {
			log.Printf("[%s:%s] ❌ Failed to download chapter after retries: %v", manga.Shortname, cbzName, err)
			continue
		}

		log.Printf("[%s:%s] ✓ Successfully fetched %d image URLs", manga.Shortname, cbzName, len(imgURLs))

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

		sortedImgIndices, err := parser.SortKeysNumeric(imgURLs)
		if err != nil {
			log.Printf("[%s:%s] Failed to sort image indices: %v", manga.Shortname, cbzName, err)
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

			log.Printf("[%s:%s] Downloading image %d/%d: %s", manga.Shortname, cbzName, imgNum+1, len(imgURLs), imgURL)
			imgConvertErr := parser.DownloadConvertToJPGRename(imgIdx, imgURL, chapterDir)
			if imgConvertErr != nil {
				log.Printf("[%s:%s] ⚠️ Failed to download/convert image %s: %v", manga.Shortname, cbzName, imgURL, imgConvertErr)
			} else {
				successCount++
				log.Printf("[%s:%s] ✓ Successfully downloaded and converted image %d/%d", manga.Shortname, cbzName, imgNum+1, len(imgURLs))
			}
		}

		log.Printf("[%s:%s] Download complete: %d/%d images successful", manga.Shortname, cbzName, successCount, len(imgURLs))

		if successCount == 0 {
			log.Printf("[%s:%s] ⚠️ Skipping CBZ creation - no images downloaded", manga.Shortname, cbzName)
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

// xbatoChapterUrls retrieves all chapter URLs from an Xbato manga page.
// This is a site-specific function as each manga site has different HTML structure.
//
// Parameters:
//   - url: The main manga page URL on xbato.com
//
// Returns:
//   - []string: Slice of strings in format "Chapter X|URL" for parsing
//   - error: Any error encountered during scraping, nil on success
//
// The function uses Colly to scrape links with class "chapt" from the manga page.
// Returns format: "Chapter 1|https://xbato.com/chapter/3890889"
// xbatoChapterUrls retrieves all chapter URLs from an Xbato manga page with retry logic.
// Retries up to 5 times with increasing timeout (10s, 15s, 20s, 25s, 30s)
func xbatoChapterUrls(url string) ([]string, error) {
	maxRetries := 5
	baseTimeout := 10 * time.Second

	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		timeout := baseTimeout + (time.Duration(attempt) * 5 * time.Second)

		if attempt > 0 {
			log.Printf("<xbato> Retry attempt %d/%d with timeout %v for: %s",
				attempt+1, maxRetries, timeout, url)
		} else {
			log.Printf("<xbato> Fetching chapter list from: %s (timeout: %v)", url, timeout)
		}

		chapters, err := xbatoChapterUrlsAttempt(url, timeout)

		// Success!
		if err == nil {
			if attempt > 0 {
				log.Printf("<xbato> ✓ Success after %d retries", attempt+1)
			}
			return chapters, nil
		}

		// Check if it's a timeout error
		isTimeout := strings.Contains(err.Error(), "context deadline exceeded") ||
			strings.Contains(err.Error(), "Client.Timeout exceeded")

		// If it's a CF challenge, don't retry - return immediately
		if _, isCfErr := err.(*cf.CfChallengeError); isCfErr {
			log.Printf("<xbato> CF challenge detected, not retrying")
			return nil, err
		}

		lastErr = err

		// If it's not a timeout, don't retry
		if !isTimeout {
			log.Printf("<xbato> Non-timeout error, not retrying: %v", err)
			return nil, err
		}

		// Log the timeout and prepare to retry
		log.Printf("<xbato> ⚠️ Timeout on attempt %d/%d: %v", attempt+1, maxRetries, err)

		// Don't sleep on the last attempt
		if attempt < maxRetries-1 {
			sleepTime := 2 * time.Second
			log.Printf("<xbato> Waiting %v before retry...", sleepTime)
			time.Sleep(sleepTime)
		}
	}

	log.Printf("<xbato> ❌ Failed after %d attempts with timeout errors", maxRetries)
	return nil, fmt.Errorf("failed after %d retries: %w", maxRetries, lastErr)
}

// xbatoChapterUrlsAttempt performs a single attempt to fetch chapter URLs with the given timeout
func xbatoChapterUrlsAttempt(url string, timeout time.Duration) ([]string, error) {
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
		log.Printf("<xbato> Found stored bypass data for %s (type: %s)", domain, bypassData.Type)

		// Check if cf_clearance exists
		if bypassData.CfClearanceStruct != nil {
			log.Printf("<xbato> cf_clearance found, expires: %v", bypassData.CfClearanceStruct.Expires)

			// Check expiration
			if bypassData.CfClearanceStruct.Expires != nil && time.Now().After(*bypassData.CfClearanceStruct.Expires) {
				log.Printf("<xbato> ⚠️ cf_clearance has EXPIRED!")
				hasStoredData = false
			}
		}

		if hasStoredData {
			// Apply the stored data
			if err := cf.ApplyToCollector(c, url); err != nil {
				log.Printf("<xbato> Failed to apply bypass data: %v", err)
				hasStoredData = false
			} else {
				log.Printf("<xbato> ✓ Applied stored cf_clearance cookie")
			}
		}
	} else {
		log.Printf("<xbato> No stored bypass data found for %s", domain)
	}

	var cfDetected bool
	var cfInfo *cf.CfInfo
	var scrapeErr error

	c.OnResponse(func(r *colly.Response) {
		// Automatically decompress the response (handles gzip and Brotli)
		if decompressed, err := cf.DecompressResponse(r, "<xbato>"); err != nil {
			log.Printf("<xbato> ERROR: Failed to decompress response: %v", err)
			return
		} else if decompressed {
			log.Printf("<xbato> Response successfully decompressed")
		}

		log.Printf("<xbato> Chapter list response: status=%d, size=%d bytes", r.StatusCode, len(r.Body))

		isCF, info, _ := cf.DetectFromColly(r)
		if isCF {
			cfDetected = true
			cfInfo = info
			log.Printf("<xbato> ⚠️ cf challenge detected despite using stored cookie!")
			log.Printf("<xbato> Indicators that triggered detection: %v", info.Indicators)
		}
	})

	// Xbato stores chapter links in <a> tags with class "chapt"
	c.OnHTML("a.chapt", func(e *colly.HTMLElement) {
		href := e.Attr("href")
		chapterText := strings.TrimSpace(e.Text)

		if href != "" && strings.HasPrefix(href, "/chapter/") {
			fullURL := e.Request.AbsoluteURL(href)
			chapterEntry := fmt.Sprintf("%s|%s", chapterText, fullURL)
			chapters = append(chapters, chapterEntry)
			log.Printf("<xbato> Added chapter entry: '%s' -> %s", chapterText, fullURL)
		}
	})

	// Capture any scraping errors
	c.OnError(func(r *colly.Response, err error) {
		log.Printf("<xbato> ERROR: %v, Status: %d", err, r.StatusCode)

		isCF, info, _ := cf.DetectFromColly(r)
		if isCF {
			cfDetected = true
			cfInfo = info
			log.Printf("<xbato> cf block detected: %v", info.Indicators)
		}
		scrapeErr = err
	})

	// Make the request
	visitErr := c.Visit(url)
	if visitErr != nil {
		log.Printf("<xbato> Visit error: %v", visitErr)
	}

	// Handle cf detection
	if cfDetected {
		if hasStoredData {
			log.Printf("<xbato> ⚠️ Stored cf_clearance failed validation - cookie is expired/invalid")
			log.Printf("<xbato> Deleting invalid data and requesting fresh challenge")
			cf.DeleteDomain(domain)
		}

		log.Printf("<xbato> Opening browser for cf challenge...")
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

	log.Printf("<xbato> Successfully scraped %d chapter URLs", len(chapters))

	if len(chapters) == 0 {
		log.Printf("<xbato> WARNING: No chapters found at %s", url)
	}

	return chapters, nil
}

// xbatoChapterMap takes a slice of Xbato chapter entries and returns a normalized map.
// This is a site-specific function as URL patterns differ between manga sites.
//
// Parameters:
//   - entries: Slice of chapter entries from xbato.com in format "Chapter X|URL"
//
// Returns:
//   - map[string]string: Map where key = normalized filename (ch###.cbz or special.cbz)
//     and value = full chapter URL
//
// The function handles:
// - Standard chapters: "Chapter 1" -> ch001.cbz
// - Decimal chapters: "Chapter 1.5" -> ch001.5.cbz
// - Prologues: "Prologue", "Prologue 1", "Prologue 2" -> prologue.cbz, prologue_1.cbz, etc.
// - Epilogues: "Epilogue", "Epilogue 1", "Epilogue 7 (Epilogue Finale)" -> epilogue.cbz, epilogue_1.cbz, etc.
// - Afterwords: "Afterword" -> afterword.cbz
func xbatoChapterMap(entries []string) map[string]string {
	chapterMap := make(map[string]string)

	log.Printf("<xbato> Processing %d chapter entries", len(entries))

	// Regex patterns
	chapterRe := regexp.MustCompile(`Chapter\s+(\d+)(?:\.(\d+))?`)
	prologueRe := regexp.MustCompile(`(?i)Prologue(?:\s+(\d+))?`)
	epilogueRe := regexp.MustCompile(`(?i)Epilogue(?:\s+(\d+))?`)
	afterwordRe := regexp.MustCompile(`(?i)Afterword`)

	for _, entry := range entries {
		// Split the entry into text and URL
		parts := strings.Split(entry, "|")
		if len(parts) != 2 {
			log.Printf("<xbato> WARNING: Invalid entry format: %s", entry)
			continue
		}

		chapterText := parts[0]
		url := parts[1]
		var filename string

		// Try to match standard chapter
		if matches := chapterRe.FindStringSubmatch(chapterText); len(matches) > 0 {
			mainNum := matches[1] // Main chapter number
			partNum := ""
			if len(matches) > 2 && matches[2] != "" {
				partNum = matches[2] // Decimal part
			}

			filename = fmt.Sprintf("ch%03s", mainNum)
			if partNum != "" {
				filename += "." + partNum
			}
			filename += ".cbz"
		} else if matches := prologueRe.FindStringSubmatch(chapterText); matches != nil {
			// Handle prologue
			if len(matches) > 1 && matches[1] != "" {
				// Prologue with number (e.g., "Prologue 1", "Prologue 2")
				num := matches[1]
				filename = fmt.Sprintf("prologue_%s.cbz", num)
			} else {
				// Just "Prologue"
				filename = "prologue.cbz"
			}
		} else if matches := epilogueRe.FindStringSubmatch(chapterText); matches != nil {
			// Handle epilogue
			if len(matches) > 1 && matches[1] != "" {
				// Epilogue with number (e.g., "Epilogue 1", "Epilogue 7")
				num := matches[1]
				filename = fmt.Sprintf("epilogue_%s.cbz", num)
			} else {
				// Just "Epilogue"
				filename = "epilogue.cbz"
			}
		} else if afterwordRe.MatchString(chapterText) {
			// Handle afterword
			filename = "afterword.cbz"
		} else {
			log.Printf("<xbato> WARNING: Could not parse chapter type from text: %s", chapterText)
			continue
		}

		chapterMap[filename] = url
		log.Printf("<xbato> Mapped '%s' -> %s (URL: %s)", chapterText, filename, url)
	}

	log.Printf("<xbato> Created chapter map with %d entries", len(chapterMap))

	return chapterMap
}

// extractImageUrlsFromResponse parses HTML body to extract image URLs from imgHttps array
func extractImageUrlsFromResponse(body []byte) (map[string]string, error) {
	// Parse HTML using goquery
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	// Find <script> containing "const imgHttps"
	var scriptText string
	doc.Find("script").Each(func(i int, s *goquery.Selection) {
		if strings.Contains(s.Text(), "const imgHttps") {
			scriptText = s.Text()
		}
	})

	if scriptText == "" {
		return nil, fmt.Errorf("imgHttps script block not found")
	}

	// Extract the array contents
	re := regexp.MustCompile(`const\s+imgHttps\s*=\s*\[(.*?)\];`)
	match := re.FindStringSubmatch(scriptText)
	if len(match) < 2 {
		return nil, fmt.Errorf("imgHttps array not found inside script")
	}
	arrayText := match[1]

	// Extract URLs from quotes
	urlRe := regexp.MustCompile(`"([^"]+)"`)
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

// downloadXbatoChapterWithRetry attempts to download a single chapter with retries
func downloadXbatoChapterWithRetry(chapterURL string, manga *config.Bookmarks, cbzName string) (map[string]string, error) {
	maxRetries := 5
	baseTimeout := 10 * time.Second

	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		timeout := baseTimeout + (time.Duration(attempt) * 5 * time.Second)

		if attempt > 0 {
			log.Printf("[%s:%s] Retry attempt %d/%d with timeout %v",
				manga.Shortname, cbzName, attempt+1, maxRetries, timeout)
		}

		imgURLs, err := downloadXbatoChapterAttempt(chapterURL, manga, cbzName, timeout)

		// Success!
		if err == nil {
			if attempt > 0 {
				log.Printf("[%s:%s] ✓ Success after %d retries", manga.Shortname, cbzName, attempt+1)
			}
			return imgURLs, nil
		}

		// Check if it's a timeout error
		isTimeout := strings.Contains(err.Error(), "context deadline exceeded") ||
			strings.Contains(err.Error(), "Client.Timeout exceeded")

		lastErr = err

		// If it's not a timeout, don't retry
		if !isTimeout {
			log.Printf("[%s:%s] Non-timeout error, not retrying: %v", manga.Shortname, cbzName, err)
			return nil, err
		}

		// Log the timeout and prepare to retry
		log.Printf("[%s:%s] ⚠️ Timeout on attempt %d/%d: %v",
			manga.Shortname, cbzName, attempt+1, maxRetries, err)

		// Don't sleep on the last attempt
		if attempt < maxRetries-1 {
			sleepTime := 2 * time.Second
			log.Printf("[%s:%s] Waiting %v before retry...", manga.Shortname, cbzName, sleepTime)
			time.Sleep(sleepTime)
		}
	}

	log.Printf("[%s:%s] ❌ Failed after %d attempts with timeout errors",
		manga.Shortname, cbzName, maxRetries)
	return nil, fmt.Errorf("failed after %d retries: %w", maxRetries, lastErr)
}

// downloadXbatoChapterAttempt performs a single attempt to download chapter images
func downloadXbatoChapterAttempt(chapterURL string, manga *config.Bookmarks, cbzName string, timeout time.Duration) (map[string]string, error) {
	// Create a NEW Colly collector for this chapter
	c := colly.NewCollector(
		colly.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/114.0.0.0 Safari/537.36"),
	)

	// Set custom timeout
	c.SetRequestTimeout(timeout)

	if applyErr := cf.ApplyToCollector(c, chapterURL); applyErr != nil {
		log.Printf("[%s:%s] WARNING: Failed to apply bypass data: %v", manga.Shortname, cbzName, applyErr)
	} else {
		log.Printf("[%s:%s] ✓ cf bypass applied to chapter collector", manga.Shortname, cbzName)
	}

	// Scrape image URLs from the chapter page
	var imgURLs map[string]string
	var scrapeErr error

	c.OnResponse(func(r *colly.Response) {
		if decompressed, err := cf.DecompressResponse(r, fmt.Sprintf("[%s]", cbzName)); err != nil {
			log.Printf("[%s:%s] ERROR: Failed to decompress: %v", manga.Shortname, cbzName, err)
			return
		} else if decompressed {
			log.Printf("[%s:%s] ✓ Chapter page decompressed", manga.Shortname, cbzName)
		}

		log.Printf("[%s:%s] Chapter page response: status=%d, size=%d bytes",
			manga.Shortname, cbzName, r.StatusCode, len(r.Body))

		imgURLs, scrapeErr = extractImageUrlsFromResponse(r.Body)
		if scrapeErr != nil {
			log.Printf("[%s:%s] ERROR parsing image URLs: %v", manga.Shortname, cbzName, scrapeErr)
		} else {
			log.Printf("[%s:%s] Found %d images to download", manga.Shortname, cbzName, len(imgURLs))
		}
	})

	c.OnError(func(r *colly.Response, err error) {
		log.Printf("[%s:%s] ERROR fetching chapter page %s: %v (status: %d)",
			manga.Shortname, cbzName, chapterURL, err, r.StatusCode)

		isCF, cfInfo, _ := cf.DetectFromColly(r)
		if isCF {
			log.Printf("[%s:%s] ⚠️ cf challenge detected on chapter page!", manga.Shortname, cbzName)
			log.Printf("[%s:%s] Indicators: %v", manga.Shortname, cbzName, cfInfo.Indicators)
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
