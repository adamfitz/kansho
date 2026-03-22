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

// MangakatanaSite implements the SitePlugin interface for mangakatana.com
type MangakatanaSite struct{}

// Ensure MangakatanaSite implements SitePlugin
var _ downloader.SitePlugin = (*MangakatanaSite)(nil)

// GetSiteName returns the site identifier
func (m *MangakatanaSite) GetSiteName() string {
	return "mangakatana"
}

// GetDomain returns the site domain
func (m *MangakatanaSite) GetDomain() string {
	return "mangakatana.com"
}

// NeedsCFBypass returns whether this site needs Cloudflare bypass
func (m *MangakatanaSite) NeedsCFBypass() bool {
	return false
}

// GetChapterExtractionMethod returns HOW to extract chapters
// Uses JavaScript (not html_selector) to properly support CF bypass detection
func (m *MangakatanaSite) GetChapterExtractionMethod() *downloader.ChapterExtractionMethod {
	return &downloader.ChapterExtractionMethod{
		Type:         "javascript",
		WaitSelector: ".chapters a",
		JavaScript: `
			[...document.querySelectorAll('.chapters a')]
			.map(a => ({
				text: a.textContent.trim(),
				url: a.href
			}))
		`,
	}
}

// GetImageExtractionMethod returns HOW to extract images.
// thzq is present in the static HTML — no browser needed.
// Uses custom HTTP fetch + regex to extract image URLs directly.
func (m *MangakatanaSite) GetImageExtractionMethod() *downloader.ImageExtractionMethod {
	return &downloader.ImageExtractionMethod{
		Type:         "custom",
		WaitSelector: "",
		CustomParser: parseMangakatanaImages,
	}
}

// parseMangakatanaImages extracts image URLs from the thzq JS variable in static HTML
func parseMangakatanaImages(html string) ([]string, error) {
	re := regexp.MustCompile(`var\s+thzq\s*=\s*\[([^\]]+)\]`)
	match := re.FindStringSubmatch(html)
	if len(match) < 2 {
		return nil, fmt.Errorf("[MangaKatana] var thzq not found in page HTML")
	}

	urlRe := regexp.MustCompile(`'([^']+)'`)
	urlMatches := urlRe.FindAllStringSubmatch(match[1], -1)
	if len(urlMatches) == 0 {
		return nil, fmt.Errorf("[MangaKatana] thzq found but no URLs extracted")
	}

	var urls []string
	for _, u := range urlMatches {
		if u[1] != "" {
			urls = append(urls, u[1])
		}
	}

	log.Printf("[MangaKatana] Found %d images", len(urls))
	return urls, nil
}

// NormalizeChapterURL converts raw URL to absolute URL
// PARSING LOGIC ONLY - returns a string
func (m *MangakatanaSite) NormalizeChapterURL(rawURL, baseURL string) string {
	return rawURL
}

// NormalizeChapterFilename converts chapter data to filename
// PARSING LOGIC ONLY - returns a string
func (m *MangakatanaSite) NormalizeChapterFilename(data map[string]string) string {
	text := data["text"]

	re := regexp.MustCompile(`Chapter\s+(\d+)(?:\.(\d+))?`)
	matches := re.FindStringSubmatch(text)
	if len(matches) == 0 {
		sanitized := strings.ReplaceAll(text, " ", "-")
		sanitized = strings.ToLower(sanitized)
		log.Printf("[MangaKatana] WARNING: Could not parse chapter number from text: %s", text)
		return fmt.Sprintf("%s.cbz", sanitized)
	}

	mainNum := matches[1]
	partNum := ""
	if len(matches) > 2 && matches[2] != "" {
		partNum = matches[2]
	}

	filename := fmt.Sprintf("ch%03s", mainNum)
	if partNum != "" {
		filename += "." + partNum
	}

	log.Printf("[MangaKatana] Normalized: %s → %s.cbz", text, filename)
	return filename + ".cbz"
}

// MangakatanaDownloadChapters is the entry point called by the download queue
func MangakatanaDownloadChapters(ctx context.Context, manga *config.Bookmarks, progressCallback func(string, float64, int, int, int)) error {
	site := &MangakatanaSite{}

	cfg := &downloader.DownloadConfig{
		Manga:            manga,
		Site:             site,
		ProgressCallback: progressCallback,
	}

	manager := downloader.NewManager(cfg)
	return manager.Download(ctx)
}
