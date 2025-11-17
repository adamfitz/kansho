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
func XbatoDownloadChapters(ctx context.Context, manga *config.Bookmarks, progressCallback func(string, float64, int, int)) error {
	// Validate input manga data
	if manga == nil {
		return fmt.Errorf("no manga provided")
	}

	// Ensure required fields are present
	// Note: Xbato uses shortname instead of URL for chapter lookups
	if manga.Shortname == "" {
		return fmt.Errorf("manga shortname is empty")
	}
	if manga.Location == "" {
		return fmt.Errorf("manga location is empty")
	}

	log.Printf("<%s> Starting download [%s]", manga.Site, manga.Title)
	if progressCallback != nil {
		progressCallback(fmt.Sprintf("Fetching chapter list for %s...", manga.Title), 0, 0, 0)
	}

	// Step 1: Build the manga URL from shortname
	// Xbato URL pattern: https://xbato.com/series/<shortname>
	mangaUrl := fmt.Sprintf("https://xbato.com/series/%s", manga.Shortname)

	// Get all chapter entries (format: "Chapter X|URL") from the manga's main page
	chapterEntries, err := xbatoChapterUrls(mangaUrl)
	if err != nil {
		// Just pass the error up - it will be a cfChallengeError if CF was detected
		// The UI layer will handle it appropriately
		return err
	}

	// Log the number of chapters found (similar to mgeko)
	log.Printf("<%s> Found %d total chapters on site", manga.Site, len(chapterEntries))

	// Step 2: Build chapter map (key = "chXXX.cbz", value = URL)
	// This normalizes chapter names for consistent file naming
	chapterMap := xbatoChapterMap(chapterEntries)
	log.Printf("<%s> Mapped %d chapters to filenames", manga.Site, len(chapterMap))

	// Step 3: Get list of already downloaded chapters from manga's location directory
	// Uses shared parser.LocalChapterList function to read existing .cbz files
	downloadedChapters, err := parser.LocalChapterList(manga.Location)
	if err != nil {
		return fmt.Errorf("failed to list files in %s: %v", manga.Location, err)
	}
	log.Printf("<%s> Found %d already downloaded chapters", manga.Site, len(downloadedChapters))

	// Step 4: Remove already-downloaded chapters from the map
	// This prevents re-downloading existing chapters
	for _, chapter := range downloadedChapters {
		delete(chapterMap, chapter)
	}

	totalChapters := len(chapterMap)
	if totalChapters == 0 {
		log.Printf("<%s> No new chapters to download [%s]", manga.Site, manga.Title)
		if progressCallback != nil {
			progressCallback("No new chapters to download", 1.0, 0, 0)
		}
		return nil
	}

	log.Printf("<%s> %d new chapters to download [%s]", manga.Site, totalChapters, manga.Title)
	if progressCallback != nil {
		progressCallback(fmt.Sprintf("Found %d new chapters to download", totalChapters), 0, 0, totalChapters)
	}

	// Step 5: Sort chapter keys alphabetically for ordered downloading
	// Uses shared parser.SortKeys function
	sortedChapters, sortError := parser.SortKeys(chapterMap)
	if sortError != nil {
		return fmt.Errorf("failed to sort chapter map keys: %v", sortError)
	}

	// Step 6: Iterate over sorted chapter keys and download each one
	for idx, cbzName := range sortedChapters {

		// Check for cancellation
		select {
		case <-ctx.Done():
			return ctx.Err() // Returns context.Canceled
		default:
			// Continue with download
		}

		chapterURL := chapterMap[cbzName]

		// Calculate progress metrics for callback
		currentChapter := idx + 1
		progress := float64(currentChapter) / float64(totalChapters)

		if progressCallback != nil {
			progressCallback(fmt.Sprintf("Downloading chapter %d/%d: %s", currentChapter, totalChapters, cbzName), progress, currentChapter, totalChapters)
		}

		log.Printf("[%s:%s] Starting download from: %s", manga.Shortname, cbzName, chapterURL)

		// Create a NEW Colly collector for this chapter
		// IMPORTANT: Each chapter needs its own collector with CF bypass applied
		c := colly.NewCollector(
			colly.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/114.0.0.0 Safari/537.36"),
		)

		// -------------------------------------------------------------------------
		// APPLY cf BYPASS TO THIS CHAPTER'S COLLECTOR
		// -------------------------------------------------------------------------
		log.Printf("[%s:%s] Applying cf bypass for chapter page", manga.Shortname, cbzName)

		// Apply the bypass data to this collector (ApplyToCollector loads the data internally)
		if applyErr := cf.ApplyToCollector(c, chapterURL); applyErr != nil {
			log.Printf("[%s:%s] WARNING: Failed to apply bypass data: %v", manga.Shortname, cbzName, applyErr)
			log.Printf("[%s:%s] Chapter download may fail due to cf protection", manga.Shortname, cbzName)
		} else {
			log.Printf("[%s:%s] ✓ cf bypass applied to chapter collector", manga.Shortname, cbzName)
		}

		// Scrape image URLs from the chapter page
		imgURLs := make(map[string]string)
		var scrapeErr error

		c.OnResponse(func(r *colly.Response) {
			// DECOMPRESS THE CHAPTER PAGE TOO!
			if decompressed, err := cf.DecompressResponse(r, fmt.Sprintf("[%s]", cbzName)); err != nil {
				log.Printf("[%s:%s] ERROR: Failed to decompress: %v", manga.Shortname, cbzName, err)
				return
			} else if decompressed {
				log.Printf("[%s:%s] ✓ Chapter page decompressed", manga.Shortname, cbzName)
			}

			log.Printf("[%s:%s] Chapter page response: status=%d, size=%d bytes",
				manga.Shortname, cbzName, r.StatusCode, len(r.Body))

			// Parse the response body to extract image URLs
			imgURLs, scrapeErr = extractImageUrlsFromResponse(r.Body)
			if scrapeErr != nil {
				log.Printf("[%s:%s] ERROR parsing image URLs: %v", manga.Shortname, cbzName, scrapeErr)
			} else {
				log.Printf("[%s:%s] Found %d images to download", manga.Shortname, cbzName, len(imgURLs))
			}
		})

		// Handle errors when fetching the chapter page
		c.OnError(func(r *colly.Response, err error) {
			log.Printf("[%s:%s] ERROR fetching chapter page %s: %v (status: %d)",
				manga.Shortname, cbzName, chapterURL, err, r.StatusCode)

			// Check if this is a cf challenge
			isCF, cfInfo, _ := cf.DetectFromColly(r)
			if isCF {
				log.Printf("[%s:%s] ⚠️ cf challenge detected on chapter page!", manga.Shortname, cbzName)
				log.Printf("[%s:%s] Indicators: %v", manga.Shortname, cbzName, cfInfo.Indicators)
				log.Printf("[%s:%s] This chapter may fail to download", manga.Shortname, cbzName)
			}
			scrapeErr = err
		})

		// Visit the chapter page to scrape images
		err = c.Visit(chapterURL)
		if err != nil {
			log.Printf("[%s:%s] Failed to visit %s: %v", manga.Shortname, cbzName, chapterURL, err)
			continue
		}

		if scrapeErr != nil {
			log.Printf("[%s:%s] Scrape error: %v", manga.Shortname, cbzName, scrapeErr)
			continue
		}

		if len(imgURLs) == 0 {
			log.Printf("[%s:%s] ⚠️ WARNING: No images found for chapter (cf may be blocking)", manga.Shortname, cbzName)
			continue
		}

		// Create temporary directory for downloading chapter images
		// Format: /tmp/manga-shortname/ch###/
		chapterDir := filepath.Join("/tmp", manga.Shortname, strings.TrimSuffix(cbzName, ".cbz"))
		err = os.MkdirAll(chapterDir, 0755)
		if err != nil {
			log.Printf("[%s:%s] Failed to create temporary directory %s: %v", manga.Shortname, cbzName, chapterDir, err)
			continue
		}

		// Download and convert each image to JPG format
		// Uses shared parser.DownloadConvertToJPGRename function
		successCount := 0

		// implement basic rateLimiting
		rateLimiter := parser.NewRateLimiter(1500 * time.Millisecond)
		defer rateLimiter.Stop()

		// After getting imgURLs map, sort the keys
		sortedImgIndices, err := parser.SortKeysNumeric(imgURLs)
		if err != nil {
			log.Printf("[%s:%s] Failed to sort image indices: %v", manga.Shortname, cbzName, err)
			continue
		}

		// Then iterate the sorted slice (for consecutive download order to be shown in the GUI)
		for _, imgIdx := range sortedImgIndices {
			imgURL := imgURLs[imgIdx]

			// Check for cancellation
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				// Continue
			}

			// ratelimit connections
			rateLimiter.Wait()

			// typecast to float64
			imgNum, err := strconv.ParseInt(imgIdx, 10, 64)
			if err != nil {
				log.Printf("Invalid image index %s: %v", imgIdx, err)
				continue
			}
			// Update progress to show individual image download progress
			if progressCallback != nil {
				imgProgress := progress + (float64(imgNum) / float64(len(imgURLs)) / float64(totalChapters))
				progressCallback(fmt.Sprintf("Chapter %d/%d: Downloading image %d/%d", currentChapter, totalChapters, imgNum+1, len(imgURLs)), imgProgress, currentChapter, totalChapters)
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

		// Only create CBZ if we got at least some images
		if successCount == 0 {
			log.Printf("[%s:%s] ⚠️ Skipping CBZ creation - no images downloaded", manga.Shortname, cbzName)
			os.RemoveAll(chapterDir)
			continue
		}

		// Create CBZ file from the downloaded images in the manga's location directory
		// Uses shared parser.CreateCbzFromDir function
		if progressCallback != nil {
			progressCallback(fmt.Sprintf("Chapter %d/%d: Creating CBZ file...", currentChapter, totalChapters), progress, currentChapter, totalChapters)
		}

		cbzPath := filepath.Join(manga.Location, cbzName)
		err = parser.CreateCbzFromDir(chapterDir, cbzPath)
		if err != nil {
			log.Printf("[%s:%s] Failed to create CBZ %s: %v", manga.Shortname, cbzName, cbzPath, err)
		} else {
			log.Printf("[%s] ✓ Created CBZ: %s (%d images)\n", manga.Title, cbzName, successCount)
		}

		// Clean up: Remove temporary directory after CBZ creation
		err = os.RemoveAll(chapterDir)
		if err != nil {
			log.Printf("[%s:%s] Failed to remove temp directory %s: %v", manga.Shortname, cbzName, chapterDir, err)
		}
	}

	log.Printf("<%s> Download complete [%s]", manga.Site, manga.Title)
	if progressCallback != nil {
		progressCallback(fmt.Sprintf("Download complete! Downloaded %d chapters", totalChapters), 1.0, totalChapters, totalChapters)
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
func xbatoChapterUrls(url string) ([]string, error) {
	var chapters []string

	log.Printf("<xbato> Fetching chapter list from: %s", url)

	// Create a new Colly collector with a realistic User-Agent
	c := colly.NewCollector(
		colly.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/114.0.0.0 Safari/537.36"),
		// IMPORTANT: Allow URL revisiting in case of redirects
		colly.AllowURLRevisit(),
	)

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
				hasStoredData = false // Force browser challenge
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
			log.Printf("<xbato> StatusCode: %d", info.StatusCode)
			log.Printf("<xbato> RayID: %s", info.RayID)
			log.Printf("<xbato> MetaRedirect: %s", info.MetaRedirect)
			log.Printf("<xbato> FormAction: %s", info.FormAction)
			log.Printf("<xbato> IsBIC: %v", info.IsBIC)
			log.Printf("<xbato> Turnstile: %v", info.Turnstile)
		}

		// DEBUG: Check first 500 chars of body
		bodyPreview := string(r.Body)
		if len(bodyPreview) > 500 {
			bodyPreview = bodyPreview[:500]
		}
		log.Printf("<xbato> DEBUG: Body preview: %q", bodyPreview)
	})

	// Xbato stores chapter links in <a> tags with class "chapt"
	// We need to capture both the link text (chapter number) and the URL
	c.OnHTML("a.chapt", func(e *colly.HTMLElement) {
		href := e.Attr("href")
		chapterText := strings.TrimSpace(e.Text)

		if href != "" && strings.HasPrefix(href, "/chapter/") {
			// Build full URL using AbsoluteURL to handle relative paths
			fullURL := e.Request.AbsoluteURL(href)

			// Store as "chapterText|URL" so we can parse the chapter number later
			chapterEntry := fmt.Sprintf("%s|%s", chapterText, fullURL)
			chapters = append(chapters, chapterEntry)
			log.Printf("<xbato> Added chapter entry: '%s' -> %s", chapterText, fullURL)
		} else if href != "" {
			log.Printf("<xbato> WARNING: Skipped href that doesn't start with /chapter/: '%s'", href)
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

			// Delete the invalid stored data
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

	// Log final result
	log.Printf("<xbato> Successfully scraped %d chapter URLs", len(chapters))

	// If no chapters found, log a warning
	if len(chapters) == 0 {
		log.Printf("<xbato> WARNING: No chapters found at %s - check if the HTML selector 'a.chapt' is correct", url)
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
//   - map[string]string: Map where key = normalized filename (ch###.cbz or ch###.part.cbz)
//     and value = full chapter URL
//
// The function extracts chapter numbers from the text and normalizes them to a standard format:
// - Main chapters: ch001.cbz, ch002.cbz, etc.
// - Split chapters: ch001.1.cbz, ch001.2.cbz, etc.
//
// Example input:
//   - "Chapter 1|https://xbato.com/chapter/3890889" -> ch001.cbz
//   - "Chapter 1.5|https://xbato.com/chapter/3890890" -> ch001.5.cbz
func xbatoChapterMap(entries []string) map[string]string {
	chapterMap := make(map[string]string)

	log.Printf("<xbato> Processing %d chapter entries", len(entries))

	// Regex to extract chapter numbers from text like "Chapter 1", "Chapter 1.5", "Chapter 123"
	re := regexp.MustCompile(`Chapter\s+(\d+)(?:\.(\d+))?`)

	for _, entry := range entries {
		// Split the entry into text and URL
		parts := strings.Split(entry, "|")
		if len(parts) != 2 {
			log.Printf("<xbato> WARNING: Invalid entry format: %s", entry)
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
			log.Printf("<xbato> Mapped: %s → %s", filename, url)
		} else {
			log.Printf("<xbato> WARNING: Could not parse chapter number from text: %s", chapterText)
		}
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
