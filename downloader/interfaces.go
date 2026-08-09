package downloader

import (
	"time"

	//"context"
	"kansho/config"
)

// ChapterExtractionMethod defines how to extract chapters from a page
type ChapterExtractionMethod struct {
	// Type: "javascript", "html_selector", "custom", or "api"
	Type string

	// For Type="javascript": JavaScript code to execute
	JavaScript string

	// For Type="javascript" with a paginated list: plain JavaScript (no
	// promises) that clicks the "next page" control and returns true when it
	// did, false on the last page. When set, extraction repeats page by page
	// and unique results are accumulated, replacing async/promise IIFEs.
	NextPageJS string

	// For Type="html_selector": CSS selector
	Selector string

	// WaitSelector: CSS selector to wait for before extraction
	WaitSelector string

	// Timeout: optional maximum time for the browser extraction step.
	// Sites whose chapter lists need paginating through can raise this above
	// the 45s default. Zero means use the default.
	Timeout time.Duration

	// CustomParser: optional function for custom parsing logic
	// Receives HTML, returns map[filename]url
	CustomParser func(html string) (map[string]string, error)

	// For Type="api": Custom API extraction function
	// Receives base URL and API client, returns raw chapter data
	APIFunc func(baseURL string, client *APIClient) ([]map[string]string, error)
}

// ImageExtractionMethod defines how to extract images from a chapter page
type ImageExtractionMethod struct {
	// Type: "javascript", "html_selector", "custom", or "api"
	Type string

	// For Type="javascript": JavaScript code to execute
	JavaScript string

	// ScrollToLoad scrolls the page incrementally before extraction so
	// lazy-loaded images (e.g. long-strip readers) are fetched. The scrolling
	// is driven by the browser step by step — no JS promises required.
	ScrollToLoad bool

	// For Type="html_selector": CSS selector + attribute
	Selector  string
	Attribute string // e.g., "src", "data-src"

	// WaitSelector: CSS selector to wait for before extraction
	WaitSelector string

	// Timeout: optional maximum time for the browser extraction step.
	// Sites whose images only exist in React fiber data (lazy-loaded) may
	// need this raised above the 45s default. Zero means use the default.
	Timeout time.Duration

	// CustomParser: optional function for custom parsing logic
	// Receives HTML, returns []imageURL
	CustomParser func(html string) ([]string, error)

	// For Type="api": Custom API extraction function
	// Receives chapter URL, chapter data, and API client, returns image URLs
	APIFunc func(chapterURL string, chapterData map[string]string, client *APIClient) ([]string, error)
}

// SitePlugin defines the interface that all manga sites must implement.
// Sites provide ONLY extraction logic - the downloader handles ALL execution.
type SitePlugin interface {
	// GetSiteName returns the site identifier (e.g., "stonescape", "asura")
	GetSiteName() string

	// GetDomain returns a hint for the site domain used for CF bypass (e.g. "stonescape.xyz").
	// This is used as a fallback only. In practice the domain is always derived
	// from the actual request URL so that www/non-www mismatches between the
	// hard-coded value and what the browser extension captures are never an issue.
	GetDomain() string

	// NeedsCFBypass returns true if this site requires Cloudflare bypass
	NeedsCFBypass() bool

	// GetChapterExtractionMethod returns HOW to extract chapters
	// The downloader will execute this method
	GetChapterExtractionMethod() *ChapterExtractionMethod

	// GetImageExtractionMethod returns HOW to extract images
	// The downloader will execute this method
	GetImageExtractionMethod() *ImageExtractionMethod

	// NormalizeChapterURL converts a raw chapter URL to absolute URL if needed
	NormalizeChapterURL(rawURL, baseURL string) string

	// NormalizeChapterFilename converts raw chapter data to filename
	// e.g., "72" -> "ch072.cbz", "72.5" -> "ch072.5.cbz"
	NormalizeChapterFilename(chapterData map[string]string) string
}

// ProgressCallback is called during download to report progress
// Parameters: status message, progress (0.0-1.0), actual chapter number, current download index, total chapters
type ProgressCallback func(string, float64, int, int, int)

// DownloadConfig holds configuration for a download session
type DownloadConfig struct {
	Manga            *config.Bookmarks
	Site             SitePlugin
	ProgressCallback ProgressCallback
}

// DebuggableSite is implemented by sites that provide optional debugging support.
// Sites that do not implement this interface simply do not expose debugging features.
type DebugSite interface {
	// Debugger returns the debugging configuration for this site.
	// Returning nil means no debugging is enabled.
	Debugger() *Debugger
}

// ManualCFPromptSite is implemented by sites that need to always open the
// manga URL in the user's real browser before chapter extraction, regardless
// of whether a Cloudflare challenge is detected on the page. This is useful
// for sites where CF cookies must be captured for use during image downloads,
// even though the main manga page itself may not trigger a CF challenge.
// Sites that do not implement this interface behave as normal.
type ManualCFPromptSite interface {
	NeedsManualCFPrompt() bool
}

// ImageDecryptorSite is implemented by sites where images are encrypted in
// transit and must be decrypted/descrambled client-side. The download manager
// calls TransformImage on each raw image byte slice fetched from the network.
type ImageDecryptorSite interface {
	// TransformImage decrypts or descrambles a single image.
	// chapterKey is the raw bytes of the base64-decoded chapter decryption key.
	// gridSize is the tile grid dimension (e.g. 4 for 4x4 grid).
	// pageIndex is the 0-based index of the image in the chapter.
	// rawURL is the original image URL (for logging/context).
	// encryptedData is the raw HTTP response body.
	// Returns the clean image bytes (e.g. JPEG/PNG).
	TransformImage(chapterKey []byte, gridSize int, pageIndex int, rawURL string, encryptedData []byte) ([]byte, error)
}

// Debugger defines optional debugging behavior for a site
// Sites may return nil if no debugging is required
type Debugger struct {
	// SaveHTML indicates whether the full HTML should be saved for debugging
	SaveHTML bool
	// HTMLPath is the file path where HTML should be written
	HTMLPath string

	// not used yet
	SaveRaw     bool
	RawPath     string
	SaveHeaders bool
	HeadersPath string
}
