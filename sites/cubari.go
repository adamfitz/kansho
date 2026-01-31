package sites

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	"kansho/config"
	"kansho/downloader"
)

type CubariSite struct{}

var lastGistID string

// -------------------------
// SitePlugin implementation
// -------------------------

func (s *CubariSite) GetSiteName() string {
	return "cubari"
}

func (s *CubariSite) GetDomain() string {
	return "cubari.moe"
}

func (s *CubariSite) NeedsCFBypass() bool {
	return false
}

func (s *CubariSite) NormalizeChapterURL(rawURL, baseURL string) string {
	return rawURL
}

func (s *CubariSite) NormalizeChapterFilename(chapterData map[string]string) string {
	ch := chapterData["chapter"]
	if ch == "" {
		ch = "0"
	}
	num, _ := strconv.ParseFloat(ch, 64)
	if num == float64(int(num)) {
		return fmt.Sprintf("ch%03d.cbz", int(num))
	}
	return fmt.Sprintf("ch%03.1f.cbz", num)
}

func (s *CubariSite) GetChapterExtractionMethod() *downloader.ChapterExtractionMethod {
	return &downloader.ChapterExtractionMethod{
		Type:         "custom",
		WaitSelector: "",
		CustomParser: func(html string) (map[string]string, error) {
			var dbg *downloader.Debugger
			if d, ok := any(s).(downloader.DebugSite); ok {
				dbg = d.Debugger()
			}
			return parseCubariChapters(html, dbg)
		},
	}
}

func (s *CubariSite) GetImageExtractionMethod() *downloader.ImageExtractionMethod {
	return &downloader.ImageExtractionMethod{
		Type:         "custom",
		WaitSelector: "",
		CustomParser: parseCubariImages,
	}
}

// enable debugging to save HTML files for Cubari
func (s *CubariSite) Debugger() *downloader.Debugger {
	return &downloader.Debugger{
		SaveHTML: true,
		HTMLPath: "cubari_debug.html",
	}
}

// -------------------------
// Download entrypoint
// -------------------------

func CubariDownloadChapters(ctx context.Context, manga *config.Bookmarks, progressCallback func(string, float64, int, int, int)) error {
	site := &CubariSite{}

	cfg := &downloader.DownloadConfig{
		Manga:            manga,
		Site:             site,
		ProgressCallback: progressCallback,
	}

	manager := downloader.NewManager(cfg)
	return manager.Download(ctx)
}

// -------------------------
// Chapter extraction
// -------------------------

func parseCubariChapters(html string, dbg *downloader.Debugger) (map[string]string, error) {
	// Try normal Cubari series first
	jsonText, err := extractNextDataJSON(html)
	if err == nil {
		return parseCubariSeriesJSON(jsonText)
	}

	// If __NEXT_DATA__ missing → this is a GIST SERIES
	log.Printf("[Cubari] __NEXT_DATA__ missing — treating as Gist series")

	gistURL, err := extractGistRawURL(html)
	if err != nil {
		return nil, fmt.Errorf("Cubari: unable to extract gist raw JSON URL: %w", err)
	}

	// Use RequestExecutor (HTTP first, browser fallback)
	exec, err := downloader.NewRequestExecutor(gistURL, false, dbg)
	if err != nil {
		return nil, fmt.Errorf("Cubari: failed to create executor: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	rawJSON, err := exec.FetchHTML(ctx, gistURL, "")
	if err != nil {
		return nil, fmt.Errorf("Cubari: failed to fetch gist JSON: %w", err)
	}

	return parseCubariGistJSON(rawJSON)
}

// -------------------------
// Gist JSON parser
// -------------------------

// parseCubariGistJSON parses a Cubari gist JSON that mirrors other sources
// (e.g. mangadex) and returns filename -> chapterURL.
func parseCubariGistJSON(jsonText string) (map[string]string, error) {
	var root map[string]interface{}
	if err := json.Unmarshal([]byte(jsonText), &root); err != nil {
		return nil, fmt.Errorf("Cubari: failed to parse gist JSON: %w", err)
	}

	chaptersRaw, ok := root["chapters"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("Cubari: chapters not found in gist JSON")
	}

	result := make(map[string]string)

	for chapterKey, raw := range chaptersRaw {
		ch, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}

		groups, ok := ch["groups"].(map[string]interface{})
		if !ok {
			continue
		}

		// Take the first group entry
		for _, v := range groups {
			apiPath, ok := v.(string)
			if !ok {
				continue
			}

			// Build full API URL
			chapterURL := "https://cubari.moe" + apiPath

			filename := fmt.Sprintf("ch%03d.cbz", atoiSafe(chapterKey))
			result[filename] = chapterURL

			log.Printf("[Cubari] Found chapter %s → %s", filename, chapterURL)
			break
		}
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("Cubari: no usable chapters found in gist JSON")
	}

	return result, nil
}

// -------------------------
// Normal Cubari series JSON parser
// -------------------------

func parseCubariSeriesJSON(jsonText string) (map[string]string, error) {
	var root map[string]interface{}
	if err := json.Unmarshal([]byte(jsonText), &root); err != nil {
		return nil, fmt.Errorf("Cubari: failed to parse series JSON: %w", err)
	}

	chaptersRaw := dig(root, "props", "pageProps", "series", "chapters")
	if chaptersRaw == nil {
		return nil, fmt.Errorf("Cubari: chapters not found in series JSON")
	}

	chapters, ok := chaptersRaw.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("Cubari: invalid chapters JSON")
	}

	result := make(map[string]string)

	for _, raw := range chapters {
		ch, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}

		chapterNum := fmt.Sprintf("%v", ch["chapter"])
		id := fmt.Sprintf("%v", ch["id"])

		chapterURL := "https://cubari.moe/read/" + id + "/"
		filename := fmt.Sprintf("ch%03d.cbz", atoiSafe(chapterNum))

		result[filename] = chapterURL
		log.Printf("[Cubari] Found chapter %s → %s", filename, chapterURL)
	}

	return result, nil
}

// -------------------------
// Image extraction
// -------------------------

func parseCubariImages(html string) ([]string, error) {
	// ImgChest API returns a raw JSON array of strings
	var arr []string
	if err := json.Unmarshal([]byte(html), &arr); err != nil {
		return nil, fmt.Errorf("Cubari: failed to parse image API JSON: %w", err)
	}

	if len(arr) == 0 {
		return nil, fmt.Errorf("Cubari: no images found in API response")
	}

	log.Printf("[Cubari] Found %d images", len(arr))
	return arr, nil
}

// -------------------------
// Helpers
// -------------------------

func parseNormalCubariImages(jsonText string) ([]string, error) {
	var root map[string]interface{}
	if err := json.Unmarshal([]byte(jsonText), &root); err != nil {
		return nil, fmt.Errorf("Cubari: failed to parse chapter JSON: %w", err)
	}

	groups := dig(root, "props", "pageProps", "chapter", "groups")
	if groups == nil {
		return nil, fmt.Errorf("Cubari: chapter groups missing")
	}

	gmap, ok := groups.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("Cubari: invalid groups JSON")
	}

	var firstGroup []interface{}
	for _, v := range gmap {
		arr, ok := v.([]interface{})
		if ok {
			firstGroup = arr
			break
		}
	}

	if firstGroup == nil {
		return nil, fmt.Errorf("Cubari: no image groups found")
	}

	var images []string
	for _, raw := range firstGroup {
		entry, ok := raw.([]interface{})
		if ok && len(entry) > 0 {
			if url, ok := entry[0].(string); ok {
				images = append(images, url)
			}
		}
	}

	return images, nil
}

func parseGistChapterImages(jsonText string, chapterNum string) ([]string, error) {
	var root map[string]interface{}
	if err := json.Unmarshal([]byte(jsonText), &root); err != nil {
		return nil, fmt.Errorf("Cubari: failed to parse gist JSON: %w", err)
	}

	chapters, ok := root["chapters"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("Cubari: chapters missing in gist JSON")
	}

	chapter, ok := chapters[chapterNum].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("Cubari: chapter %s missing in gist JSON", chapterNum)
	}

	groups, ok := chapter["groups"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("Cubari: groups missing in gist chapter JSON")
	}

	// Pick first group
	var firstGroup []interface{}
	for _, v := range groups {
		arr, ok := v.([]interface{})
		if ok {
			firstGroup = arr
			break
		}
	}

	if firstGroup == nil {
		return nil, fmt.Errorf("Cubari: no image groups found in gist chapter")
	}

	var images []string
	for _, raw := range firstGroup {
		url, ok := raw.(string)
		if ok {
			images = append(images, url)
		}
	}

	return images, nil
}

func extractNextDataJSON(html string) (string, error) {
	re := regexp.MustCompile(`<script id="__NEXT_DATA__" type="application/json">(.+?)</script>`)
	m := re.FindStringSubmatch(html)
	if len(m) < 2 {
		return "", fmt.Errorf("Cubari: __NEXT_DATA__ JSON not found")
	}
	return m[1], nil
}

func extractGistRawURL(html string) (string, error) {
	re := regexp.MustCompile(`read/gist/([A-Za-z0-9\-_]+)`)
	m := re.FindStringSubmatch(html)
	if len(m) < 2 {
		return "", fmt.Errorf("Cubari: gist ID not found")
	}

	encoded := m[1]
	lastGistID = encoded

	pad := len(encoded) % 4
	if pad != 0 {
		encoded += strings.Repeat("=", 4-pad)
	}

	decodedBytes, err := base64.URLEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("Cubari: failed to decode gist ID: %w", err)
	}

	rawPath := string(decodedBytes)

	if after, ok := strings.CutPrefix(rawPath, "raw/"); ok {
		rawPath = after
	}

	rawURL := "https://raw.githubusercontent.com/" + rawPath
	log.Printf("[Cubari] Gist raw JSON URL: %s", rawURL)

	return rawURL, nil
}

func dig(m map[string]interface{}, keys ...string) interface{} {
	cur := interface{}(m)
	for _, k := range keys {
		mm, ok := cur.(map[string]interface{})
		if !ok {
			return nil
		}
		cur = mm[k]
	}
	return cur
}
