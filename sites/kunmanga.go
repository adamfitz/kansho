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

// KunmangaSite implements the SitePlugin interface for kunmanga sites
type KunmangaSite struct{}

// Ensure KunmangaSite implements SitePlugin
var _ downloader.SitePlugin = (*KunmangaSite)(nil)

// GetSiteName returns the site identifier
func (k *KunmangaSite) GetSiteName() string {
	return "kunmanga"
}

// GetDomain returns the site domain
func (k *KunmangaSite) GetDomain() string {
	return "kunmanga.com"
}

// NeedsCFBypass returns whether this site needs Cloudflare bypass
func (k *KunmangaSite) NeedsCFBypass() bool {
	return true // Kunmanga uses Cloudflare protection
}

// GetChapterExtractionMethod returns HOW to extract chapters
// Downloader will execute this - we just provide the JavaScript
func (k *KunmangaSite) GetChapterExtractionMethod() *downloader.ChapterExtractionMethod {
	return &downloader.ChapterExtractionMethod{
		Type:         "javascript",
		WaitSelector: "ul.main.version-chap li.wp-manga-chapter > a",
		JavaScript: `
			[...document.querySelectorAll('ul.main.version-chap li.wp-manga-chapter > a')]
			.map(a => {
				const href = a.href;
				return { 
					url: href 
				};
			})
			.filter(x => x !== null)
		`,
	}
}

// GetImageExtractionMethod returns HOW to extract images
// Downloader will execute this - we just provide the JavaScript
func (k *KunmangaSite) GetImageExtractionMethod() *downloader.ImageExtractionMethod {
	return &downloader.ImageExtractionMethod{
		Type:         "javascript",
		WaitSelector: "div.reading-content img",
		JavaScript: `
			[...document.querySelectorAll('div.reading-content img')]
			.map(img => {
				const src = img.src;
				return src.trim();
			})
			.filter(src => src !== '')
		`,
	}
}

// NormalizeChapterURL converts raw URL to absolute URL
// PARSING LOGIC ONLY - returns a string
func (k *KunmangaSite) NormalizeChapterURL(rawURL, baseURL string) string {
	// URLs from kunmanga are already absolute
	return rawURL
}

// NormalizeChapterFilename converts chapter data to filename
// PARSING LOGIC ONLY - returns a string
func (k *KunmangaSite) NormalizeChapterFilename(data map[string]string) string {
	chapterURL := data["url"]

	// Regex: match chapter number and optional part numbers
	// Handles URLs like: /chapter-18/ or /chapter-18-5/
	re := regexp.MustCompile(`chapter[-_\.]?(\d+)((?:[-_\.]\d+)*)`)

	matches := re.FindStringSubmatch(chapterURL)
	if len(matches) == 0 {
		log.Printf("[Kunmanga] WARNING: Could not parse chapter number from URL: %s", chapterURL)
		return "ch000.cbz"
	}

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

	log.Printf("[Kunmanga] Normalized: %s → %s.cbz", chapterURL, filename)
	return filename + ".cbz"
}

// KunmangaDownloadChapters is the entry point called by the download queue
func KunmangaDownloadChapters(ctx context.Context, manga *config.Bookmarks, progressCallback func(string, float64, int, int, int)) error {
	site := &KunmangaSite{}

	cfg := &downloader.DownloadConfig{
		Manga:            manga,
		Site:             site,
		ProgressCallback: progressCallback,
	}

	manager := downloader.NewManager(cfg)
	return manager.Download(ctx)
}
