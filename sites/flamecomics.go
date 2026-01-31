package sites

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"kansho/config"
	"kansho/downloader"
)

type FlameComicsSite struct{}

// -------------------------
// SitePlugin implementation
// -------------------------

func (s *FlameComicsSite) GetSiteName() string {
	return "flamecomics"
}

func (s *FlameComicsSite) GetDomain() string {
	return "flamecomics.xyz"
}

func (s *FlameComicsSite) NeedsCFBypass() bool {
	return true
}

func (s *FlameComicsSite) NormalizeChapterURL(rawURL, baseURL string) string {
	if strings.HasPrefix(rawURL, "http") {
		return rawURL
	}
	return "https://flamecomics.xyz" + rawURL
}

func (s *FlameComicsSite) NormalizeChapterFilename(data map[string]string) string {
	raw := data["num"]
	if raw == "" {
		raw = data["text"]
	}

	ch := extractFlameChapterNumber(raw)
	if ch == 0 {
		ch = extractFlameChapterNumber(data["url"])
	}

	return fmt.Sprintf("ch%03d.cbz", ch)
}

func (s *FlameComicsSite) GetChapterExtractionMethod() *downloader.ChapterExtractionMethod {
	return &downloader.ChapterExtractionMethod{
		Type:         "custom",
		CustomParser: parseFlameComicsChapters,
	}
}

func (s *FlameComicsSite) GetImageExtractionMethod() *downloader.ImageExtractionMethod {
	return &downloader.ImageExtractionMethod{
		Type:         "custom",
		CustomParser: parseFlameComicsImages,
	}
}

// enable debugging to save HTML files
func (s *FlameComicsSite) Debugger() *downloader.Debugger {
	return &downloader.Debugger{
		SaveHTML: false,
		HTMLPath: "flamecomics_debug.html",
	}
}

// -------------------------
// Download entrypoint
// -------------------------

func FlameComicsDownloadChapters(ctx context.Context, manga *config.Bookmarks, progressCallback func(string, float64, int, int, int)) error {
	site := &FlameComicsSite{}

	cfg := &downloader.DownloadConfig{
		Manga:            manga,
		Site:             site,
		ProgressCallback: progressCallback,
	}

	manager := downloader.NewManager(cfg)
	return manager.Download(ctx)
}

// -------------------------
// Chapter list extraction (Next.js JSON-based)
// -------------------------

// NextJsData represents the structure of the __NEXT_DATA__ script
type NextJsData struct {
	Props struct {
		PageProps struct {
			Series struct {
				SeriesID int    `json:"series_id"`
				Status   string `json:"status"`
			} `json:"series"`
			Chapters []struct {
				ChapterID int     `json:"chapter_id"`
				Chapter   string  `json:"chapter"`
				Title     *string `json:"title"`
				Token     string  `json:"token"`
			} `json:"chapters"`
		} `json:"pageProps"`
	} `json:"props"`
}

func parseFlameComicsChapters(html string) (map[string]string, error) {
	// Extract the __NEXT_DATA__ JSON from the HTML
	re := regexp.MustCompile(`<script id="__NEXT_DATA__" type="application/json">(.+?)</script>`)
	matches := re.FindStringSubmatch(html)
	if len(matches) < 2 {
		return nil, fmt.Errorf("FlameComics: __NEXT_DATA__ script not found")
	}

	// Parse the JSON
	var data NextJsData
	if err := json.Unmarshal([]byte(matches[1]), &data); err != nil {
		return nil, fmt.Errorf("FlameComics: failed to parse __NEXT_DATA__: %w", err)
	}

	result := make(map[string]string)
	seriesID := data.Props.PageProps.Series.SeriesID

	// Build chapter URLs from the JSON data
	// Pattern: /series/{series_id}/{token}
	for _, ch := range data.Props.PageProps.Chapters {
		// Skip if chapter field is empty or token is missing
		if ch.Chapter == "" || ch.Token == "" {
			continue
		}

		// Extract chapter number from the "chapter" field (e.g., "11.00" -> 11, "0.00" -> 0)
		chapterNum := extractFlameChapterNumber(ch.Chapter)

		// FlameComics uses /series/{series_id}/{token} for chapter pages
		url := fmt.Sprintf("https://flamecomics.xyz/series/%d/%s", seriesID, ch.Token)

		filename := fmt.Sprintf("ch%03d.cbz", chapterNum)
		result[filename] = url
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("FlameComics: no chapters found in __NEXT_DATA__")
	}

	return result, nil
}

// -------------------------
// Image extraction
// -------------------------

// ChapterData represents the chapter page structure
type ChapterPageData struct {
	Props struct {
		PageProps struct {
			Chapter struct {
				Images []string `json:"images"`
			} `json:"chapter"`
			Images []interface{} `json:"images"` // Can be string or object
		} `json:"pageProps"`
	} `json:"props"`
}

func parseFlameComicsImages(html string) ([]string, error) {
	// First, try to extract images from __NEXT_DATA__ JSON
	re := regexp.MustCompile(`<script id="__NEXT_DATA__" type="application/json">(.+?)</script>`)
	matches := re.FindStringSubmatch(html)

	if len(matches) >= 2 {
		// Parse the JSON to extract image URLs
		var data ChapterPageData
		if err := json.Unmarshal([]byte(matches[1]), &data); err == nil {
			var imageURLs []string

			// Try pageProps.chapter.images first
			if len(data.Props.PageProps.Chapter.Images) > 0 {
				return data.Props.PageProps.Chapter.Images, nil
			}

			// Try pageProps.images
			if len(data.Props.PageProps.Images) > 0 {
				for _, img := range data.Props.PageProps.Images {
					if imgStr, ok := img.(string); ok {
						imageURLs = append(imageURLs, imgStr)
					} else if imgMap, ok := img.(map[string]interface{}); ok {
						// Image might be an object with a url/src field
						if url, ok := imgMap["url"].(string); ok {
							imageURLs = append(imageURLs, url)
						} else if src, ok := imgMap["src"].(string); ok {
							imageURLs = append(imageURLs, src)
						}
					}
				}
				if len(imageURLs) > 0 {
					return imageURLs, nil
				}
			}
		}
	}

	// Fallback 1: Look for CDN URLs in the HTML
	// FlameComics uses cdn.flamecomics.xyz for images
	cdnPattern := regexp.MustCompile(`https://cdn\.flamecomics\.xyz/[^"'\s]+\.(?:jpg|jpeg|png|webp|gif)`)
	cdnMatches := cdnPattern.FindAllString(html, -1)

	if len(cdnMatches) > 0 {
		// Deduplicate
		seen := make(map[string]bool)
		var images []string
		for _, url := range cdnMatches {
			// Skip Next.js optimized image URLs, get the actual CDN URL
			if strings.Contains(url, "/_next/image?url=") {
				// Extract the actual URL from the Next.js image proxy
				if idx := strings.Index(url, "url="); idx != -1 {
					actualURL := url[idx+4:]
					if endIdx := strings.IndexAny(actualURL, "&\"'"); endIdx != -1 {
						actualURL = actualURL[:endIdx]
					}
					url = actualURL
				}
			}

			if !seen[url] {
				seen[url] = true
				images = append(images, url)
			}
		}
		if len(images) > 0 {
			return images, nil
		}
	}

	// Fallback 2: Look for base64 encoded images or data attributes
	dataPatterns := []string{
		`data-src=["']([^"']+)["']`,
		`src=["']([^"']+\.(?:jpg|jpeg|png|webp|gif)[^"']*)["']`,
	}

	var images []string
	seen := make(map[string]bool)

	for _, pattern := range dataPatterns {
		re := regexp.MustCompile(pattern)
		allMatches := re.FindAllStringSubmatch(html, -1)

		for _, match := range allMatches {
			if len(match) > 1 {
				url := match[1]

				// Skip if already seen, if it's not an image URL, or if it's an icon/logo
				if seen[url] || strings.Contains(url, "icon") || strings.Contains(url, "logo") {
					continue
				}

				// Only include if it looks like a manga page image
				if strings.Contains(url, "cdn") || strings.HasPrefix(url, "http") {
					seen[url] = true
					images = append(images, url)
				}
			}
		}
	}

	if len(images) == 0 {
		return nil, fmt.Errorf("FlameComics: no images found - series may be dropped or unavailable")
	}

	return images, nil
}

// -------------------------
// Helper functions
// -------------------------

func extractFlameChapterNumber(s string) int {
	// Extract the first number from the string (handles "11.00", "0.00", "11", "Chapter 11", etc.)
	re := regexp.MustCompile(`(\d+)`)
	m := re.FindStringSubmatch(s)
	if len(m) < 2 {
		return -1 // Return -1 for invalid/no number found, so 0 is a valid chapter number
	}

	n, err := strconv.Atoi(m[1])
	if err != nil {
		return -1
	}
	return n
}
