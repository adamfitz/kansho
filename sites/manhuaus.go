package sites

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/url"
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

// ManhuausDownloadChapters downloads manga chapters from manhuaus-like websites.
// This function follows the same pattern as XbatoDownloadChapters and RizzfablesDownloadChapters for consistency.
//
// NOTE: Unlike xbato (which uses shortname), this function uses the full manga URL from the bookmark,
// making it flexible for different domains (manhuaus.com, manhuaus.org, etc.)
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
func ManhuausDownloadChapters(ctx context.Context, manga *config.Bookmarks, progressCallback func(string, float64, int, int)) error {
	// Validate input manga data
	if manga == nil {
		return fmt.Errorf("no manga provided")
	}

	if manga.Url == "" {
		return fmt.Errorf("manga URL is empty")
	}
	if manga.Location == "" {
		return fmt.Errorf("manga location is empty")
	}

	log.Printf("<%s> Starting download [%s]", manga.Site, manga.Title)
	if progressCallback != nil {
		progressCallback(fmt.Sprintf("Fetching chapter list for %s...", manga.Title), 0, 0, 0)
	}

	// Step 1: Get all chapter URLs from the manga's main page
	// Uses the full URL from manga.URL (like rizzfables)
	chapterURLs, err := manhuausChapterUrls(manga.Url)
	if err != nil {
		// Pass the error up - it will be a cfChallengeError if CF was detected
		return err
	}

	log.Printf("<%s> Found %d total chapters on site", manga.Site, len(chapterURLs))

	// Step 2: Build chapter map (key = "chXXX.cbz", value = URL)
	chapterMap := manhuausChapterMap(chapterURLs)
	log.Printf("<%s> Mapped %d chapters to filenames", manga.Site, len(chapterMap))

	// Step 3: Get list of already downloaded chapters from manga's location directory
	downloadedChapters, err := parser.LocalChapterList(manga.Location)
	if err != nil {
		return fmt.Errorf("failed to list files in %s: %v", manga.Location, err)
	}
	log.Printf("<%s> Found %d already downloaded chapters", manga.Site, len(downloadedChapters))

	// Step 4: Remove already-downloaded chapters from the map
	for _, chapter := range downloadedChapters {
		if _, exists := chapterMap[chapter]; exists {
			delete(chapterMap, chapter)
			log.Printf("<%s> %s already downloaded, removed from download queue", manga.Site, chapter)
		}
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

		chapterNum := strings.TrimSuffix(cbzName, ".cbz")
		log.Printf("[%s:%s] Starting download from: %s", manga.Title, cbzName, chapterURL)

		// Get image URLs for this chapter
		imgURLs, err := manhuausChapterImageUrls(chapterURL, cbzName, manga.Title)
		if err != nil {
			log.Printf("[%s:%s] Failed to get image URLs: %v", manga.Title, cbzName, err)
			continue
		}

		if len(imgURLs) == 0 {
			log.Printf("[%s:%s] ⚠️ WARNING: No images found for chapter", manga.Title, cbzName)
			continue
		}

		log.Printf("[%s:%s] Found %d images to download", manga.Title, cbzName, len(imgURLs))

		// Create temporary directory for downloading chapter images
		chapterDir := filepath.Join("/tmp", strings.ReplaceAll(manga.Title, " ", "-"), chapterNum)
		err = os.MkdirAll(chapterDir, 0755)
		if err != nil {
			log.Printf("[%s:%s] Failed to create temporary directory %s: %v", manga.Title, cbzName, chapterDir, err)
			continue
		}

		// Download and convert each image to JPG format with rate limiting
		successCount := 0
		rateLimiter := parser.NewRateLimiter(1500 * time.Millisecond)
		defer rateLimiter.Stop()

		for i, imgURL := range imgURLs {

			// Check for cancellation
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				// Continue
			}

			rateLimiter.Wait() // Rate limit - waits 1.5 seconds between images

			imgNum := i + 1

			// Update progress to show individual image download progress
			if progressCallback != nil {
				imgProgress := progress + (float64(imgNum) / float64(len(imgURLs)) / float64(totalChapters))
				progressCallback(fmt.Sprintf("Chapter %d/%d: Downloading image %d/%d", currentChapter, totalChapters, imgNum, len(imgURLs)), imgProgress, currentChapter, totalChapters)
			}

			log.Printf("[%s:%s] Downloading image %d/%d: %s", manga.Title, cbzName, imgNum, len(imgURLs), imgURL)

			// Use shared parser function - pass imgNum as string for filename
			filename := fmt.Sprintf("%d", imgNum)
			imgConvertErr := parser.DownloadConvertToJPGRename(filename, imgURL, chapterDir)
			if imgConvertErr != nil {
				log.Printf("[%s:%s] ⚠️ Failed to download/convert image %s: %v", manga.Title, cbzName, imgURL, imgConvertErr)
			} else {
				successCount++
				log.Printf("[%s:%s] ✓ Successfully downloaded and converted image %d/%d", manga.Title, cbzName, imgNum, len(imgURLs))
			}
		}

		log.Printf("[%s:%s] Download complete: %d/%d images successful", manga.Title, cbzName, successCount, len(imgURLs))

		// Only create CBZ if we got at least some images
		if successCount == 0 {
			log.Printf("[%s:%s] ⚠️ Skipping CBZ creation - no images downloaded", manga.Title, cbzName)
			os.RemoveAll(chapterDir)
			continue
		}

		// Create CBZ file from the downloaded images in the manga's location directory
		if progressCallback != nil {
			progressCallback(fmt.Sprintf("Chapter %d/%d: Creating CBZ file...", currentChapter, totalChapters), progress, currentChapter, totalChapters)
		}

		cbzPath := filepath.Join(manga.Location, cbzName)
		err = parser.CreateCbzFromDir(chapterDir, cbzPath)
		if err != nil {
			log.Printf("[%s:%s] Failed to create CBZ %s: %v", manga.Title, cbzName, cbzPath, err)
		} else {
			log.Printf("[%s] ✓ Created CBZ: %s (%d images)\n", manga.Title, cbzName, successCount)
		}

		// Clean up: Remove temporary directory after CBZ creation
		err = os.RemoveAll(chapterDir)
		if err != nil {
			log.Printf("[%s:%s] Failed to remove temp directory %s: %v", manga.Title, cbzName, chapterDir, err)
		}
	}

	log.Printf("<%s> Download complete [%s]", manga.Site, manga.Title)
	if progressCallback != nil {
		progressCallback(fmt.Sprintf("Download complete! Downloaded %d chapters", totalChapters), 1.0, totalChapters, totalChapters)
	}

	return nil
}

// manhuausChapterUrls retrieves all chapter URLs from a Manhuaus manga page.
// This is a site-specific function as each manga site has different HTML structure.
//
// Parameters:
//   - mangaURL: The main manga page URL (can be any manhuaus domain)
//
// Returns:
//   - []string: Slice of chapter URLs
//   - error: Any error encountered during scraping, nil on success
//
// The function uses Colly to scrape chapter links from li.wp-manga-chapter a elements.
func manhuausChapterUrls(mangaURL string) ([]string, error) {
	var chapterURLs []string

	log.Printf("<manhuaus> Fetching chapter list from: %s", mangaURL)

	// Create a new Colly collector with a realistic User-Agent
	c := colly.NewCollector(
		colly.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/115.0.0.0 Safari/537.36"),
		colly.AllowURLRevisit(),
	)

	// Check for stored cf data
	parsedURL, _ := url.Parse(mangaURL)
	domain := parsedURL.Hostname()

	bypassData, err := cf.LoadFromFile(domain)
	hasStoredData := (err == nil)

	if hasStoredData {
		log.Printf("<manhuaus> Found stored bypass data for %s (type: %s)", domain, bypassData.Type)

		// Check if cf_clearance exists and is valid
		if bypassData.CfClearanceStruct != nil {
			log.Printf("<manhuaus> cf_clearance found, expires: %v", bypassData.CfClearanceStruct.Expires)

			// Check expiration
			if bypassData.CfClearanceStruct.Expires != nil && time.Now().After(*bypassData.CfClearanceStruct.Expires) {
				log.Printf("<manhuaus> ⚠️ cf_clearance has EXPIRED!")
				hasStoredData = false // Force browser challenge
			}
		}

		if hasStoredData {
			// Apply the stored data
			if err := cf.ApplyToCollector(c, mangaURL); err != nil {
				log.Printf("<manhuaus> Failed to apply bypass data: %v", err)
				hasStoredData = false
			} else {
				log.Printf("<manhuaus> ✓ Applied stored cf_clearance cookie")
			}
		}
	} else {
		log.Printf("<manhuaus> No stored bypass data found for %s", domain)
	}

	var cfDetected bool
	var cfInfo *cf.CfInfo
	var scrapeErr error

	c.OnResponse(func(r *colly.Response) {
		// Automatically decompress the response (handles gzip and Brotli)
		if decompressed, err := cf.DecompressResponse(r, "<manhuaus>"); err != nil {
			log.Printf("<manhuaus> ERROR: Failed to decompress response: %v", err)
			return
		} else if decompressed {
			log.Printf("<manhuaus> Response successfully decompressed")
		}

		log.Printf("<manhuaus> Chapter list response: status=%d, size=%d bytes", r.StatusCode, len(r.Body))

		// Check for Cloudflare challenge
		isCF, info, _ := cf.DetectFromColly(r)
		if isCF {
			cfDetected = true
			cfInfo = info
			log.Printf("<manhuaus> ⚠️ cf challenge detected!")
			log.Printf("<manhuaus> Indicators: %v", info.Indicators)
			log.Printf("<manhuaus> StatusCode: %d", info.StatusCode)
			log.Printf("<manhuaus> RayID: %s", info.RayID)
		}
	})

	// Scrape chapter links from li.wp-manga-chapter a elements
	c.OnHTML("li.wp-manga-chapter a", func(e *colly.HTMLElement) {
		href := e.Attr("href")
		if href != "" {
			chapterURLs = append(chapterURLs, href)
			log.Printf("<manhuaus> Added chapter URL: %s", href)
		}
	})

	c.OnError(func(r *colly.Response, err error) {
		log.Printf("<manhuaus> ERROR: %v, Status: %d", err, r.StatusCode)

		isCF, info, _ := cf.DetectFromColly(r)
		if isCF {
			cfDetected = true
			cfInfo = info
			log.Printf("<manhuaus> cf block detected: %v", info.Indicators)
		}
		scrapeErr = err
	})

	// Make the request
	visitErr := c.Visit(mangaURL)
	if visitErr != nil {
		log.Printf("<manhuaus> Visit error: %v", visitErr)
	}

	// Handle cf detection
	if cfDetected {
		if hasStoredData {
			log.Printf("<manhuaus> ⚠️ Stored cf_clearance failed validation - cookie is expired/invalid")
			log.Printf("<manhuaus> Deleting invalid data and requesting fresh challenge")

			// Delete the invalid stored data
			cf.DeleteDomain(domain)
		}

		log.Printf("<manhuaus> Opening browser for cf challenge...")
		challengeURL := cf.GetChallengeURL(cfInfo, mangaURL)

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

	if len(chapterURLs) == 0 {
		log.Printf("<manhuaus> WARNING: No chapters found at %s", mangaURL)
		return nil, fmt.Errorf("no chapter URLs found at %s", mangaURL)
	}

	log.Printf("<manhuaus> Successfully scraped %d chapter URLs", len(chapterURLs))

	return chapterURLs, nil
}

// manhuausChapterMap takes a slice of chapter URLs and returns a normalized map.
// This is a site-specific function as URL patterns differ between manga sites.
//
// Parameters:
//   - urls: Slice of chapter URLs from manhuaus-like sites
//
// Returns:
//   - map[string]string: Map where key = normalized filename (ch###.cbz or ch###.part.cbz)
//     and value = full chapter URL
//
// The function extracts chapter numbers from URLs and normalizes them to a standard format:
// - Main chapters: ch001.cbz, ch002.cbz, etc.
// - Split chapters: ch001.5.cbz, ch001.2.cbz, etc.
//
// Example input:
//   - "https://manhuaus.com/manga/title/chapter-1/" -> ch001.cbz
//   - "https://manhuaus.com/manga/title/chapter-1-5/" -> ch001.5.cbz
func manhuausChapterMap(urls []string) map[string]string {
	chapterMap := make(map[string]string)

	log.Printf("<manhuaus> Processing %d chapter URLs", len(urls))

	// Regex to extract chapter numbers from URLs like "chapter-1", "chapter-1.5", "chapter-123"
	re := regexp.MustCompile(`chapter-([\d.]+)`)

	for _, chapterURL := range urls {
		match := re.FindStringSubmatch(chapterURL)
		if len(match) < 2 {
			log.Printf("<manhuaus> WARNING: Could not parse chapter number from URL: %s", chapterURL)
			continue
		}

		raw := match[1]
		var filename string

		// Handle decimal chapters (e.g., "1.5")
		if strings.Contains(raw, ".") {
			parts := strings.SplitN(raw, ".", 2)
			intPart, err := strconv.Atoi(parts[0])
			if err != nil {
				log.Printf("<manhuaus> WARNING: Invalid integer part in chapter number: %v", err)
				continue
			}
			filename = fmt.Sprintf("ch%03d.%s.cbz", intPart, parts[1])
		} else {
			// Standard chapter number
			num, err := strconv.Atoi(raw)
			if err != nil {
				log.Printf("<manhuaus> WARNING: Invalid chapter number: %v", err)
				continue
			}
			filename = fmt.Sprintf("ch%03d.cbz", num)
		}

		chapterMap[filename] = chapterURL
		log.Printf("<manhuaus> Mapped: %s → %s", filename, chapterURL)
	}

	log.Printf("<manhuaus> Created chapter map with %d entries", len(chapterMap))

	return chapterMap
}

// manhuausChapterImageUrls returns all image URLs for a chapter by fetching and parsing the chapter page.
//
// Parameters:
//   - chapterURL: The chapter page URL
//   - cbzName: The chapter filename (for logging)
//   - mangaTitle: The manga title (for logging)
//
// Returns:
//   - []string: Slice of image URLs in order
//   - error: Any error encountered during scraping, nil on success
func manhuausChapterImageUrls(chapterURL, cbzName, mangaTitle string) ([]string, error) {
	log.Printf("[%s:%s] Fetching chapter page to extract image URLs", mangaTitle, cbzName)

	// Create a new Colly collector for this chapter
	c := colly.NewCollector(
		colly.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/115.0.0.0 Safari/537.36"),
	)

	// Apply CF bypass if available
	parsedURL, _ := url.Parse(chapterURL)
	domain := parsedURL.Hostname()

	if _, err := cf.LoadFromFile(domain); err == nil {
		if applyErr := cf.ApplyToCollector(c, chapterURL); applyErr != nil {
			log.Printf("[%s:%s] WARNING: Failed to apply bypass data: %v", mangaTitle, cbzName, applyErr)
		} else {
			log.Printf("[%s:%s] ✓ cf bypass applied to chapter collector", mangaTitle, cbzName)
		}
	}

	var imgURLs []string
	var scrapeErr error

	c.OnResponse(func(r *colly.Response) {
		// Decompress response if needed
		if decompressed, err := cf.DecompressResponse(r, fmt.Sprintf("[%s]", cbzName)); err != nil {
			log.Printf("[%s:%s] ERROR: Failed to decompress: %v", mangaTitle, cbzName, err)
			return
		} else if decompressed {
			log.Printf("[%s:%s] ✓ Chapter page decompressed", mangaTitle, cbzName)
		}

		log.Printf("[%s:%s] Chapter page response: status=%d, size=%d bytes",
			mangaTitle, cbzName, r.StatusCode, len(r.Body))

		// Parse HTML using goquery
		doc, err := goquery.NewDocumentFromReader(bytes.NewReader(r.Body))
		if err != nil {
			scrapeErr = fmt.Errorf("failed to parse chapter HTML: %v", err)
			return
		}

		// Extract images from div.reading-content img with data-src attribute
		doc.Find("div.reading-content img").Each(func(i int, s *goquery.Selection) {
			src := strings.TrimSpace(s.AttrOr("data-src", ""))
			if src != "" {
				imgURLs = append(imgURLs, src)
			}
		})

		log.Printf("[%s:%s] Found %d images in chapter page", mangaTitle, cbzName, len(imgURLs))
	})

	c.OnError(func(r *colly.Response, err error) {
		log.Printf("[%s:%s] ERROR fetching chapter page: %v (status: %d)",
			mangaTitle, cbzName, err, r.StatusCode)
		scrapeErr = err
	})

	// Visit the chapter page
	err := c.Visit(chapterURL)
	if err != nil {
		return nil, fmt.Errorf("failed to visit chapter page: %w", err)
	}

	if scrapeErr != nil {
		return nil, scrapeErr
	}

	if len(imgURLs) == 0 {
		return nil, fmt.Errorf("no images found in chapter page")
	}

	return imgURLs, nil
}
