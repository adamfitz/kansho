package config

import (
	"context"
	"fmt"
	"log"
)

// SiteDownloadFunc is the function signature for site-specific download functions
type SiteDownloadFunc func(context.Context, *Bookmarks, func(string, float64, int, int, int)) error

// registeredSites maps site names to their download functions
var registeredSites = make(map[string]SiteDownloadFunc)

// RegisterSite registers a site's download function
// This should be called during initialization by each site package
func RegisterSite(siteName string, downloadFunc SiteDownloadFunc) {
	registeredSites[siteName] = downloadFunc
	log.Printf("[Queue] Registered site: %s", siteName)
}

// ExecuteSiteDownload dispatches to the appropriate site-specific download function
func ExecuteSiteDownload(ctx context.Context, manga *Bookmarks, progressCallback func(string, float64, int, int, int)) error {
	downloadFunc, exists := registeredSites[manga.Site]
	if !exists {
		log.Printf("[Queue] ERROR: Site '%s' not registered. Available sites: %v", manga.Site, getRegisteredSiteNames())
		return fmt.Errorf("download not supported for site: %s (not registered)", manga.Site)
	}

	log.Printf("[Queue] Dispatching download for site: %s", manga.Site)
	return downloadFunc(ctx, manga, progressCallback)
}

// getRegisteredSiteNames returns a list of all registered site names (for debugging)
func getRegisteredSiteNames() []string {
	names := make([]string, 0, len(registeredSites))
	for name := range registeredSites {
		names = append(names, name)
	}
	return names
}

// ChapterDownloadFunc downloads a single chapter of a manga.
// Parameters: context, manga, chapter URL, chapter CBZ filename, progress callback
type ChapterDownloadFunc func(context.Context, *Bookmarks, string, string, func(string, float64, int, int, int)) error

// registeredChapterDownload is the generic single-chapter download dispatcher.
// It is registered by the sites package during initialization.
var registeredChapterDownload ChapterDownloadFunc

// RegisterChapterDownload registers the generic single-chapter download function.
// This should be called during initialization by the sites package.
func RegisterChapterDownload(downloadFunc ChapterDownloadFunc) {
	registeredChapterDownload = downloadFunc
	log.Printf("[Queue] Registered chapter download dispatcher")
}

// ExecuteChapterDownload dispatches a single-chapter download.
// Parameters:
//   - ctx: cancellable context for aborting the download
//   - manga: the manga the chapter belongs to
//   - chapterURL: the URL of the chapter on the target site
//   - cbzName: the CBZ filename to produce, e.g. "ch001.cbz"
//   - progressCallback: callback for reporting download progress
func ExecuteChapterDownload(ctx context.Context, manga *Bookmarks, chapterURL, cbzName string, progressCallback func(string, float64, int, int, int)) error {
	if registeredChapterDownload == nil {
		return fmt.Errorf("no chapter download dispatcher registered")
	}
	return registeredChapterDownload(ctx, manga, chapterURL, cbzName, progressCallback)
}
