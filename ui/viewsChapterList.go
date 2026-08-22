package ui

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"kansho/cf"
	"kansho/config"
	"kansho/downloader"
	"kansho/parser"
	"kansho/refreshpool"
	"kansho/sites"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// chapterState is the per-chapter download state shown in the chapter list.
type chapterState int

const (
	chapterNotDownloaded chapterState = iota
	chapterDownloaded
	chapterQueued
	chapterDownloading
	chapterWaitingCF
	chapterFailed
	chapterCancelled
)

// ChapterItem is a single row in the chapter list.
// The chapter list is split into three panes: name, progress bar, and the
// download indicator / action controls.
type ChapterItem struct {
	Name       string
	URL        string
	Downloaded bool
	State      chapterState
	Progress   float64
}

// ChapterListView is the right-hand card that shows the chapters for the
// currently selected manga. Each chapter row has three panes:
//   - Left:  the chapter name
//   - Middle: a per-chapter download progress bar
//   - Right:  a green tick (downloaded), a download arrow button (not
//     downloaded), and a red circle-with-slash button (cancel / delete),
//     right-aligned
//
// Selecting a manga loads ONLY the chapters that exist on disk plus any remote
// chapters that were previously fetched for that manga via the Refresh button.
// The per-manga remote chapter cache (remoteChapters) persists for the lifetime
// of the application, so switching away to another manga and back keeps showing
// the remote chapters. Remote chapters are NEVER loaded automatically; they are
// populated only when the user clicks Refresh. New chapters are added to the
// list but NOT downloaded until the user explicitly starts them.
type ChapterListView struct {
	Card                fyne.CanvasObject
	selectedMangaLabel  *widget.Label
	chapterList         *widget.List
	contentContainer    *fyne.Container
	downloadAllButton   *widget.Button
	refreshButton       *widget.Button
	downloadQueueButton *DownloadQueueButton
	loadingIndicator    *ajaxSpinner
	state               *KanshoAppState
	chapters            []*ChapterItem
	cfDialogShown       map[string]bool
	loadGeneration      int
	refreshing          bool
	// remoteChapters caches, per manga title, the chapters fetched from the
	// target site (chapter name -> download URL). It persists for the lifetime
	// of the application so refreshed chapters stay visible across manga
	// switches.
	remoteChapters map[string]map[string]string

	// statusBar is the main window's status bar. It is optional (nil in
	// tests); when attached, it shows the selected manga's site and chapter
	// counts.
	statusBar *MainStatusBar
}

func NewChapterListView(state *KanshoAppState, downloadQueueButton *DownloadQueueButton) *ChapterListView {
	view := &ChapterListView{
		state:               state,
		chapters:            []*ChapterItem{},
		cfDialogShown:       make(map[string]bool),
		downloadQueueButton: downloadQueueButton,
		remoteChapters:      make(map[string]map[string]string),
	}

	view.selectedMangaLabel = widget.NewLabel("")
	view.selectedMangaLabel.Truncation = fyne.TextTruncateEllipsis
	view.selectedMangaLabel.TextStyle = fyne.TextStyle{Bold: true}

	// Download All button - adds all missing chapters for the selected manga
	// to the download queue
	view.downloadAllButton = widget.NewButton("Download All Missing", func() {
		view.onDownloadAllClicked()
	})
	view.downloadAllButton.Disable()

	// Refresh button - fetches the remote chapter list from the target site
	// and merges new (not downloaded) chapters into the list. Remote chapters
	// are never loaded automatically.
	view.refreshButton = widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), func() {
		view.onRefreshClicked()
	})
	view.refreshButton.Disable()

	// AJAX-style spinner wheel shown while the chapter list is loading (from
	// disk or the site)
	view.loadingIndicator = newAJAXSpinner()
	view.loadingIndicator.Hide()

	// The chapter list, split into 3 panes per row:
	// | chapter name | progress bar | tick / download / cross controls |
	view.chapterList = widget.NewList(
		func() int {
			return len(view.chapters)
		},
		view.createChapterRow,
		view.updateChapterRow,
	)

	view.contentContainer = container.NewStack(
		widget.NewLabel(""),
	)

	// Bottom bar: loading spinner (left), Refresh + Download All (right)
	bottomBar := container.NewBorder(
		nil,
		nil,
		view.loadingIndicator,
		nil,
		container.NewHBox(
			layout.NewSpacer(),
			view.refreshButton,
			view.downloadAllButton,
		),
	)

	cardContent := container.NewBorder(
		container.NewVBox(
			NewBoldLabel("Chapter List"),
			NewSeparator(),
			view.selectedMangaLabel,
		),
		container.NewVBox(
			NewSeparator(),
			bottomBar,
		),
		nil,
		nil,
		view.contentContainer,
	)

	view.Card = NewCard(cardContent)

	// Register queue callbacks so per-chapter progress and the download queue
	// summary button stay in sync with the active downloads.
	queue := config.GetDownloadQueue()
	queue.SetCallbacks(
		func(task *config.DownloadTask) {
			fyne.Do(func() {
				view.refreshAfterTaskChange()
			})
		},
		func(task *config.DownloadTask) {
			fyne.Do(func() {
				if task.Status == "waiting_cf" && !view.cfDialogShown[task.ID] {
					view.showCFDialog(task)
					view.cfDialogShown[task.ID] = true
				}
				view.refreshAfterTaskChange()
			})
		},
		func(taskID string) {
			fyne.Do(func() {
				delete(view.cfDialogShown, taskID)
				view.refreshAfterTaskChange()
			})
		},
		func() {
			fyne.Do(func() {
				view.refreshAfterTaskChange()
			})
		},
	)

	view.state.RegisterMangaSelectedCallback(func(id int) {
		view.onMangaSelected(id)
	})

	view.state.RegisterMangaDeletedCallback(func(id int) {
		if view.state.SelectedMangaID == -1 {
			view.showNoSelection()
		}
	})

	return view
}

// SetStatusBar attaches the main window's status bar to this view. The view
// keeps it up to date as the selection, chapter list, and download queue
// change. The bar is optional; without it the view works exactly as before.
func (v *ChapterListView) SetStatusBar(bar *MainStatusBar) {
	v.statusBar = bar
	bar.SetIdle()
}

// updateStatusBar refreshes the main window status bar for the currently
// selected manga. The site and the number of downloaded chapters are always
// shown. The number of not-downloaded chapters is only shown once remote
// chapters have been fetched for this manga — i.e. after the user pressed
// Refresh on the chapter list (the per-manga remote cache is only populated by
// a successful refresh).
func (v *ChapterListView) updateStatusBar() {
	if v.statusBar == nil {
		return
	}

	manga := v.state.GetSelectedManga()
	if manga == nil {
		v.statusBar.SetIdle()
		return
	}

	downloaded := 0
	for _, ch := range v.chapters {
		if ch.Downloaded {
			downloaded++
		}
	}
	notDownloaded := len(v.chapters) - downloaded

	site := manga.Site
	if site == "" && manga.Url != "" {
		if host, err := url.Parse(manga.Url); err == nil && host.Hostname() != "" {
			site = strings.TrimPrefix(host.Hostname(), "www.")
		}
	}

	_, refreshed := v.remoteChapters[manga.Title]
	v.statusBar.ShowManga(site, downloaded, notDownloaded, refreshed)
}

// onMangaSelected rebuilds the chapter list for the newly selected manga from
// the chapters that exist on disk merged with any remote chapters previously
// fetched for this manga via the Refresh button. It does NOT contact the target
// site; remote chapters are only populated when the user clicks Refresh. The
// cached remote chapters persist for the lifetime of the application.
func (v *ChapterListView) onMangaSelected(id int) {
	v.loadGeneration++
	v.refreshing = false

	manga := v.state.GetSelectedManga()
	if manga == nil {
		v.showNoSelection()
		return
	}

	v.selectedMangaLabel.SetText(manga.Title)
	v.downloadAllButton.Disable()
	v.refreshButton.Disable()
	v.chapters = []*ChapterItem{}
	v.chapterList.Refresh()
	v.startLoading()

	v.presentChapters(manga)

	v.refreshAfterTaskChange()
	v.refreshButton.Enable()
	v.stopLoading()

	v.updateStatusBar()
}

// presentChapters builds the displayed chapter list for the selected manga from
// its on-disk chapters merged with the manga's cached remote chapters.
func (v *ChapterListView) presentChapters(manga *config.Bookmarks) {
	items := buildChapterItems(localChapterNames(manga), v.remoteChapters[manga.Title])

	v.chapters = items
	if len(items) == 0 {
		v.contentContainer.Objects = []fyne.CanvasObject{
			widget.NewLabel("No chapters found for this manga"),
		}
	} else {
		v.contentContainer.Objects = []fyne.CanvasObject{v.chapterList}
	}
	v.contentContainer.Refresh()
	v.chapterList.Refresh()
}

// localChapterNames lists the chapter CBZ files for the manga that exist on
// disk, sorted by name.
func localChapterNames(manga *config.Bookmarks) []string {
	if manga == nil || manga.Location == "" {
		return nil
	}
	names, err := parser.LocalChapterList(manga.Location)
	if err != nil {
		log.Printf("[UI] Failed to list local chapters for %s: %v", manga.Title, err)
		return nil
	}
	sort.Strings(names)
	return names
}

// buildChapterItems merges the on-disk chapters with the cached remote chapters
// into the ChapterItems shown in the chapter list. On-disk chapters are marked
// as downloaded; remote chapters that are not on disk keep their download URL
// so the user can start them.
func buildChapterItems(localNames []string, remote map[string]string) []*ChapterItem {
	downloaded := make(map[string]bool, len(localNames))
	for _, name := range localNames {
		downloaded[name] = true
	}

	names := make([]string, 0, len(localNames)+len(remote))
	seen := make(map[string]bool, len(localNames)+len(remote))
	for _, name := range localNames {
		if !seen[name] {
			names = append(names, name)
			seen[name] = true
		}
	}
	for name := range remote {
		if !seen[name] {
			names = append(names, name)
			seen[name] = true
		}
	}
	sort.Strings(names)

	items := make([]*ChapterItem, 0, len(names))
	for _, name := range names {
		item := &ChapterItem{Name: name, Downloaded: downloaded[name]}
		if url, ok := remote[name]; ok {
			item.URL = url
		}
		if item.Downloaded {
			item.State = chapterDownloaded
			item.Progress = 1.0
		} else {
			item.State = chapterNotDownloaded
		}
		items = append(items, item)
	}
	return items
}

// refreshAttemptTimeout bounds a single chapter-list scrape attempt. The
// refresh pool owns retries and backoff, so each attempt simply needs enough
// time for one full navigation/pagination pass.
const refreshAttemptTimeout = 90 * time.Second

// onRefreshClicked submits a chapter-list refresh job to the dedicated
// refresh worker pool (refreshpool) instead of spawning its own goroutine.
// The pool serializes scrapes per site, caps global parallelism and applies
// exponential backoff with retries, so rate-limited or slow sites are handled
// without blocking downloads or the UI.
func (v *ChapterListView) onRefreshClicked() {
	if v.refreshing {
		return
	}
	manga := v.state.GetSelectedManga()
	if manga == nil {
		return
	}

	site := sites.GetSitePlugin(manga.Site)
	if site == nil || manga.Url == "" {
		log.Printf("[UI] No site plugin or URL for %s - nothing to refresh", manga.Title)
		return
	}

	v.refreshing = true
	v.refreshButton.Disable()
	v.startLoading()

	gen := v.loadGeneration
	title, siteName, targetURL := manga.Title, manga.Site, manga.Url

	submitted := refreshpool.Get().Submit(&refreshpool.Task{
		Site: siteName,
		Desc: title,
		// Reject duplicate submissions while this manga's fetch is already
		// pending or running somewhere in the pool.
		DedupeKey: "chapters:" + targetURL,
		// A CF challenge waits for the user to import bypass data; retrying
		// would just keep reopening the browser, so fail immediately and let
		// OnError show the import dialog.
		NoRetry: func(err error) bool {
			var cfErr *cf.CfChallengeError
			return errors.As(err, &cfErr)
		},
		Run: func(ctx context.Context) error {
			fetchCtx, cancel := context.WithTimeout(ctx, refreshAttemptTimeout)
			defer cancel()

			remote, err := downloader.FetchChapterURLsSingle(fetchCtx, targetURL, site)
			if err != nil {
				return err
			}
			fyne.Do(func() {
				if v.loadGeneration != gen || v.state.SelectedMangaID < 0 {
					return // user navigated away; discard stale result
				}

				// Persist the fetched remote chapters in the per-manga cache
				// and merge them into the displayed list. The cache keeps them
				// visible whenever the user switches back to this manga.
				v.mergeRemoteChapters(title, remote)

				v.contentContainer.Objects = []fyne.CanvasObject{v.chapterList}
				v.contentContainer.Refresh()
				v.refreshAfterTaskChange()
				v.updateStatusBar()
			})
			return nil
		},
		OnSuccess: func() {
			fyne.Do(func() { v.finishRefresh(gen, "") })
		},
		OnError: func(err error) {
			var cfErr *cf.CfChallengeError
			if errors.As(err, &cfErr) {
				url := cfErr.URL
				fyne.Do(func() { v.finishRefresh(gen, url) })
				return
			}
			log.Printf("[UI] Failed to fetch remote chapters for %s: %v", title, err)
			fyne.Do(func() { v.finishRefresh(gen, "") })
		},
	})
	if !submitted {
		// Either this manga's fetch is already pending in the pool or the
		// submission was rejected - either way restore the button immediately.
		log.Printf("[UI] Refresh for %s not submitted (duplicate or rejected)", title)
		v.finishRefresh(gen, "")
	}
}

// finishRefresh ends a refresh run: restores the refresh button and loading
// indicator, and shows the CF import dialog when the failure was a Cloudflare
// challenge (re-triggering the refresh once fresh CF data is imported).
// Must be called on the UI goroutine; does nothing when gen is stale.
func (v *ChapterListView) finishRefresh(gen int, cfURL string) {
	if v.loadGeneration != gen {
		return
	}
	v.refreshing = false
	v.refreshButton.Enable()
	v.stopLoading()
	if cfURL != "" {
		ShowcfDialog(v.state.Window, cfURL, func() {
			v.onRefreshClicked()
		})
	}
}

// mergeRemoteChapters stores fetched remote chapters in the per-manga cache
// (which persists for the lifetime of the application) and merges them into the
// currently displayed chapter list.
func (v *ChapterListView) mergeRemoteChapters(mangaTitle string, remote map[string]string) {
	cached := v.remoteChapters[mangaTitle]
	if cached == nil {
		cached = make(map[string]string)
		v.remoteChapters[mangaTitle] = cached
	}
	for name, url := range remote {
		cached[name] = url
	}

	existing := make(map[string]*ChapterItem, len(v.chapters))
	for _, ch := range v.chapters {
		existing[ch.Name] = ch
	}

	added := false
	for name, url := range cached {
		if ch, ok := existing[name]; ok {
			if ch.URL == "" {
				ch.URL = url
			}
			continue
		}
		v.chapters = append(v.chapters, &ChapterItem{
			Name:  name,
			URL:   url,
			State: chapterNotDownloaded,
		})
		added = true
	}

	if added {
		sort.Slice(v.chapters, func(i, j int) bool {
			return v.chapters[i].Name < v.chapters[j].Name
		})
	}
}

// createChapterRow builds the template object for a chapter list row. It is
// split into three panes: chapter name, progress bar, and a right-aligned
// column of controls (green tick, download button, red X button).
func (v *ChapterListView) createChapterRow() fyne.CanvasObject {
	label := widget.NewLabel("template")
	label.Truncation = fyne.TextTruncateEllipsis

	progressBar := widget.NewProgressBar()
	progressBar.Min = 0
	progressBar.Max = 1

	// Green tick indicator for downloaded chapters. widget.Icon has no button
	// chrome and no hover highlight.
	tickIcon := widget.NewIcon(greenTickResource)
	tickIcon.Hide()

	// Download arrow icon for chapters that are not downloaded. It is a plain
	// tappable icon (no button chrome, no hover highlight) so the row controls
	// stay visually quiet, especially while a download is running.
	downloadButton := newIconButton(theme.DownloadIcon())

	// Red circle-with-slash icon: cancels an active download or deletes the
	// local chapter file (with confirmation). Like the download arrow it is a
	// plain tappable icon with no hover highlight.
	xButton := newIconButton(redNoEntryResource)

	rightColumn := container.NewHBox(tickIcon, downloadButton, xButton)
	// The Border places the control column on the far right of its grid cell.
	rightCell := container.NewBorder(nil, nil, nil, rightColumn)

	return container.NewGridWithColumns(3, label, progressBar, rightCell)
}

// updateChapterRow sets the contents and controls of a chapter list row based
// on the chapter's download state and any task in the download queue.
func (v *ChapterListView) updateChapterRow(id widget.ListItemID, item fyne.CanvasObject) {
	if int(id) >= len(v.chapters) {
		return
	}
	ch := v.chapters[id]

	grid := item.(*fyne.Container)
	label := grid.Objects[0].(*widget.Label)
	progressBar := grid.Objects[1].(*widget.ProgressBar)
	rightCell := grid.Objects[2].(*fyne.Container)
	rightColumn := rightCell.Objects[0].(*fyne.Container)
	tickIcon := rightColumn.Objects[0].(*widget.Icon)
	downloadButton := rightColumn.Objects[1].(*iconButton)
	xButton := rightColumn.Objects[2].(*iconButton)

	label.SetText(ch.Name)
	rowID := int(id)

	switch {
	case ch.Downloaded:
		// Downloaded: green tick indicator, X deletes the local file.
		progressBar.SetValue(1.0)
		tickIcon.Show()
		tickIcon.Refresh()
		downloadButton.Hide()
		xButton.Enable()
		xButton.OnTapped = func() {
			v.deleteChapterFile(rowID)
		}
	case ch.State == chapterQueued || ch.State == chapterDownloading || ch.State == chapterWaitingCF:
		// Active download: progress in the middle pane, X cancels it.
		progressBar.SetValue(ch.Progress)
		tickIcon.Hide()
		downloadButton.Disable()
		xButton.Enable()
		xButton.OnTapped = func() {
			v.cancelChapterTask(rowID)
		}
	default:
		// Not downloaded and idle: download button starts the download.
		progressBar.SetValue(0)
		tickIcon.Hide()
		downloadButton.Show()
		downloadButton.Enable()
		downloadButton.OnTapped = func() {
			v.startChapterDownload(rowID)
		}
		xButton.Disable()
	}
}

// isActiveTaskStatus returns true if the task status represents a download
// that is currently active (queued, downloading, waiting on a CF challenge, or
// skipped because no CF data was provided). Stale (finished) statuses are the
// only ones a chapter re-download may remove from the queue.
func isActiveTaskStatus(status string) bool {
	return status == "queued" || status == "downloading" || status == "waiting_cf" || status == "skipped_cf"
}

// startChapterDownload queues a single chapter for download via the download
// arrow button.
func (v *ChapterListView) startChapterDownload(id int) {
	if id < 0 || id >= len(v.chapters) {
		return
	}
	ch := v.chapters[id]
	manga := v.state.GetSelectedManga()
	if manga == nil {
		return
	}
	if ch.Downloaded {
		return
	}
	if ch.URL == "" {
		dialog.ShowInformation("Cannot Download", fmt.Sprintf("No download URL is available for '%s'", ch.Name), v.state.Window)
		return
	}

	queue := config.GetDownloadQueue()

	// If a stale (non-active) task still exists for this chapter, drop it so
	// the chapter can be re-queued.
	if task := queue.GetTaskForChapter(manga.Title, ch.Name); task != nil && !isActiveTaskStatus(task.Status) {
		if err := queue.RemoveTask(task.ID); err != nil {
			log.Printf("[UI] Failed to remove stale task %s: %v", task.ID, err)
		}
	}

	if _, err := queue.AddChapterTask(manga, ch.Name, ch.URL); err != nil {
		dialog.ShowError(err, v.state.Window)
		return
	}

	log.Printf("[UI] Queued chapter download: %s - %s", manga.Title, ch.Name)
	// Intentionally NO "added to download queue" dialog.
}

// cancelChapterTask cancels the active download task for a chapter.
func (v *ChapterListView) cancelChapterTask(id int) {
	if id < 0 || id >= len(v.chapters) {
		return
	}
	ch := v.chapters[id]
	manga := v.state.GetSelectedManga()
	if manga == nil {
		return
	}

	queue := config.GetDownloadQueue()
	task := queue.GetTaskForChapter(manga.Title, ch.Name)
	if task == nil {
		return
	}

	if err := queue.CancelTask(task.ID); err != nil {
		dialog.ShowError(err, v.state.Window)
		return
	}
	log.Printf("[UI] Cancelled chapter download: %s - %s", manga.Title, ch.Name)
}

// deleteChapterFile asks the user to confirm deletion of the local CBZ file
// for a chapter before removing it from disk.
func (v *ChapterListView) deleteChapterFile(id int) {
	if id < 0 || id >= len(v.chapters) {
		return
	}
	ch := v.chapters[id]
	manga := v.state.GetSelectedManga()
	if manga == nil || manga.Location == "" {
		return
	}

	dialog.ShowConfirm(
		"Delete Chapter",
		fmt.Sprintf("Delete '%s' from disk?\nThis cannot be undone.", ch.Name),
		func(confirmed bool) {
			if !confirmed {
				return
			}
			if err := deleteChapterFileOnDisk(manga.Location, ch.Name); err != nil {
				dialog.ShowError(err, v.state.Window)
				return
			}

			// Drop any stale (non-active) task for this chapter so its
			// completed/cancelled status does not re-mark the file as
			// downloaded on the next refresh.
			queue := config.GetDownloadQueue()
			if task := queue.GetTaskForChapter(manga.Title, ch.Name); task != nil && !isActiveTaskStatus(task.Status) {
				if err := queue.RemoveTask(task.ID); err != nil {
					log.Printf("[UI] Failed to remove stale task %s: %v", task.ID, err)
				}
			}

			log.Printf("[UI] Deleted chapter file: %s - %s", manga.Title, ch.Name)
			v.onMangaSelected(v.state.SelectedMangaID)
		},
		v.state.Window,
	)
}

// deleteChapterFileOnDisk removes a chapter CBZ file from the manga's local
// directory. Missing files are treated as success so the UI can reload.
func deleteChapterFileOnDisk(location, name string) error {
	expanded, err := parser.ExpandPath(location)
	if err != nil {
		return err
	}
	path := filepath.Join(expanded, name)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// onDownloadAllClicked adds all missing chapters for the currently selected
// manga to the download queue.
func (v *ChapterListView) onDownloadAllClicked() {
	manga := v.state.GetSelectedManga()
	if manga == nil {
		return
	}

	queue := config.GetDownloadQueue()
	added := 0
	for _, ch := range v.chapters {
		if ch.Downloaded || ch.URL == "" {
			continue
		}
		if queue.ChapterQueued(manga.Title, ch.Name) {
			continue
		}
		if _, err := queue.AddChapterTask(manga, ch.Name, ch.URL); err != nil {
			log.Printf("[UI] Failed to queue %s for %s: %v", ch.Name, manga.Title, err)
			continue
		}
		added++
	}

	log.Printf("[UI] Queued %d missing chapters for %s", added, manga.Title)
}

// refreshAfterTaskChange reconciles the chapter list with the current state of
// the download queue and updates the queue summary button.
func (v *ChapterListView) refreshAfterTaskChange() {
	if v.downloadQueueButton != nil {
		v.downloadQueueButton.Refresh()
	}

	if v.state.SelectedMangaID < 0 {
		return
	}

	// Re-read the on-disk chapters for the selected manga. A completed chapter
	// task is removed from the queue as soon as its download finishes, which can
	// happen before the UI processes the "completed" update, so the queue alone
	// cannot be relied on to know that a download succeeded. The disk is the
	// source of truth for whether a chapter has been downloaded.
	v.reconcileDownloadedChapters()

	// Reset to the on-disk truth
	for _, ch := range v.chapters {
		if ch.Downloaded {
			ch.State = chapterDownloaded
			ch.Progress = 1.0
		} else {
			ch.State = chapterNotDownloaded
			ch.Progress = 0
		}
	}

	// Overlay active queue state
	for _, task := range config.GetDownloadQueue().GetTasks() {
		ch := v.findChapter(task.Manga.Title, task.Chapter)
		if ch == nil {
			continue
		}
		switch task.Status {
		case "queued":
			ch.State = chapterQueued
		case "downloading":
			ch.State = chapterDownloading
			ch.Progress = task.Progress
		case "waiting_cf", "skipped_cf":
			ch.State = chapterWaitingCF
		case "failed":
			ch.State = chapterFailed
		case "cancelled":
			ch.State = chapterCancelled
		case "completed":
			ch.Downloaded = true
			ch.State = chapterDownloaded
			ch.Progress = 1.0
		}
	}

	v.chapterList.Refresh()
	v.updateDownloadAllButton()
	v.updateStatusBar()
}

// reconcileDownloadedChapters marks every chapter in the list as downloaded when
// its CBZ file currently exists on disk for the selected manga. The on-disk
// state is the source of truth for whether a chapter has been downloaded: a
// chapter's completed queue task is removed from the queue as soon as it
// finishes, so the UI cannot rely on seeing the task to know the download
// succeeded.
func (v *ChapterListView) reconcileDownloadedChapters() {
	manga := v.state.GetSelectedManga()
	if manga == nil || manga.Location == "" {
		return
	}
	onDisk := make(map[string]bool)
	for _, name := range localChapterNames(manga) {
		onDisk[name] = true
	}
	for _, ch := range v.chapters {
		ch.Downloaded = onDisk[ch.Name]
	}
}

// findChapter returns the chapter item for the given manga title and chapter,
// but only if the manga is the currently selected one.
func (v *ChapterListView) findChapter(mangaTitle, chapterName string) *ChapterItem {
	manga := v.state.GetSelectedManga()
	if manga == nil || manga.Title != mangaTitle {
		return nil
	}
	for _, ch := range v.chapters {
		if ch.Name == chapterName {
			return ch
		}
	}
	return nil
}

// updateDownloadAllButton enables "Download All Missing" only when there is at
// least one missing chapter that can still be added to the queue.
func (v *ChapterListView) updateDownloadAllButton() {
	hasMissing := false
	for _, ch := range v.chapters {
		if !ch.Downloaded && ch.URL != "" && ch.State != chapterQueued && ch.State != chapterDownloading {
			hasMissing = true
			break
		}
	}

	if hasMissing {
		v.downloadAllButton.Enable()
	} else {
		v.downloadAllButton.Disable()
	}
}

// showCFDialog displays the Cloudflare challenge dialog for a waiting task.
func (v *ChapterListView) showCFDialog(task *config.DownloadTask) {
	cfErr, ok := task.Error.(*cf.CfChallengeError)
	if !ok {
		return
	}

	ShowcfDialog(v.state.Window, cfErr.URL, func() {
		delete(v.cfDialogShown, task.ID)
		// Resume every CF-blocked task (including this one) whose bypass data
		// is now available, so downloads that were paused on a CF challenge
		// start again automatically after the data is imported.
		config.GetDownloadQueue().ResumeCfTasks()
	})
}

func (v *ChapterListView) startLoading() {
	v.loadingIndicator.Show()
	v.loadingIndicator.Start()
}

func (v *ChapterListView) stopLoading() {
	v.loadingIndicator.Stop()
	v.loadingIndicator.Hide()
}

func (v *ChapterListView) showNoSelection() {
	v.loadGeneration++
	v.refreshing = false
	v.chapters = []*ChapterItem{}
	v.downloadAllButton.Disable()
	v.refreshButton.Disable()
	v.selectedMangaLabel.SetText("")
	v.contentContainer.Objects = []fyne.CanvasObject{
		widget.NewLabel(""),
	}
	v.contentContainer.Refresh()
	v.stopLoading()
	v.updateStatusBar()
}
