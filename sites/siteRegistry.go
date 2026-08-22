package sites

import (
	"context"
	"fmt"

	"kansho/config"
	"kansho/downloader"
)

// sitePlugins maps site names to SitePlugin constructors.
// These are used for fetching chapter lists remotely and for single-chapter
// downloads. Sites without a plugin entry (e.g. hls, which has its own legacy
// downloader) cannot be browsed/queried remotely and will only show locally
// downloaded chapters.
var sitePlugins = map[string]func() downloader.SitePlugin{
	"mgeko":       func() downloader.SitePlugin { return &MgekoSite{} },
	"manhuaus":    func() downloader.SitePlugin { return &ManhuausSite{} },
	"kunmanga":    func() downloader.SitePlugin { return &KunmangaSite{} },
	"asurascans":  func() downloader.SitePlugin { return &AsuraSite{} },
	"mangakatana": func() downloader.SitePlugin { return &MangakatanaSite{} },
	"mangadex":    func() downloader.SitePlugin { return &MangadexSite{} },
	"stonescape":  func() downloader.SitePlugin { return &StonescapeSite{} },
	"ravenscans":  func() downloader.SitePlugin { return &RavenscansSite{} },
	"cubari":      func() downloader.SitePlugin { return &CubariSite{} },
	"flamecomics": func() downloader.SitePlugin { return &FlameComicsSite{} },
	"weebcentral": func() downloader.SitePlugin { return &WeebcentralSite{} },
	"philiascans": func() downloader.SitePlugin { return &PhiliaScansSite{} },
	"comix":       func() downloader.SitePlugin { return &ComixSite{} },
	"kingofshojo": func() downloader.SitePlugin { return &KingOfShojoSite{} },
	"arenascan":   func() downloader.SitePlugin { return &ArenascanSite{} },
}

// GetSitePlugin returns a new SitePlugin instance for the given site name,
// or nil if the site does not implement the plugin interface.
// The caller should treat the returned plugin as immutable and not shared.
func GetSitePlugin(name string) downloader.SitePlugin {
	if fn, ok := sitePlugins[name]; ok {
		return fn()
	}
	return nil
}

// DownloadSingleChapter is the generic entry point used by the download queue
// to download a single chapter of a manga. It looks up the site plugin for the
// manga's site and delegates to the downloader manager.
func DownloadSingleChapter(ctx context.Context, manga *config.Bookmarks, chapterURL, cbzName string, progressCallback func(string, float64, int, int, int)) error {
	site := GetSitePlugin(manga.Site)
	if site == nil {
		return fmt.Errorf("no site plugin registered for site: %s", manga.Site)
	}

	cfg := &downloader.DownloadConfig{
		Manga:            manga,
		Site:             site,
		ProgressCallback: progressCallback,
	}

	manager := downloader.NewManager(cfg)
	return manager.DownloadSingleChapter(ctx, chapterURL, cbzName)
}

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
	config.RegisterSite("weebcentral", WeebcentralDownloadChapters) // Implements downloader interface
	config.RegisterSite("philiascans", PhiliaScansDownloadChapters)
	config.RegisterSite("comix", ComixDownloadChapters)
	config.RegisterSite("kingofshojo", KingOfShojoDownloadChapters)
	config.RegisterSite("arenascan", ArenascanDownloadChapters)

	// Register the generic single-chapter download dispatcher
	config.RegisterChapterDownload(DownloadSingleChapter)

	// Register which sites require a Cloudflare bypass so the download queue
	// can skip CF-protected chapters when no bypass data has been provided.
	for name, ctor := range sitePlugins {
		config.RegisterSiteCfRequirement(name, ctor().NeedsCFBypass())
	}

	// Add new sites here in the future:
	// config.RegisterSite("newsite", NewsiteDownloadChapters)
}
