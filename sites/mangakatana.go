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
	return false //not currntly using cf bypass
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

// GetImageExtractionMethod returns HOW to extract images
// Uses JavaScript as images are in a JavaScript variable (var thzq)
func (m *MangakatanaSite) GetImageExtractionMethod() *downloader.ImageExtractionMethod {
	return &downloader.ImageExtractionMethod{
		Type:         "javascript",
		WaitSelector: "img.img", // Wait for images to load
		JavaScript: `
			// Extract image URLs from the thzq JavaScript variable
			(function() {
				// Find the script tag containing var thzq
				const scripts = document.querySelectorAll('script');
				for (let script of scripts) {
					const text = script.textContent;
					if (text.includes('var thzq')) {
						// Extract the array: var thzq=['url1','url2',...];
						const match = text.match(/var\s+thzq\s*=\s*\[(.*?)\];/);
						if (match && match[1]) {
							// Extract URLs from quotes
							const urls = match[1].match(/'([^']+)'/g);
							if (urls) {
								return urls.map(u => u.replace(/'/g, ''));
							}
						}
					}
				}
				return [];
			})()
		`,
	}
}

// NormalizeChapterURL converts raw URL to absolute URL
// PARSING LOGIC ONLY - returns a string
func (m *MangakatanaSite) NormalizeChapterURL(rawURL, baseURL string) string {
	// URLs from mangakatana are already absolute
	return rawURL
}

// NormalizeChapterFilename converts chapter data to filename
// PARSING LOGIC ONLY - returns a string
func (m *MangakatanaSite) NormalizeChapterFilename(data map[string]string) string {
	// Extract chapter number from text like "Chapter 1: Title" or "Chapter 1.5: Title"
	text := data["text"]

	// Regex to extract chapter numbers from text like "Chapter 1:", "Chapter 1.5:", "Chapter 123:"
	re := regexp.MustCompile(`Chapter\s+(\d+)(?:\.(\d+))?`)

	matches := re.FindStringSubmatch(text)
	if len(matches) == 0 {
		// Fallback: use the text as-is
		sanitized := strings.ReplaceAll(text, " ", "-")
		sanitized = strings.ToLower(sanitized)
		log.Printf("[MangaKatana] WARNING: Could not parse chapter number from text: %s", text)
		return fmt.Sprintf("%s.cbz", sanitized)
	}

	mainNum := matches[1] // Main chapter number (e.g., "1", "10", "123")
	partNum := ""
	if len(matches) > 2 && matches[2] != "" {
		partNum = matches[2] // Decimal part (e.g., "5" from "1.5")
	}

	// Build final filename: pad main number to 3 digits
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
