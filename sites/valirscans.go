package sites

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"kansho/config"
	"kansho/downloader"

	"github.com/PuerkitoBio/goquery"
)

// ValirscansSite implements SitePlugin for valirscans.org.
//
// Site characteristics:
//   - Next.js App Router app (React Server Components). Both the series page
//     and the chapter page are fully server-rendered, so plain HTTP requests
//     return everything the plugin needs — no browser or JavaScript execution.
//   - The data lives in the RSC flight payload, streamed into the HTML as
//     <script>self.__next_f.push([1,"..."])</script> chunks. Each chunk is a JS
//     string literal, so it has to be unescaped once before the JSON inside it
//     can be read (see valirscansFlightPayload).
//   - Series page: "chapters":[{number, title, isLocked, hasAccess, ...}] gives
//     the complete chapter list in one request (no pagination). Locked chapters
//     (early-access/premium) are skipped — their pages come back redacted with
//     no image URLs.
//   - Chapter page: the reader's "pages":[{pageNumber, kind, isRedacted,
//     imageUrl}] array holds every page image in reading order.
//   - Images are plain, unencrypted WebP files on media.valirscans.org, served
//     with a normal 200 to any user agent and no Referer requirement.
//   - The domain is proxied by Cloudflare, but a browser-like request gets a
//     normal 200 with the full page (no cf-mitigated challenge), so
//     NeedsCFBypass is false. The site has a second, independent guard: its own
//     scanner blocklist that answers an unwanted User-Agent with HTTP 200 and
//     an "Access Denied" page (see valirscansBlockedErr).
type ValirscansSite struct{}

// Ensure ValirscansSite implements SitePlugin
var _ downloader.SitePlugin = (*ValirscansSite)(nil)

// valirscansChapter mirrors one entry of the series page's "chapters" array.
type valirscansChapter struct {
	Number    json.Number `json:"number"`
	Title     string      `json:"title"`
	IsLocked  bool        `json:"isLocked"`
	HasAccess *bool       `json:"hasAccess"`
}

// valirscansPage mirrors one entry of the chapter page reader's "pages" array.
type valirscansPage struct {
	PageNumber int    `json:"pageNumber"`
	IsRedacted bool   `json:"isRedacted"`
	ImageURL   string `json:"imageUrl"`
}

// -------------------------
// SitePlugin implementation
// -------------------------

func (s *ValirscansSite) GetSiteName() string {
	return "valirscans"
}

func (s *ValirscansSite) GetDomain() string {
	return "valirscans.org"
}

func (s *ValirscansSite) NeedsCFBypass() bool {
	return false
}

func (s *ValirscansSite) Debugger() *downloader.Debugger {
	return &downloader.Debugger{
		SaveHTML: false,
		HTMLPath: "valirscans_debug.html",
	}
}

// GetChapterExtractionMethod uses "custom" extraction: the whole chapter list
// is server-rendered into the RSC flight payload of the series page, so a
// single HTTP request is enough.
func (s *ValirscansSite) GetChapterExtractionMethod() *downloader.ChapterExtractionMethod {
	return &downloader.ChapterExtractionMethod{
		Type:         "custom",
		CustomParser: parseValirscansChapters,
	}
}

// GetImageExtractionMethod uses "custom" extraction: the chapter page ships the
// full page list (image URLs in reading order) inside the RSC flight payload.
func (s *ValirscansSite) GetImageExtractionMethod() *downloader.ImageExtractionMethod {
	return &downloader.ImageExtractionMethod{
		Type:         "custom",
		CustomParser: parseValirscansImages,
	}
}

// NormalizeChapterURL turns the series-relative chapter path into an absolute
// https://valirscans.org URL.
func (s *ValirscansSite) NormalizeChapterURL(rawURL, baseURL string) string {
	if strings.HasPrefix(rawURL, "http://") || strings.HasPrefix(rawURL, "https://") {
		return rawURL
	}
	if strings.HasPrefix(rawURL, "//") {
		return "https:" + rawURL
	}
	if !strings.HasPrefix(rawURL, "/") {
		rawURL = "/" + rawURL
	}
	return "https://valirscans.org" + rawURL
}

// NormalizeChapterFilename converts chapter data into a padded CBZ filename.
// The chapter number comes from data["num"] (the site's own number, also used
// to build the chapter URL); the row title is the fallback.
func (s *ValirscansSite) NormalizeChapterFilename(data map[string]string) string {
	raw := data["num"]
	if raw == "" {
		raw = data["text"]
	}
	if raw == "" {
		return "chapter.cbz"
	}

	num, frac := valirscansChapterNumber(raw)
	if num < 0 {
		sanitized := strings.ToLower(strings.TrimSpace(raw))
		sanitized = regexp.MustCompile(`[^a-z0-9.]+`).ReplaceAllString(sanitized, "-")
		log.Printf("[Valirscans] WARNING: could not parse chapter number from %q", raw)
		return sanitized + ".cbz"
	}

	name := fmt.Sprintf("ch%03d", num)
	if frac != "" {
		name += "." + frac
	}
	return name + ".cbz"
}

// -------------------------
// Chapter list parsing
// -------------------------

// parseValirscansChapters extracts the chapter list from the series page HTML.
// Returns a map of chapter filename -> chapter URL.
func parseValirscansChapters(html string) (map[string]string, error) {
	if err := valirscansBlockedErr(html); err != nil {
		return nil, err
	}

	payload := valirscansFlightPayload(html)
	if payload == "" {
		return nil, fmt.Errorf("Valirscans: no RSC flight payload found in series page HTML")
	}

	seriesURL := valirscansSeriesURL(html)
	if seriesURL == "" {
		return nil, fmt.Errorf("Valirscans: could not determine series URL from page (og:url)")
	}

	raw, ok := valirscansJSONArray(payload, "chapters")
	if !ok {
		return nil, fmt.Errorf("Valirscans: chapters array not found in RSC payload")
	}

	var chapters []valirscansChapter
	if err := json.Unmarshal([]byte("["+raw+"]"), &chapters); err != nil {
		return nil, fmt.Errorf("Valirscans: failed to parse chapters JSON: %w", err)
	}

	site := &ValirscansSite{}
	result := make(map[string]string)
	seen := make(map[string]bool)
	skipped := 0

	for _, ch := range chapters {
		number := ch.Number.String()
		if number == "" {
			continue
		}

		// Locked chapters (early access / premium) are listed by the site but
		// their pages come back redacted with no image URLs.
		if ch.IsLocked || (ch.HasAccess != nil && !*ch.HasAccess) {
			skipped++
			continue
		}

		key := number
		if seen[key] {
			continue
		}
		seen[key] = true

		url := seriesURL + "/chapter/" + number
		label := strings.TrimSpace(ch.Title)
		if label == "" {
			label = "Chapter " + number
		}

		result[site.NormalizeChapterFilename(map[string]string{"num": number, "text": label})] = url
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("Valirscans: no downloadable chapters found (%d locked/early-access chapters skipped)", skipped)
	}

	log.Printf("[Valirscans] Found %d chapters (%d locked/early-access skipped)", len(result), skipped)
	return result, nil
}

// -------------------------
// Image parsing
// -------------------------

// parseValirscansImages extracts the page image URLs from a chapter page HTML,
// in reading order, from the reader's "pages" array in the RSC flight payload.
func parseValirscansImages(html string) ([]string, error) {
	if err := valirscansBlockedErr(html); err != nil {
		return nil, err
	}

	payload := valirscansFlightPayload(html)
	if payload == "" {
		return nil, fmt.Errorf("Valirscans: no RSC flight payload found in chapter page HTML")
	}

	raw, ok := valirscansJSONArray(payload, "pages")
	if !ok {
		return nil, fmt.Errorf("Valirscans: pages array not found in RSC payload")
	}

	var pages []valirscansPage
	if err := json.Unmarshal([]byte("["+raw+"]"), &pages); err != nil {
		return nil, fmt.Errorf("Valirscans: failed to parse pages JSON: %w", err)
	}

	// The payload is already ordered, but sorting by pageNumber keeps the CBZ
	// page order correct even if the site ever emits them out of order.
	sort.SliceStable(pages, func(i, j int) bool {
		return pages[i].PageNumber < pages[j].PageNumber
	})

	images := make([]string, 0, len(pages))
	withoutImage := 0
	allRedacted := len(pages) > 0
	seen := make(map[string]bool)

	for _, page := range pages {
		img := strings.TrimSpace(page.ImageURL)
		if img == "" || !strings.HasPrefix(img, "http") {
			withoutImage++
			continue
		}
		allRedacted = allRedacted && page.IsRedacted
		if seen[img] {
			continue
		}
		seen[img] = true
		images = append(images, img)
	}

	if len(images) == 0 {
		if allRedacted {
			return nil, fmt.Errorf("Valirscans: all %d pages are redacted (chapter is locked/early access)", withoutImage)
		}
		return nil, fmt.Errorf("Valirscans: no image URLs found in chapter page")
	}

	log.Printf("[Valirscans] Found %d chapter images (%d pages without an image)", len(images), withoutImage)
	return images, nil
}

// -------------------------
// Bot-block detection
// -------------------------

// valirscansBlockedTitleRe matches the title of the block page the site serves
// to scanners it does not like.
var valirscansBlockedTitleRe = regexp.MustCompile(`(?i)<title>\s*Access Denied\s*</title>`)

// valirscansBlockedErr reports the site's own "Access Denied" interstitial
// (served with HTTP 200 and an x-blocked-by: scanner-blocklist header) as an
// error.
//
// This protection is independent of Cloudflare and, crucially, arrives with a
// 200 status, so the downloader's Cloudflare challenge detection does not fire
// on it — without this check a blocked fetch would only surface as "no RSC
// flight payload found". It cannot be fixed by capturing cf_clearance either:
// the block is a User-Agent blocklist, so the fix is a browser-like User-Agent.
func valirscansBlockedErr(html string) error {
	if !valirscansBlockedTitleRe.MatchString(html) {
		return nil
	}
	return fmt.Errorf("Valirscans: request was bot-blocked by the site (scanner-blocklist, served with HTTP 200); " +
		"this is not a Cloudflare challenge, so a CF bypass will not fix it — the site is refusing the request User-Agent")
}

// -------------------------
// RSC flight payload helpers
// -------------------------

// valirscansFlightRe captures the body of every
// <script>self.__next_f.push([1,"..."])</script> chunk.
var valirscansFlightRe = regexp.MustCompile(`(?s)self\.__next_f\.push\(\[1,"(.*?)"\]\)</script>`)

// valirscansFlightPayload concatenates and unescapes all RSC flight chunks of a
// Next.js page. Each chunk body is a JS string literal, so it must be unescaped
// once before the JSON it carries can be parsed.
func valirscansFlightPayload(html string) string {
	matches := valirscansFlightRe.FindAllStringSubmatch(html, -1)
	if len(matches) == 0 {
		return ""
	}

	var b strings.Builder
	for _, m := range matches {
		b.WriteString(valirscansUnescape(m[1]))
	}
	return b.String()
}

// valirscansUnescape decodes the JS string escapes Next.js uses inside the
// flight chunks (" → "), \n, \uXXXX, ...). Escapes it does not recognise
// (e.g. the \d and \. of embedded JS regular expressions) are kept verbatim.
func valirscansUnescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}

	var b strings.Builder
	b.Grow(len(s))

	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}

		switch next := s[i+1]; next {
		case '"', '\\', '/':
			b.WriteByte(next)
			i++
		case 'n':
			b.WriteByte('\n')
			i++
		case 'r':
			b.WriteByte('\r')
			i++
		case 't':
			b.WriteByte('\t')
			i++
		case 'u':
			r, size := valirscansUnescapeRune(s[i+1:])
			b.WriteRune(r)
			i += size
		default:
			b.WriteByte('\\')
		}
	}

	return b.String()
}

// valirscansUnescapeRune decodes a \uXXXX escape (plus the low surrogate when
// the escape is the first half of a surrogate pair) starting at s[0]. It
// returns the rune and how many bytes of s it consumed.
func valirscansUnescapeRune(s string) (rune, int) {
	value, ok := valirscansHex4(s)
	if !ok {
		return utf8.RuneError, 1
	}

	r := rune(value)
	if !utf16.IsSurrogate(r) || !strings.HasPrefix(s[4:], `\u`) {
		if utf16.IsSurrogate(r) {
			return utf8.RuneError, 4
		}
		return r, 4
	}

	low, ok := valirscansHex4(s[4+2:])
	if !ok {
		return utf8.RuneError, 4
	}
	lowRune := rune(low)
	if dec := utf16.DecodeRune(r, lowRune); dec != utf8.RuneError {
		return dec, 10
	}
	return utf8.RuneError, 4
}

// valirscansHex4 reads exactly four hex digits from the start of s.
func valirscansHex4(s string) (int, bool) {
	if len(s) < 4 {
		return 0, false
	}
	n, err := strconv.ParseUint(s[:4], 16, 32)
	if err != nil {
		return 0, false
	}
	return int(n), true
}

// valirscansJSONArray returns the contents of the JSON array stored under the
// given key in an RSC payload, with the surrounding brackets removed. A regex
// cannot do this: the arrays contain nested objects and arrays.
func valirscansJSONArray(payload, key string) (string, bool) {
	marker := `"` + key + `":[`
	idx := strings.Index(payload, marker)
	if idx < 0 {
		return "", false
	}

	start := idx + len(marker)
	depth := 1
	pos := start

	for pos < len(payload) && depth > 0 {
		switch payload[pos] {
		case '\\':
			// Skip the escaped character so an escaped ] does not close the array.
			pos++
		case '[':
			depth++
		case ']':
			depth--
		}
		pos++
	}

	if depth != 0 {
		return "", false
	}

	return payload[start : pos-1], true
}

// -------------------------
// Chapter number parsing
// -------------------------

// valirscansChapterNumber extracts the chapter number (and optional fractional
// part) from a chapter number or label such as "91", "1.5" or "Chapter 18.5".
// Returns num=-1 when nothing parseable is found.
func valirscansChapterNumber(s string) (int, string) {
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

// valirscansSeriesURL reads the canonical series URL from the page's og:url
// meta tag. It is used as the base for chapter URLs so the site path segment
// (e.g. "/series/comic/{slug}") never has to be guessed.
func valirscansSeriesURL(html string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return ""
	}

	seriesURL := strings.TrimSpace(doc.Find(`meta[property="og:url"]`).First().AttrOr("content", ""))
	seriesURL = strings.TrimSuffix(seriesURL, "/")

	if !strings.HasPrefix(seriesURL, "https://") {
		return ""
	}
	return seriesURL
}

// -------------------------
// Download entrypoint
// -------------------------

func ValirscansDownloadChapters(ctx context.Context, manga *config.Bookmarks, progressCallback func(string, float64, int, int, int)) error {
	site := &ValirscansSite{}

	cfg := &downloader.DownloadConfig{
		Manga:            manga,
		Site:             site,
		ProgressCallback: progressCallback,
	}

	manager := downloader.NewManager(cfg)
	return manager.Download(ctx)
}
