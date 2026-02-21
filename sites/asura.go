package sites

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"

	"kansho/config"
	"kansho/downloader"

	"github.com/PuerkitoBio/goquery"
)

type AsuraSite struct{}

var _ downloader.SitePlugin = (*AsuraSite)(nil)

func (a *AsuraSite) GetSiteName() string { return "asurascans" }
func (a *AsuraSite) GetDomain() string   { return "asuracomic.net" }
func (a *AsuraSite) NeedsCFBypass() bool { return true }

func (a *AsuraSite) NormalizeChapterURL(rawURL, _ string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	if !strings.HasPrefix(rawURL, "http") {
		rawURL = "https://asuracomic.net/series/" + strings.TrimPrefix(rawURL, "/")
	}
	if !strings.HasSuffix(rawURL, "/") {
		rawURL += "/"
	}
	return rawURL
}

func (a *AsuraSite) NormalizeChapterFilename(data map[string]string) string {
	return asuraChapterFilename(data["url"])
}

func (a *AsuraSite) GetChapterExtractionMethod() *downloader.ChapterExtractionMethod {
	return &downloader.ChapterExtractionMethod{
		Type:         "custom",
		CustomParser: parseAsuraChapters,
	}
}

func (a *AsuraSite) GetImageExtractionMethod() *downloader.ImageExtractionMethod {
	return &downloader.ImageExtractionMethod{
		// No WaitSelector → HTTP path, no browser needed.
		// Image URLs are embedded in <script> tags in the raw HTTP response.
		// parseAsuraImages finds the script tag whose URLs all share the most
		// consecutive media IDs — that script contains only the chapter images.
		Type:         "custom",
		WaitSelector: "",
		CustomParser: parseAsuraImages,
	}
}

func (a *AsuraSite) Debugger() *downloader.Debugger {
	return &downloader.Debugger{
		// Set SaveHTML: true to save the raw HTTP response for inspection.
		// Useful when the image URL regex stops matching — inspect the saved
		// file and update asuraImageRe accordingly.
		SaveHTML: false,
		HTMLPath: "/tmp/asura_debug.html",
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

func parseAsuraChapters(html string) (map[string]string, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader([]byte(html)))
	if err != nil {
		return nil, fmt.Errorf("asura: failed to parse HTML: %w", err)
	}

	seen := make(map[string]struct{})
	result := make(map[string]string)

	doc.Find("a").Each(func(i int, s *goquery.Selection) {
		href, exists := s.Attr("href")
		if !exists || href == "" {
			return
		}
		href = strings.TrimSpace(href)
		if !strings.Contains(href, "/chapter/") {
			return
		}
		if !strings.HasPrefix(href, "http") {
			href = "https://asuracomic.net/series/" + strings.TrimPrefix(href, "/")
		}
		if _, already := seen[href]; already {
			return
		}
		seen[href] = struct{}{}

		filename := asuraChapterFilename(href)
		if filename == "unknown.cbz" {
			log.Printf("[Asura] Could not extract chapter number from: %s", href)
			return
		}
		log.Printf("[Asura] Found chapter: %s -> %s", filename, href)
		result[filename] = href
	})

	if len(result) == 0 {
		return nil, fmt.Errorf("asura: no chapters found")
	}
	log.Printf("[Asura] Found %d chapters", len(result))
	return result, nil
}

// ---------------- IMAGE PARSER ----------------
// Asura's Next.js page embeds image URLs in <script> tags in the raw HTTP response.
// Multiple script tags contain image URLs: some are chapter pages, others are
// thumbnails/ads for other series. We find the script whose URLs form the most
// consecutive block of media IDs — that is the chapter content script.
//
// URL format (ULID filenames since early 2025):
//   https://gg.asuracomic.net/storage/media/419492/conversions/01KHXWQ1WJE4VCYD23HSM4BHBV-optimized.webp
//   ↑ CDN host                              ↑ media ID (numeric, sequential per chapter)
//
// If this stops working, set Debugger.SaveHTML = true, run once, inspect
// /tmp/asura_debug.html and update asuraImageRe to match the new URL pattern.

var (
	// Matches any optimized image URL regardless of filename format (ULID, UUID, numeric)
	asuraImageRe = regexp.MustCompile(`https://gg\.asuracomic\.net/storage/media/([0-9]+)/conversions/[0-9A-Za-z-]+-optimized\.(?:webp|jpg|png)`)

	// Extracts all <script> tag contents
	asuraScriptRe = regexp.MustCompile(`(?s)<script[^>]*>(.*?)</script>`)
)

func parseAsuraImages(html string) ([]string, error) {
	scripts := asuraScriptRe.FindAllStringSubmatch(html, -1)
	log.Printf("[Asura] Found %d script tags", len(scripts))

	type scriptResult struct {
		urls     []string // deduplicated, order-preserved
		mediaIDs []int    // sorted numeric media IDs
		maxGap   int      // largest gap between consecutive sorted media IDs
	}

	var best *scriptResult

	for i, m := range scripts {
		script := m[1]
		matches := asuraImageRe.FindAllStringSubmatch(script, -1)
		if len(matches) == 0 {
			continue
		}

		// Deduplicate preserving order, collect media IDs
		seen := make(map[string]bool)
		var urls []string
		var ids []int
		idSeen := make(map[int]bool)

		for _, match := range matches {
			url := match[0]
			if seen[url] {
				continue
			}
			seen[url] = true
			urls = append(urls, url)

			// Parse the numeric media ID
			var id int
			fmt.Sscanf(match[1], "%d", &id)
			if !idSeen[id] {
				idSeen[id] = true
				ids = append(ids, id)
			}
		}

		sort.Ints(ids)

		// Calculate the largest gap between consecutive media IDs
		maxGap := 0
		for j := 1; j < len(ids); j++ {
			gap := ids[j] - ids[j-1]
			if gap > maxGap {
				maxGap = gap
			}
		}

		log.Printf("[Asura] Script %d: %d unique URLs, media ID range %d-%d, max gap %d",
			i, len(urls), ids[0], ids[len(ids)-1], maxGap)

		// Pick the script with the smallest max gap (most consecutive IDs).
		// Ties broken by most URLs.
		if best == nil ||
			maxGap < best.maxGap ||
			(maxGap == best.maxGap && len(urls) > len(best.urls)) {
			best = &scriptResult{urls: urls, mediaIDs: ids, maxGap: maxGap}
		}
	}

	if best == nil || len(best.urls) == 0 {
		return nil, fmt.Errorf("asura: no images found in HTML (CDN pattern not matched)")
	}

	log.Printf("[Asura] Selected script with %d images (max consecutive gap: %d)", len(best.urls), best.maxGap)
	return best.urls, nil
}

// ---------------- HELPERS ----------------

var asuraChapterNumRe = regexp.MustCompile(`chapter/([\d]+(?:[.-]\d+)?)`)

func asuraChapterFilename(url string) string {
	m := asuraChapterNumRe.FindStringSubmatch(url)
	if len(m) < 2 {
		return "unknown.cbz"
	}
	chNum := m[1]
	main, part := chNum, ""
	if strings.ContainsAny(chNum, ".-") {
		if strings.Contains(chNum, ".") {
			p := strings.SplitN(chNum, ".", 2)
			main, part = p[0], "."+p[1]
		} else {
			p := strings.SplitN(chNum, "-", 2)
			main, part = p[0], "."+p[1]
		}
	}
	return fmt.Sprintf("ch%03s%s.cbz", main, part)
}
