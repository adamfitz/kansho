package config

import (
	"context"
	"fmt"
)

// SiteDownloadFunc is the function signature for site-specific download functions
type SiteDownloadFunc func(context.Context, *Bookmarks, func(string, float64, int, int, int)) error

// registeredSites maps site names to their download functions
var registeredSites = make(map[string]SiteDownloadFunc)

// RegisterSite registers a site's download function
// This should be called during initialization by each site package
func RegisterSite(siteName string, downloadFunc SiteDownloadFunc) {
	registeredSites[siteName] = downloadFunc
}

// ExecuteSiteDownload dispatches to the appropriate site-specific download function
func ExecuteSiteDownload(ctx context.Context, manga *Bookmarks, progressCallback func(string, float64, int, int, int)) error {
	downloadFunc, exists := registeredSites[manga.Site]
	if !exists {
		return fmt.Errorf("download not supported for site: %s", manga.Site)
	}

	return downloadFunc(ctx, manga, progressCallback)
}
