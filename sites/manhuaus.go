package sites

import (
	"context"
	"fmt"
	"log"
	"strings"

	"kansho/config"
	"kansho/downloader"
)

// ManhuausSite implements the SitePlugin interface for manhuaus sites
type ManhuausSite struct{}

// Ensure ManhuausSite implements SitePlugin
var _ downloader.SitePlugin = (*ManhuausSite)(nil)

// GetSiteName returns the site identifier
func (m *ManhuausSite) GetSiteName() string {
	return "manhuaus"
}

// GetDomain returns the site domain
func (m *ManhuausSite) GetDomain() string {
	return "manhuaus.com"
}

// NeedsCFBypass returns whether this site needs Cloudflare bypass
func (m *ManhuausSite) NeedsCFBypass() bool {
	return true // Manhuaus uses Cloudflare protection
}

// GetChapterExtractionMethod returns HOW to extract chapters
// Downloader will execute this - we just provide the JavaScript
func (m *ManhuausSite) GetChapterExtractionMethod() *downloader.ChapterExtractionMethod {
	return &downloader.ChapterExtractionMethod{
		Type:         "javascript",
		WaitSelector: "li.wp-manga-chapter a",
		JavaScript: `
			[...document.querySelectorAll('li.wp-manga-chapter a')]
			.map(a => {
				const href = a.href;
				const match = href.match(/chapter-([\d.]+)/);
				if (match) {
					return { 
						num: match[1], 
						url: href 
					};
				}
				return null;
			})
			.filter(x => x !== null)
		`,
	}
}

// GetImageExtractionMethod returns HOW to extract images
// Downloader will execute this - we just provide the JavaScript
func (m *ManhuausSite) GetImageExtractionMethod() *downloader.ImageExtractionMethod {
	return &downloader.ImageExtractionMethod{
		Type:         "javascript",
		WaitSelector: "div.reading-content img",
		JavaScript: `
			[...document.querySelectorAll('div.reading-content img')]
			.map(img => {
				const src = img.getAttribute('data-src') || img.src;
				return src.trim();
			})
			.filter(src => src !== '')
		`,
	}
}

// NormalizeChapterURL converts raw URL to absolute URL
// PARSING LOGIC ONLY - returns a string
func (m *ManhuausSite) NormalizeChapterURL(rawURL, baseURL string) string {
	// URLs from manhuaus are already absolute
	return rawURL
}

// NormalizeChapterFilename converts chapter data to filename
// PARSING LOGIC ONLY - returns a string
func (m *ManhuausSite) NormalizeChapterFilename(data map[string]string) string {
	num := data["num"]

	var filename string

	// Handle decimal chapters (e.g., "1.5")
	if strings.Contains(num, ".") {
		parts := strings.SplitN(num, ".", 2)
		// Pad integer part to 3 digits
		intPart := parts[0]
		decimalPart := parts[1]
		filename = fmt.Sprintf("ch%03s.%s", intPart, decimalPart)
	} else {
		// Standard chapter number - pad to 3 digits
		filename = fmt.Sprintf("ch%03s", num)
	}

	log.Printf("[Manhuaus] Normalized: %s → %s.cbz", num, filename)
	return filename + ".cbz"
}

// ManhuausDownloadChapters is the entry point called by the download queue
func ManhuausDownloadChapters(ctx context.Context, manga *config.Bookmarks, progressCallback func(string, float64, int, int, int)) error {
	site := &ManhuausSite{}

	cfg := &downloader.DownloadConfig{
		Manga:            manga,
		Site:             site,
		ProgressCallback: progressCallback,
	}

	manager := downloader.NewManager(cfg)
	return manager.Download(ctx)
}
