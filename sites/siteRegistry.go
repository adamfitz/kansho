package sites

import (
	"kansho/config"
)

// init() is called automatically when the package is imported
// This registers all site download functions with the queue system
func init() {
	config.RegisterSite("mgeko", MgekoDownloadChapters)
	config.RegisterSite("rizzfables", RizzfablesDownloadChapters)
	config.RegisterSite("manhuaus", ManhuausDownloadChapters)
	config.RegisterSite("kunmanga", KunmangaDownloadChapters)
	config.RegisterSite("hls", HlsDownloadChapters)
	config.RegisterSite("asurascans", AsuraDownloadChapters)
	config.RegisterSite("mangakatana", MangakatanaDownloadChapters)
	config.RegisterSite("mangadex", MangadexDownloadChapters)
	config.RegisterSite("stonescape", StonescapeDownloadChapters) // Implements downloader interface

	// Add new sites here in the future:
	// config.RegisterSite("newsite", NewsiteDownloadChapters)
}
