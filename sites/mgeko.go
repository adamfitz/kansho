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

// MgekoSite implements the SitePlugin interface for mgeko.cc
type MgekoSite struct{}

// Ensure MgekoSite implements SitePlugin
var _ downloader.SitePlugin = (*MgekoSite)(nil)

// GetSiteName returns the site identifier
func (m *MgekoSite) GetSiteName() string {
	return "mgeko"
}

// GetDomain returns the site domain
func (m *MgekoSite) GetDomain() string {
	return "mgeko.cc"
}

// NeedsCFBypass returns whether this site needs Cloudflare bypass
func (m *MgekoSite) NeedsCFBypass() bool {
	return true // Mgeko uses CF protection
}

// GetChapterExtractionMethod returns HOW to extract chapters
// Uses JavaScript to properly support CF bypass detection
func (m *MgekoSite) GetChapterExtractionMethod() *downloader.ChapterExtractionMethod {
	return &downloader.ChapterExtractionMethod{
		Type:         "javascript",
		WaitSelector: "ul.chapter-list li a",
		JavaScript: `
			[...document.querySelectorAll('ul.chapter-list li a')]
			.map(a => ({
				url: a.href,
				text: a.textContent.trim()
			}))
		`,
	}
}

// GetImageExtractionMethod returns HOW to extract images
// Uses JavaScript to extract image URLs from the chapter page
func (m *MgekoSite) GetImageExtractionMethod() *downloader.ImageExtractionMethod {
	return &downloader.ImageExtractionMethod{
		Type:         "javascript",
		WaitSelector: "#chapter-reader img",
		JavaScript: `
			[...document.querySelectorAll('#chapter-reader img')]
			.map(img => img.src)
			.filter(src => src && src.trim() !== '')
		`,
	}
}

// NormalizeChapterURL converts raw URL to absolute URL
// PARSING LOGIC ONLY - returns a string
func (m *MgekoSite) NormalizeChapterURL(rawURL, baseURL string) string {
	// URLs from mgeko are already absolute
	// They come in the format: https://www.mgeko.cc/read-manga/...
	return rawURL
}

// NormalizeChapterFilename converts chapter data to filename
// PARSING LOGIC ONLY - returns a string
func (m *MgekoSite) NormalizeChapterFilename(data map[string]string) string {
	url := data["url"]

	// Regex: match main chapter number, then any sequence of part numbers separated by -, _, or .
	// Example URLs:
	// - https://www.mgeko.cc/read-manga/title/chapter-72
	// - https://www.mgeko.cc/read-manga/title/chapter-72-5
	// - https://www.mgeko.cc/read-manga/title/chapter-72.5
	// - https://www.mgeko.cc/read-manga/title/chapter-72-5-1
	re := regexp.MustCompile(`chapter[-_\.]?(\d+)((?:[-_\.]\d+)*)`)

	matches := re.FindStringSubmatch(url)
	if len(matches) == 0 {
		// Fallback: use a sanitized version of the URL
		sanitized := strings.ReplaceAll(url, "/", "-")
		sanitized = strings.ToLower(sanitized)
		log.Printf("[Mgeko] WARNING: Could not parse chapter number from URL: %s", url)
		return fmt.Sprintf("%s.cbz", sanitized)
	}

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

	log.Printf("[Mgeko] Normalized: %s → %s.cbz", url, filename)
	return filename + ".cbz"
}

// MgekoDownloadChapters is the entry point called by the download queue
func MgekoDownloadChapters(ctx context.Context, manga *config.Bookmarks, progressCallback func(string, float64, int, int, int)) error {
	site := &MgekoSite{}

	cfg := &downloader.DownloadConfig{
		Manga:            manga,
		Site:             site,
		ProgressCallback: progressCallback,
	}

	manager := downloader.NewManager(cfg)
	return manager.Download(ctx)
}
