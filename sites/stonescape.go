package sites

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"

	"kansho/config"
	"kansho/downloader"
)

// StonescapeSite implements the SitePlugin interface for stonescape.xyz
type StonescapeSite struct{}

// Ensure StonescapeSite implements SitePlugin
var _ downloader.SitePlugin = (*StonescapeSite)(nil)

// --- JSON response structs ---

type stonescapeSeriesResponse struct {
	SeriesID string `json:"seriesId"`
}

type stonescapeChapter struct {
	ChapterID     string `json:"chapterId"`
	ChapterNumber string `json:"chapterNumber"`
}

type stonescapeChaptersResponse struct {
	Chapters []stonescapeChapter `json:"chapters"`
}

type stonescapePage struct {
	PageNumber int    `json:"pageNumber"`
	URL        string `json:"url"`
}

type stonescapePagesResponse struct {
	Pages []stonescapePage `json:"pages"`
}

// --- SitePlugin interface ---

func (s *StonescapeSite) GetSiteName() string {
	return "stonescape"
}

func (s *StonescapeSite) GetDomain() string {
	return "stonescape.xyz"
}

func (s *StonescapeSite) NeedsCFBypass() bool {
	return false
}

// GetChapterExtractionMethod returns an "api" type method.
// APIFunc receives the manga URL and an APIClient, and returns raw chapter data
// as []map[string]string with keys "num" (chapterNumber) and "url" (chapterID).
// The chapterID is stored as the "url" value because the manager passes it
// directly to GetImageExtractionMethod's APIFunc as the chapterURL parameter,
// and NormalizeChapterURL returns it unchanged.
func (s *StonescapeSite) GetChapterExtractionMethod() *downloader.ChapterExtractionMethod {
	return &downloader.ChapterExtractionMethod{
		Type: "api",
		APIFunc: func(mangaURL string, client *downloader.APIClient) ([]map[string]string, error) {
			slug, err := stonescapeExtractSlug(mangaURL)
			if err != nil {
				return nil, fmt.Errorf("[Stonescape] could not extract slug from %q: %w", mangaURL, err)
			}

			// Step 1: slug → seriesId
			seriesURL := fmt.Sprintf("https://stonescape.xyz/api/series/by-slug/%s", slug)
			log.Printf("[Stonescape] Fetching series info: %s", seriesURL)

			var seriesResp stonescapeSeriesResponse
			if err := client.FetchJSON(context.Background(), seriesURL, &seriesResp); err != nil {
				return nil, fmt.Errorf("[Stonescape] failed to fetch series info: %w", err)
			}
			if seriesResp.SeriesID == "" {
				return nil, fmt.Errorf("[Stonescape] empty seriesId for slug %q", slug)
			}

			// Step 2: seriesId → chapters
			chaptersURL := fmt.Sprintf("https://stonescape.xyz/api/series/%s/chapters", seriesResp.SeriesID)
			log.Printf("[Stonescape] Fetching chapters: %s", chaptersURL)

			var chaptersResp stonescapeChaptersResponse
			if err := client.FetchJSON(context.Background(), chaptersURL, &chaptersResp); err != nil {
				return nil, fmt.Errorf("[Stonescape] failed to fetch chapters: %w", err)
			}
			if len(chaptersResp.Chapters) == 0 {
				return nil, fmt.Errorf("[Stonescape] no chapters found for series %q", seriesResp.SeriesID)
			}

			// Build raw chapter data.
			// "url" holds the chapterID — the manager passes this straight into
			// GetImageExtractionMethod's APIFunc as the chapterURL parameter,
			// and NormalizeChapterURL returns it unchanged.
			result := make([]map[string]string, 0, len(chaptersResp.Chapters))
			for _, ch := range chaptersResp.Chapters {
				result = append(result, map[string]string{
					"num": ch.ChapterNumber,
					"url": ch.ChapterID,
				})
				log.Printf("[Stonescape] Found chapter: %s → %s", ch.ChapterNumber, ch.ChapterID)
			}

			return result, nil
		},
	}
}

// GetImageExtractionMethod returns an "api" type method.
// APIFunc receives the chapterID (stored as chapterURL by the manager) and
// returns the sorted page image URLs for that chapter.
func (s *StonescapeSite) GetImageExtractionMethod() *downloader.ImageExtractionMethod {
	return &downloader.ImageExtractionMethod{
		Type: "api",
		APIFunc: func(chapterID string, chapterData map[string]string, client *downloader.APIClient) ([]string, error) {
			pagesURL := fmt.Sprintf("https://stonescape.xyz/api/chapters/%s/pages", chapterID)
			log.Printf("[Stonescape] Fetching pages: %s", pagesURL)

			var pagesResp stonescapePagesResponse
			if err := client.FetchJSON(context.Background(), pagesURL, &pagesResp); err != nil {
				return nil, fmt.Errorf("[Stonescape] failed to fetch pages: %w", err)
			}
			if len(pagesResp.Pages) == 0 {
				return nil, fmt.Errorf("[Stonescape] no pages found for chapter %q", chapterID)
			}

			// Sort by pageNumber (insertion sort — typically already sorted but be safe)
			pages := pagesResp.Pages
			for i := 1; i < len(pages); i++ {
				for j := i; j > 0 && pages[j].PageNumber < pages[j-1].PageNumber; j-- {
					pages[j], pages[j-1] = pages[j-1], pages[j]
				}
			}

			// Page URLs from the API are relative: /pub/manhwa/...
			urls := make([]string, 0, len(pages))
			for _, p := range pages {
				urls = append(urls, "https://stonescape.xyz"+p.URL)
			}

			log.Printf("[Stonescape] Found %d pages for chapter %q", len(urls), chapterID)
			return urls, nil
		},
	}
}

// NormalizeChapterURL is a no-op.
// The "url" value from GetChapterExtractionMethod is a chapterID UUID, which
// is passed directly to GetImageExtractionMethod's APIFunc — no URL manipulation needed.
func (s *StonescapeSite) NormalizeChapterURL(rawURL, baseURL string) string {
	return rawURL
}

// NormalizeChapterFilename converts a chapterNumber from the API (e.g. "1.00",
// "30.00", "1.50") into a padded CBZ filename (e.g. "ch001.cbz", "ch030.cbz",
// "ch001.5.cbz").
func (s *StonescapeSite) NormalizeChapterFilename(data map[string]string) string {
	num := data["num"]

	// API returns values like "1.00", "10.00", "1.50"
	re := regexp.MustCompile(`^([0-9]+)(?:\.([0-9]+))?$`)
	matches := re.FindStringSubmatch(num)
	if len(matches) == 0 {
		return fmt.Sprintf("ch%s.cbz", num)
	}

	whole := matches[1]
	decimal := matches[2]

	// Pad integer part to 3 digits
	padded := fmt.Sprintf("%03s", whole)

	var fileName string
	if decimal != "" && decimal != "00" {
		// e.g. "1.50" → "ch001.5"  (trim trailing zeros from decimal)
		trimmed := strings.TrimRight(decimal, "0")
		fileName = fmt.Sprintf("ch%s.%s", padded, trimmed)
	} else {
		fileName = fmt.Sprintf("ch%s", padded)
	}

	log.Printf("[Stonescape] Normalized: %s → %s.cbz", num, fileName)
	return fileName + ".cbz"
}

// --- Helpers ---

// stonescapeExtractSlug pulls the manga slug from a URL like:
//
//	https://stonescape.xyz/series/mia-has-returned
//	https://stonescape.xyz/series/mia-has-returned/
func stonescapeExtractSlug(mangaURL string) (string, error) {
	re := regexp.MustCompile(`/series/([^/?#]+)`)
	m := re.FindStringSubmatch(mangaURL)
	if len(m) < 2 || m[1] == "" {
		return "", fmt.Errorf("no /series/<slug> found in %q", mangaURL)
	}
	return m[1], nil
}

// --- Entry point ---

// StonescapeDownloadChapters is the entry point called by the download queue.
func StonescapeDownloadChapters(ctx context.Context, manga *config.Bookmarks, progressCallback func(string, float64, int, int, int)) error {
	site := &StonescapeSite{}

	cfg := &downloader.DownloadConfig{
		Manga:            manga,
		Site:             site,
		ProgressCallback: progressCallback,
	}

	manager := downloader.NewManager(cfg)
	return manager.Download(ctx)
}
