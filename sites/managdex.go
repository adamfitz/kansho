package sites

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
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
	mangadexAPIBase = "https://api.mangadex.org"
	//mangadexCDN     = "https://uploads.mangadex.org"
)

// MangaDex API response structures
type MangaDexChapterList struct {
	Result   string            `json:"result"`
	Response string            `json:"response"`
	Data     []MangaDexChapter `json:"data"`
	Limit    int               `json:"limit"`
	Offset   int               `json:"offset"`
	Total    int               `json:"total"`
}

type MangaDexChapter struct {
	ID         string                    `json:"id"`
	Type       string                    `json:"type"`
	Attributes MangaDexChapterAttributes `json:"attributes"`
}

type MangaDexChapterAttributes struct {
	Volume             *string `json:"volume"`
	Chapter            *string `json:"chapter"`
	Title              string  `json:"title"`
	TranslatedLanguage string  `json:"translatedLanguage"`
	Pages              int     `json:"pages"`
}

type MangaDexAtHomeResponse struct {
	Result  string                    `json:"result"`
	BaseUrl string                    `json:"baseUrl"`
	Chapter MangaDexAtHomeChapterData `json:"chapter"`
}

type MangaDexAtHomeChapterData struct {
	Hash      string   `json:"hash"`
	Data      []string `json:"data"`
	DataSaver []string `json:"dataSaver"`
}

// MangadexDownloadChapters downloads manga chapters from MangaDex
func MangadexDownloadChapters(ctx context.Context, manga *config.Bookmarks, progressCallback func(string, float64, int, int, int)) error {
	// Step 1: Extract manga ID from URL
	mangaID, err := extractMangaDexID(manga.Url)
	if err != nil {
		return fmt.Errorf("failed to extract manga ID: %v", err)
	}

	log.Printf("<%s> Extracted manga ID: %s", manga.Site, mangaID)

	// Step 2: Get all chapters from API (with pagination)
	allChapters, err := mangadexGetAllChapters(mangaID)
	if err != nil {
		return err
	}

	log.Printf("<%s> Found %d total chapters on site", manga.Site, len(allChapters))

	// Step 3: Create chapter map (filename -> chapter ID)
	chapterMap := mangadexChapterMap(allChapters)
	log.Printf("<%s> Mapped %d chapters to filenames", manga.Site, len(chapterMap))

	// Step 4: Get already downloaded chapters
	downloadedChapters, err := parser.LocalChapterList(manga.Location)
	if err != nil {
		return fmt.Errorf("failed to list files in %s: %v", manga.Location, err)
	}
	log.Printf("<%s> Found %d already downloaded chapters", manga.Site, len(downloadedChapters))

	// Store total chapters BEFORE filtering
	totalChaptersFound := len(chapterMap)

	// Step 5: Remove already-downloaded chapters
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

		chapterID := chapterMap[cbzName]
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

		log.Printf("[%s:%s] Starting download, chapter ID: %s", manga.Site, cbzName, chapterID)

		// Get image URLs from MangaDex@Home
		imageURLs, err := mangadexGetChapterImages(chapterID)
		if err != nil {
			log.Printf("[%s:%s] Failed to get chapter images: %v", manga.Site, cbzName, err)
			continue
		}

		if len(imageURLs) == 0 {
			log.Printf("[%s:%s] ⚠️ WARNING: No images found for chapter", manga.Site, cbzName)
			continue
		}

		log.Printf("[%s:%s] Found %d images to download", manga.Site, cbzName, len(imageURLs))

		// Create temp directory
		chapterDir := filepath.Join("/tmp", manga.Site, strings.TrimSuffix(cbzName, ".cbz"))
		err = os.MkdirAll(chapterDir, 0755)
		if err != nil {
			log.Printf("[%s:%s] Failed to create temporary directory %s: %v", manga.Site, cbzName, chapterDir, err)
			continue
		}

		successCount := 0
		rateLimiter := parser.NewRateLimiter(500 * time.Millisecond) // MangaDex has generous rate limits
		defer rateLimiter.Stop()

		// Download images
		for imgIdx, imgURL := range imageURLs {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			rateLimiter.Wait()

			if progressCallback != nil {
				imgProgress := progress + (float64(imgIdx) / float64(len(imageURLs)) / float64(newChaptersToDownload))
				progressCallback(
					fmt.Sprintf("Chapter %d/%d: Downloading image %d/%d", actualChapterNum, totalChaptersFound, imgIdx+1, len(imageURLs)),
					imgProgress,
					actualChapterNum,
					currentDownload,
					totalChaptersFound,
				)
			}

			log.Printf("[%s:%s] Downloading image %d/%d: %s", manga.Site, cbzName, imgIdx+1, len(imageURLs), imgURL)

			// Use custom filename format: 001.jpg, 002.jpg, etc.
			filename := fmt.Sprintf("%03d", imgIdx)
			err := parser.DownloadConvertToJPGRename(filename, imgURL, chapterDir)
			if err != nil {
				log.Printf("[%s:%s] ⚠️ Failed to download/convert image %s: %v", manga.Site, cbzName, imgURL, err)
			} else {
				successCount++
				log.Printf("[%s:%s] ✓ Successfully downloaded and converted image %d/%d", manga.Site, cbzName, imgIdx+1, len(imageURLs))
			}
		}

		log.Printf("[%s:%s] Download complete: %d/%d images successful", manga.Site, cbzName, successCount, len(imageURLs))

		if successCount == 0 {
			log.Printf("[%s:%s] ⚠️ Skipping CBZ creation - no images downloaded", manga.Site, cbzName)
			os.RemoveAll(chapterDir)
			continue
		}

		// Create CBZ
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
			log.Printf("[%s:%s] Failed to create CBZ %s: %v", manga.Site, cbzName, cbzPath, err)
		} else {
			log.Printf("[%s] ✓ Created CBZ: %s (%d images)\n", manga.Title, cbzName, successCount)
		}

		err = os.RemoveAll(chapterDir)
		if err != nil {
			log.Printf("[%s:%s] Failed to remove temp directory %s: %v", manga.Site, cbzName, chapterDir, err)
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

// extractMangaDexID extracts the manga ID from a MangaDex URL
// Supports formats like:
// - https://mangadex.org/title/MANGA_ID/title-name
// - https://mangadex.org/title/MANGA_ID
func extractMangaDexID(mangaURL string) (string, error) {
	parsedURL, err := url.Parse(mangaURL)
	if err != nil {
		return "", err
	}

	// Split path into segments
	segments := strings.Split(strings.Trim(parsedURL.Path, "/"), "/")

	// Look for "title" followed by the ID
	for i, segment := range segments {
		if segment == "title" && i+1 < len(segments) {
			return segments[i+1], nil
		}
	}

	return "", fmt.Errorf("could not extract manga ID from URL: %s", mangaURL)
}

// mangadexGetAllChapters retrieves all chapters for a manga with pagination
func mangadexGetAllChapters(mangaID string) ([]MangaDexChapter, error) {
	var allChapters []MangaDexChapter
	offset := 0
	limit := 100 // MangaDex allows up to 100 per request

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	for {
		// Build API URL with pagination and filters
		apiURL := fmt.Sprintf("%s/manga/%s/feed?limit=%d&offset=%d&translatedLanguage[]=en&order[chapter]=asc&contentRating[]=safe&contentRating[]=suggestive&contentRating[]=erotica",
			mangadexAPIBase, mangaID, limit, offset)

		log.Printf("<mangadex> Fetching chapters: offset=%d, limit=%d", offset, limit)

		req, err := http.NewRequest("GET", apiURL, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %v", err)
		}

		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch chapters: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
		}

		var chapterList MangaDexChapterList
		if err := json.NewDecoder(resp.Body).Decode(&chapterList); err != nil {
			return nil, fmt.Errorf("failed to decode response: %v", err)
		}

		log.Printf("<mangadex> Retrieved %d chapters (total: %d)", len(chapterList.Data), chapterList.Total)

		allChapters = append(allChapters, chapterList.Data...)

		// Check if we've retrieved all chapters
		if len(allChapters) >= chapterList.Total {
			break
		}

		offset += limit

		// Rate limiting - be nice to MangaDex API
		time.Sleep(250 * time.Millisecond)
	}

	log.Printf("<mangadex> Successfully retrieved %d total chapters", len(allChapters))
	return allChapters, nil
}

// mangadexChapterMap creates a map of filename -> chapter ID
func mangadexChapterMap(chapters []MangaDexChapter) map[string]string {
	chapterMap := make(map[string]string)

	for _, chapter := range chapters {
		if chapter.Attributes.Chapter == nil {
			log.Printf("<mangadex> WARNING: Chapter has no number, skipping (ID: %s)", chapter.ID)
			continue
		}

		chapterNum := *chapter.Attributes.Chapter

		// Parse chapter number - handle decimals like "91.5" or "91.2"
		filename := formatChapterFilename(chapterNum)

		// Check for duplicates (multiple groups translating same chapter)
		if existingID, exists := chapterMap[filename]; exists {
			log.Printf("<mangadex> WARNING: Duplicate chapter %s found (existing: %s, new: %s) - keeping first",
				filename, existingID, chapter.ID)
			continue
		}

		chapterMap[filename] = chapter.ID
		log.Printf("<mangadex> Mapped: %s → %s", filename, chapter.ID)
	}

	return chapterMap
}

// formatChapterFilename formats a chapter number string into a filename
// Examples: "1" -> "ch001.cbz", "91.5" -> "ch091.5.cbz", "100.2" -> "ch100.2.cbz"
func formatChapterFilename(chapterNum string) string {
	// Split on decimal point
	parts := strings.Split(chapterNum, ".")

	// Parse main chapter number
	mainNum := parts[0]

	// Pad main number to 3 digits
	filename := fmt.Sprintf("ch%03s", mainNum)

	// Add decimal part if it exists
	if len(parts) > 1 {
		filename += "." + parts[1]
	}

	filename += ".cbz"
	return filename
}

// mangadexGetChapterImages retrieves image URLs for a specific chapter
func mangadexGetChapterImages(chapterID string) ([]string, error) {
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Get the @Home server URL and image list
	apiURL := fmt.Sprintf("%s/at-home/server/%s", mangadexAPIBase, chapterID)

	log.Printf("<mangadex> Fetching image list for chapter: %s", chapterID)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch @Home data: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("@Home API returned status %d: %s", resp.StatusCode, string(body))
	}

	var atHomeResp MangaDexAtHomeResponse
	if err := json.NewDecoder(resp.Body).Decode(&atHomeResp); err != nil {
		return nil, fmt.Errorf("failed to decode @Home response: %v", err)
	}

	// Build full image URLs
	var imageURLs []string
	for _, filename := range atHomeResp.Chapter.Data {
		imageURL := fmt.Sprintf("%s/data/%s/%s", atHomeResp.BaseUrl, atHomeResp.Chapter.Hash, filename)
		imageURLs = append(imageURLs, imageURL)
	}

	log.Printf("<mangadex> Found %d images for chapter %s", len(imageURLs), chapterID)
	return imageURLs, nil
}

// mangadexCheckCloudflare checks if Cloudflare protection is active (future-proofing)
// This follows the same pattern as mgeko.go but MangaDex API typically doesn't use CF
func mangadexCheckCloudflare(apiURL string) error {
	c := colly.NewCollector(
		colly.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"),
		colly.AllowURLRevisit(),
	)

	// Apply CF bypass if data exists
	parsedURL, _ := url.Parse(apiURL)
	domain := parsedURL.Hostname()

	bypassData, err := cf.LoadFromFile(domain)
	hasStoredData := (err == nil)

	if hasStoredData {
		log.Printf("<mangadex> Found stored bypass data for %s", domain)

		if bypassData.CfClearanceStruct != nil {
			if bypassData.CfClearanceStruct.Expires != nil && time.Now().After(*bypassData.CfClearanceStruct.Expires) {
				log.Printf("<mangadex> ⚠️ cf_clearance has EXPIRED!")
				hasStoredData = false
			}
		}

		if hasStoredData {
			if err := cf.ApplyToCollector(c, apiURL); err != nil {
				log.Printf("<mangadex> Failed to apply bypass data: %v", err)
			} else {
				log.Printf("<mangadex> ✓ Applied stored cf_clearance cookie")
			}
		}
	}

	var cfDetected bool
	var cfInfo *cf.CfInfo

	c.OnResponse(func(r *colly.Response) {
		if decompressed, err := cf.DecompressResponse(r, "<mangadex>"); err != nil {
			log.Printf("<mangadex> ERROR: Failed to decompress response: %v", err)
		} else if decompressed {
			log.Printf("<mangadex> Response successfully decompressed")
		}

		isCF, info, _ := cf.DetectFromColly(r)
		if isCF {
			cfDetected = true
			cfInfo = info
			log.Printf("<mangadex> ⚠️ Cloudflare challenge detected!")
		}
	})

	c.OnError(func(r *colly.Response, err error) {
		isCF, info, _ := cf.DetectFromColly(r)
		if isCF {
			cfDetected = true
			cfInfo = info
			log.Printf("<mangadex> Cloudflare block detected: %v", info.Indicators)
		}
	})

	// Visit to test
	if err := c.Visit(apiURL); err != nil {
		return err
	}

	if cfDetected {
		if hasStoredData {
			cf.DeleteDomain(domain)
		}

		challengeURL := cf.GetChallengeURL(cfInfo, apiURL)
		if err := cf.OpenInBrowser(challengeURL); err != nil {
			return fmt.Errorf("cloudflare detected but failed to open browser: %w", err)
		}

		return &cf.CfChallengeError{
			URL:        challengeURL,
			StatusCode: cfInfo.StatusCode,
			Indicators: cfInfo.Indicators,
		}
	}

	return nil
}
