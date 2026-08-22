package sites

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"

	"kansho/config"
	"kansho/downloader"

	"github.com/PuerkitoBio/goquery"
)

// arenascanSite implements SitePlugin for arenascan.com (served from
// arenascan.com).
//
// Site characteristics (see openspec/specs/site-plugin-system/spec.md):
//   - WordPress "mangareader" theme. The manga page (/manga/{slug}/) contains
//     the FULL chapter list server-rendered inside <div class="eplister"
//     id="chapterlist"><ul class="clstyle">, one <li data-num="..."> per
//     chapter. The paginated category pages (/category/{slug}/page/N/) are
//     never needed — always fetch the /manga/ URL.
//   - Each chapter page serves the page images inside <div id="readerarea">.
//     In the static HTML the images live inside a <noscript> fallback block;
//     x/net/html stores noscript content as a single raw-text node, so the
//     inner HTML must be re-parsed to recover the <img> elements. When the
//     page is browser-rendered the images are direct children instead.
//   - Images are served from cdn.arenascan.com without requiring a Referer
//     header or CF clearance cookie, so the plain HTTP image downloader is
//     sufficient.
type ArenascanSite struct{}

// Ensure arenascanSite implements SitePlugin
var _ downloader.SitePlugin = (*ArenascanSite)(nil)

// -------------------------
// SitePlugin implementation
// -------------------------

func (s *ArenascanSite) GetSiteName() string {
	return "arenascan"
}

func (s *ArenascanSite) GetDomain() string {
	return "arenascan.com"
}

func (s *ArenascanSite) NeedsCFBypass() bool {
	return false
}

func (s *ArenascanSite) Debugger() *downloader.Debugger {
	return &downloader.Debugger{
		SaveHTML: false,
		HTMLPath: "arenascan_debug.html",
	}
}

// GetChapterExtractionMethod uses "custom" extraction: the full chapter list
// is server-rendered in the manga page HTML, so no browser execution is
// needed.
func (s *ArenascanSite) GetChapterExtractionMethod() *downloader.ChapterExtractionMethod {
	return &downloader.ChapterExtractionMethod{
		Type:         "custom",
		CustomParser: parsearenascanChapters,
	}
}

// GetImageExtractionMethod uses "custom" extraction: the page images are
// embedded in the static chapter page HTML (#readerarea, possibly inside a
// <noscript> fallback).
func (s *ArenascanSite) GetImageExtractionMethod() *downloader.ImageExtractionMethod {
	return &downloader.ImageExtractionMethod{
		Type:         "custom",
		CustomParser: parsearenascanImages,
	}
}

func (s *ArenascanSite) NormalizeChapterURL(rawURL, baseURL string) string {
	if strings.HasPrefix(rawURL, "http://") || strings.HasPrefix(rawURL, "https://") {
		return rawURL
	}
	if strings.HasPrefix(rawURL, "//") {
		return "https:" + rawURL
	}
	if !strings.HasPrefix(rawURL, "/") {
		rawURL = "/" + rawURL
	}
	return "https://arenascan.com" + rawURL
}

func (s *ArenascanSite) NormalizeChapterFilename(data map[string]string) string {
	raw := data["num"]
	if raw == "" {
		raw = data["text"]
	}
	if raw == "" {
		raw = data["url"]
	}

	num, frac := arenascanChapterNumber(raw)
	if num < 0 {
		sanitized := strings.ToLower(strings.TrimSpace(raw))
		sanitized = regexp.MustCompile(`[^a-z0-9.]+`).ReplaceAllString(sanitized, "-")
		log.Printf("[arenascan] WARNING: could not parse chapter number from %q", raw)
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

// parsearenascanChapters extracts the chapter list from the manga page HTML.
// Returns a map of chapter filename -> chapter URL.
func parsearenascanChapters(html string) (map[string]string, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader([]byte(html)))
	if err != nil {
		return nil, fmt.Errorf("arenascan: failed to parse chapter list HTML: %w", err)
	}

	result := make(map[string]string)
	doc.Find("#chapterlist li").Each(func(_ int, li *goquery.Selection) {
		href, exists := li.Find("a").Attr("href")
		if !exists {
			return
		}

		data := map[string]string{
			"url":  href,
			"text": li.Find(".chapternum").First().Text(),
			"num":  li.AttrOr("data-num", ""),
		}

		filename := (&ArenascanSite{}).NormalizeChapterFilename(data)
		url := (&ArenascanSite{}).NormalizeChapterURL(href, "")
		result[filename] = url
	})

	if len(result) == 0 {
		return nil, fmt.Errorf("arenascan: no chapters found in #chapterlist")
	}

	return result, nil
}

// -------------------------
// Image parsing
// -------------------------

// collectarenascanImages appends valid image URLs from the given selection to
// dst, preserving order and skipping duplicates.
func collectarenascanImages(dst []string, seen map[string]bool, sel *goquery.Selection) []string {
	sel.Find("img[src]").Each(func(_ int, img *goquery.Selection) {
		src := strings.TrimSpace(img.AttrOr("src", ""))
		if src == "" || !strings.HasPrefix(src, "http") || seen[src] {
			return
		}
		seen[src] = true
		dst = append(dst, src)
	})
	return dst
}

// parsearenascanImages extracts image URLs from the #readerarea element of a
// chapter page.
//
// Two shapes are handled:
//  1. Browser-rendered pages: <img> elements are direct descendants of
//     #readerarea.
//  2. Static HTML (HTTP fetch): the images are inside a <noscript> fallback.
//     x/net/html keeps noscript content as one raw-text node, so its text is
//     re-parsed as an HTML fragment to extract the <img> elements in order.
func parsearenascanImages(html string) ([]string, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader([]byte(html)))
	if err != nil {
		return nil, fmt.Errorf("arenascan: failed to parse chapter HTML: %w", err)
	}

	readerarea := doc.Find("#readerarea").First()
	if readerarea.Length() == 0 {
		return nil, fmt.Errorf("arenascan: #readerarea not found in chapter HTML")
	}

	seen := make(map[string]bool)
	var images []string
	images = collectarenascanImages(images, seen, readerarea)

	if len(images) == 0 {
		// Noscript content is a raw-text node under x/net/html — re-parse it.
		noscriptHTML := readerarea.Find("noscript").First().Text()
		if noscriptHTML != "" {
			subDoc, subErr := goquery.NewDocumentFromReader(strings.NewReader(noscriptHTML))
			if subErr == nil {
				images = collectarenascanImages(images, seen, subDoc.Selection)
			}
		}
	}

	if len(images) == 0 {
		return nil, fmt.Errorf("arenascan: no image URLs found in #readerarea")
	}

	log.Printf("[arenascan] Found %d images", len(images))
	return images, nil
}

// -------------------------
// Chapter number parsing
// -------------------------

// arenascanChapterNumber extracts the chapter number (and optional fractional
// part) from a chapter label, data-num attribute, or URL. Returns num=-1 when
// nothing parseable is found.
func arenascanChapterNumber(s string) (int, string) {
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

func ArenascanDownloadChapters(ctx context.Context, manga *config.Bookmarks, progressCallback func(string, float64, int, int, int)) error {
	site := &ArenascanSite{}

	cfg := &downloader.DownloadConfig{
		Manga:            manga,
		Site:             site,
		ProgressCallback: progressCallback,
	}

	manager := downloader.NewManager(cfg)
	return manager.Download(ctx)
}
