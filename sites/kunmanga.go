package sites

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
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

	"github.com/chai2010/webp"
	"github.com/gocolly/colly"
	"golang.org/x/net/publicsuffix"
)

// KunmangaDownloadChapters downloads manga chapters from kunmanga website
// progressCallback is called with status updates during download
// Parameters: status string, progress (0.0-1.0), actual chapter number, current download, total chapters
func KunmangaDownloadChapters(ctx context.Context, manga *config.Bookmarks, progressCallback func(string, float64, int, int, int)) error {
	// Step 1: Get all chapter URLs from the manga page
	chapterUrls, err := kunmangaChapterUrls(manga.Url)
	if err != nil {
		return err
	}

	log.Printf("<%s> Found %d total chapters on site", manga.Site, len(chapterUrls))

	// Step 2: Map chapter URLs to CBZ filenames
	chapterMap := kunmangaChapterMap(chapterUrls)
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

	// Step 5: Create HTTP client with CF cookies for image downloads
	httpClient, err := createKunmangaHTTPClient(manga.Url)
	if err != nil {
		log.Printf("<%s> WARNING: Failed to create HTTP client with CF cookies: %v", manga.Site, err)
		log.Printf("<%s> Image downloads may fail with 403 errors", manga.Site)
		// Continue anyway with a basic client
		httpClient = http.DefaultClient
	}

	// Step 6: Sort chapter keys
	sortedChapters, sortError := parser.SortKeys(chapterMap)
	if sortError != nil {
		return fmt.Errorf("failed to sort chapter map keys: %v", sortError)
	}

	// Step 7: Iterate over sorted chapter keys and download
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

		// Scrape images from the chapter page
		var imgURLs []string
		c.OnHTML("div.reading-content img", func(e *colly.HTMLElement) {
			src := e.Attr("src")
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

		// Download and convert images with CF-enabled client
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

			// Use CF-enabled HTTP client
			outputFilename := fmt.Sprintf("%d.jpg", imgIdx+1)
			err := downloadKunmangaImage(httpClient, imgURL, chapterDir, outputFilename, chapterURL)
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

// createKunmangaHTTPClient creates an HTTP client with CF cookies loaded
// This client can be used to download images that require CF bypass
func createKunmangaHTTPClient(mangaURL string) (*http.Client, error) {
	// Parse URL to get domain
	parsedURL, err := url.Parse(mangaURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}
	domain := parsedURL.Hostname()

	// Load CF bypass data
	bypassData, err := cf.LoadFromFile(domain)
	if err != nil {
		return nil, fmt.Errorf("failed to load CF bypass data: %w", err)
	}

	// Create cookie jar
	jar, err := cookiejar.New(&cookiejar.Options{
		PublicSuffixList: publicsuffix.List,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create cookie jar: %w", err)
	}

	// Prepare cookies to add to jar
	var cookies []*http.Cookie

	// Add cf_clearance cookie if available
	if bypassData.CfClearanceStruct != nil {
		cookie := &http.Cookie{
			Name:     bypassData.CfClearanceStruct.Name,
			Value:    bypassData.CfClearanceStruct.Value,
			Domain:   bypassData.CfClearanceStruct.Domain,
			Path:     bypassData.CfClearanceStruct.Path,
			Expires:  time.Time{}, // Let it be session cookie
			HttpOnly: bypassData.CfClearanceStruct.HttpOnly,
			Secure:   bypassData.CfClearanceStruct.Secure,
			SameSite: http.SameSiteNoneMode,
		}
		cookies = append(cookies, cookie)
		log.Printf("✓ Added cf_clearance cookie to HTTP client (value: %.20s...)", cookie.Value)
	}

	// Add any additional cookies from bypass data
	if len(bypassData.AllCookies) > 0 {
		for _, c := range bypassData.AllCookies {
			if c.Name != "" && c.Name != "cf_clearance" {
				cookie := &http.Cookie{
					Name:     c.Name,
					Value:    c.Value,
					Domain:   c.Domain,
					Path:     c.Path,
					HttpOnly: c.HTTPOnly,
					Secure:   c.Secure,
				}
				cookies = append(cookies, cookie)
				log.Printf("✓ Added additional cookie: %s", c.Name)
			}
		}
	}

	// Set cookies for both main domain and image subdomain
	mainURL, _ := url.Parse("https://" + domain)
	imgURL, _ := url.Parse("https://img-1." + domain)

	jar.SetCookies(mainURL, cookies)
	jar.SetCookies(imgURL, cookies)

	log.Printf("✓ Set %d cookies for %s and img-1.%s", len(cookies), domain, domain)

	// Create HTTP client with the jar
	client := &http.Client{
		Jar:     jar,
		Timeout: 30 * time.Second,
	}

	return client, nil
}

// kunmangaChapterUrls retrieves all chapter URLs from a kunmanga manga page
// Implements Cloudflare bypass detection and handling
func kunmangaChapterUrls(mangaURL string) ([]string, error) {
	var chapterLinks []string

	c := colly.NewCollector(
		colly.AllowedDomains("kunmanga.com"),
		colly.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"),
		colly.AllowURLRevisit(),
	)

	// Check for stored CF data
	parsedURL, _ := url.Parse(mangaURL)
	domain := parsedURL.Hostname()

	bypassData, err := cf.LoadFromFile(domain)
	hasStoredData := (err == nil)

	if hasStoredData {
		log.Printf("<kunmanga> Found stored bypass data for %s (type: %s)", domain, bypassData.Type)

		// Check if cf_clearance exists
		if bypassData.CfClearanceStruct != nil {
			log.Printf("<kunmanga> cf_clearance found, expires: %v", bypassData.CfClearanceStruct.Expires)

			// Check expiration
			if bypassData.CfClearanceStruct.Expires != nil && time.Now().After(*bypassData.CfClearanceStruct.Expires) {
				log.Printf("<kunmanga> ⚠️ cf_clearance has EXPIRED!")
				hasStoredData = false
			}
		}

		if hasStoredData {
			// Apply the stored data
			if err := cf.ApplyToCollector(c, mangaURL); err != nil {
				log.Printf("<kunmanga> Failed to apply bypass data: %v", err)
				hasStoredData = false
			} else {
				log.Printf("<kunmanga> ✓ Applied stored cf_clearance cookie")
			}
		}
	} else {
		log.Printf("<kunmanga> No stored bypass data found for %s", domain)
	}

	var cfDetected bool
	var cfInfo *cf.CfInfo
	var scrapeErr error

	c.OnResponse(func(r *colly.Response) {
		// Automatically decompress the response (handles gzip and Brotli)
		if decompressed, err := cf.DecompressResponse(r, "<kunmanga>"); err != nil {
			log.Printf("<kunmanga> ERROR: Failed to decompress response: %v", err)
			return
		} else if decompressed {
			log.Printf("<kunmanga> Response successfully decompressed")
		}

		log.Printf("<kunmanga> Chapter list response: status=%d, size=%d bytes", r.StatusCode, len(r.Body))

		isCF, info, _ := cf.DetectFromColly(r)
		if isCF {
			cfDetected = true
			cfInfo = info
			log.Printf("<kunmanga> ⚠️ cf challenge detected despite using stored cookie!")
			log.Printf("<kunmanga> Indicators that triggered detection: %v", info.Indicators)
		}
	})

	// Select all chapter links
	c.OnHTML("ul.main.version-chap li.wp-manga-chapter > a", func(e *colly.HTMLElement) {
		link := e.Attr("href")
		if link != "" {
			chapterLinks = append(chapterLinks, link)
		}
	})

	c.OnError(func(r *colly.Response, err error) {
		log.Printf("<kunmanga> ERROR: %v, Status: %d", err, r.StatusCode)

		isCF, info, _ := cf.DetectFromColly(r)
		if isCF {
			cfDetected = true
			cfInfo = info
			log.Printf("<kunmanga> cf block detected: %v", info.Indicators)
		}
		scrapeErr = err
	})

	c.OnRequest(func(r *colly.Request) {
		log.Printf("<kunmanga> Visiting: %s", r.URL.String())
	})

	// Make the request
	visitErr := c.Visit(mangaURL)
	if visitErr != nil {
		log.Printf("<kunmanga> Visit error: %v", visitErr)
	}

	// Handle CF detection
	if cfDetected {
		if hasStoredData {
			log.Printf("<kunmanga> ⚠️ Stored cf_clearance failed validation - cookie is expired/invalid")
			log.Printf("<kunmanga> Deleting invalid data and requesting fresh challenge")

			// Delete the invalid stored data
			cf.DeleteDomain(domain)
		}

		log.Printf("<kunmanga> Opening browser for cf challenge...")
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

	log.Printf("<kunmanga> Successfully scraped %d chapter URLs", len(chapterLinks))

	return chapterLinks, nil
}

// kunmangaChapterMap takes a slice of chapter URLs and returns a map:
// key = normalized filename (ch###.cbz or ch###.part.cbz), value = URL
func kunmangaChapterMap(urls []string) map[string]string {
	chapterMap := make(map[string]string)

	// Regex: match chapter number and optional part numbers
	// Handles URLs like: /chapter-18/ or /chapter-18-5/
	re := regexp.MustCompile(`chapter[-_\.]?(\d+)((?:[-_\.]\d+)*)`)

	for _, chapterURL := range urls {
		matches := re.FindStringSubmatch(chapterURL)
		if len(matches) > 0 {
			mainNum := matches[1] // main chapter number
			partStr := matches[2] // optional part string, e.g., "-5" or ".5"

			// Normalize separators: replace - or _ with .
			normalizedPart := strings.ReplaceAll(partStr, "-", ".")
			normalizedPart = strings.ReplaceAll(normalizedPart, "_", ".")

			// Remove leading dot (if any)
			normalizedPart = strings.TrimPrefix(normalizedPart, ".")

			// Final filename: pad main number to 3 digits
			filename := fmt.Sprintf("ch%03s", mainNum)
			if normalizedPart != "" {
				filename += "." + normalizedPart
			}
			filename += ".cbz"

			chapterMap[filename] = chapterURL
			log.Printf("<kunmanga> Mapped: %s → %s", filename, chapterURL)
		} else {
			log.Printf("<kunmanga> WARNING: Could not parse chapter number from URL: %s", chapterURL)
		}
	}

	return chapterMap
}

// downloadKunmangaImage downloads an image from kunmanga with proper headers and CF cookies
// Required headers include User-Agent and Referer to avoid 403 errors
func downloadKunmangaImage(client *http.Client, imgURL, targetDir, filename, referer string) error {
	req, err := http.NewRequest("GET", imgURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Required headers to avoid 403
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36")
	req.Header.Set("Referer", referer)
	req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-GB,en;q=0.9")
	req.Header.Set("Sec-Fetch-Dest", "image")
	req.Header.Set("Sec-Fetch-Mode", "no-cors")
	req.Header.Set("Sec-Fetch-Site", "same-site")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	// Read image data
	imgBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read image data: %w", err)
	}

	// Detect format
	format, err := parser.DetectImageFormat(imgBytes)
	if err != nil {
		return fmt.Errorf("failed to detect image format: %w", err)
	}

	// Pad filename to 3 digits
	paddedFilename := padImageFilename(filename)
	outputPath := filepath.Join(targetDir, paddedFilename)

	// If already JPEG, save directly
	if format == "jpeg" {
		err = os.WriteFile(outputPath, imgBytes, 0644)
		if err != nil {
			return fmt.Errorf("failed to save jpeg image: %w", err)
		}
		return nil
	}

	// Decode and convert to JPEG
	var img image.Image
	switch format {
	case "png", "gif":
		img, _, err = image.Decode(bytes.NewReader(imgBytes))
		if err != nil {
			return fmt.Errorf("failed to decode image: %w", err)
		}
	case "webp":
		img, err = webp.Decode(bytes.NewReader(imgBytes))
		if err != nil {
			return fmt.Errorf("failed to decode webp image: %w", err)
		}
	default:
		return fmt.Errorf("unsupported image format: %s", format)
	}

	// Save as JPEG
	outFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer outFile.Close()

	opts := jpeg.Options{Quality: 90}
	err = jpeg.Encode(outFile, img, &opts)
	if err != nil {
		return fmt.Errorf("failed to encode jpeg: %w", err)
	}

	return nil
}

// padImageFilename pads numeric filenames to 3 digits
// Example: "1.jpg" -> "001.jpg", "25.jpg" -> "025.jpg"
func padImageFilename(filename string) string {
	// Split into name and extension
	ext := filepath.Ext(filename)
	name := strings.TrimSuffix(filename, ext)

	// Try to parse as number
	num, err := strconv.Atoi(name)
	if err != nil {
		// If not a number, return as-is
		return filename
	}

	// Pad to 3 digits
	return fmt.Sprintf("%03d%s", num, ext)
}
