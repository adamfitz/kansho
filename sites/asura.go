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

type AsuraSite struct{}

var _ downloader.SitePlugin = (*AsuraSite)(nil)

func (a *AsuraSite) GetSiteName() string { return "asurascans" }
func (a *AsuraSite) GetDomain() string   { return "asurascans.com" }
func (a *AsuraSite) NeedsCFBypass() bool { return true }

func (a *AsuraSite) NormalizeChapterURL(rawURL, _ string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	if strings.HasPrefix(rawURL, "/") {
		rawURL = "https://asurascans.com" + rawURL
	}
	if !strings.HasPrefix(rawURL, "http") {
		rawURL = "https://asurascans.com/" + strings.TrimPrefix(rawURL, "/")
	}
	return rawURL
}

func (a *AsuraSite) NormalizeChapterFilename(data map[string]string) string {
	return asuraChapterFilename(data["number"])
}

func (a *AsuraSite) GetChapterExtractionMethod() *downloader.ChapterExtractionMethod {
	return &downloader.ChapterExtractionMethod{
		Type:         "custom",
		CustomParser: parseAsuraChapters,
	}
}

func (a *AsuraSite) GetImageExtractionMethod() *downloader.ImageExtractionMethod {
	return &downloader.ImageExtractionMethod{
		// No WaitSelector → HTTP path (plain Astro SSR, no JS needed).
		// Image URLs are embedded as JSON in astro-island props in the SSR HTML.
		Type:         "custom",
		WaitSelector: "",
		CustomParser: parseAsuraImages,
	}
}

func (a *AsuraSite) Debugger() *downloader.Debugger {
	return &downloader.Debugger{
		SaveHTML: false,
		HTMLPath: "/tmp/asura_chapter_debug.html",
	}
}

func AsuraDownloadChapters(ctx context.Context, manga *config.Bookmarks, progressCallback func(string, float64, int, int, int)) error {
	cfg := &downloader.DownloadConfig{
		Manga:            manga,
		Site:             &AsuraSite{},
		ProgressCallback: progressCallback,
	}
	return downloader.NewManager(cfg).Download(ctx)
}

// ---------------- CHAPTER PARSER ----------------
//
// The chapter list page (e.g. https://asurascans.com/comics/some-title-slug)
// is an Astro SSR page. The chapter list is serialised as JSON in the
// astro-island props attribute of the <ChapterListReact> component.
//
// The props blob contains a "chapters" key whose value is an Astro-encoded
// array of chapter objects. Each chapter object has at minimum:
//
//	"number"      – integer chapter number (e.g. 93)
//	"series_slug" – the series slug (e.g. "absolute-regression")
//
// Chapter URL pattern:
//
//	https://asurascans.com/comics/{series_slug}/chapter/{number}
//
// We parse the series_slug from the page URL or from the first chapter's
// series_slug field, then build URLs from number + series_slug.
//
// The Astro serialisation wraps every value as [type, value]:
//
//	[0, <scalar>]   → scalar
//	[1, [<items>]]  → array
//
// Rather than importing a full JSON decoder and fighting the nested format,
// we use targeted regexes to pull out the fields we need.

var (
	// Matches the ChapterListReact astro-island component-url attribute and captures its props.
	// Attribute order in HTML: component-url="...ChapterListReact..." ... props="..."
	// (?s) makes . match newlines in case the tag spans multiple lines.
	asuraChapterListPropsRe = regexp.MustCompile(`(?s)component-url="[^"]*ChapterListReact[^"]*"[^>]*props="([^"]+)"`)

	// Extracts each chapter number from the chapters array.
	// Format: &quot;number&quot;:[0,93]
	asuraChapterNumInPropRe = regexp.MustCompile(`&quot;number&quot;:\[0,(\d+)\]`)
)

func parseAsuraChapters(html string) (map[string]string, error) {
	// Find the ChapterListReact props blob
	m := asuraChapterListPropsRe.FindStringSubmatch(html)
	if len(m) < 2 {
		return nil, fmt.Errorf("asura: ChapterListReact props not found in HTML")
	}
	props := m[1]

	// Extract the full series slug from publicUrl (e.g. "absolute-regression-7f873ca6").
	// This includes the hash suffix required by the chapter reader URLs.
	// "seriesSlug" in the props is the bare slug without the hash and must NOT be used
	// for chapter URLs — only publicUrl contains the correct full slug.
	pubRe := regexp.MustCompile(`&quot;publicUrl&quot;:\[0,&quot;/comics/([^&]+)&quot;\]`)
	pm := pubRe.FindStringSubmatch(props)
	if len(pm) < 2 {
		return nil, fmt.Errorf("asura: could not extract publicUrl (full series slug) from props")
	}
	seriesSlug := pm[1]
	log.Printf("[Asura] Series slug: %s", seriesSlug)

	// Extract all chapter numbers
	numMatches := asuraChapterNumInPropRe.FindAllStringSubmatch(props, -1)
	if len(numMatches) == 0 {
		return nil, fmt.Errorf("asura: no chapter numbers found in props")
	}

	result := make(map[string]string)
	seen := make(map[string]bool)

	for _, nm := range numMatches {
		numStr := nm[1]
		filename := asuraChapterFilenameFromInt(numStr)
		if filename == "unknown.cbz" {
			continue
		}
		if seen[filename] {
			continue
		}
		seen[filename] = true

		url := fmt.Sprintf("https://asurascans.com/comics/%s/chapter/%s", seriesSlug, numStr)
		log.Printf("[Asura] Found chapter: %s -> %s", filename, url)
		result[filename] = url
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("asura: no chapters found")
	}
	log.Printf("[Asura] Found %d chapters", len(result))
	return result, nil
}

// ---------------- IMAGE PARSER ----------------
//
// The chapter reader page (e.g. https://asurascans.com/comics/{series}/chapter/{n})
// is also Astro SSR. The page list is in the astro-island props for the reader
// component. It contains a "pages" key – an array of page objects each with a
// "url" field:
//
//	"pages":[1,[[0,{"url":[0,"https://cdn.asurascans.com/asura-images/chapters/38d433f0/92/001.webp"],...}],...]]
//
// We extract every cdn.asurascans.com chapter image URL from the props blob.
// The HTML-entity-encoded props use &quot; for quotes.

var (
	// Captures the full props blob of the ChapterReader astro-island.
	// Attribute order: component-url="...ChapterReader..." ... props="..."
	// (?s) makes . match newlines in case the tag spans multiple lines.
	asuraReaderPropsRe = regexp.MustCompile(`(?s)component-url="[^"]*ChapterReader[^"]*"[^>]*props="([^"]+)"`)

	// Extracts cdn chapter image URLs.
	// Path format changed from hash-based to slug-based:
	//   old: /asura-images/chapters/38d433f0/92/001.webp
	//   new: /asura-images/chapters/absolute-regression/89/001.webp
	// Accept any non-slash path segment in place of the hash.
	asuraCDNImageRe = regexp.MustCompile(`https://cdn\.asurascans\.com/asura-images/chapters/[^/&"]+/\d+/\d+\.\w+`)
)

func parseAsuraImages(html string) ([]string, error) {
	// Find the ChapterReader props blob
	pm := asuraReaderPropsRe.FindStringSubmatch(html)
	if len(pm) < 2 {
		return nil, fmt.Errorf("asura: ChapterReader props not found in HTML")
	}

	// Unescape HTML entities so the URL regex can match
	props := strings.ReplaceAll(pm[1], "&quot;", `"`)
	props = strings.ReplaceAll(props, "&#34;", `"`)

	matches := asuraCDNImageRe.FindAllString(props, -1)
	if len(matches) == 0 {
		// Fallback: scan the full HTML
		log.Printf("[Asura] ChapterReader props gave no image URLs, falling back to full-page scan")
		unescaped := strings.ReplaceAll(html, "&quot;", `"`)
		matches = asuraCDNImageRe.FindAllString(unescaped, -1)
		if len(matches) == 0 {
			return nil, fmt.Errorf("asura: no chapter images found in HTML")
		}
	}

	// Deduplicate preserving order
	seen := make(map[string]bool)
	var urls []string
	for _, u := range matches {
		if seen[u] {
			continue
		}
		seen[u] = true
		urls = append(urls, u)
	}

	log.Printf("[Asura] Found %d images", len(urls))
	return urls, nil
}

// ---------------- HELPERS ----------------

// asuraChapterFilename converts a chapter number string (e.g. "92", "92.5") to
// a zero-padded CBZ filename (e.g. "ch092.cbz", "ch092.5.cbz").
func asuraChapterFilename(numStr string) string {
	if numStr == "" {
		return "unknown.cbz"
	}
	return asuraChapterFilenameFromInt(numStr)
}

func asuraChapterFilenameFromInt(numStr string) string {
	if numStr == "" {
		return "unknown.cbz"
	}
	// Handle decimal chapter numbers like "92.5"
	main, part := numStr, ""
	if idx := strings.IndexByte(numStr, '.'); idx >= 0 {
		main = numStr[:idx]
		part = numStr[idx:]
	}
	return fmt.Sprintf("ch%03s%s.cbz", main, part)
}
