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

type WeebcentralSite struct{}

// Ensure WeebcentralSite implements SitePlugin
var _ downloader.SitePlugin = (*WeebcentralSite)(nil)

// -------------------------
// SitePlugin implementation
// -------------------------

func (w *WeebcentralSite) GetSiteName() string {
	return "weebcentral"
}

func (w *WeebcentralSite) GetDomain() string {
	return "weebcentral.com"
}

func (w *WeebcentralSite) NeedsCFBypass() bool {
	return true
}

func (w *WeebcentralSite) Debugger() *downloader.Debugger {
	return &downloader.Debugger{
		SaveHTML: false,
		HTMLPath: "weebcentral_debug.html",
	}
}

// GetChapterExtractionMethod uses the "full-chapter-list" HTMX endpoint which
// returns plain HTML containing every chapter link — no JavaScript needed.
//
// The series page only shows a handful of recent chapters, but includes a
// "Show All Chapters" button whose hx-get points to:
//
//	https://weebcentral.com/series/{ID}/full-chapter-list
//
// We derive this URL from the manga URL and fetch it directly with HTTP.
// The response is plain HTML with <a href="/chapters/...">Chapter N</a> links.
func (w *WeebcentralSite) GetChapterExtractionMethod() *downloader.ChapterExtractionMethod {
	return &downloader.ChapterExtractionMethod{
		Type:         "custom",
		CustomParser: parseWeebcentralChapters,
	}
}

// GetImageExtractionMethod fetches the HTMX images endpoint directly.
// The chapter page loads images via:
//
//	hx-get="/chapters/{ID}/images?is_prev=False&current_page=1"
//	hx-include="[name='reading_style']"
//
// The server requires reading_style to be present or it returns 400.
// We include reading_style=long_strip in the URL to get all images at once.
func (w *WeebcentralSite) GetImageExtractionMethod() *downloader.ImageExtractionMethod {
	return &downloader.ImageExtractionMethod{
		Type:         "custom",
		CustomParser: parseWeebcentralImages,
	}
}

func (w *WeebcentralSite) NormalizeChapterURL(rawURL, baseURL string) string {
	if strings.HasPrefix(rawURL, "http://") || strings.HasPrefix(rawURL, "https://") {
		return rawURL
	}
	if strings.HasPrefix(rawURL, "//") {
		return "https:" + rawURL
	}
	if !strings.HasPrefix(rawURL, "/") {
		rawURL = "/" + rawURL
	}
	return "https://weebcentral.com" + rawURL
}

func (w *WeebcentralSite) NormalizeChapterFilename(data map[string]string) string {
	text := data["text"]

	re := regexp.MustCompile(`(?i)(?:Episode|Chapter)\s+(\d+)(?:\.(\d+))?`)
	matches := re.FindStringSubmatch(text)
	if len(matches) == 0 {
		sanitized := strings.ToLower(strings.ReplaceAll(text, " ", "-"))
		log.Printf("[WeebCentral] WARNING: Could not parse chapter number from: %s", text)
		return fmt.Sprintf("%s.cbz", sanitized)
	}

	mainNum := matches[1]
	partNum := ""
	if len(matches) > 2 && matches[2] != "" {
		partNum = matches[2]
	}

	// Use "ch" for both chapters and episodes to keep filenames consistent
	prefix := "ch"
	// if strings.Contains(strings.ToLower(text), "episode") {
	// 	prefix = "ep"
	// }

	filename := fmt.Sprintf("%s%03s", prefix, mainNum)
	if partNum != "" {
		filename += "." + partNum
	}

	log.Printf("[WeebCentral] Normalized: %s → %s.cbz", text, filename)
	return filename + ".cbz"
}

// -------------------------
// Chapter extraction
// -------------------------

// parseWeebcentralChapters fetches the full chapter list endpoint directly.
// It derives the full-chapter-list URL from the series page HTML which contains:
//
//	hx-get="https://weebcentral.com/series/{ID}/full-chapter-list"
//
// That endpoint returns a plain HTML fragment with all chapter <a> links.
func parseWeebcentralChapters(html string) (map[string]string, error) {
	// Find the full-chapter-list endpoint URL from the "Show All Chapters" button
	endpointRe := regexp.MustCompile(`hx-get="(https://weebcentral\.com/series/[^"]+/full-chapter-list[^"]*)"`)
	matches := endpointRe.FindStringSubmatch(html)

	var fullListURL string
	if len(matches) >= 2 {
		fullListURL = matches[1]
		log.Printf("[WeebCentral] Found full-chapter-list endpoint: %s", fullListURL)
	} else {
		// Fallback: parse chapters directly from the current page HTML
		log.Printf("[WeebCentral] No full-chapter-list button found, parsing chapters from current page")
		return extractChapterLinks(html)
	}

	// Fetch the full chapter list HTML directly
	exec, err := downloader.NewRequestExecutor(fullListURL, true, nil)
	if err != nil {
		return nil, fmt.Errorf("WeebCentral: failed to create executor for chapter list: %w", err)
	}

	ctx := context.Background()
	fullListHTML, err := exec.FetchHTML(ctx, fullListURL, "")
	if err != nil {
		return nil, fmt.Errorf("WeebCentral: failed to fetch full chapter list: %w", err)
	}

	return extractChapterLinks(fullListHTML)
}

// extractChapterLinks parses <a href="/chapters/...">Chapter N</a> links from HTML.
func extractChapterLinks(html string) (map[string]string, error) {
	// Match chapter links: <a href="https://weebcentral.com/chapters/...">
	// The chapter title is in a <span> inside: <span class="">Chapter N</span>
	linkRe := regexp.MustCompile(`<a\s+href="(https://weebcentral\.com/chapters/[^"]+)"[^>]*>[\s\S]*?<span[^>]*>\s*((?:Chapter|Episode)\s+\d+(?:\.\d+)?)\s*</span>`)
	matches := linkRe.FindAllStringSubmatch(html, -1)

	if len(matches) == 0 {
		// Simpler fallback: just find all /chapters/ links and nearby text
		return extractChapterLinksSimple(html)
	}

	result := make(map[string]string)
	for _, m := range matches {
		url := m[1]
		text := strings.TrimSpace(m[2])
		if url == "" || text == "" {
			continue
		}

		data := map[string]string{"url": url, "text": text}
		site := &WeebcentralSite{}
		filename := site.NormalizeChapterFilename(data)
		result[filename] = url
	}

	if len(result) == 0 {
		return extractChapterLinksSimple(html)
	}

	log.Printf("[WeebCentral] Found %d chapters", len(result))
	return result, nil
}

// extractChapterLinksSimple is a broader fallback that finds chapter hrefs
// then looks for "Chapter N" text nearby in the same anchor tag.
func extractChapterLinksSimple(html string) (map[string]string, error) {
	// Find anchors to /chapters/ and grab surrounding text for the chapter number
	anchorRe := regexp.MustCompile(`<a\s[^>]*href="(https://weebcentral\.com/chapters/[^"]+)"[^>]*>([\s\S]*?)</a>`)
	chNumRe := regexp.MustCompile(`(?i)((?:Chapter|Episode)\s+\d+(?:\.\d+)?)`)

	result := make(map[string]string)
	for _, m := range anchorRe.FindAllStringSubmatch(html, -1) {
		url := m[1]
		inner := m[2]

		numMatch := chNumRe.FindStringSubmatch(inner)
		if len(numMatch) < 2 {
			continue
		}
		text := strings.TrimSpace(numMatch[1])

		data := map[string]string{"url": url, "text": text}
		site := &WeebcentralSite{}
		filename := site.NormalizeChapterFilename(data)
		if _, exists := result[filename]; !exists {
			result[filename] = url
		}
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("WeebCentral: no chapters found in HTML")
	}

	log.Printf("[WeebCentral] Found %d chapters (simple parser)", len(result))
	return result, nil
}

// -------------------------
// Image extraction
// -------------------------

// parseWeebcentralImages extracts the HTMX images endpoint from the chapter page,
// then fetches it with reading_style=long_strip to get all image tags at once.
//
// The chapter page contains:
//
//	hx-get="/chapters/{ID}/images?is_prev=False&current_page=1"
//
// The server requires a reading_style parameter (missing = 400 Bad Request).
// We append reading_style=long_strip which returns all images in a single response.
func parseWeebcentralImages(html string) ([]string, error) {
	// Extract the images HTMX endpoint from the chapter page
	endpointRe := regexp.MustCompile(`hx-get="(https://weebcentral\.com/chapters/[^"]+/images\?[^"]+)"`)
	matches := endpointRe.FindStringSubmatch(html)

	if len(matches) < 2 {
		// Also try relative URLs
		relRe := regexp.MustCompile(`hx-get="(/chapters/[^"]+/images\?[^"]+)"`)
		relMatches := relRe.FindStringSubmatch(html)
		if len(relMatches) < 2 {
			return nil, fmt.Errorf("WeebCentral: no images endpoint found in chapter page")
		}
		matches = []string{relMatches[0], "https://weebcentral.com" + relMatches[1]}
	}

	imagesURL := strings.ReplaceAll(matches[1], "&amp;", "&")

	// Append reading_style=long_strip so the server returns all images
	if !strings.Contains(imagesURL, "reading_style=") {
		imagesURL += "&reading_style=long_strip"
	}

	log.Printf("[WeebCentral] Fetching images from: %s", imagesURL)

	exec, err := downloader.NewRequestExecutor(imagesURL, true, nil)
	if err != nil {
		return nil, fmt.Errorf("WeebCentral: failed to create executor for images: %w", err)
	}

	ctx := context.Background()
	imagesHTML, err := exec.FetchHTML(ctx, imagesURL, "")
	if err != nil {
		return nil, fmt.Errorf("WeebCentral: failed to fetch images: %w", err)
	}

	return extractImageURLs(imagesHTML)
}

// extractImageURLs pulls image src URLs from the images endpoint response.
func extractImageURLs(html string) ([]string, error) {
	imgRe := regexp.MustCompile(`<img[^>]+src="([^"]+)"[^>]*>`)
	seen := make(map[string]bool)
	var images []string

	for _, m := range imgRe.FindAllStringSubmatch(html, -1) {
		url := m[1]
		if !strings.HasPrefix(url, "http") {
			continue
		}
		// Skip icons/logos/UI elements
		if strings.Contains(url, "icon") || strings.Contains(url, "logo") ||
			strings.Contains(url, "brand") || strings.Contains(url, "static/") {
			continue
		}
		if seen[url] {
			continue
		}
		seen[url] = true
		images = append(images, url)
	}

	if len(images) == 0 {
		return nil, fmt.Errorf("WeebCentral: no images found in response")
	}

	log.Printf("[WeebCentral] Found %d images", len(images))
	return images, nil
}

// -------------------------
// Download entrypoint
// -------------------------

func WeebcentralDownloadChapters(ctx context.Context, manga *config.Bookmarks, progressCallback func(string, float64, int, int, int)) error {
	site := &WeebcentralSite{}

	cfg := &downloader.DownloadConfig{
		Manga:            manga,
		Site:             site,
		ProgressCallback: progressCallback,
	}

	manager := downloader.NewManager(cfg)
	return manager.Download(ctx)
}
