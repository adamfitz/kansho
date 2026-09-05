package sites

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"kansho/config"
	"kansho/downloader"

	"github.com/PuerkitoBio/goquery"
)

// ManhuausSite implements the SitePlugin interface for manhuaus sites
type ManhuausSite struct{}

// Ensure ManhuausSite implements SitePlugin
var _ downloader.SitePlugin = (*ManhuausSite)(nil)

// GetRetryPolicy configures manhuaus's unusually tricky retry behavior: the
// site frequently stops answering for short stretches, so the fetch and image
// loops get a high retry budget with a doubled backoff, and DecayBackoff makes
// that backoff build up across failing chapters and step back down gradually
// once the site starts answering again.
func (m *ManhuausSite) GetRetryPolicy() downloader.SiteRetryPolicy {
	return downloader.SiteRetryPolicy{
		MaxChapterRetries: 5,
		ChapterBackoff:    2 * time.Second,
		MaxImageRetries:   8,
		ImageBackoff:      2 * time.Second,
		MaxFetchRetries:   8,
		FetchBackoff:      2 * time.Second,
		DecayBackoff:      true,
		DecayStep:         2 * time.Second,
		DecayMax:          60 * time.Second,
	}
}

// GetSiteName returns the site identifier
func (m *ManhuausSite) GetSiteName() string {
	return "manhuaus"
}

// GetDomain returns the site domain
func (m *ManhuausSite) GetDomain() string {
	return "manhuaus.com"
}

// NeedsCFBypass returns whether this site needs Cloudflare bypass
func (m *ManhuausSite) NeedsCFBypass() bool {
	return true // Manhuaus uses Cloudflare protection
}

// chapterNumRe extracts the chapter number from a manhuaus chapter URL:
// https://manhuaus.com/manga/<series>/chapter-<num>/ — matching the shape the
// old JS extraction produced so NormalizeChapterFilename keeps working.
var chapterNumRe = regexp.MustCompile(`/chapter-([\d.]+)/?$`)

// GetChapterExtractionMethod returns HOW to extract chapters.
//
// Manhuaus runs WordPress Madara, so the full chapter list is ALWAYS
// server-rendered in the manga page HTML (<li class="wp-manga-chapter"><a
// href=".../chapter-N/">). The browser/JS path is unnecessary and actively
// harmful: headless Chrome regularly stalls on manhuaus's Cloudflare challenge
// for minutes, then dies at the refresh pool's 90s deadline. Plain HTTP with
// the captured cf bypass data returns the complete list in ~2s, so extraction
// uses the "custom" parser over the executor's HTTP-first path (browser only
// as a last resort).
func (m *ManhuausSite) GetChapterExtractionMethod() *downloader.ChapterExtractionMethod {
	return &downloader.ChapterExtractionMethod{
		Type: "custom",
		// Used only if the executor ever falls back to the browser.
		WaitSelector: "li.wp-manga-chapter a",
		Timeout:      60 * time.Second,
		CustomParser: func(html string) (map[string]string, error) {
			doc, err := goquery.NewDocumentFromReader(bytes.NewReader([]byte(html)))
			if err != nil {
				return nil, fmt.Errorf("failed to parse manhuaus chapter list: %w", err)
			}

			result := make(map[string]string)
			parsed := 0
			doc.Find("li.wp-manga-chapter a").Each(func(_ int, s *goquery.Selection) {
				href, _ := s.Attr("href")
				if href == "" {
					return
				}
				match := chapterNumRe.FindStringSubmatch(href)
				if len(match) < 2 {
					return
				}

				data := map[string]string{
					"num":  match[1],
					"url":  href,
					"text": strings.TrimSpace(s.Text()),
				}

				filename := m.NormalizeChapterFilename(data)
				result[filename] = m.NormalizeChapterURL(href, "")
				parsed++
			})

			if parsed == 0 {
				return nil, fmt.Errorf("no wp-manga-chapter links found in manhuaus HTML")
			}
			return result, nil
		},
	}
}

// GetImageExtractionMethod returns HOW to extract images.
//
// The reading page also server-renders every image (<img class="wp-manga-chapter-img"
// data-src="https://img.manhuaus.com/...">) in the initial HTML, so images are
// pulled from that static HTML over plain HTTP like the chapter list. No
// WaitSelector is set (an empty one routes extractImagesCustom through the
// executor's HTTP-first path instead of forcing the browser).
func (m *ManhuausSite) GetImageExtractionMethod() *downloader.ImageExtractionMethod {
	return &downloader.ImageExtractionMethod{
		Type:    "custom",
		Timeout: 60 * time.Second,
		CustomParser: func(html string) ([]string, error) {
			doc, err := goquery.NewDocumentFromReader(bytes.NewReader([]byte(html)))
			if err != nil {
				return nil, fmt.Errorf("failed to parse manhuaus reading page: %w", err)
			}

			var imageURLs []string
			doc.Find("div.reading-content img").Each(func(_ int, s *goquery.Selection) {
				src := s.AttrOr("data-src", "")
				if src == "" {
					src = s.AttrOr("src", "")
				}
				src = strings.TrimSpace(src)
				if src != "" {
					imageURLs = append(imageURLs, src)
				}
			})

			if len(imageURLs) == 0 {
				return nil, fmt.Errorf("no reading-content images found in manhuaus HTML")
			}
			return imageURLs, nil
		},
	}
}

// NormalizeChapterURL converts raw URL to absolute URL
// PARSING LOGIC ONLY - returns a string
func (m *ManhuausSite) NormalizeChapterURL(rawURL, baseURL string) string {
	// URLs from manhuaus are already absolute
	return rawURL
}

// NormalizeChapterFilename converts chapter data to filename
// PARSING LOGIC ONLY - returns a string
func (m *ManhuausSite) NormalizeChapterFilename(data map[string]string) string {
	num := data["num"]

	var filename string

	// Handle decimal chapters (e.g., "1.5")
	if strings.Contains(num, ".") {
		parts := strings.SplitN(num, ".", 2)
		// Pad integer part to 3 digits
		intPart := parts[0]
		decimalPart := parts[1]
		filename = fmt.Sprintf("ch%03s.%s", intPart, decimalPart)
	} else {
		// Standard chapter number - pad to 3 digits
		filename = fmt.Sprintf("ch%03s", num)
	}

	log.Printf("[Manhuaus] Normalized: %s → %s.cbz", num, filename)
	return filename + ".cbz"
}

// ManhuausDownloadChapters is the entry point called by the download queue
func ManhuausDownloadChapters(ctx context.Context, manga *config.Bookmarks, progressCallback func(string, float64, int, int, int)) error {
	site := &ManhuausSite{}

	cfg := &downloader.DownloadConfig{
		Manga:            manga,
		Site:             site,
		ProgressCallback: progressCallback,
	}

	manager := downloader.NewManager(cfg)
	return manager.Download(ctx)
}
