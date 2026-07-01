package sites

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"kansho/config"
	"kansho/downloader"
)

// KunmangaSite implements the SitePlugin interface for kunmanga sites
type KunmangaSite struct{}

// Ensure KunmangaSite implements SitePlugin
var _ downloader.SitePlugin = (*KunmangaSite)(nil)

// Ensure KunmangaSite implements ManualCFPromptSite
var _ downloader.ManualCFPromptSite = (*KunmangaSite)(nil)

// GetSiteName returns the site identifier
func (k *KunmangaSite) GetSiteName() string {
	return "kunmanga"
}

// GetDomain returns the site domain
func (k *KunmangaSite) GetDomain() string {
	return "www.kunmanga.online"
}

// NeedsCFBypass returns whether this site needs Cloudflare bypass
func (k *KunmangaSite) NeedsCFBypass() bool {
	return true // Kunmanga uses Cloudflare protection
}

// NeedsManualCFPrompt returns true so the manga URL is always opened in the
// user's real browser before chapter extraction. This ensures the browser
// extension captures CF cookies even when the main manga page does not
// trigger a CF challenge — the cookies are needed for image CDN requests.
func (k *KunmangaSite) NeedsManualCFPrompt() bool { return true }

// GetChapterExtractionMethod returns HOW to extract chapters.
// The new kunmanga.online site loads chapters dynamically via an internal
// JSON API, so we use the "api" extraction type to fetch them directly.
func (k *KunmangaSite) GetChapterExtractionMethod() *downloader.ChapterExtractionMethod {
	return &downloader.ChapterExtractionMethod{
		Type: "api",
		APIFunc: func(baseURL string, client *downloader.APIClient) ([]map[string]string, error) {
			return k.fetchChaptersViaAPI(baseURL, client)
		},
	}
}

// GetImageExtractionMethod returns HOW to extract images
// Downloader will execute this - we just provide the JavaScript
func (k *KunmangaSite) GetImageExtractionMethod() *downloader.ImageExtractionMethod {
	return &downloader.ImageExtractionMethod{
		Type:         "javascript",
		WaitSelector: "div.reading-content img",
		JavaScript: `
			[...document.querySelectorAll('div.reading-content img')]
			.map(img => {
				const src = img.src;
				return src.trim();
			})
			.filter(src => src !== '')
		`,
	}
}

// --- API types for chapter list ---

type kunmangaChapterResponse struct {
	Success bool                `json:"success"`
	Data    kunmangaChapterData `json:"data"`
}

type kunmangaChapterData struct {
	Chapters []kunmangaChapterItem `json:"chapters"`
	Total    int                   `json:"total"`
	Page     int                   `json:"current_page"`
	PerPage  int                   `json:"per_page"`
	LastPage int                   `json:"last_page"`
}

type kunmangaChapterItem struct {
	Num  float64 `json:"chapter_num"`
	Slug string  `json:"chapter_slug"`
	Name string  `json:"chapter_name"`
}

func (k *KunmangaSite) fetchChaptersViaAPI(baseURL string, client *downloader.APIClient) ([]map[string]string, error) {
	slug, err := extractKunmangaSlug(baseURL)
	if err != nil {
		return nil, fmt.Errorf("[kunmanga] %w", err)
	}

	log.Printf("[kunmanga] Fetching chapters for slug: %s", slug)

	apiBase := fmt.Sprintf("https://www.kunmanga.online/api/comics/%s/chapters", slug)
	page := 1
	var allChapters []map[string]string

	for {
		apiURL := fmt.Sprintf("%s?page=%d", apiBase, page)

		var resp kunmangaChapterResponse
		if err := client.FetchJSON(context.Background(), apiURL, &resp); err != nil {
			return nil, fmt.Errorf("[kunmanga] failed to fetch page %d: %w", page, err)
		}

		if !resp.Success {
			return nil, fmt.Errorf("[kunmanga] API returned success=false on page %d", page)
		}

		for _, ch := range resp.Data.Chapters {
			chapterURL := fmt.Sprintf("https://www.kunmanga.online/manga/%s/%s", slug, ch.Slug)
			numStr := strconv.FormatFloat(ch.Num, 'f', -1, 64)
			allChapters = append(allChapters, map[string]string{
				"num": numStr,
				"url": chapterURL,
			})
		}

		if page >= resp.Data.LastPage {
			break
		}
		page++

		// Rate limiting — be nice to the API
		time.Sleep(200 * time.Millisecond)
	}

	log.Printf("[kunmanga] Found %d chapters", len(allChapters))

	return allChapters, nil
}

// extractKunmangaSlug extracts the manga slug from a URL like
//
//	https://www.kunmanga.online/manga/ugly-complex
func extractKunmangaSlug(mangaURL string) (string, error) {
	parsed, err := url.Parse(mangaURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse manga URL: %w", err)
	}

	path := strings.Trim(parsed.Path, "/")
	segments := strings.Split(path, "/")

	for i, seg := range segments {
		if seg == "manga" && i+1 < len(segments) {
			return segments[i+1], nil
		}
	}

	// Fallback: return the last non-empty segment
	for i := len(segments) - 1; i >= 0; i-- {
		if segments[i] != "" {
			return segments[i], nil
		}
	}

	return "", fmt.Errorf("could not extract slug from URL: %s", mangaURL)
}

// NormalizeChapterURL converts raw URL to absolute URL
// PARSING LOGIC ONLY - returns a string
func (k *KunmangaSite) NormalizeChapterURL(rawURL, baseURL string) string {
	// URLs from kunmanga are already absolute
	return rawURL
}

// NormalizeChapterFilename converts chapter data to filename
// PARSING LOGIC ONLY - returns a string
func (k *KunmangaSite) NormalizeChapterFilename(data map[string]string) string {
	chapterURL := data["url"]

	// Regex: match chapter number and optional part numbers
	// Handles URLs like: /chapter-18/ or /chapter-18-5/
	re := regexp.MustCompile(`chapter[-_\.]?(\d+)((?:[-_\.]\d+)*)`)

	matches := re.FindStringSubmatch(chapterURL)
	if len(matches) == 0 {
		log.Printf("[Kunmanga] WARNING: Could not parse chapter number from URL: %s", chapterURL)
		return "ch000.cbz"
	}

	mainNum := matches[1] // main chapter number
	partStr := matches[2] // optional part string, e.g., "-5" or ".5"

	// Normalize separators: replace - or _ with .
	normalizedPart := strings.ReplaceAll(partStr, "-", ".")
	normalizedPart = strings.ReplaceAll(normalizedPart, "_", ".")

	// Remove leading dot (if any)
	normalizedPart = strings.TrimPrefix(normalizedPart, ".")

	// Final filename: pad main number to 3 digits
	filename := fmt.Sprintf("ch%03s", mainNum)
	if normalizedPart != "" {
		filename += "." + normalizedPart
	}

	log.Printf("[Kunmanga] Normalized: %s → %s.cbz", chapterURL, filename)
	return filename + ".cbz"
}

// KunmangaDownloadChapters is the entry point called by the download queue
func KunmangaDownloadChapters(ctx context.Context, manga *config.Bookmarks, progressCallback func(string, float64, int, int, int)) error {
	site := &KunmangaSite{}

	cfg := &downloader.DownloadConfig{
		Manga:            manga,
		Site:             site,
		ProgressCallback: progressCallback,
	}

	manager := downloader.NewManager(cfg)
	return manager.Download(ctx)
}
