package sites

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"

	"kansho/config"
	"kansho/downloader"

	"github.com/PuerkitoBio/goquery"
)

// ThunderscansSite implements SitePlugin for en-thunderscans.com.
//
// Site characteristics:
//   - WordPress "mangareader" theme. The manga page server-renders the full
//     chapter list inside <div class="eplister" id="chapterlist"><ul>, one
//     <li data-num="..."> per chapter, so the whole list is recoverable from a
//     single static fetch — no pagination or browser needed.
//   - Each chapter page embeds the page list as a JSON object inside a
//     ts_reader.run({...}); call (the same reader pattern as KingOfShojo),
//     so the image URLs are also extracted from the static HTML.
//   - The domain is Cloudflare-fronted but serves pages and images without a
//     CF clearance cookie (cf-cache-status DYNAMIC, plain HTTP returns 200),
//     so the plain HTTP downloader suffices. A CF challenge is still detected
//     and handled by the downloader automatically if it ever appears.
type ThunderscansSite struct{}

// Ensure ThunderscansSite implements SitePlugin
var _ downloader.SitePlugin = (*ThunderscansSite)(nil)

// -------------------------
// SitePlugin implementation
// -------------------------

func (s *ThunderscansSite) GetSiteName() string {
	return "thunderscans"
}

func (s *ThunderscansSite) GetDomain() string {
	return "en-thunderscans.com"
}

func (s *ThunderscansSite) NeedsCFBypass() bool {
	return false
}

func (s *ThunderscansSite) Debugger() *downloader.Debugger {
	return &downloader.Debugger{
		SaveHTML: false,
		HTMLPath: "thunderscans_debug.html",
	}
}

// GetChapterExtractionMethod uses "custom" extraction: the chapter list is
// server-rendered in the manga page HTML, so no browser execution is needed.
func (s *ThunderscansSite) GetChapterExtractionMethod() *downloader.ChapterExtractionMethod {
	return &downloader.ChapterExtractionMethod{
		Type:         "custom",
		CustomParser: parseThunderscansChapters,
	}
}

// GetImageExtractionMethod uses "custom" extraction: the page images are
// embedded in the ts_reader.run({...}) JSON in the static chapter page HTML.
func (s *ThunderscansSite) GetImageExtractionMethod() *downloader.ImageExtractionMethod {
	return &downloader.ImageExtractionMethod{
		Type:         "custom",
		CustomParser: parseThunderscansImages,
	}
}

func (s *ThunderscansSite) NormalizeChapterURL(rawURL, baseURL string) string {
	if strings.HasPrefix(rawURL, "http://") || strings.HasPrefix(rawURL, "https://") {
		return rawURL
	}
	if strings.HasPrefix(rawURL, "//") {
		return "https:" + rawURL
	}
	if !strings.HasPrefix(rawURL, "/") {
		rawURL = "/" + rawURL
	}
	return "https://en-thunderscans.com" + rawURL
}

func (s *ThunderscansSite) NormalizeChapterFilename(data map[string]string) string {
	raw := data["num"]
	if raw == "" {
		raw = data["text"]
	}
	if raw == "" {
		raw = data["url"]
	}

	num, frac := thunderscansChapterNumber(raw)
	if num < 0 {
		sanitized := strings.ToLower(strings.TrimSpace(raw))
		sanitized = regexp.MustCompile(`[^a-z0-9.]+`).ReplaceAllString(sanitized, "-")
		log.Printf("[Thunderscans] WARNING: could not parse chapter number from %q", raw)
		return sanitized + ".cbz"
	}

	name := fmt.Sprintf("ch%03d", num)
	if frac != "" {
		name += "." + frac
	}
	return name + ".cbz"
}

// -------------------------
// Chapter list parsing
// -------------------------

// parseThunderscansChapters extracts the chapter list from the manga page HTML.
// Returns a map of chapter filename -> chapter URL.
func parseThunderscansChapters(html string) (map[string]string, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader([]byte(html)))
	if err != nil {
		return nil, fmt.Errorf("Thunderscans: failed to parse chapter list HTML: %w", err)
	}

	result := make(map[string]string)
	doc.Find("#chapterlist li").Each(func(_ int, s *goquery.Selection) {
		href, exists := s.Find("a").Attr("href")
		if !exists {
			return
		}

		data := map[string]string{
			"url":  href,
			"text": s.Find(".chapternum").First().Text(),
			"num":  s.AttrOr("data-num", ""),
		}

		filename := (&ThunderscansSite{}).NormalizeChapterFilename(data)
		url := (&ThunderscansSite{}).NormalizeChapterURL(href, "")
		result[filename] = url
	})

	if len(result) == 0 {
		return nil, fmt.Errorf("Thunderscans: no chapters found in chapter list")
	}

	return result, nil
}

// -------------------------
// Image parsing
// -------------------------

// thunderscansReader mirrors the JSON object passed to ts_reader.run({...}).
type thunderscansReader struct {
	Sources []struct {
		Source string   `json:"source"`
		Images []string `json:"images"`
	} `json:"sources"`
}

// parseThunderscansImages extracts image URLs from the ts_reader.run({...})
// JSON embedded in the chapter page HTML.
func parseThunderscansImages(html string) ([]string, error) {
	re := regexp.MustCompile(`(?s)ts_reader\.run\((\{.*?\})\);`)
	m := re.FindStringSubmatch(html)
	if len(m) < 2 {
		return nil, fmt.Errorf("Thunderscans: ts_reader.run JSON not found in chapter HTML")
	}

	var reader thunderscansReader
	if err := json.Unmarshal([]byte(m[1]), &reader); err != nil {
		return nil, fmt.Errorf("Thunderscans: failed to parse ts_reader.run JSON: %w", err)
	}

	if len(reader.Sources) == 0 || len(reader.Sources[0].Images) == 0 {
		return nil, fmt.Errorf("Thunderscans: no image sources found in ts_reader.run JSON")
	}

	var images []string
	seen := make(map[string]bool)
	for _, img := range reader.Sources[0].Images {
		img = strings.TrimSpace(img)
		if img == "" || !strings.HasPrefix(img, "http") || seen[img] {
			continue
		}
		seen[img] = true
		images = append(images, img)
	}

	if len(images) == 0 {
		return nil, fmt.Errorf("Thunderscans: no valid image URLs found")
	}

	return images, nil
}

// -------------------------
// Chapter number parsing
// -------------------------

// thunderscansChapterNumber extracts the chapter number (and optional
// fractional part) from a chapter label, data-num attribute, or URL. Returns
// num=-1 when nothing parseable is found.
func thunderscansChapterNumber(s string) (int, string) {
	re := regexp.MustCompile(`(\d+)(?:\.(\d+))?`)
	m := re.FindStringSubmatch(s)
	if len(m) < 2 {
		return -1, ""
	}

	n, err := strconv.Atoi(m[1])
	if err != nil {
		return -1, ""
	}
	return n, m[2]
}

// -------------------------
// Download entrypoint
// -------------------------

func ThunderscansDownloadChapters(ctx context.Context, manga *config.Bookmarks, progressCallback func(string, float64, int, int, int)) error {
	site := &ThunderscansSite{}

	cfg := &downloader.DownloadConfig{
		Manga:            manga,
		Site:             site,
		ProgressCallback: progressCallback,
	}

	manager := downloader.NewManager(cfg)
	return manager.Download(ctx)
}
