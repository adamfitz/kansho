package sites

import (
	"context"
	"fmt"
	"log"
	url2 "net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"kansho/cf"
	"kansho/config"
	"kansho/parser"

	"github.com/gocolly/colly"
)

// downloads manga chapters from mgeko website
// progressCallback is called with status updates during download
// Parameters: status string, progress (0.0-1.0), current chapter, total chapters
func MgekoDownloadChapters(ctx context.Context, manga *config.Bookmarks, progressCallback func(string, float64, int, int)) error {
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
		// Just pass the error up - it will be a cfChallengeError if CF was detected
		// The UI layer will handle it appropriately
		return err
	}

	// Log the number of chapters found (similar to xbato)
	log.Printf("<%s> Found %d total chapters on site", manga.Site, len(chapterUrls))

	// Step 2: Build chapter map (key = "chXXX.cbz", value = URL)
	chapterMap := chapterMap(chapterUrls)
	log.Printf("<%s> Mapped %d chapters to filenames", manga.Site, len(chapterMap))

	// Step 3: Get list of already downloaded chapters from manga's location
	downloadedChapters, err := parser.LocalChapterList(manga.Location)
	if err != nil {
		return fmt.Errorf("failed to list files in %s: %v", manga.Location, err)
	}
	log.Printf("<%s> Found %d already downloaded chapters", manga.Site, len(downloadedChapters))

	// Step 4: Remove already-downloaded chapters
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

	// Step 5: Sort chapter keys
	sortedChapters, sortError := parser.SortKeys(chapterMap)
	if sortError != nil {
		return fmt.Errorf("failed to sort chapter map keys: %v", sortError)
	}

	// Step 6: Iterate over sorted chapter keys
	for idx, cbzName := range sortedChapters {

		// Check for cancellation
		select {
		case <-ctx.Done():
			return ctx.Err() // Returns context.Canceled
		default:
			// Continue with download
		}

		chapterURL := chapterMap[cbzName]

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

		// Scrape image URLs from #chapter-reader
		var imgURLs []string
		c.OnHTML("#chapter-reader img", func(e *colly.HTMLElement) {
			src := e.Attr("src")
			if src != "" {
				imgURLs = append(imgURLs, src)
				log.Printf("[%s:%s] Found image URL: %s", manga.Shortname, cbzName, src)
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
		})

		// Log successful response
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
		})
		// Visit the chapter page to scrape images
		err = c.Visit(chapterURL)
		if err != nil {
			log.Printf("[%s:%s] Failed to visit %s: %v", manga.Shortname, cbzName, chapterURL, err)
			continue
		}

		if len(imgURLs) == 0 {
			log.Printf("[%s:%s] ⚠️ WARNING: No images found for chapter (cf may be blocking)", manga.Shortname, cbzName)
			continue
		}

		log.Printf("[%s:%s] Found %d images to download", manga.Shortname, cbzName, len(imgURLs))

		// Create temp directory for chapter
		chapterDir := filepath.Join("/tmp", manga.Shortname, strings.TrimSuffix(cbzName, ".cbz"))
		err = os.MkdirAll(chapterDir, 0755)
		if err != nil {
			log.Printf("[%s:%s] Failed to create temporary directory %s: %v", manga.Shortname, cbzName, chapterDir, err)
			continue
		}

		// Download and convert each image using DownloadAndConvertToJPG
		successCount := 0

		// implement basic rateLimiting
		rateLimiter := parser.NewRateLimiter(1500 * time.Millisecond)
		defer rateLimiter.Stop()

		for imgIdx, imgURL := range imgURLs {

			// Check for cancellation
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				// Continue
			}

			// ratelimit connections
			rateLimiter.Wait()

			if progressCallback != nil {
				imgProgress := progress + (float64(imgIdx) / float64(len(imgURLs)) / float64(totalChapters))
				progressCallback(fmt.Sprintf("Chapter %d/%d: Downloading image %d/%d", currentChapter, totalChapters, imgIdx+1, len(imgURLs)), imgProgress, currentChapter, totalChapters)
			}

			log.Printf("[%s:%s] Downloading image %d/%d: %s", manga.Shortname, cbzName, imgIdx+1, len(imgURLs), imgURL)
			err := parser.DownloadAndConvertToJPG(imgURL, chapterDir)
			if err != nil {
				log.Printf("[%s:%s] ⚠️ Failed to download/convert image %s: %v", manga.Shortname, cbzName, imgURL, err)
			} else {
				successCount++
				log.Printf("[%s:%s] ✓ Successfully downloaded and converted image %d/%d", manga.Shortname, cbzName, imgIdx+1, len(imgURLs))
			}
		}

		log.Printf("[%s:%s] Download complete: %d/%d images successful", manga.Shortname, cbzName, successCount, len(imgURLs))

		// Only create CBZ if we got at least some images
		if successCount == 0 {
			log.Printf("[%s:%s] ⚠️ Skipping CBZ creation - no images downloaded", manga.Shortname, cbzName)
			os.RemoveAll(chapterDir)
			continue
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
			log.Printf("[%s] ✓ Created CBZ: %s (%d images)\n", manga.Title, cbzName, successCount)
		}

		// Remove temp directory
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

// retrieve mgeko chapter list
func chapterUrls(url string) ([]string, error) {
	var chapters []string

	c := colly.NewCollector(
		colly.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"),
		// IMPORTANT: Allow URL revisiting in case of redirects
		colly.AllowURLRevisit(),
	)

	// Check for stored cf data
	parsedURL, _ := url2.Parse(url)
	domain := parsedURL.Hostname()

	bypassData, err := cf.LoadFromFile(domain)
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
			if err := cf.ApplyToCollector(c, url); err != nil {
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
	var cfInfo *cf.CfInfo
	var scrapeErr error

	c.OnResponse(func(r *colly.Response) {
		// Automatically decompress the response (handles gzip and Brotli)
		if decompressed, err := cf.DecompressResponse(r, "<mgeko>"); err != nil {
			log.Printf("<mgeko> ERROR: Failed to decompress response: %v", err)
			return
		} else if decompressed {
			log.Printf("<mgeko> Response successfully decompressed")
		}

		log.Printf("<mgeko> Chapter list response: status=%d, size=%d bytes", r.StatusCode, len(r.Body))

		isCF, info, _ := cf.DetectFromColly(r)
		if isCF {
			cfDetected = true
			cfInfo = info
			log.Printf("<mgeko> ⚠️ cf challenge detected despite using stored cookie!")
			log.Printf("<mgeko> Indicators that triggered detection: %v", info.Indicators)
			log.Printf("<mgeko> StatusCode: %d", info.StatusCode)
			log.Printf("<mgeko> RayID: %s", info.RayID)
			log.Printf("<mgeko> MetaRedirect: %s", info.MetaRedirect)
			log.Printf("<mgeko> FormAction: %s", info.FormAction)
			log.Printf("<mgeko> IsBIC: %v", info.IsBIC)
			log.Printf("<mgeko> Turnstile: %v", info.Turnstile)
		}

		// DEBUG: Check first 500 chars of body
		bodyPreview := string(r.Body)
		if len(bodyPreview) > 500 {
			bodyPreview = bodyPreview[:500]
		}
		log.Printf("<mgeko> DEBUG: Body preview: %q", bodyPreview)
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

		isCF, info, _ := cf.DetectFromColly(r)
		if isCF {
			cfDetected = true
			cfInfo = info
			log.Printf("<mgeko> cf block detected: %v", info.Indicators)
		}
		scrapeErr = err
	})

	// Make the request
	visitErr := c.Visit(url)
	if visitErr != nil {
		log.Printf("<mgeko> Visit error: %v", visitErr)
	}

	// Handle cf detection
	if cfDetected {
		if hasStoredData {
			log.Printf("<mgeko> ⚠️ Stored cf_clearance failed validation - cookie is expired/invalid")
			log.Printf("<mgeko> Deleting invalid data and requesting fresh challenge")

			// Delete the invalid stored data
			cf.DeleteDomain(domain)
		}

		log.Printf("<mgeko> Opening browser for cf challenge...")
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

	// Log the final count
	log.Printf("<mgeko> Successfully scraped %d chapter URLs", len(chapters))
	// for key, value := range chapters {
	// 	log.Printf("Chapter: %d -> %s", key, value)
	// }

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
			log.Printf("<mgeko> Mapped: %s → %s", filename, url) // ADD THIS DEBUG LINE
		} else {
			log.Printf("<mgeko> WARNING: Could not parse chapter number from URL: %s", url) // ADD THIS TOO
		}
	}

	return chapterMap
}
