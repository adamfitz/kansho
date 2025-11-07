package sites

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"kansho/config"
	"kansho/parser"

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
	if manga.Url == "" {
		return fmt.Errorf("manga URL is empty")
	}
	if manga.Location == "" {
		return fmt.Errorf("manga location is empty")
	}

	log.Printf("Starting download for manga: %s", manga.Title)
	if progressCallback != nil {
		progressCallback(fmt.Sprintf("Fetching chapter list for %s...", manga.Title), 0, 0, 0)
	}

	// Step 1: Get all chapter URLs from the manga's main page
	chapterUrls, err := xbatoChapterUrls(manga.Url)
	if err != nil {
		return fmt.Errorf("failed to fetch chapter URLs: %v", err)
	}

	// Step 2: Build chapter map (key = "chXXX.cbz", value = URL)
	// This normalizes chapter names for consistent file naming
	chapterMap := xbatoChapterMap(chapterUrls)

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

		// Scrape image URLs from the chapter page using Colly
		var imgURLs []string
		c := colly.NewCollector(
			colly.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/114.0.0.0 Safari/537.36"),
		)

		// Xbato stores images in elements with class "comicimg"
		c.OnHTML("img.comicimg", func(e *colly.HTMLElement) {
			src := e.Attr("src")
			if src != "" {
				imgURLs = append(imgURLs, src)
				log.Printf("[%s:%s] Found image URL: %s", manga.Shortname, cbzName, src)
			}
		})

		// Log any errors during scraping
		c.OnError(func(_ *colly.Response, err error) {
			log.Printf("[%s:%s] Failed to fetch chapter page %s: %v", manga.Shortname, cbzName, chapterURL, err)
		})

		// Visit the chapter page to trigger scraping
		err := c.Visit(chapterURL)
		if err != nil {
			log.Printf("[%s:%s] Failed to visit %s: %v", manga.Shortname, cbzName, chapterURL, err)
			continue
		}

		// Skip if no images were found
		if len(imgURLs) == 0 {
			log.Printf("[%s:%s] No images found for chapter", manga.Shortname, cbzName)
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
		// Uses shared parser.DownloadAndConvertToJPG function
		for imgIdx, imgURL := range imgURLs {
			// Update progress to show individual image download progress
			if progressCallback != nil {
				imgProgress := progress + (float64(imgIdx) / float64(len(imgURLs)) / float64(totalChapters))
				progressCallback(fmt.Sprintf("Chapter %d/%d: Downloading image %d/%d", currentChapter, totalChapters, imgIdx+1, len(imgURLs)), imgProgress, currentChapter, totalChapters)
			}

			log.Printf("[%s:%s] Downloading image %d/%d: %s", manga.Shortname, cbzName, imgIdx+1, len(imgURLs), imgURL)
			err := parser.DownloadAndConvertToJPG(imgURL, chapterDir)
			if err != nil {
				log.Printf("[%s:%s] Failed to download/convert image %s: %v", manga.Shortname, cbzName, imgURL, err)
			} else {
				log.Printf("[%s:%s] Successfully downloaded and converted image: %s", manga.Shortname, cbzName, imgURL)
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
//   - []string: Slice of full chapter URLs
//   - error: Any error encountered during scraping, nil on success
//
// The function uses Colly to scrape links with class "chap" from the manga page.
func xbatoChapterUrls(url string) ([]string, error) {
	var chapters []string

	// Create a new Colly collector with a realistic User-Agent
	c := colly.NewCollector(
		colly.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/114.0.0.0 Safari/537.36"),
	)

	// Xbato stores chapter links in <a> tags with class "chap"
	c.OnHTML("a.chap", func(e *colly.HTMLElement) {
		href := e.Attr("href")
		if href != "" {
			// Build full URL if href is relative
			fullURL := e.Request.AbsoluteURL(href)
			chapters = append(chapters, fullURL)
		}
	})

	// Capture any scraping errors
	var scrapeErr error
	c.OnError(func(_ *colly.Response, err error) {
		scrapeErr = err
	})

	// Visit the manga page to trigger scraping
	err := c.Visit(url)
	if err != nil {
		return nil, err
	}

	// Return any error that occurred during OnError callback
	if scrapeErr != nil {
		return nil, scrapeErr
	}

	return chapters, nil
}

// xbatoChapterMap takes a slice of Xbato chapter URLs and returns a normalized map.
// This is a site-specific function as URL patterns differ between manga sites.
//
// Parameters:
//   - urls: Slice of chapter URLs from xbato.com
//
// Returns:
//   - map[string]string: Map where key = normalized filename (ch###.cbz or ch###.part.cbz)
//     and value = full chapter URL
//
// The function extracts chapter numbers from URLs and normalizes them to a standard format:
// - Main chapters: ch001.cbz, ch002.cbz, etc.
// - Split chapters: ch001.1.cbz, ch001.2.cbz, etc.
//
// Example URL patterns:
//   - https://xbato.com/manga/title/chapter-1 -> ch001.cbz
//   - https://xbato.com/manga/title/chapter-1-5 -> ch001.5.cbz
func xbatoChapterMap(urls []string) map[string]string {
	chapterMap := make(map[string]string)

	// Regex to match chapter numbers in Xbato URLs
	// Matches patterns like "chapter-1", "chapter-1-5", "chapter-10-2", etc.
	re := regexp.MustCompile(`chapter[-_]?(\d+)((?:[-_\.]\d+)*)`)

	for _, url := range urls {
		matches := re.FindStringSubmatch(url)
		if len(matches) > 0 {
			mainNum := matches[1] // Main chapter number (e.g., "1", "10", "123")
			partStr := matches[2] // Optional part string (e.g., "-5", ".2", "_3")

			// Normalize separators: replace - or _ with .
			normalizedPart := strings.ReplaceAll(partStr, "-", ".")
			normalizedPart = strings.ReplaceAll(normalizedPart, "_", ".")

			// Remove leading dot if present
			normalizedPart = strings.TrimPrefix(normalizedPart, ".")

			// Build final filename: pad main number to 3 digits
			filename := fmt.Sprintf("ch%03s", mainNum)
			if normalizedPart != "" {
				filename += "." + normalizedPart
			}
			filename += ".cbz"

			chapterMap[filename] = url
		}
	}

	log.Printf("found chapters: %v", chapterMap)

	return chapterMap
}
