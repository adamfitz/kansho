package sites

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	"kansho/config"
	"kansho/downloader"
)

// ComixSite implements SitePlugin for comix.to.
//
// Site characteristics:
//   - Vue SPA: the title/read pages render into #app-root client-side, so
//     extraction must run in a browser.
//   - The title page chapter list is paginated (mchap-list rows + npager
//     pager). Pagination is driven from Go by clicking the next page control,
//     never by an async/promise JS IIFE.
//   - The reader page renders one <img class="rpage-page__img"> per page and
//     lazy-loads them as the reader is scrolled. Images are collected after
//     scrolling the page step by step, so no await/promise scrolling is needed.
//   - Image URLs are on a static CDN and 403 without a Referer from comix.to,
//     so downloads go through the Referer-based image downloader.
type ComixSite struct{}

// Ensure ComixSite implements SitePlugin
var _ downloader.SitePlugin = (*ComixSite)(nil)

// -------------------------
// SitePlugin implementation
// -------------------------

func (s *ComixSite) GetSiteName() string {
	return "comix"
}

func (s *ComixSite) GetDomain() string {
	return "comix.to"
}

func (s *ComixSite) NeedsCFBypass() bool {
	return true
}

func (s *ComixSite) Debugger() *downloader.Debugger {
	return &downloader.Debugger{
		SaveHTML: false,
		HTMLPath: "comix_debug.html",
	}
}

// GetChapterExtractionMethod uses "javascript" extraction with Go-driven
// pagination. The JavaScript expressions are plain (no promises): the first
// one returns the chapters rendered on the current page, the second clicks the
// next page control and returns whether a next page existed.
func (s *ComixSite) GetChapterExtractionMethod() *downloader.ChapterExtractionMethod {
	return &downloader.ChapterExtractionMethod{
		Type:         "javascript",
		WaitSelector: "ul.mchap-list a.mchap-row__primary",
		JavaScript: `
			(() => [...document.querySelectorAll('ul.mchap-list a.mchap-row__primary')]
				.map(a => {
					const label = a.querySelector('span.mchap-row__ch');
					return { url: a.href, text: (label || a).textContent.trim() };
				}))()
		`,
		NextPageJS: `
			(() => {
				const active = document.querySelector('.npager__num.is-active');
				if (!active) return false;
				const next = active.nextElementSibling;
				if (!next) return false;
				const isNextNav = next.classList.contains('npager__nav') && next.getAttribute('aria-label') === 'Next page';
				const isNum = next.classList.contains('npager__num');
				if (!isNextNav && !isNum) return false;
				next.click();
				return true;
			})()
		`,
		Timeout: 5 * time.Minute,
	}
}

// GetImageExtractionMethod uses "javascript" extraction with browser-driven
// scrolling. The reader lazy-loads images as the page is scrolled, so the
// downloader scrolls one viewport at a time (no JS promises) before reading
// the image srcs from the DOM.
func (s *ComixSite) GetImageExtractionMethod() *downloader.ImageExtractionMethod {
	return &downloader.ImageExtractionMethod{
		Type:         "javascript",
		WaitSelector: ".rpage-page",
		ScrollToLoad: true,
		JavaScript: `
			(() => {
				const seen = new Set();
				return [...document.querySelectorAll('.rpage-page img.rpage-page__img')]
					.map(i => i.src)
					.filter(src => src && src.indexOf('http') === 0)
					.map(src => {
						const q = src.indexOf('?r=');
						return q !== -1 ? src.slice(0, q) : src;
					})
					.filter(src => !seen.has(src) && seen.add(src));
			})()
		`,
		Timeout: 10 * time.Minute,
	}
}

func (s *ComixSite) NormalizeChapterURL(rawURL, baseURL string) string {
	if strings.HasPrefix(rawURL, "http://") || strings.HasPrefix(rawURL, "https://") {
		return rawURL
	}
	if strings.HasPrefix(rawURL, "//") {
		return "https:" + rawURL
	}
	if !strings.HasPrefix(rawURL, "/") {
		rawURL = "/" + rawURL
	}
	return "https://comix.to" + rawURL
}

func (s *ComixSite) NormalizeChapterFilename(data map[string]string) string {
	raw := data["text"]
	if raw == "" {
		raw = data["url"]
	}

	// Chapter URLs end with /{chapterId}-chapter-{num}, and the leading digits
	// are the chapter id, so the URL pattern is checked before the generic one.
	num, frac := comixChapterNumber(data["url"])
	if num < 0 {
		num, frac = comixChapterNumber(raw)
	}

	if num < 0 {
		sanitized := strings.ToLower(strings.TrimSpace(raw))
		sanitized = regexp.MustCompile(`[^a-z0-9.]+`).ReplaceAllString(sanitized, "-")
		log.Printf("[Comix] WARNING: could not parse chapter number from %q", raw)
		return sanitized + ".cbz"
	}

	name := fmt.Sprintf("ch%03d", num)
	if frac != "" {
		name += "." + frac
	}
	return name + ".cbz"
}

// -------------------------
// Chapter number parsing
// -------------------------

// comixChapterNumber extracts the chapter number (and optional fractional part)
// from a comix label or URL. Returns num=-1 when nothing parseable is found.
func comixChapterNumber(s string) (int, string) {
	reURL := regexp.MustCompile(`-chapter-(\d+)(?:\.(\d+))?$`)
	if m := reURL.FindStringSubmatch(s); len(m) >= 2 {
		if n, err := strconv.Atoi(m[1]); err == nil {
			return n, m[2]
		}
	}

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

func ComixDownloadChapters(ctx context.Context, manga *config.Bookmarks, progressCallback func(string, float64, int, int, int)) error {
	site := &ComixSite{}

	cfg := &downloader.DownloadConfig{
		Manga:            manga,
		Site:             site,
		ProgressCallback: progressCallback,
	}

	manager := downloader.NewManager(cfg)
	return manager.Download(ctx)
}
