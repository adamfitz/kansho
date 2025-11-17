package sites

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"kansho/cf"
	"kansho/config"
	"kansho/parser"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"github.com/gocolly/colly"
)

// RizzfablesDownloadChapters downloads manga chapters from rizzfables website.
// This function follows the same pattern as XbatoDownloadChapters and MgekoDownloadChapters for consistency.
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
// 5. Downloads each new chapter by scraping image URLs using chromedp
// 6. Creates CBZ files from downloaded images
func RizzfablesDownloadChapters(ctx context.Context, manga *config.Bookmarks, progressCallback func(string, float64, int, int)) error {
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

	// Step 1: Get all chapter entries (format: map[chapterName]URL) from the manga's main page
	chapterMap, err := rizzfablesChapterUrls(manga.Url)
	if err != nil {
		// Pass the error up - it will be a cfChallengeError if CF was detected
		return err
	}

	// Log the number of chapters found
	log.Printf("<%s> Found %d total chapters on site", manga.Site, len(chapterMap))

	// Step 2: Get list of already downloaded chapters from manga's location directory
	downloadedChapters, err := parser.LocalChapterList(manga.Location)
	if err != nil {
		return fmt.Errorf("failed to list files in %s: %v", manga.Location, err)
	}
	log.Printf("<%s> Found %d already downloaded chapters", manga.Site, len(downloadedChapters))

	// Step 3: Remove already-downloaded chapters from the map
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

	// Step 4: Sort chapter keys alphabetically for ordered downloading
	sortedChapters, sortError := parser.SortKeys(chapterMap)
	if sortError != nil {
		return fmt.Errorf("failed to sort chapter map keys: %v", sortError)
	}

	// Step 5: Iterate over sorted chapter keys and download each one
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

		// Get image URLs for this chapter using chromedp
		imgURLs, err := rizzfablesChapterImageUrls(chapterURL, cbzName, manga.Title)
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

		// Download and convert each image to JPG format
		successCount := 0

		// implement basic rateLimiting
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

			// ratelimit connections
			rateLimiter.Wait()

			imgNum := i + 1

			// Update progress to show individual image download progress
			if progressCallback != nil {
				imgProgress := progress + (float64(imgNum) / float64(len(imgURLs)) / float64(totalChapters))
				progressCallback(fmt.Sprintf("Chapter %d/%d: Downloading image %d/%d", currentChapter, totalChapters, imgNum, len(imgURLs)), imgProgress, currentChapter, totalChapters)
			}

			log.Printf("[%s:%s] Downloading image %d/%d: %s", manga.Title, cbzName, imgNum, len(imgURLs), imgURL)

			// Use shared parser function with custom filename
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

// rizzfablesChapterUrls retrieves all chapter URLs from a Rizzfables manga page.
// This is a site-specific function as each manga site has different HTML structure.
//
// Parameters:
//   - mangaUrl: The main manga page URL on rizzfables.com
//
// Returns:
//   - map[string]string: Map where key = normalized filename (ch###.cbz) and value = chapter URL
//   - error: Any error encountered during scraping, nil on success
//
// The function uses Colly to scrape chapter links from div.eplister ul li elements.
// Chapter numbers are extracted from data-num attribute and normalized to ch###.cbz format.
func rizzfablesChapterUrls(mangaUrl string) (map[string]string, error) {
	chapterMap := make(map[string]string)

	log.Printf("<rizzfables> Fetching chapter list from: %s", mangaUrl)

	// Create a new Colly collector with a realistic User-Agent
	c := colly.NewCollector(
		colly.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/115.0.0.0 Safari/537.36"),
		colly.AllowURLRevisit(),
	)

	// Check for stored cf data
	parsedURL, _ := url.Parse(mangaUrl)
	domain := parsedURL.Hostname()

	bypassData, err := cf.LoadFromFile(domain)
	hasStoredData := (err == nil)

	if hasStoredData {
		log.Printf("<rizzfables> Found stored bypass data for %s (type: %s)", domain, bypassData.Type)

		// Check if cf_clearance exists and is valid
		if bypassData.CfClearanceStruct != nil {
			log.Printf("<rizzfables> cf_clearance found, expires: %v", bypassData.CfClearanceStruct.Expires)

			// Check expiration
			if bypassData.CfClearanceStruct.Expires != nil && time.Now().After(*bypassData.CfClearanceStruct.Expires) {
				log.Printf("<rizzfables> ⚠️ cf_clearance has EXPIRED!")
				hasStoredData = false // Force browser challenge
			}
		}

		if hasStoredData {
			// Apply the stored data
			if err := cf.ApplyToCollector(c, mangaUrl); err != nil {
				log.Printf("<rizzfables> Failed to apply bypass data: %v", err)
				hasStoredData = false
			} else {
				log.Printf("<rizzfables> ✓ Applied stored cf_clearance cookie")
			}
		}
	} else {
		log.Printf("<rizzfables> No stored bypass data found for %s", domain)
	}

	var cfDetected bool
	var cfInfo *cf.CfInfo
	var scrapeErr error

	c.OnResponse(func(r *colly.Response) {
		// Automatically decompress the response (handles gzip and Brotli)
		if decompressed, err := cf.DecompressResponse(r, "<rizzfables>"); err != nil {
			log.Printf("<rizzfables> ERROR: Failed to decompress response: %v", err)
			return
		} else if decompressed {
			log.Printf("<rizzfables> Response successfully decompressed")
		}

		log.Printf("<rizzfables> Chapter list response: status=%d, size=%d bytes", r.StatusCode, len(r.Body))

		// Check for Cloudflare challenge
		isCF, info, _ := cf.DetectFromColly(r)
		if isCF {
			cfDetected = true
			cfInfo = info
			log.Printf("<rizzfables> ⚠️ cf challenge detected!")
			log.Printf("<rizzfables> Indicators: %v", info.Indicators)
			log.Printf("<rizzfables> StatusCode: %d", info.StatusCode)
			log.Printf("<rizzfables> RayID: %s", info.RayID)
		}
	})

	// Scrape chapter links from div.eplister ul li elements
	c.OnHTML("div.eplister ul li", func(e *colly.HTMLElement) {
		chapterNum := e.Attr("data-num") // e.g. "7.5", "81", "42.25"
		href := e.ChildAttr("a", "href")

		if chapterNum == "" || href == "" {
			return
		}

		// Parse the chapterNum as float64 to validate it
		_, err := strconv.ParseFloat(chapterNum, 64)
		if err != nil {
			log.Printf("<rizzfables> error converting chapter string to float: %v", err)
			return
		}

		// Split chapterNum into whole and fractional parts as strings
		parts := strings.Split(chapterNum, ".")

		wholePart := parts[0]
		fracPart := ""
		if len(parts) > 1 {
			fracPart = parts[1]
		}

		// Pad the whole part to 3 digits
		wholeNum, err := strconv.Atoi(wholePart)
		if err != nil {
			log.Printf("<rizzfables> error converting whole part to int: %v", err)
			return
		}
		paddedWhole := fmt.Sprintf("%03d", wholeNum)

		// Compose final chapter name string
		var chName string
		if fracPart != "" {
			chName = fmt.Sprintf("ch%s.%s.cbz", paddedWhole, fracPart)
		} else {
			chName = fmt.Sprintf("ch%s.cbz", paddedWhole)
		}

		// Add to map
		chapterMap[chName] = href
		log.Printf("<rizzfables> Added chapter: %s -> %s", chName, href)
	})

	c.OnError(func(r *colly.Response, err error) {
		log.Printf("<rizzfables> ERROR: %v, Status: %d", err, r.StatusCode)

		isCF, info, _ := cf.DetectFromColly(r)
		if isCF {
			cfDetected = true
			cfInfo = info
			log.Printf("<rizzfables> cf block detected: %v", info.Indicators)
		}
		scrapeErr = err
	})

	// Make the request
	visitErr := c.Visit(mangaUrl)
	if visitErr != nil {
		log.Printf("<rizzfables> Visit error: %v", visitErr)
	}

	// Handle cf detection
	if cfDetected {
		if hasStoredData {
			log.Printf("<rizzfables> ⚠️ Stored cf_clearance failed validation - cookie is expired/invalid")
			log.Printf("<rizzfables> Deleting invalid data and requesting fresh challenge")

			// Delete the invalid stored data
			cf.DeleteDomain(domain)
		}

		log.Printf("<rizzfables> Opening browser for cf challenge...")
		challengeURL := cf.GetChallengeURL(cfInfo, mangaUrl)

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

	if len(chapterMap) == 0 {
		log.Printf("<rizzfables> WARNING: No chapters found at %s", mangaUrl)
		return nil, fmt.Errorf("no chapter URLs found at %s", mangaUrl)
	}

	log.Printf("<rizzfables> Successfully scraped %d chapter URLs", len(chapterMap))

	return chapterMap, nil
}

// rizzfablesChapterImageUrls returns all image URLs for a chapter using chromedp.
// This function uses headless Chrome to execute JavaScript and extract image URLs
// from the #readerarea img elements.
//
// Parameters:
//   - chapterUrl: The chapter page URL
//   - cbzName: The chapter filename (for logging)
//   - mangaTitle: The manga title (for logging)
//
// Returns:
//   - []string: Slice of image URLs in order
//   - error: Any error encountered during scraping, nil on success
func rizzfablesChapterImageUrls(chapterUrl, cbzName, mangaTitle string) ([]string, error) {
	log.Printf("[%s:%s] Using chromedp to fetch image URLs", mangaTitle, cbzName)

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.UserAgent(`Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/115.0.0.0 Safari/537.36`),
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("disable-gpu", true),
	)

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancelAlloc()

	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	// Enable network events
	if err := chromedp.Run(ctx, network.Enable()); err != nil {
		return nil, fmt.Errorf("failed to enable network events: %w", err)
	}

	// Listen to network events (filtered to reduce noise)
	chromedp.ListenTarget(ctx, func(ev any) {
		switch ev := ev.(type) {
		case *network.EventRequestWillBeSent:
			url := ev.Request.URL
			if strings.Contains(url, "rizzfables.com") &&
				(strings.HasSuffix(url, ".webp") || strings.HasSuffix(url, ".jpg") || strings.HasSuffix(url, ".png")) {
				log.Printf("[%s:%s] REQUEST: %s %s", mangaTitle, cbzName, ev.Request.Method, url)
			}
		case *network.EventResponseReceived:
			url := ev.Response.URL
			if strings.Contains(url, "rizzfables.com") &&
				(strings.HasSuffix(url, ".webp") || strings.HasSuffix(url, ".jpg") || strings.HasSuffix(url, ".png")) {
				log.Printf("[%s:%s] RESPONSE: %d %s", mangaTitle, cbzName, ev.Response.Status, url)
			}
		}
	})

	var imageURLs []string

	// JavaScript to extract image URLs from #readerarea
	jsGetImages := `
        Array.from(document.querySelectorAll('#readerarea img'))
            .map(img => img.src)
            .filter(src => src.includes('cdn.rizzfables.com/wp-content/uploads'))
    `

	err := chromedp.Run(ctx,
		chromedp.Navigate(chapterUrl),
		chromedp.WaitVisible("#readerarea img", chromedp.ByQuery),
		chromedp.Evaluate(jsGetImages, &imageURLs),
	)
	if err != nil {
		return nil, fmt.Errorf("chromedp execution failed: %w", err)
	}

	log.Printf("[%s:%s] Extracted %d image URLs using chromedp", mangaTitle, cbzName, len(imageURLs))

	return imageURLs, nil
}
