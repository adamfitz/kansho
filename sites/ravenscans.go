package sites

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"kansho/config"
	"kansho/downloader"
)

// RavenscansSite implements the SitePlugin interface for ravenscans.com
type RavenscansSite struct{}

// Ensure RavenscansSite implements SitePlugin
var _ downloader.SitePlugin = (*RavenscansSite)(nil)

// GetSiteName returns the site identifier
func (r *RavenscansSite) GetSiteName() string {
	return "ravenscans"
}

// GetDomain returns the site domain
func (r *RavenscansSite) GetDomain() string {
	return "ravenscans.com"
}

// NeedsCFBypass returns whether this site needs Cloudflare bypass
func (r *RavenscansSite) NeedsCFBypass() bool {
	return false // Ravenscans doesn't use CF protection
}

// GetChapterExtractionMethod returns HOW to extract chapters
// Uses JavaScript to extract chapters from div.eplister ul li elements
func (r *RavenscansSite) GetChapterExtractionMethod() *downloader.ChapterExtractionMethod {
	return &downloader.ChapterExtractionMethod{
		Type:         "javascript",
		WaitSelector: "div.eplister ul li",
		JavaScript: `
			[...document.querySelectorAll('div.eplister ul li')]
			.map(li => ({
				url: li.querySelector('a').href,
				text: li.getAttribute('data-num'),
				title: li.querySelector('div.eph-num span.chapternum')?.textContent || ''
			}))
			.filter(item => item.url && item.text)
		`,
	}
}

// GetImageExtractionMethod returns HOW to extract images
func (r *RavenscansSite) GetImageExtractionMethod() *downloader.ImageExtractionMethod {
	return &downloader.ImageExtractionMethod{
		Type:         "custom",
		WaitSelector: "",
		CustomParser: func(html string) ([]string, error) {
			return parseRavenScansImages(html)
		},
	}
}

// NormalizeChapterURL converts raw URL to absolute URL
// PARSING LOGIC ONLY - returns a string
func (r *RavenscansSite) NormalizeChapterURL(rawURL, baseURL string) string {
	// URLs from ravenscans are already absolute
	// They come in the format: https://ravenscans.org/manga/title/chapter-x/
	return rawURL
}

// NormalizeChapterFilename converts chapter data to filename
// PARSING LOGIC ONLY - returns a string
func (r *RavenscansSite) NormalizeChapterFilename(data map[string]string) string {
	// For ravenscans, the chapter number comes from data-num attribute
	// This is passed in the data map with key "text"
	chapterNum := data["text"]

	if chapterNum == "" {
		// Fallback: try to parse from URL
		url := data["url"]
		re := regexp.MustCompile(`chapter[-_\.]?(\d+)((?:[-_\.]\d+)*)`)
		matches := re.FindStringSubmatch(url)
		if len(matches) > 1 {
			chapterNum = matches[1]
			if len(matches) > 2 && matches[2] != "" {
				// Normalize separators
				partStr := strings.TrimPrefix(matches[2], "-")
				partStr = strings.TrimPrefix(partStr, ".")
				partStr = strings.TrimPrefix(partStr, "_")
				chapterNum = chapterNum + "." + partStr
			}
		} else {
			log.Printf("[Ravenscans] WARNING: Could not parse chapter number from URL: %s", url)
			sanitized := strings.ReplaceAll(url, "/", "-")
			return fmt.Sprintf("%s.cbz", sanitized)
		}
	}

	// Parse the chapterNum as float64 to validate it
	_, err := strconv.ParseFloat(chapterNum, 64)
	if err != nil {
		log.Printf("[Ravenscans] WARNING: Invalid chapter number '%s': %v", chapterNum, err)
		return fmt.Sprintf("ch%s.cbz", chapterNum)
	}

	// Split chapterNum into whole and fractional parts as strings
	parts := strings.Split(chapterNum, ".")

	wholePart := parts[0]
	fracPart := ""
	if len(parts) > 1 {
		fracPart = parts[1]
	}

	// Pad the whole part to 3 digits
	wholeNum, err := strconv.Atoi(wholePart)
	if err != nil {
		log.Printf("[Ravenscans] WARNING: error converting whole part to int: %v", err)
		return fmt.Sprintf("ch%s.cbz", chapterNum)
	}
	paddedWhole := fmt.Sprintf("%03d", wholeNum)

	// Compose final chapter name string
	var filename string
	if fracPart != "" {
		filename = fmt.Sprintf("ch%s.%s", paddedWhole, fracPart)
	} else {
		filename = fmt.Sprintf("ch%s", paddedWhole)
	}

	log.Printf("[Ravenscans] Normalized: %s → %s.cbz", chapterNum, filename)
	return filename + ".cbz"
}

// RavenscansDownloadChapters is the entry point called by the download queue
func RavenscansDownloadChapters(ctx context.Context, manga *config.Bookmarks, progressCallback func(string, float64, int, int, int)) error {
	site := &RavenscansSite{}

	cfg := &downloader.DownloadConfig{
		Manga:            manga,
		Site:             site,
		ProgressCallback: progressCallback,
	}

	manager := downloader.NewManager(cfg)
	return manager.Download(ctx)
}

func parseRavenScansImages(html string) ([]string, error) {
	// Regex: match any cdnX.ravenscans.org/.../chapter-<num>/<page>.jpg
	re := regexp.MustCompile(`https://cdn\d+\.ravenscans\.org/[^\s"']+/chapter-\d+/(\d+)\.jpg`)

	matches := re.FindAllStringSubmatch(html, -1)
	log.Printf("[Ravenscans] DEBUG: Regex found %d matches", len(matches))

	if len(matches) == 0 {
		return nil, fmt.Errorf("[Ravenscans] ERROR: No chapter images found in HTML")
	}

	type imgInfo struct {
		url       string
		pageIndex int
	}

	seen := make(map[string]struct{})
	var images []imgInfo

	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		url := m[0]
		pageStr := m[1]

		if _, ok := seen[url]; ok {
			continue
		}
		seen[url] = struct{}{}

		pageNum, err := strconv.Atoi(pageStr)
		if err != nil {
			continue
		}

		images = append(images, imgInfo{
			url:       url,
			pageIndex: pageNum,
		})
	}

	if len(images) == 0 {
		return nil, fmt.Errorf("[Ravenscans] ERROR: No valid chapter images after dedupe")
	}

	sort.Slice(images, func(i, j int) bool {
		return images[i].pageIndex < images[j].pageIndex
	})

	ordered := make([]string, 0, len(images))
	for _, img := range images {
		ordered = append(ordered, img.url)
	}

	log.Printf("[Ravenscans] DEBUG: Final ordered image list (%d images)", len(ordered))
	return ordered, nil
}
