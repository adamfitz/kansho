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

// KingOfShojoSite implements SitePlugin for kingofshojo.com.
//
// Site characteristics:
//   - WordPress "mangareader" theme. The manga page's full chapter list is
//     server-rendered inside <div class="eplister" id="chapterlist"><ul
//     class="clstyle">, one <li data-num="..."> per chapter, so chapters are
//     extracted with a plain HTML parser — no browser needed.
//   - Each chapter page embeds the page list as a JSON object inside
//     ts_reader.run({...}); so images are also extracted from the static HTML.
//   - Images are served from cdn.kingofshojo.com (Cloudflare fronted) without
//     requiring a Referer or CF clearance cookie, so the plain HTTP image
//     downloader is sufficient.
type KingOfShojoSite struct{}

// Ensure KingOfShojoSite implements SitePlugin
var _ downloader.SitePlugin = (*KingOfShojoSite)(nil)

// -------------------------
// SitePlugin implementation
// -------------------------

func (s *KingOfShojoSite) GetSiteName() string {
	return "kingofshojo"
}

func (s *KingOfShojoSite) GetDomain() string {
	return "kingofshojo.com"
}

func (s *KingOfShojoSite) NeedsCFBypass() bool {
	return false
}

func (s *KingOfShojoSite) Debugger() *downloader.Debugger {
	return &downloader.Debugger{
		SaveHTML: false,
		HTMLPath: "kingofshojo_debug.html",
	}
}

// GetChapterExtractionMethod uses "custom" extraction: the chapter list is
// server-rendered in the manga page HTML, so no browser execution is needed.
func (s *KingOfShojoSite) GetChapterExtractionMethod() *downloader.ChapterExtractionMethod {
	return &downloader.ChapterExtractionMethod{
		Type:         "custom",
		CustomParser: parseKingOfShojoChapters,
	}
}

// GetImageExtractionMethod uses "custom" extraction: the page images are
// embedded in the ts_reader.run(...) JSON in the static chapter page HTML.
func (s *KingOfShojoSite) GetImageExtractionMethod() *downloader.ImageExtractionMethod {
	return &downloader.ImageExtractionMethod{
		Type:         "custom",
		CustomParser: parseKingOfShojoImages,
	}
}

func (s *KingOfShojoSite) NormalizeChapterURL(rawURL, baseURL string) string {
	if strings.HasPrefix(rawURL, "http://") || strings.HasPrefix(rawURL, "https://") {
		return rawURL
	}
	if strings.HasPrefix(rawURL, "//") {
		return "https:" + rawURL
	}
	if !strings.HasPrefix(rawURL, "/") {
		rawURL = "/" + rawURL
	}
	return "https://kingofshojo.com" + rawURL
}

func (s *KingOfShojoSite) NormalizeChapterFilename(data map[string]string) string {
	raw := data["num"]
	if raw == "" {
		raw = data["text"]
	}
	if raw == "" {
		raw = data["url"]
	}

	num, frac := kingOfShojoChapterNumber(raw)
	if num < 0 {
		sanitized := strings.ToLower(strings.TrimSpace(raw))
		sanitized = regexp.MustCompile(`[^a-z0-9.]+`).ReplaceAllString(sanitized, "-")
		log.Printf("[KingOfShojo] WARNING: could not parse chapter number from %q", raw)
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

// parseKingOfShojoChapters extracts the chapter list from the manga page HTML.
// Returns a map of chapter filename -> chapter URL.
func parseKingOfShojoChapters(html string) (map[string]string, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader([]byte(html)))
	if err != nil {
		return nil, fmt.Errorf("KingOfShojo: failed to parse chapter list HTML: %w", err)
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

		filename := (&KingOfShojoSite{}).NormalizeChapterFilename(data)
		url := (&KingOfShojoSite{}).NormalizeChapterURL(href, "")
		result[filename] = url
	})

	if len(result) == 0 {
		return nil, fmt.Errorf("KingOfShojo: no chapters found in chapter list")
	}

	return result, nil
}

// -------------------------
// Image parsing
// -------------------------

// kingOfShojoReader mirrors the JSON object passed to ts_reader.run({...}).
type kingOfShojoReader struct {
	Sources []struct {
		Source string   `json:"source"`
		Images []string `json:"images"`
	} `json:"sources"`
}

// parseKingOfShojoImages extracts image URLs from the ts_reader.run({...})
// JSON embedded in the chapter page HTML.
func parseKingOfShojoImages(html string) ([]string, error) {
	re := regexp.MustCompile(`(?s)ts_reader\.run\((\{.*?\})\);`)
	m := re.FindStringSubmatch(html)
	if len(m) < 2 {
		return nil, fmt.Errorf("KingOfShojo: ts_reader.run JSON not found in chapter HTML")
	}

	var reader kingOfShojoReader
	if err := json.Unmarshal([]byte(m[1]), &reader); err != nil {
		return nil, fmt.Errorf("KingOfShojo: failed to parse ts_reader.run JSON: %w", err)
	}

	if len(reader.Sources) == 0 || len(reader.Sources[0].Images) == 0 {
		return nil, fmt.Errorf("KingOfShojo: no image sources found in ts_reader.run JSON")
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
		return nil, fmt.Errorf("KingOfShojo: no valid image URLs found")
	}

	return images, nil
}

// -------------------------
// Chapter number parsing
// -------------------------

// kingOfShojoChapterNumber extracts the chapter number (and optional fractional
// part) from a chapter label, data-num attribute, or URL. Returns num=-1 when
// nothing parseable is found.
func kingOfShojoChapterNumber(s string) (int, string) {
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

func KingOfShojoDownloadChapters(ctx context.Context, manga *config.Bookmarks, progressCallback func(string, float64, int, int, int)) error {
	site := &KingOfShojoSite{}

	cfg := &downloader.DownloadConfig{
		Manga:            manga,
		Site:             site,
		ProgressCallback: progressCallback,
	}

	manager := downloader.NewManager(cfg)
	return manager.Download(ctx)
}
