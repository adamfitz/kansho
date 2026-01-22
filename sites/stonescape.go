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

// GetSiteName returns the site identifier
func (s *StonescapeSite) GetSiteName() string {
	return "stonescape"
}

// GetDomain returns the site domain
func (s *StonescapeSite) GetDomain() string {
	return "stonescape.xyz"
}

// NeedsCFBypass returns whether this site needs Cloudflare bypass
func (s *StonescapeSite) NeedsCFBypass() bool {
	return false // StoneScape doesn't use CF protection
}

// GetChapterExtractionMethod returns HOW to extract chapters
// Downloader will execute this - we just provide the JavaScript
func (s *StonescapeSite) GetChapterExtractionMethod() *downloader.ChapterExtractionMethod {
	return &downloader.ChapterExtractionMethod{
		Type:         "javascript",
		WaitSelector: "div.listing-chapters_wrap ul.main.version-chap li.wp-manga-chapter a",
		JavaScript: `
			[...document.querySelectorAll('div.listing-chapters_wrap ul.main.version-chap li.wp-manga-chapter a')]
			.map(a => {
				const txt = a.textContent.trim();
				const m = txt.match(/^Ch\.\s*(.+)$/i);
				if (m) {
					return { 
						num: m[1].trim().replace(/\s+/g, '-').toLowerCase(), 
						url: a.href 
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
func (s *StonescapeSite) GetImageExtractionMethod() *downloader.ImageExtractionMethod {
	return &downloader.ImageExtractionMethod{
		Type:         "javascript",
		WaitSelector: "img.wp-manga-chapter-img",
		JavaScript: `
			[...document.querySelectorAll('img.wp-manga-chapter-img')].map(img => img.src.trim())
		`,
	}
}

// NormalizeChapterURL converts raw URL to absolute URL
// PARSING LOGIC ONLY - returns a string
func (s *StonescapeSite) NormalizeChapterURL(rawURL, baseURL string) string {
	// URLs from stonescape are already absolute
	return rawURL
}

// NormalizeChapterFilename converts chapter data to filename
// PARSING LOGIC ONLY - returns a string
func (s *StonescapeSite) NormalizeChapterFilename(data map[string]string) string {
	num := data["num"]

	// Regex: captures integer + optional decimal + optional suffix
	re := regexp.MustCompile(`^([0-9]+)(?:[-.]([0-9]+))?(.*)`)

	matches := re.FindStringSubmatch(num)
	if len(matches) == 0 {
		return fmt.Sprintf("ch%s.cbz", num)
	}

	whole := matches[1]
	decimal := matches[2]
	suffix := strings.Trim(matches[3], "-")

	// Pad integer part to 3 digits
	padded := fmt.Sprintf("%03s", whole)

	var fileName string
	if decimal != "" {
		fileName = fmt.Sprintf("ch%s.%s", padded, decimal)
	} else {
		fileName = fmt.Sprintf("ch%s", padded)
	}

	if suffix != "" {
		fileName = fmt.Sprintf("%s-%s", fileName, suffix)
	}

	log.Printf("[Stonescape] Normalized: %s → %s.cbz", num, fileName)
	return fileName + ".cbz"
}

// StonescapeDownloadChapters is the entry point called by the download queue
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
