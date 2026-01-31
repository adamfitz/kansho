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

// enable debugging to save HTML files
func (w *WeebcentralSite) Debugger() *downloader.Debugger {
	return &downloader.Debugger{
		SaveHTML: true,
		HTMLPath: "weebcentral_debug.html",
	}
}

func (w *WeebcentralSite) GetChapterExtractionMethod() *downloader.ChapterExtractionMethod {
	// JS does:
	// 1) Try to find a "Show All Chapters" button.
	// 2) If found, read its hx-get/data-hx-get, build absolute URL, do a *synchronous* XHR.
	// 3) Parse the returned HTML in a detached div and collect all /chapters/ links.
	// 4) If that fails or button not found, fall back to scraping current document.
	js := `(() => {
        function collectChapters(root) {
            var out = [];
            var links = root.querySelectorAll('a[href*="/chapters/"]');
            for (var i = 0; i < links.length; i++) {
                var a = links[i];
                var text = (a.textContent || "").trim();
                if (!text) continue;
                if (!/^(Chapter|Episode)\s+\d+(\.\d+)?/i.test(text)) continue;
                var href = a.getAttribute("href") || "";
                if (!href) continue;
                out.push({ url: href, text: text });
            }
            return out;
        }

        function findShowAllButton() {
            var nodes = document.querySelectorAll("button, a");
            for (var i = 0; i < nodes.length; i++) {
                var t = (nodes[i].textContent || "").trim().toLowerCase();
                if (t.indexOf("show all chapters") !== -1) {
                    return nodes[i];
                }
            }
            return null;
        }

        var results = [];

        // Try JS-driven "Show All Chapters" first
        var btn = findShowAllButton();
        if (btn) {
            var hx = btn.getAttribute("hx-get") || btn.getAttribute("data-hx-get");
            if (hx) {
                var u = hx;
                if (u.indexOf("http://") !== 0 && u.indexOf("https://") !== 0) {
                    if (u.charAt(0) !== "/") {
                        u = "/" + u;
                    }
                    u = window.location.origin + u;
                }

                try {
                    var xhr = new XMLHttpRequest();
                    xhr.open("GET", u, false); // synchronous, no promises
                    xhr.send(null);
                    if (xhr.status >= 200 && xhr.status < 300) {
                        var tmp = document.createElement("div");
                        tmp.innerHTML = xhr.responseText;
                        results = collectChapters(tmp);
                    }
                } catch (e) {
                    // ignore and fall back
                }
            }
        }

        // Fallback: scrape whatever is already in the page
        if (!results || results.length === 0) {
            results = collectChapters(document);
        }

        return results;
    })()`

	return &downloader.ChapterExtractionMethod{
		Type:         "javascript",
		JavaScript:   js,
		WaitSelector: "body",
	}
}

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

	// Extract chapter/episode number from text like "Episode 273", "Chapter 3", etc.
	re := regexp.MustCompile(`(?i)(?:Episode|Chapter)\s+(\d+)(?:\.(\d+))?`)
	matches := re.FindStringSubmatch(text)
	if len(matches) == 0 {
		// Fallback: use the text as-is
		sanitized := strings.ReplaceAll(text, " ", "-")
		sanitized = strings.ToLower(sanitized)
		log.Printf("[WeebCentral] WARNING: Could not parse chapter/episode number from text: %s", text)
		return fmt.Sprintf("%s.cbz", sanitized)
	}

	mainNum := matches[1]
	partNum := ""
	if len(matches) > 2 && matches[2] != "" {
		partNum = matches[2]
	}

	prefix := "ch"
	if strings.Contains(strings.ToLower(text), "episode") {
		prefix = "ep"
	}

	filename := fmt.Sprintf("%s%03s", prefix, mainNum)
	if partNum != "" {
		filename += "." + partNum
	}

	log.Printf("[WeebCentral] Normalized: %s → %s.cbz", text, filename)
	return filename + ".cbz"
}

// -------------------------
// Image extraction
// -------------------------

func parseWeebcentralImages(html string) ([]string, error) {
	// Find the HTMX images endpoint
	re := regexp.MustCompile(`hx-get=["']([^"']*/chapters/[^"']*/images[^"']*)["']`)
	matches := re.FindStringSubmatch(html)

	if len(matches) < 2 {
		return nil, fmt.Errorf("WeebCentral: no images endpoint found")
	}

	imagesPath := matches[1]
	imagesPath = strings.ReplaceAll(imagesPath, "&amp;", "&")

	if !strings.HasPrefix(imagesPath, "http") {
		if !strings.HasPrefix(imagesPath, "/") {
			imagesPath = "/" + imagesPath
		}
		imagesPath = "https://weebcentral.com" + imagesPath
	}

	log.Printf("[WeebCentral] Fetching images from: %s", imagesPath)

	exec, err := downloader.NewRequestExecutor(imagesPath, true, nil)
	if err != nil {
		return nil, fmt.Errorf("WeebCentral: failed to create executor for images: %w", err)
	}

	ctx := context.Background()
	imagesHTML, err := exec.FetchHTML(ctx, imagesPath, "")
	if err != nil {
		return nil, fmt.Errorf("WeebCentral: failed to fetch images HTML: %w", err)
	}

	// Extract image URLs from img tags
	imgPattern := regexp.MustCompile(`<img[^>]+(?:src|data-src)=["']([^"']+)["'][^>]*>`)
	imgMatches := imgPattern.FindAllStringSubmatch(imagesHTML, -1)

	var images []string
	seen := make(map[string]bool)

	for _, m := range imgMatches {
		if len(m) < 2 {
			continue
		}
		url := m[1]

		if !strings.HasPrefix(url, "http") {
			continue
		}
		if strings.Contains(url, "icon") || strings.Contains(url, "logo") {
			continue
		}
		if seen[url] {
			continue
		}

		seen[url] = true
		images = append(images, url)
	}

	if len(images) == 0 {
		return nil, fmt.Errorf("WeebCentral: no images found in images endpoint")
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
