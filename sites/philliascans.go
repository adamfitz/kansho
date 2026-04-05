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

// PhiliaScansSite implements SitePlugin for philiascans.org.
//
// Site characteristics:
//   - Fully server-side rendered (no JS rendering needed)
//   - No Cloudflare protection
//   - Chapter list page contains two identical #free-list divs (desktop + mobile)
//     so deduplication by chapter label is required
//   - Premium chapters link to "#" — they are skipped automatically
//   - Chapter image page uses <img data-src="..."> inside <div id="ch-images">
//   - Images are hosted at /wp-content/uploads/WP-manga/data/...
//   - Last image in every chapter is "9999.webp" (subscribe banner) — filtered out
type PhiliaScansSite struct{}

// Ensure PhiliaScansSite implements SitePlugin
var _ downloader.SitePlugin = (*PhiliaScansSite)(nil)

// -------------------------
// SitePlugin implementation
// -------------------------

func (p *PhiliaScansSite) GetSiteName() string {
	return "philiascans"
}

func (p *PhiliaScansSite) GetDomain() string {
	return "philiascans.org"
}

func (p *PhiliaScansSite) NeedsCFBypass() bool {
	return false
}

func (p *PhiliaScansSite) Debugger() *downloader.Debugger {
	return &downloader.Debugger{
		SaveHTML: false,
		HTMLPath: "philiascans_debug.html",
	}
}

// GetChapterExtractionMethod uses "custom" extraction.
//
// The manga series page is fully SSR. Free chapters live inside:
//
//	<li class="item free-chap" data-chapter="Chapter N">
//	    <a href="https://philiascans.org/series/.../chapter-N/">
//
// The page renders two identical #free-list blocks (desktop + mobile),
// so the parser deduplicates by chapter label before returning.
// Premium chapters have href="#" and are ignored.
func (p *PhiliaScansSite) GetChapterExtractionMethod() *downloader.ChapterExtractionMethod {
	return &downloader.ChapterExtractionMethod{
		Type:         "custom",
		CustomParser: parsePhiliaScansChapters,
	}
}

// GetImageExtractionMethod uses "custom" extraction.
//
// The chapter page is fully SSR. All manga images live inside:
//
//	<div id="ch-images">
//	    <img class="preload-image ... lazyload" data-src="https://philiascans.org/wp-content/uploads/WP-manga/data/...">
//
// Images use lazy-loading via data-src (not src). The final image in
// every chapter is "9999.webp" — a subscribe banner — and is filtered out.
func (p *PhiliaScansSite) GetImageExtractionMethod() *downloader.ImageExtractionMethod {
	return &downloader.ImageExtractionMethod{
		Type:         "custom",
		CustomParser: parsePhiliaScansImages,
	}
}

func (p *PhiliaScansSite) NormalizeChapterURL(rawURL, baseURL string) string {
	if strings.HasPrefix(rawURL, "http://") || strings.HasPrefix(rawURL, "https://") {
		return rawURL
	}
	if strings.HasPrefix(rawURL, "//") {
		return "https:" + rawURL
	}
	if !strings.HasPrefix(rawURL, "/") {
		rawURL = "/" + rawURL
	}
	return "https://philiascans.org" + rawURL
}

// NormalizeChapterFilename converts chapter data to a CBZ filename.
//
// Input data["text"] examples:
//   - "Chapter 33"   → ch033.cbz
//   - "Chapter 18.5" → ch018.5.cbz
//   - "Chapter 1"    → ch001.cbz
func (p *PhiliaScansSite) NormalizeChapterFilename(chapterData map[string]string) string {
	text := chapterData["text"]

	re := regexp.MustCompile(`(?i)Chapter\s+(\d+)(?:\.(\d+))?`)
	matches := re.FindStringSubmatch(text)
	if len(matches) == 0 {
		sanitized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(text), " ", "-"))
		log.Printf("[PhiliaScans] WARNING: Could not parse chapter number from: %q", text)
		return sanitized + ".cbz"
	}

	mainNum := matches[1]
	partNum := ""
	if len(matches) > 2 && matches[2] != "" {
		partNum = matches[2]
	}

	filename := fmt.Sprintf("ch%03s", mainNum)
	if partNum != "" {
		filename += "." + partNum
	}

	log.Printf("[PhiliaScans] Normalized: %q → %s.cbz", text, filename)
	return filename + ".cbz"
}

// -------------------------
// Chapter extraction
// -------------------------

// parsePhiliaScansChapters extracts free chapter links from the series page HTML.
//
// Pattern matched:
//
//	<li class="item free-chap" data-chapter="Chapter N">
//	    <a href="https://philiascans.org/series/.../chapter-N/">
//
// Two identical lists are rendered (desktop + mobile), so we use a seen-map
// to deduplicate by chapter label and keep the first occurrence.
// Chapters with href="#" (premium/locked) are automatically excluded because
// their href does not start with "https://".
func parsePhiliaScansChapters(html string) (map[string]string, error) {
	// Match free chapter <li> blocks: capture data-chapter label and href.
	// The [\s\S]*? between the li open tag and the <a> is non-greedy to avoid
	// crossing into the next list item.
	chapterRe := regexp.MustCompile(
		`<li[^>]+class="[^"]*free-chap[^"]*"[^>]+data-chapter="(Chapter\s+[\d\.]+)"[\s\S]*?<a\s+href="(https://philiascans\.org/series/[^"]+)"`,
	)

	matches := chapterRe.FindAllStringSubmatch(html, -1)
	if len(matches) == 0 {
		return nil, fmt.Errorf("PhiliaScans: no free chapters found in HTML")
	}

	site := &PhiliaScansSite{}
	result := make(map[string]string)
	seen := make(map[string]bool)

	for _, m := range matches {
		label := strings.TrimSpace(m[1]) // e.g. "Chapter 33"
		url := strings.TrimSpace(m[2])   // e.g. "https://philiascans.org/series/.../chapter-33/"

		// Deduplicate: the page renders two identical lists (desktop + mobile)
		if seen[label] {
			continue
		}
		seen[label] = true

		data := map[string]string{
			"url":  url,
			"text": label,
		}

		filename := site.NormalizeChapterFilename(data)
		normalizedURL := site.NormalizeChapterURL(url, "")
		result[filename] = normalizedURL
	}

	log.Printf("[PhiliaScans] Found %d unique free chapters", len(result))
	return result, nil
}

// -------------------------
// Image extraction
// -------------------------

// parsePhiliaScansImages extracts manga page image URLs from a chapter page.
//
// Images are inside <div id="ch-images"> and use lazy-loading:
//
//	<img class="preload-image fit-w y lazyload" data-src="https://philiascans.org/wp-content/uploads/WP-manga/data/...">
//
// The final image in every chapter is "9999.webp" — a subscribe/promo banner —
// and is excluded from the output.
func parsePhiliaScansImages(html string) ([]string, error) {
	// Isolate the #ch-images div to avoid accidentally picking up thumbnail
	// images from the navigation or sidebar.
	chImagesRe := regexp.MustCompile(`(?s)<div[^>]+id="ch-images"[^>]*>(.*?)</div>\s*</div>\s*</div>\s*<footer`)
	sectionMatch := chImagesRe.FindStringSubmatch(html)

	searchHTML := html // fallback: search full page if section not found
	if len(sectionMatch) >= 2 {
		searchHTML = sectionMatch[1]
		log.Printf("[PhiliaScans] Isolated #ch-images section (%d bytes)", len(searchHTML))
	} else {
		log.Printf("[PhiliaScans] WARNING: Could not isolate #ch-images — searching full page")
	}

	// Match data-src on lazy-loaded manga images
	imgRe := regexp.MustCompile(`<img[^>]+data-src="(https://philiascans\.org/wp-content/uploads/WP-manga/[^"]+)"[^>]*>`)
	imgMatches := imgRe.FindAllStringSubmatch(searchHTML, -1)

	if len(imgMatches) == 0 {
		return nil, fmt.Errorf("PhiliaScans: no chapter images found in HTML")
	}

	seen := make(map[string]bool)
	var images []string

	for _, m := range imgMatches {
		url := strings.TrimSpace(m[1])

		// Filter out the 9999.webp sentinel (subscribe/promo banner)
		if strings.HasSuffix(url, "/9999.webp") {
			log.Printf("[PhiliaScans] Skipping sentinel image: %s", url)
			continue
		}

		// Deduplicate (shouldn't occur but be safe)
		if seen[url] {
			continue
		}
		seen[url] = true

		images = append(images, url)
	}

	if len(images) == 0 {
		return nil, fmt.Errorf("PhiliaScans: no usable images found after filtering")
	}

	log.Printf("[PhiliaScans] Found %d chapter images", len(images))
	return images, nil
}

// -------------------------
// Download entrypoint
// -------------------------

// PhiliaScansDownloadChapters is the public entry point called by the queue/UI layer.
func PhiliaScansDownloadChapters(ctx context.Context, manga *config.Bookmarks, progressCallback func(string, float64, int, int, int)) error {
	site := &PhiliaScansSite{}

	cfg := &downloader.DownloadConfig{
		Manga:            manga,
		Site:             site,
		ProgressCallback: progressCallback,
	}

	manager := downloader.NewManager(cfg)
	return manager.Download(ctx)
}
