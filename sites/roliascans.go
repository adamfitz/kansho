package sites

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	"kansho/config"
	"kansho/downloader"

	"github.com/PuerkitoBio/goquery"
)

// RoliascansSite implements SitePlugin for roliascan.com.
//
// Site characteristics:
//   - Custom "mangapeak" WordPress theme (Tailwind markup, not the mangareader
//     eplister theme). The manga page server-renders only a loading spinner
//     inside <div class="chapter-list" data-manga-id="...">; the real chapter
//     list is fetched from the site's JSON API.
//   - Chapter list: GET /auth/manga-chapters?manga_id={id}&offset=N&limit=500&order=DESC&_t={token}&_ts={ts}
//     → {success, chapters:[{id, chapter, title, chapter_type, group_id,
//     language, group_name, likes, url}], total, offset, limit, has_more}.
//     The _t/_ts pair is an anti-scraping token: md5(ts + "mng_ch_" + UTC hour
//     YYYYMMDDhh) truncated to the first 16 hex chars (replicated from manga.js).
//   - Chapter images: GET /auth/chapter-content?chapter_id={id}
//     → {success, chapter_id, chapter_type, images:[...]}. Images live on a
//     separate domain (roliascan.org/storage/chapters/...), which answers plain
//     HTTP requests with a normal 200 and needs no referer header.
//   - Neither roliascan.com nor roliascan.org Cloudflare-challenges plain HTTP
//     requests (verified with the api client's "kansho/1.0" UA), so
//     NeedsCFBypass is false and the plain HTTP downloader suffices.
type RoliascansSite struct{}

// Ensure RoliascansSite implements SitePlugin
var _ downloader.SitePlugin = (*RoliascansSite)(nil)

// --- JSON response structs ---

type roliascansChaptersResponse struct {
	Success  bool                `json:"success"`
	Chapters []roliascansChapter `json:"chapters"`
	HasMore  bool                `json:"has_more"`
}

type roliascansChapter struct {
	ID      string `json:"id"`
	Chapter string `json:"chapter"`
	Title   string `json:"title"`
	URL     string `json:"url"`
}

type roliascansChapterContentResponse struct {
	Success bool     `json:"success"`
	Images  []string `json:"images"`
}

// --- SitePlugin implementation ---

func (s *RoliascansSite) GetSiteName() string {
	return "roliascans"
}

func (s *RoliascansSite) GetDomain() string {
	return "roliascan.com"
}

func (s *RoliascansSite) NeedsCFBypass() bool {
	return false
}

// GetChapterExtractionMethod returns an "api" type method. The chapter list is
// only reachable over the site's JSON API: the manga page URL yields the manga
// id (data-manga-id) and the API returns the full list in absolute URL form.
func (s *RoliascansSite) GetChapterExtractionMethod() *downloader.ChapterExtractionMethod {
	return &downloader.ChapterExtractionMethod{
		Type: "api",
		ContextAPIFunc: func(ctx context.Context, mangaURL string, client *downloader.APIClient) ([]map[string]string, error) {
			mangaID, err := roliascansMangaID(ctx, mangaURL, client)
			if err != nil {
				return nil, err
			}

			token, ts := roliascansAPIToken()

			var chapters []roliascansChapter
			offset := 0
			for {
				chaptersURL := fmt.Sprintf(
					"https://roliascan.com/auth/manga-chapters?manga_id=%s&offset=%d&limit=500&order=DESC&_t=%s&_ts=%s",
					mangaID, offset, token, ts,
				)
				log.Printf("[Roliascans] Fetching chapters (offset %d): %s", offset, chaptersURL)

				var resp roliascansChaptersResponse
				if err := client.FetchJSON(ctx, chaptersURL, &resp); err != nil {
					return nil, fmt.Errorf("[Roliascans] failed to fetch chapters: %w", err)
				}
				if !resp.Success {
					return nil, fmt.Errorf("[Roliascans] API returned success=false for manga %s", mangaID)
				}

				chapters = append(chapters, resp.Chapters...)
				if len(resp.Chapters) == 0 || !resp.HasMore {
					break
				}
				offset += len(resp.Chapters)
			}

			if len(chapters) == 0 {
				return nil, fmt.Errorf("[Roliascans] no chapters found for manga %s", mangaID)
			}

			result := make([]map[string]string, 0, len(chapters))
			for _, ch := range chapters {
				result = append(result, map[string]string{
					"num":  ch.Chapter,
					"id":   ch.ID,
					"url":  ch.URL,
					"text": ch.Title,
				})
			}

			log.Printf("[Roliascans] Found %d chapters for manga %s", len(chapters), mangaID)
			return result, nil
		},
	}
}

// GetImageExtractionMethod returns an "api" type method: the chapter id from
// the chapter URL is passed to the chapter-content endpoint, which returns the
// full page-image list in order.
func (s *RoliascansSite) GetImageExtractionMethod() *downloader.ImageExtractionMethod {
	return &downloader.ImageExtractionMethod{
		Type: "api",
		APIFunc: func(chapterURL string, chapterData map[string]string, client *downloader.APIClient) ([]string, error) {
			chapterID, err := roliascansChapterID(chapterURL)
			if err != nil {
				return nil, err
			}

			contentURL := fmt.Sprintf("https://roliascan.com/auth/chapter-content?chapter_id=%s", chapterID)
			log.Printf("[Roliascans] Fetching chapter content: %s", contentURL)

			var resp roliascansChapterContentResponse
			if err := client.FetchJSON(context.Background(), contentURL, &resp); err != nil {
				return nil, fmt.Errorf("[Roliascans] failed to fetch chapter content: %w", err)
			}
			if !resp.Success {
				return nil, fmt.Errorf("[Roliascans] API returned success=false for chapter %s", chapterID)
			}

			var images []string
			seen := make(map[string]bool)
			for _, img := range resp.Images {
				img = strings.TrimSpace(img)
				if img == "" || !strings.HasPrefix(img, "http") || seen[img] {
					continue
				}
				seen[img] = true
				images = append(images, img)
			}

			if len(images) == 0 {
				return nil, fmt.Errorf("[Roliascans] no image URLs found for chapter %s", chapterID)
			}

			log.Printf("[Roliascans] Found %d images for chapter %s", len(images), chapterID)
			return images, nil
		},
	}
}

// NormalizeChapterURL returns absolute chapter URLs unchanged; the API always
// provides fully-qualified https://roliascan.com/read/... URLs.
func (s *RoliascansSite) NormalizeChapterURL(rawURL, baseURL string) string {
	if strings.HasPrefix(rawURL, "http://") || strings.HasPrefix(rawURL, "https://") {
		return rawURL
	}
	if strings.HasPrefix(rawURL, "//") {
		return "https:" + rawURL
	}
	if !strings.HasPrefix(rawURL, "/") {
		rawURL = "/" + rawURL
	}
	return "https://roliascan.com" + rawURL
}

// NormalizeChapterFilename converts the API chapter number (e.g. "91",
// "1.5") into a padded CBZ filename (e.g. "ch091.cbz", "ch001.5.cbz").
func (s *RoliascansSite) NormalizeChapterFilename(data map[string]string) string {
	raw := data["num"]
	if raw == "" {
		raw = data["text"]
	}
	if raw == "" {
		raw = data["url"]
	}

	num, frac := roliascansChapterNumber(raw)
	if num < 0 {
		sanitized := strings.ToLower(strings.TrimSpace(raw))
		sanitized = regexp.MustCompile(`[^a-z0-9.]+`).ReplaceAllString(sanitized, "-")
		log.Printf("[Roliascans] WARNING: could not parse chapter number from %q", raw)
		return sanitized + ".cbz"
	}

	name := fmt.Sprintf("ch%03d", num)
	if frac != "" {
		name += "." + frac
	}
	return name + ".cbz"
}

// --- Helpers ---

// roliascansMangaID fetches the manga page and extracts the numeric manga id
// from the .chapter-list data-manga-id attribute (the body carries it too).
func roliascansMangaID(ctx context.Context, mangaURL string, client *downloader.APIClient) (string, error) {
	raw, err := client.FetchRaw(ctx, mangaURL)
	if err != nil {
		return "", fmt.Errorf("[Roliascans] failed to fetch manga page: %w", err)
	}

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("[Roliascans] failed to parse manga page: %w", err)
	}

	id := doc.Find(".chapter-list").First().AttrOr("data-manga-id", "")
	if id == "" {
		id = doc.Find("body").First().AttrOr("data-manga-id", "")
	}
	if id == "" {
		return "", fmt.Errorf("[Roliascans] no data-manga-id found on manga page %s", mangaURL)
	}
	return id, nil
}

// roliascansChapterID extracts the numeric chapter id from a chapter URL like
// https://roliascan.com/read/my-bias-gets-on-the-last-train/ch84-264600/ → "264600".
func roliascansChapterID(chapterURL string) (string, error) {
	re := regexp.MustCompile(`-([0-9]+)/?$`)
	m := re.FindStringSubmatch(chapterURL)
	if len(m) < 2 || m[1] == "" {
		return "", fmt.Errorf("[Roliascans] could not extract chapter id from %q", chapterURL)
	}
	return m[1], nil
}

// roliascansAPIToken reproduces the anti-scraping token the site's manga.js
// sends with the chapter-list request: md5(timestamp + "mng_ch_" + UTC hour
// YYYYMMDDhh) truncated to the first 16 hex characters.
func roliascansAPIToken() (string, string) {
	ts := time.Now().Unix()
	hour := time.Now().UTC().Format("2006010215")
	secret := "mng_ch_" + hour
	hash := md5.Sum([]byte(strconv.FormatInt(ts, 10) + secret))
	return hex.EncodeToString(hash[:])[:16], strconv.FormatInt(ts, 10)
}

// roliascansChapterNumber extracts the chapter number (and optional fractional
// part) from a chapter label or URL. Returns num=-1 when nothing parseable is
// found.
func roliascansChapterNumber(s string) (int, string) {
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

// --- Entry point ---

// RoliascansDownloadChapters is the entry point called by the download queue.
func RoliascansDownloadChapters(ctx context.Context, manga *config.Bookmarks, progressCallback func(string, float64, int, int, int)) error {
	site := &RoliascansSite{}

	cfg := &downloader.DownloadConfig{
		Manga:            manga,
		Site:             site,
		ProgressCallback: progressCallback,
	}

	manager := downloader.NewManager(cfg)
	return manager.Download(ctx)
}
