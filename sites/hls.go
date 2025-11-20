package sites

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"kansho/cf"
	"kansho/config"
	"kansho/parser"

	"github.com/gocolly/colly"
)

const (
	HLS_BASE_URL = "https://honeylemonsoda.xyz/"
	HLS_SITE     = "hls"
)

// HlsDownloadChapters downloads manga chapters from honeylemonsoda.xyz website
// This site is hardcoded for a specific manga and doesn't require URL/shortname
// progressCallback is called with status updates during download
// Parameters: status string, progress (0.0-1.0), actual chapter number, current download, total chapters
func HlsDownloadChapters(ctx context.Context, manga *config.Bookmarks, progressCallback func(string, float64, int, int, int)) error {
	// Step 1: Get all chapter URLs from the manga page
	chapterUrls, err := hlsChapterUrls()
	if err != nil {
		return err
	}

	log.Printf("<%s> Found %d total chapters on site", manga.Site, len(chapterUrls))

	// Step 2: Map chapter URLs to CBZ filenames
	chapterMap := hlsChapterMap(chapterUrls)
	log.Printf("<%s> Mapped %d chapters to filenames", manga.Site, len(chapterMap))

	// Step 3: Get already downloaded chapters
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

	// Step 6: Iterate over sorted chapter keys and download
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

		// Create collector and apply CF bypass
		c := colly.NewCollector(
			colly.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/114.0.0.0 Safari/537.36"),
		)

		log.Printf("[%s:%s] Applying cf bypass for chapter page", manga.Shortname, cbzName)

		if applyErr := cf.ApplyToCollector(c, chapterURL); applyErr != nil {
			log.Printf("[%s:%s] WARNING: Failed to apply bypass data: %v", manga.Shortname, cbzName, applyErr)
		} else {
			log.Printf("[%s:%s] ✓ cf bypass applied to chapter collector", manga.Shortname, cbzName)
		}

		// Scrape images from the chapter page (robust selectors)
		var imgURLs []string
		c.OnHTML("div#content img, div.reading-content img", func(e *colly.HTMLElement) {
			// Try data-src first, then src
			src := e.Attr("data-src")
			if src == "" {
				src = e.Attr("src")
			}
			if src != "" {
				imgURLs = append(imgURLs, strings.TrimSpace(src))
				log.Printf("[%s:%s] Found image URL: %s", manga.Shortname, cbzName, src)
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
		})

		c.OnResponse(func(r *colly.Response) {
			if decompressed, err := cf.DecompressResponse(r, fmt.Sprintf("[%s]", cbzName)); err != nil {
				log.Printf("[%s:%s] ERROR: Failed to decompress: %v", manga.Shortname, cbzName, err)
				return
			} else if decompressed {
				log.Printf("[%s:%s] ✓ Chapter page decompressed", manga.Shortname, cbzName)
			}

			log.Printf("[%s:%s] Chapter page response: status=%d, size=%d bytes",
				manga.Shortname, cbzName, r.StatusCode, len(r.Body))
		})

		err = c.Visit(chapterURL)
		if err != nil {
			log.Printf("[%s:%s] Failed to visit %s: %v", manga.Shortname, cbzName, chapterURL, err)
			continue
		}

		if len(imgURLs) == 0 {
			log.Printf("[%s:%s] ⚠️ WARNING: No images found for chapter", manga.Shortname, cbzName)
			continue
		}

		log.Printf("[%s:%s] Found %d images to download", manga.Shortname, cbzName, len(imgURLs))

		// Create temp directory for this chapter
		chapterDir := filepath.Join("/tmp", manga.Shortname, strings.TrimSuffix(cbzName, ".cbz"))
		err = os.MkdirAll(chapterDir, 0755)
		if err != nil {
			log.Printf("[%s:%s] Failed to create temporary directory %s: %v", manga.Shortname, cbzName, chapterDir, err)
			continue
		}

		successCount := 0
		rateLimiter := parser.NewRateLimiter(1500 * time.Millisecond)
		defer rateLimiter.Stop()

		// Download and convert images
		for imgIdx, imgURL := range imgURLs {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			rateLimiter.Wait()

			if progressCallback != nil {
				imgProgress := progress + (float64(imgIdx) / float64(len(imgURLs)) / float64(newChaptersToDownload))
				progressCallback(
					fmt.Sprintf("Chapter %d/%d: Downloading image %d/%d", actualChapterNum, totalChaptersFound, imgIdx+1, len(imgURLs)),
					imgProgress,
					actualChapterNum,
					currentDownload,
					totalChaptersFound,
				)
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

		if successCount == 0 {
			log.Printf("[%s:%s] ⚠️ Skipping CBZ creation - no images downloaded", manga.Shortname, cbzName)
			os.RemoveAll(chapterDir)
			continue
		}

		// Create CBZ file
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

		// Clean up temp directory
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

// hlsChapterUrls retrieves all chapter URLs from honeylemonsoda.xyz
// Implements Cloudflare bypass detection and handling
func hlsChapterUrls() ([]string, error) {
	var chapterLinks []string

	c := colly.NewCollector(
		colly.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"),
		colly.AllowURLRevisit(),
	)

	// Check for stored CF data
	parsedURL, _ := url.Parse(HLS_BASE_URL)
	domain := parsedURL.Hostname()

	bypassData, err := cf.LoadFromFile(domain)
	hasStoredData := (err == nil)

	if hasStoredData {
		log.Printf("<hls> Found stored bypass data for %s (type: %s)", domain, bypassData.Type)

		// Check if cf_clearance exists
		if bypassData.CfClearanceStruct != nil {
			log.Printf("<hls> cf_clearance found, expires: %v", bypassData.CfClearanceStruct.Expires)

			// Check expiration
			if bypassData.CfClearanceStruct.Expires != nil && time.Now().After(*bypassData.CfClearanceStruct.Expires) {
				log.Printf("<hls> ⚠️ cf_clearance has EXPIRED!")
				hasStoredData = false
			}
		}

		if hasStoredData {
			// Apply the stored data
			if err := cf.ApplyToCollector(c, HLS_BASE_URL); err != nil {
				log.Printf("<hls> Failed to apply bypass data: %v", err)
				hasStoredData = false
			} else {
				log.Printf("<hls> ✓ Applied stored cf_clearance cookie")
			}
		}
	} else {
		log.Printf("<hls> No stored bypass data found for %s", domain)
	}

	var cfDetected bool
	var cfInfo *cf.CfInfo
	var scrapeErr error

	c.OnResponse(func(r *colly.Response) {
		// Automatically decompress the response (handles gzip and Brotli)
		if decompressed, err := cf.DecompressResponse(r, "<hls>"); err != nil {
			log.Printf("<hls> ERROR: Failed to decompress response: %v", err)
			return
		} else if decompressed {
			log.Printf("<hls> Response successfully decompressed")
		}

		log.Printf("<hls> Chapter list response: status=%d, size=%d bytes", r.StatusCode, len(r.Body))

		isCF, info, _ := cf.DetectFromColly(r)
		if isCF {
			cfDetected = true
			cfInfo = info
			log.Printf("<hls> ⚠️ cf challenge detected despite using stored cookie!")
			log.Printf("<hls> Indicators that triggered detection: %v", info.Indicators)
		}
	})

	// Select all chapter links
	c.OnHTML("li.item a", func(e *colly.HTMLElement) {
		link := e.Attr("href")
		if link != "" {
			chapterLinks = append(chapterLinks, link)
		}
	})

	c.OnError(func(r *colly.Response, err error) {
		log.Printf("<hls> ERROR: %v, Status: %d", err, r.StatusCode)

		isCF, info, _ := cf.DetectFromColly(r)
		if isCF {
			cfDetected = true
			cfInfo = info
			log.Printf("<hls> cf block detected: %v", info.Indicators)
		}
		scrapeErr = err
	})

	c.OnRequest(func(r *colly.Request) {
		log.Printf("<hls> Visiting: %s", r.URL.String())
	})

	// Make the request
	visitErr := c.Visit(HLS_BASE_URL)
	if visitErr != nil {
		log.Printf("<hls> Visit error: %v", visitErr)
	}

	// Handle CF detection
	if cfDetected {
		if hasStoredData {
			log.Printf("<hls> ⚠️ Stored cf_clearance failed validation - cookie is expired/invalid")
			log.Printf("<hls> Deleting invalid data and requesting fresh challenge")

			// Delete the invalid stored data
			cf.DeleteDomain(domain)
		}

		log.Printf("<hls> Opening browser for cf challenge...")
		challengeURL := cf.GetChallengeURL(cfInfo, HLS_BASE_URL)

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

	log.Printf("<hls> Successfully scraped %d chapter URLs", len(chapterLinks))

	return chapterLinks, nil
}

// hlsChapterMap takes a slice of chapter URLs and returns a map:
// key = normalized filename (ch###.cbz), value = URL
// Extracts chapter number from URL pattern like "/chapter-18/" or "/chapter-18-5/"
func hlsChapterMap(urls []string) map[string]string {
	chapterMap := make(map[string]string)

	for _, chapterURL := range urls {
		// Trim trailing slash and split on "-"
		parts := strings.Split(strings.TrimRight(chapterURL, "/"), "-")
		if len(parts) == 0 {
			log.Printf("<hls> WARNING: Could not parse chapter number from URL: %s", chapterURL)
			continue
		}

		// Last part is the chapter number
		chapterNum := parts[len(parts)-1]

		// Pad to 3 digits and create filename
		filename := fmt.Sprintf("ch%03s.cbz", chapterNum)

		chapterMap[filename] = chapterURL
		log.Printf("<hls> Mapped: %s → %s", filename, chapterURL)
	}

	return chapterMap
}
