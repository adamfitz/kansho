package sites

import (
	"context"
	"encoding/json"
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
//   - Next.js app with React Server Components (RSC) streaming payloads
//   - Fully server-side rendered (no JS rendering needed)
//   - No Cloudflare protection
//   - Chapter data is embedded in <script>self.__next_f.push(...)</script> tags
//     as langChapters arrays with coinPrice/isEarlyAccess metadata
//   - Free chapters have coinPrice=0; premium chapters have coinPrice>0
//   - Chapter image page uses <div id="ch-images"> with lazy-loaded images
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
// The manga series page is a Next.js app with RSC streaming payloads.
// Chapter data lives in langChapters arrays inside __next_f script tags.
// Each chapter has coinPrice (0=free) and isEarlyAccess metadata.
// The parser extracts free chapters (coinPrice=0) from the RSC data,
// falling back to HTML parsing if RSC data is unavailable.
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
// The site is a Next.js app with RSC streaming payloads. Chapter data lives in
// langChapters arrays inside __next_f script tags, structured as:
//
//	{"number":"38","slug":"chapter-38","lang":"en","coinPrice":0,...}
//
// Free chapters have coinPrice=0. Premium/paid chapters have coinPrice>0 and
// are skipped. The parser also deduplicates chapters by number since multiple
// RSC blocks may contain overlapping data.
//
// Falls back to HTML <a> tag parsing if RSC data extraction fails.
func parsePhiliaScansChapters(html string) (map[string]string, error) {
	// Try RSC payload extraction first (preferred — has pricing metadata)
	if result, err := parseRSCChapters(html); err == nil && len(result) > 0 {
		return result, nil
	}

	// Fallback: parse chapter links from HTML
	log.Printf("[PhiliaScans] RSC extraction failed, falling back to HTML parsing")
	return parseHTMLChapters(html)
}

// parseRSCChapters extracts chapter data from React Server Components streaming
// payloads embedded in __next_f script tags. Returns only free chapters (coinPrice=0).
func parseRSCChapters(html string) (map[string]string, error) {
	site := &PhiliaScansSite{}
	result := make(map[string]string)
	seen := make(map[string]bool)

	// Find all __next_f.push script tags
	scriptRe := regexp.MustCompile(`<script>self\.__next_f\.push\(\[1,"(.+?)"\]\)</script>`)

	for _, match := range scriptRe.FindAllStringSubmatch(html, -1) {
		if len(match) < 2 {
			continue
		}

		payload := match[1]

		// Find the langChapters key and use bracket-counting to extract the full array.
		// A simple regex like (.+?)] fails because the array contains nested structures.
		// The payload is inside a JS string, so quotes are escaped: \"langChapters\":[
		startMarker := `\"langChapters\":[`
		idx := strings.Index(payload, startMarker)
		if idx < 0 {
			continue
		}

		// Start scanning after \"langChapters\":[
		start := idx + len(startMarker)
		depth := 1
		pos := start
		for pos < len(payload) && depth > 0 {
			ch := payload[pos]
			// Handle escape sequences (e.g. \" \\ \n) — skip the escaped char
			if ch == '\\' && pos+1 < len(payload) {
				pos += 2
				continue
			}
			if ch == '[' {
				depth++
			} else if ch == ']' {
				depth--
			}
			pos++
		}

		if depth != 0 {
			log.Printf("[PhiliaScans] WARNING: unmatched brackets in langChapters, skipping payload")
			continue
		}

		// payload[start:pos-1] contains the raw langChapters array content
		// (the final ] was consumed by the loop, so pos-1 is past it)
		rawArr := payload[start : pos-1]
		jsonStr := "[" + rawArr + "]"

		// Decode RSC string escapes: \" → ", \\ → \, \u0026 → &
		jsonStr = strings.ReplaceAll(jsonStr, `\"`, `"`)
		jsonStr = strings.ReplaceAll(jsonStr, `\\`, `\`)

		var chapters []struct {
			Number        string `json:"number"`
			Slug          string `json:"slug"`
			Lang          string `json:"lang"`
			CoinPrice     int    `json:"coinPrice"`
			IsEarlyAccess bool   `json:"isEarlyAccess"`
		}

		if err := json.Unmarshal([]byte(jsonStr), &chapters); err != nil {
			log.Printf("[PhiliaScans] Failed to parse langChapters JSON: %v", err)
			continue
		}

		for _, ch := range chapters {
			if ch.Number == "" || ch.Slug == "" {
				continue
			}

			// Skip paid/premium chapters
			if ch.CoinPrice > 0 {
				log.Printf("[PhiliaScans] Skipping paid chapter %s (price=%d coins)", ch.Number, ch.CoinPrice)
				continue
			}

			num := ch.Number
			if seen[num] {
				continue
			}
			seen[num] = true

			url := "https://philiascans.org/read/" + ch.Slug + "?lang=" + ch.Lang
			label := "Chapter " + ch.Number

			data := map[string]string{
				"url":  url,
				"text": label,
			}

			filename := site.NormalizeChapterFilename(data)
			normalizedURL := site.NormalizeChapterURL(url, "")
			result[filename] = normalizedURL
		}

		// If we found chapters in at least one payload, stop searching
		if len(result) > 0 {
			break
		}
	}

	if len(result) > 0 {
		log.Printf("[PhiliaScans] Found %d unique free chapters via RSC", len(result))
		return result, nil
	}

	return nil, fmt.Errorf("PhiliaScans: no langChapters data found in RSC payloads")
}

// parseHTMLChapters is a fallback parser that extracts chapter links from HTML <a> tags.
// Used when RSC payload extraction fails.
func parseHTMLChapters(html string) (map[string]string, error) {
	// Match chapter links that point to /read/ URLs with chapter number in text
	chapterRe := regexp.MustCompile(
		`<a[^>]+href="(/read/[^"]+)"[^>]*>[\s\S]*?<div[^>]+class="chapter-num"[^>]*>\s*Ch\.([\d\.]+)\s*</div>`,
	)

	matches := chapterRe.FindAllStringSubmatch(html, -1)
	if len(matches) == 0 {
		return nil, fmt.Errorf("PhiliaScans: no free chapters found in HTML")
	}

	site := &PhiliaScansSite{}
	result := make(map[string]string)
	seen := make(map[string]bool)

	for _, m := range matches {
		href := strings.TrimSpace(m[1])
		chNum := strings.TrimSpace(m[2])

		if chNum == "" || seen[chNum] {
			continue
		}
		seen[chNum] = true

		url := "https://philiascans.org" + href
		label := "Chapter " + chNum

		data := map[string]string{
			"url":  url,
			"text": label,
		}

		filename := site.NormalizeChapterFilename(data)
		normalizedURL := site.NormalizeChapterURL(url, "")
		result[filename] = normalizedURL
	}

	log.Printf("[PhiliaScans] Found %d unique chapters via HTML fallback", len(result))
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
