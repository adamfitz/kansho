package sites

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"kansho/config"
	"kansho/downloader"
)

const (
	mangadexAPIBase = "https://api.mangadex.org"
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

// MangadexSite implements the SitePlugin interface for MangaDex
type MangadexSite struct {
	mangaID string
}

// Ensure MangadexSite implements SitePlugin
var _ downloader.SitePlugin = (*MangadexSite)(nil)

// GetSiteName returns the site identifier
func (m *MangadexSite) GetSiteName() string {
	return "mangadex"
}

// GetDomain returns the site domain
func (m *MangadexSite) GetDomain() string {
	return "api.mangadex.org"
}

// NeedsCFBypass returns whether this site needs Cloudflare bypass
func (m *MangadexSite) NeedsCFBypass() bool {
	return false // MangaDex API doesn't use CF protection
}

// GetChapterExtractionMethod returns HOW to extract chapters
func (m *MangadexSite) GetChapterExtractionMethod() *downloader.ChapterExtractionMethod {
	return &downloader.ChapterExtractionMethod{
		Type: "api",
		APIFunc: func(baseURL string, client *downloader.APIClient) ([]map[string]string, error) {
			// Get all chapters from API with pagination
			allChapters, err := m.getAllChaptersAPI(client)
			if err != nil {
				return nil, err
			}

			log.Printf("<%s> Found %d total chapters on site", m.GetSiteName(), len(allChapters))

			// Convert to map format
			var chapters []map[string]string
			for _, chapter := range allChapters {
				if chapter.Attributes.Chapter == nil {
					log.Printf("<%s> WARNING: Chapter has no number, skipping (ID: %s)", m.GetSiteName(), chapter.ID)
					continue
				}

				// Skip chapters with 0 pages (deleted/unavailable)
				if chapter.Attributes.Pages == 0 {
					log.Printf("<%s> WARNING: Chapter %s has 0 pages, skipping (ID: %s)",
						m.GetSiteName(), *chapter.Attributes.Chapter, chapter.ID)
					continue
				}

				chapterNum := *chapter.Attributes.Chapter
				chapters = append(chapters, map[string]string{
					"num": chapterNum,
					"id":  chapter.ID,
					// Store the ID in the URL field so we can access it later
					"url": chapter.ID,
				})
			}

			return chapters, nil
		},
	}
}

// GetImageExtractionMethod returns HOW to extract images
func (m *MangadexSite) GetImageExtractionMethod() *downloader.ImageExtractionMethod {
	return &downloader.ImageExtractionMethod{
		Type: "api",
		APIFunc: func(chapterURL string, chapterData map[string]string, client *downloader.APIClient) ([]string, error) {
			// The chapterURL is actually the chapter ID stored earlier
			chapterID := chapterURL

			// Get image URLs from MangaDex@Home API
			imageURLs, err := m.getChapterImagesAPI(chapterID, client)
			if err != nil {
				return nil, fmt.Errorf("failed to get chapter images: %w", err)
			}

			return imageURLs, nil
		},
	}
}

// NormalizeChapterURL converts raw URL to absolute URL
// For MangaDex API, the "URL" is actually the chapter ID
func (m *MangadexSite) NormalizeChapterURL(rawURL, baseURL string) string {
	// Return the chapter ID as-is
	return rawURL
}

// NormalizeChapterFilename converts chapter data to filename
func (m *MangadexSite) NormalizeChapterFilename(data map[string]string) string {
	chapterNum := data["num"]

	// Parse chapter number - handle decimals like "91.5" or "91.2"
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

	log.Printf("[Mangadex] Normalized: %s → %s.cbz", chapterNum, filename)
	return filename + ".cbz"
}

// getAllChaptersAPI retrieves all chapters for a manga with pagination using APIClient
func (m *MangadexSite) getAllChaptersAPI(client *downloader.APIClient) ([]MangaDexChapter, error) {
	var allChapters []MangaDexChapter
	offset := 0
	limit := 100 // MangaDex allows up to 100 per request

	for {
		// Build API URL with pagination and filters
		apiURL := fmt.Sprintf("%s/manga/%s/feed?limit=%d&offset=%d&translatedLanguage[]=en&order[chapter]=asc&contentRating[]=safe&contentRating[]=suggestive&contentRating[]=erotica",
			mangadexAPIBase, m.mangaID, limit, offset)

		log.Printf("<mangadex> Fetching chapters: offset=%d, limit=%d", offset, limit)

		var chapterList MangaDexChapterList
		if err := client.FetchJSON(context.Background(), apiURL, &chapterList); err != nil {
			return nil, fmt.Errorf("failed to fetch chapters: %w", err)
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

// getChapterImagesAPI retrieves image URLs for a specific chapter using APIClient
func (m *MangadexSite) getChapterImagesAPI(chapterID string, client *downloader.APIClient) ([]string, error) {
	// Get the @Home server URL and image list
	apiURL := fmt.Sprintf("%s/at-home/server/%s", mangadexAPIBase, chapterID)

	log.Printf("<mangadex> Fetching image list for chapter: %s", chapterID)

	var atHomeResp MangaDexAtHomeResponse
	if err := client.FetchJSON(context.Background(), apiURL, &atHomeResp); err != nil {
		return nil, fmt.Errorf("failed to fetch @Home data: %w", err)
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

// MangadexDownloadChapters is the entry point called by the download queue
func MangadexDownloadChapters(ctx context.Context, manga *config.Bookmarks, progressCallback func(string, float64, int, int, int)) error {
	// Extract manga ID from URL
	mangaID, err := extractMangaDexID(manga.Url)
	if err != nil {
		return fmt.Errorf("failed to extract manga ID: %v", err)
	}

	log.Printf("<%s> Extracted manga ID: %s", manga.Site, mangaID)

	site := &MangadexSite{
		mangaID: mangaID,
	}

	cfg := &downloader.DownloadConfig{
		Manga:            manga,
		Site:             site,
		ProgressCallback: progressCallback,
	}

	manager := downloader.NewManager(cfg)
	return manager.Download(ctx)
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
