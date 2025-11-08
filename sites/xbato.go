package sites

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

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
func XbatoDownloadChapters(manga *config.Bookmarks, progressCallback func(string, float64, int, int)) error {
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

	log.Printf("Starting download for manga: %s", manga.Title)
	if progressCallback != nil {
		progressCallback(fmt.Sprintf("Fetching chapter list for %s...", manga.Title), 0, 0, 0)
	}

	// Step 1: Build the manga URL from shortname
	// Xbato URL pattern: https://xbato.com/series/<shortname>
	mangaUrl := fmt.Sprintf("https://xbato.com/series/%s", manga.Shortname)

	// Get all chapter entries (format: "Chapter X|URL") from the manga's main page
	chapterEntries, err := xbatoChapterUrls(mangaUrl)
	if err != nil {
		return fmt.Errorf("failed to fetch chapter URLs: %v", err)
	}

	// Step 2: Build chapter map (key = "chXXX.cbz", value = URL)
	// This normalizes chapter names for consistent file naming
	chapterMap := xbatoChapterMap(chapterEntries)

	// Step 3: Get list of already downloaded chapters from manga's location directory
	// Uses shared parser.LocalChapterList function to read existing .cbz files
	downloadedChapters, err := parser.LocalChapterList(manga.Location)
	if err != nil {
		return fmt.Errorf("failed to list files in %s: %v", manga.Location, err)
	}

	// Step 4: Remove already-downloaded chapters from the map
	// This prevents re-downloading existing chapters
	for _, chapter := range downloadedChapters {
		delete(chapterMap, chapter)
	}

	totalChapters := len(chapterMap)
	if totalChapters == 0 {
		if progressCallback != nil {
			progressCallback("No new chapters to download", 1.0, 0, 0)
		}
		return nil
	}

	log.Printf("[%s] %d chapters to download", manga.Shortname, totalChapters)
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
		chapterURL := chapterMap[cbzName]

		// Calculate progress metrics for callback
		currentChapter := idx + 1
		progress := float64(currentChapter) / float64(totalChapters)

		if progressCallback != nil {
			progressCallback(fmt.Sprintf("Downloading chapter %d/%d: %s", currentChapter, totalChapters, cbzName), progress, currentChapter, totalChapters)
		}

		// Scrape image URLs from the chapter page
		imgURLs, imgUrlsErr := extractImageUrlsMap(chapterURL)
		if imgUrlsErr != nil {
			log.Printf("error downloading chapter URls %v", imgUrlsErr)
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
		// Uses shared parser.DownloadAndConvertToJPG function
		for imgIdx, imgURL := range imgURLs {
			// typecast to float64
			imgNum, err := strconv.ParseInt(imgIdx, 10, 64)
			if err != nil {
				log.Printf("Invalid image index %d: %v", imgNum, err)
				continue
			}
			// Update progress to show individual image download progress
			if progressCallback != nil {
				imgProgress := progress + (float64(imgNum) / float64(len(imgURLs)) / float64(totalChapters))
				progressCallback(fmt.Sprintf("Chapter %d/%d: Downloading image %d/%d", currentChapter, totalChapters, imgNum+1, len(imgURLs)), imgProgress, currentChapter, totalChapters)
			}

			log.Printf("[%s:%s] Downloading image %d/%d: %s", manga.Title, cbzName, imgNum+1, len(imgURLs), imgURL)
			imgConvertErr := parser.DownloadConvertToJPGRename(imgIdx, imgURL, chapterDir)
			if imgConvertErr != nil {
				log.Printf("[%s:%s] Failed to download/convert image %s: %v", manga.Title, cbzName, imgURL, imgConvertErr)
			} else {
				log.Printf("[%s:%s] Successfully downloaded and converted image: %s", manga.Title, cbzName, imgURL)
			}
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
			log.Printf("[%s] Created CBZ: %s\n", manga.Title, cbzName)
		}

		// Clean up: Remove temporary directory after CBZ creation
		err = os.RemoveAll(chapterDir)
		if err != nil {
			log.Printf("[%s:%s] Failed to remove temp directory %s: %v", manga.Shortname, cbzName, chapterDir, err)
		}
	}

	log.Printf("[%s] Download complete", manga.Title)
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

	log.Printf("[xbato] Fetching chapter list from: %s", url)

	// Create a new Colly collector with a realistic User-Agent
	c := colly.NewCollector(
		colly.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/114.0.0.0 Safari/537.36"),
	)

	// Log when the page is successfully visited
	c.OnResponse(func(r *colly.Response) {
		log.Printf("[xbato] Successfully fetched page, status: %d, size: %d bytes", r.StatusCode, len(r.Body))
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
			log.Printf("[xbato] Added chapter entry: '%s' -> %s", chapterText, fullURL)
		} else if href != "" {
			log.Printf("[xbato] WARNING: Skipped href that doesn't start with /chapter/: '%s'", href)
		}
	})

	// Capture any scraping errors
	var scrapeErr error
	c.OnError(func(r *colly.Response, err error) {
		log.Printf("[xbato] ERROR during scraping: %v, Status: %d", err, r.StatusCode)
		scrapeErr = err
	})

	// Visit the manga page to trigger scraping
	err := c.Visit(url)
	if err != nil {
		log.Printf("[xbato] Failed to visit URL %s: %v", url, err)
		return nil, err
	}

	// Log final result
	log.Printf("[xbato] Scraping complete. Found %d chapters", len(chapters))

	// Return any error that occurred during OnError callback
	if scrapeErr != nil {
		return nil, scrapeErr
	}

	// If no chapters found, log a warning
	if len(chapters) == 0 {
		log.Printf("[xbato] WARNING: No chapters found at %s - check if the HTML selector 'a.chapt' is correct", url)
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

	log.Printf("[xbato] Processing %d chapter entries", len(entries))

	// Regex to extract chapter numbers from text like "Chapter 1", "Chapter 1.5", "Chapter 123"
	re := regexp.MustCompile(`Chapter\s+(\d+)(?:\.(\d+))?`)

	for _, entry := range entries {
		// Split the entry into text and URL
		parts := strings.Split(entry, "|")
		if len(parts) != 2 {
			log.Printf("[xbato] WARNING: Invalid entry format: %s", entry)
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
			log.Printf("[xbato] Mapped '%s' -> %s (URL: %s)", chapterText, filename, url)
		} else {
			log.Printf("[xbato] WARNING: Could not parse chapter number from text: %s", chapterText)
		}
	}

	log.Printf("[xbato] Created chapter map with %d entries", len(chapterMap))

	return chapterMap
}

// extractImageURLsMap downloads the page at targetURL, finds the imgHttps array,
// and returns a map where key = zero-based index as string, value = original URL.
func extractImageUrlsMap(targetURL string) (map[string]string, error) {
	// Step 1: Download the HTML page
	resp, err := http.Get(targetURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Step 2: Parse HTML
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}

	// Step 3: Find <script> containing "const imgHttps"
	var scriptText string
	doc.Find("script").Each(func(i int, s *goquery.Selection) {
		if strings.Contains(s.Text(), "const imgHttps") {
			scriptText = s.Text()
		}
	})

	if scriptText == "" {
		return nil, fmt.Errorf("imgHttps script block not found")
	}

	// Step 4: Extract the array contents
	re := regexp.MustCompile(`const\s+imgHttps\s*=\s*\[(.*?)\];`)
	match := re.FindStringSubmatch(scriptText)
	if len(match) < 2 {
		return nil, fmt.Errorf("imgHttps array not found inside script")
	}
	arrayText := match[1]

	// Step 5: Extract URLs from quotes
	urlRe := regexp.MustCompile(`"([^"]+)"`)
	matches := urlRe.FindAllStringSubmatch(arrayText, -1)

	if len(matches) == 0 {
		return nil, fmt.Errorf("no image URLs found")
	}

	// Step 6: Build the map with zero-based index as string
	urlMap := make(map[string]string, len(matches))
	for i, m := range matches {
		urlMap[fmt.Sprintf("%d", i)] = m[1]
	}

	return urlMap, nil
}
