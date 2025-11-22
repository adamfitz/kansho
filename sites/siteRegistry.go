package sites

import (
	"kansho/config"
)

// RegisterAllSites registers all site download functions with the download queue
// This should be called during application initialization
func RegisterAllSites() {
	config.RegisterSite("mgeko", MgekoDownloadChapters)
	config.RegisterSite("xbato", XbatoDownloadChapters)
	config.RegisterSite("rizzfables", RizzfablesDownloadChapters)
	config.RegisterSite("manhuaus", ManhuausDownloadChapters)
	config.RegisterSite("kunmanga", KunmangaDownloadChapters)
	config.RegisterSite("hls", HlsDownloadChapters)

	// Add new sites here in the future:
	// config.RegisterSite("newsite", NewsiteDownloadChapters)
}
