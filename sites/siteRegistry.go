package sites

import (
	"kansho/config"
)

// init() is called automatically when the package is imported
// This registers all site download functions with the queue system
func init() {
	config.RegisterSite("mgeko", MgekoDownloadChapters)       // Implements downloader interface
	config.RegisterSite("manhuaus", ManhuausDownloadChapters) // Implements downloader interface
	config.RegisterSite("kunmanga", KunmangaDownloadChapters) // Implements downloader interface
	config.RegisterSite("hls", HlsDownloadChapters)
	config.RegisterSite("asurascans", AsuraDownloadChapters)
	config.RegisterSite("mangakatana", MangakatanaDownloadChapters) // Implements downloader interface
	config.RegisterSite("mangadex", MangadexDownloadChapters)       // Implements downloader interface
	config.RegisterSite("stonescape", StonescapeDownloadChapters)   // Implements downloader interface
	config.RegisterSite("ravenscans", RavenscansDownloadChapters)   // Implements downloader interface
	config.RegisterSite("cubari", CubariDownloadChapters)           // Implements downloader interface
	config.RegisterSite("flamecomics", FlameComicsDownloadChapters) // Implements downloader interface

	// Add new sites here in the future:
	// config.RegisterSite("newsite", NewsiteDownloadChapters)
}
