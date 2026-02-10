package sites

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"kansho/config"
	"kansho/downloader"
)

type AsuraSite struct{}

var _ downloader.SitePlugin = (*AsuraSite)(nil)

func (a *AsuraSite) GetSiteName() string { return "asurascans" }
func (a *AsuraSite) GetDomain() string   { return "asuracomic.net" }
func (a *AsuraSite) NeedsCFBypass() bool { return true }

func (a *AsuraSite) NormalizeChapterURL(rawURL, _ string) string {
	original := rawURL // keep for debug

	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}

	// Fix double https://https:// cases
	rawURL = strings.TrimPrefix(rawURL, "https://https://")

	if strings.HasPrefix(rawURL, "//") {
		rawURL = "https:" + rawURL
	}

	if strings.HasPrefix(rawURL, "/") {
		rawURL = "https://asuracomic.net" + rawURL
	}

	if !strings.HasPrefix(rawURL, "http") {
		rawURL = "https://asuracomic.net/" + rawURL
	}

	if !strings.HasSuffix(rawURL, "/") {
		rawURL += "/"
	}

	// 🔥 DEBUG LOGGING
	fmt.Printf("[Asura][DEBUG] RAW URL: %s → NORMALIZED: %s\n", original, rawURL)

	return rawURL
}

func (a *AsuraSite) NormalizeChapterFilename(data map[string]string) string {
	url := data["url"]

	re := regexp.MustCompile(`chapter/([\d]+(?:[.-]\d+)?)`)
	matches := re.FindStringSubmatch(url)
	if len(matches) < 2 {
		return "unknown.cbz"
	}

	chNum := matches[1]
	main := chNum
	part := ""

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

func (a *AsuraSite) GetChapterExtractionMethod() *downloader.ChapterExtractionMethod {
	return &downloader.ChapterExtractionMethod{
		Type:         "custom",
		CustomParser: parseAsuraChapters,
	}
}

func (a *AsuraSite) GetImageExtractionMethod() *downloader.ImageExtractionMethod {
	return &downloader.ImageExtractionMethod{
		Type:         "custom",
		CustomParser: parseAsuraImages,
	}
}

func (a *AsuraSite) Debugger() *downloader.Debugger {
	return &downloader.Debugger{
		SaveHTML: false,
		HTMLPath: "asura_debug.html",
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

//
// ---------------- CHAPTER PARSER ----------------
//

var asuraChapterRegex = regexp.MustCompile(`<a[^>]+href="([^"]+/chapter/[\d.-]+/?)"`)

func parseAsuraChapters(html string) (map[string]string, error) {
	// Only match REAL chapter URLs
	re := regexp.MustCompile(`<a[^>]+href="(/series/[^"]+/chapter/[\d.-]+/?)"`)
	matches := re.FindAllStringSubmatch(html, -1)

	result := make(map[string]string)
	site := &AsuraSite{}

	for _, m := range matches {
		rawURL := strings.TrimSpace(m[1])
		fmt.Printf("[Asura][DEBUG] Extracted chapter href: %s\n", rawURL)

		normalized := site.NormalizeChapterURL(rawURL, "")
		fmt.Printf("[Asura][DEBUG] Normalized chapter URL: %s\n", normalized)

		numRe := regexp.MustCompile(`chapter/([\d]+(?:[.-]\d+)?)`)
		n := numRe.FindStringSubmatch(normalized)
		if len(n) < 2 {
			continue
		}

		chNum := n[1]
		main := chNum
		part := ""

		if strings.ContainsAny(chNum, ".-") {
			if strings.Contains(chNum, ".") {
				p := strings.SplitN(chNum, ".", 2)
				main, part = p[0], "."+p[1]
			} else {
				p := strings.SplitN(chNum, "-", 2)
				main, part = p[0], "."+p[1]
			}
		}

		filename := fmt.Sprintf("ch%03s%s.cbz", main, part)
		result[filename] = normalized
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("asura: no chapters found")
	}

	return result, nil
}

//
// ---------------- IMAGE PARSER ----------------
//

// Works with both direct <img> links and Next.js JSON blobs
var asuraImageRegex = regexp.MustCompile(`https://gg\.asuracomic\.net/storage/media/[^"]+\.(?:webp|jpg|png)`)

func parseAsuraImages(html string) ([]string, error) {
	matches := asuraImageRegex.FindAllString(html, -1)
	if len(matches) == 0 {
		return nil, fmt.Errorf("asura: no images found")
	}

	seen := map[string]bool{}
	var result []string

	for _, u := range matches {
		if !seen[u] {
			seen[u] = true
			result = append(result, u)
		}
	}

	return result, nil
}
