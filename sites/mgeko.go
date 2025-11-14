package sites

import (
	"fmt"
	"log"
	url2 "net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"kansho/cloudflare"
	"kansho/config"
	"kansho/parser"

	"github.com/gocolly/colly"
)

// downloads manga chapters from mgeko website
// progressCallback is called with status updates during download
// Parameters: status string, progress (0.0-1.0), current chapter, total chapters
func MgekoDownloadChapters(manga *config.Bookmarks, progressCallback func(string, float64, int, int)) error {
	if manga == nil {
		return fmt.Errorf("no manga provided")
	}

	// Validate manga has required fields
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

	// Step 1: Get all chapter URLs
	chapterUrls, err := chapterUrls(manga.Url)
	if err != nil {
		// Just pass the error up - it will be a CloudflareChallengeError if CF was detected
		// The UI layer will handle it appropriately
		return err
	}

	// Step 2: Build chapter map (key = "chXXX.cbz", value = URL)
	chapterMap := chapterMap(chapterUrls)

	// Step 3: Get list of already downloaded chapters from manga's location
	downloadedChapters, err := parser.LocalChapterList(manga.Location)
	if err != nil {
		return fmt.Errorf("failed to list files in %s: %v", manga.Location, err)
	}

	// Step 4: Remove already-downloaded chapters
	for _, chapter := range downloadedChapters {
		delete(chapterMap, chapter)
	}

	totalChapters := len(chapterMap)
	if totalChapters == 0 {
		log.Printf("No new chapters found [%s]", manga.Title)
		if progressCallback != nil {
			progressCallback("No new chapters to download", 1.0, 0, 0)
		}
		return nil
	}

	log.Printf("%d chapters to download [%s]", totalChapters, manga.Title)
	if progressCallback != nil {
		progressCallback(fmt.Sprintf("Found %d new chapters to download", totalChapters), 0, 0, totalChapters)
	}

	// Step 5: Sort chapter keys
	sortedChapters, sortError := parser.SortKeys(chapterMap)
	if sortError != nil {
		return fmt.Errorf("failed to sort chapter map keys: %v", sortError)
	}

	// Step 6: Iterate over sorted chapter keys
	for idx, cbzName := range sortedChapters {
		chapterURL := chapterMap[cbzName]

		currentChapter := idx + 1
		progress := float64(currentChapter) / float64(totalChapters)

		if progressCallback != nil {
			progressCallback(fmt.Sprintf("Downloading chapter %d/%d: %s", currentChapter, totalChapters, cbzName), progress, currentChapter, totalChapters)
		}

		// Colly to scrape image URLs inside #chapter-reader
		var imgURLs []string
		c := colly.NewCollector(
			colly.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/114.0.0.0 Safari/537.36"),
		)
		c.OnHTML("#chapter-reader img", func(e *colly.HTMLElement) {
			src := e.Attr("src")
			if src != "" {
				imgURLs = append(imgURLs, src)
				log.Printf("[%s:%s] Found image URL: %s", manga.Shortname, cbzName, src)
			}
		})
		c.OnError(func(_ *colly.Response, err error) {
			log.Printf("[%s:%s] Failed to fetch chapter page %s: %v", manga.Shortname, cbzName, chapterURL, err)
		})

		err := c.Visit(chapterURL)
		if err != nil {
			log.Printf("[%s:%s] Failed to visit %s: %v", manga.Shortname, cbzName, chapterURL, err)
			continue
		}

		if len(imgURLs) == 0 {
			log.Printf("[%s:%s] No images found for chapter", manga.Shortname, cbzName)
			continue
		}

		// Create temp directory for chapter
		chapterDir := filepath.Join("/tmp", manga.Shortname, strings.TrimSuffix(cbzName, ".cbz"))
		err = os.MkdirAll(chapterDir, 0755)
		if err != nil {
			log.Printf("[%s:%s] Failed to create temporary directory %s: %v", manga.Shortname, cbzName, chapterDir, err)
			continue
		}

		// Download and convert each image using DownloadAndConvertToJPG
		for imgIdx, imgURL := range imgURLs {
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

		// Create CBZ in the manga's location directory
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

		// Remove temp directory
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

// retrieve mgeko chapter list
func chapterUrls(url string) ([]string, error) {
	var chapters []string

	c := colly.NewCollector(
		colly.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"),
	)

	// Check for stored Cloudflare data
	parsedURL, _ := url2.Parse(url)
	domain := parsedURL.Hostname()

	bypassData, err := cloudflare.LoadFromFile(domain)
	hasStoredData := (err == nil)

	if hasStoredData {
		log.Printf("<mgeko> Found stored bypass data for %s (type: %s)", domain, bypassData.Type)

		// Check if cf_clearance exists
		if bypassData.CfClearanceStruct != nil {
			log.Printf("<mgeko> cf_clearance found, expires: %v", bypassData.CfClearanceStruct.Expires)

			// Check expiration
			if bypassData.CfClearanceStruct.Expires != nil && time.Now().After(*bypassData.CfClearanceStruct.Expires) {
				log.Printf("<mgeko> ⚠️ cf_clearance has EXPIRED!")
				hasStoredData = false // Force browser challenge
			}
		}

		if hasStoredData {
			// Apply the stored data
			if err := cloudflare.ApplyToCollector(c, url); err != nil {
				log.Printf("<mgeko> Failed to apply bypass data: %v", err)
				hasStoredData = false
			} else {
				log.Printf("<mgeko> ✓ Applied stored cf_clearance cookie")
			}
		}
	} else {
		log.Printf("<mgeko> No stored bypass data found for %s", domain)
	}

	var cfDetected bool
	var cfInfo *cloudflare.CloudflareInfo
	var scrapeErr error

	c.OnResponse(func(r *colly.Response) {
		log.Printf("<mgeko> Response: status=%d, size=%d bytes", r.StatusCode, len(r.Body))

		isCF, info, _ := cloudflare.DetectFromColly(r)
		if isCF {
			cfDetected = true
			cfInfo = info
			log.Printf("<mgeko> ⚠️ Cloudflare challenge detected despite using stored cookie!")
			log.Printf("<mgeko> This means the stored cf_clearance is invalid/expired")
		}
	})

	c.OnHTML("ul.chapter-list li a", func(e *colly.HTMLElement) {
		href := e.Attr("href")
		if href != "" {
			fullURL := "https://www.mgeko.cc" + href
			chapters = append(chapters, fullURL)
		}
	})

	c.OnError(func(r *colly.Response, err error) {
		log.Printf("<mgeko> ERROR: %v, Status: %d", err, r.StatusCode)

		isCF, info, _ := cloudflare.DetectFromColly(r)
		if isCF {
			cfDetected = true
			cfInfo = info
			log.Printf("<mgeko> Cloudflare block detected: %v", info.Indicators)
		}
		scrapeErr = err
	})

	// Make the request
	visitErr := c.Visit(url)
	if visitErr != nil {
		log.Printf("<mgeko> Visit error: %v", visitErr)
	}

	// Handle Cloudflare detection
	if cfDetected {
		if hasStoredData {
			log.Printf("<mgeko> ⚠️ Stored cf_clearance failed validation - cookie is expired/invalid")
			log.Printf("<mgeko> Deleting invalid data and requesting fresh challenge")

			// Delete the invalid stored data
			cloudflare.DeleteDomain(domain)
		}

		log.Printf("<mgeko> Opening browser for Cloudflare challenge...")
		challengeURL := cloudflare.GetChallengeURL(cfInfo, url)

		if err := cloudflare.OpenInBrowser(challengeURL); err != nil {
			return nil, fmt.Errorf("cloudflare detected but failed to open browser: %w", err)
		}

		return nil, &cloudflare.CloudflareChallengeError{
			URL:        challengeURL,
			StatusCode: cfInfo.StatusCode,
			Indicators: cfInfo.Indicators,
		}
	}

	if scrapeErr != nil {
		return nil, fmt.Errorf("scrape error: %w", scrapeErr)
	}

	return chapters, nil
}

// chapterMap takes a slice of URLs and returns a map:
// key = normalized filename (ch###.part1.part2.cbz), value = URL
func chapterMap(urls []string) map[string]string {
	chapterMap := make(map[string]string)

	// Regex: match main chapter number, then any sequence of part numbers separated by -, _, or .
	re := regexp.MustCompile(`chapter[-_\.]?(\d+)((?:[-_\.]\d+)*)`)

	for _, url := range urls {
		matches := re.FindStringSubmatch(url)
		if len(matches) > 0 {
			mainNum := matches[1] // main chapter number
			partStr := matches[2] // optional part string, e.g., "-2-1" or ".2.1"

			// Normalize separators: replace - or _ with .
			normalizedPart := strings.ReplaceAll(partStr, "-", ".")
			normalizedPart = strings.ReplaceAll(normalizedPart, "_", ".")

			// Remove leading dot (if any) unconditionally
			normalizedPart = strings.TrimPrefix(normalizedPart, ".")

			// Final filename: pad main number to 3 digits
			filename := fmt.Sprintf("ch%03s", mainNum)
			if normalizedPart != "" {
				filename += "." + normalizedPart
			}
			filename += ".cbz"

			chapterMap[filename] = url
		}
	}

	return chapterMap
}
