package downloader

import (
	//"context"
	"kansho/config"
)

// ChapterExtractionMethod defines how to extract chapters from a page
type ChapterExtractionMethod struct {
	// Type: "javascript", "html_selector", "custom", or "api"
	Type string

	// For Type="javascript": JavaScript code to execute
	JavaScript string

	// For Type="html_selector": CSS selector
	Selector string

	// WaitSelector: CSS selector to wait for before extraction
	WaitSelector string

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

	// For Type="html_selector": CSS selector + attribute
	Selector  string
	Attribute string // e.g., "src", "data-src"

	// WaitSelector: CSS selector to wait for before extraction
	WaitSelector string

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

	// GetDomain returns the site domain for CF bypass (e.g., "stonescape.xyz")
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
