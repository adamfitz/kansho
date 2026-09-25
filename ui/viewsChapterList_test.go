package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"kansho/config"
	"kansho/mangadex"
	"kansho/refreshpool"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

type memoryChapterStatsStore struct {
	stats map[string]mangadex.ChapterStats
}

func (s *memoryChapterStatsStore) UpsertChapterStats(key string, stats mangadex.ChapterStats) error {
	s.stats[key] = stats
	return nil
}

func (s *memoryChapterStatsStore) LookupChapterStats(key string) (*mangadex.ChapterStats, error) {
	stats, ok := s.stats[key]
	if !ok {
		return nil, nil
	}
	return &stats, nil
}

func (s *memoryChapterStatsStore) seed(manga *config.Bookmarks, total, downloaded int) {
	s.stats[mangaSourceKey(manga)] = mangadex.ChapterStats{
		Title:         manga.Title,
		Site:          manga.Site,
		URL:           manga.Url,
		Total:         total,
		Downloaded:    downloaded,
		NotDownloaded: total - downloaded,
	}
}

func testChapterStatsStore(view *ChapterListView) *memoryChapterStatsStore {
	return view.chapterStatsStore.(*memoryChapterStatsStore)
}

// newChapterListViewTest builds a ChapterListView bound to a single test manga
// stored in the given location ("" means no on-disk folder).
func newChapterListViewTest(t *testing.T, location string) (*KanshoAppState, *ChapterListView, fyne.Window) {
	t.Helper()
	app := test.NewApp()
	w := test.NewWindow(nil)
	t.Cleanup(func() {
		w.Close()
		app.Quit()
	})

	state := &KanshoAppState{
		Window:          w,
		MangaData:       config.Manga{Manga: []config.Bookmarks{{Title: "Test Manga", Location: location, Url: "http://example.com/manga", Site: "test"}}},
		SelectedMangaID: 0,
	}
	view := NewChapterListView(state, NewDownloadQueueButton(state))
	view.chapterStatsStore = &memoryChapterStatsStore{stats: make(map[string]mangadex.ChapterStats)}
	return state, view, w
}

// chapterRowParts unwraps the container structure created by createChapterRow.
func chapterRowParts(item fyne.CanvasObject) (tick *widget.Icon, downloadBtn, xBtn *iconButton) {
	grid := item.(*fyne.Container)
	rightCell := grid.Objects[2].(*fyne.Container)
	rightColumn := rightCell.Objects[0].(*fyne.Container)
	return rightColumn.Objects[0].(*widget.Icon), rightColumn.Objects[1].(*iconButton), rightColumn.Objects[2].(*iconButton)
}

// TestChapterRowDownloadedShowsTick verifies that a chapter that exists on disk
// is rendered with the green tick indicator, no download button, and an enabled
// X button that deletes the local file.
func TestChapterRowDownloadedShowsTick(t *testing.T) {
	_, view, _ := newChapterListViewTest(t, "")
	view.chapters = []*ChapterItem{{Name: "ch001.cbz", Downloaded: true, State: chapterDownloaded, Progress: 1.0}}

	row := view.createChapterRow()
	view.updateChapterRow(0, row)

	tick, downloadBtn, xBtn := chapterRowParts(row)
	if !tick.Visible() {
		t.Error("downloaded chapter should show the green tick")
	}
	if downloadBtn.Visible() {
		t.Error("downloaded chapter should not show the download button")
	}
	if xBtn.Disabled() {
		t.Error("downloaded chapter's X button should be enabled (delete)")
	}
}

// TestChapterRowNotDownloadedShowsDownloadButton verifies that an idle chapter
// that is not on disk is rendered with the download arrow button and a disabled
// X button.
func TestChapterRowNotDownloadedShowsDownloadButton(t *testing.T) {
	_, view, _ := newChapterListViewTest(t, "")
	view.chapters = []*ChapterItem{{Name: "ch002.cbz", Downloaded: false, State: chapterNotDownloaded, URL: "http://example.com/ch2"}}

	row := view.createChapterRow()
	view.updateChapterRow(0, row)

	tick, downloadBtn, xBtn := chapterRowParts(row)
	if tick.Visible() {
		t.Error("not-downloaded chapter should not show the tick")
	}
	if !downloadBtn.Visible() {
		t.Error("not-downloaded chapter should show the download button")
	}
	if downloadBtn.Disabled() {
		t.Error("download button should be enabled")
	}
	if !xBtn.Disabled() {
		t.Error("idle chapter's X button should be disabled")
	}
}

// TestChapterRowActiveShowsCancelableX verifies that a chapter with an active
// download task shows progress, a disabled download button, and an enabled X
// button that cancels the download.
func TestChapterRowActiveShowsCancelableX(t *testing.T) {
	_, view, _ := newChapterListViewTest(t, "")
	view.chapters = []*ChapterItem{{Name: "ch003.cbz", Downloaded: false, State: chapterQueued, URL: "http://example.com/ch3"}}

	row := view.createChapterRow()
	view.updateChapterRow(0, row)

	tick, downloadBtn, xBtn := chapterRowParts(row)
	if tick.Visible() {
		t.Error("queued chapter should not show the tick")
	}
	if !downloadBtn.Disabled() {
		t.Error("queued chapter's download button should be disabled")
	}
	if xBtn.Disabled() {
		t.Error("queued chapter's X button should be enabled (cancel)")
	}
}

// TestChapterRowRightColumnRightAligned verifies that the icon/button column of
// each row is laid out with a Border layout so it hugs the right edge.
func TestChapterRowRightColumnRightAligned(t *testing.T) {
	_, view, _ := newChapterListViewTest(t, "")
	view.chapters = []*ChapterItem{{Name: "ch001.cbz", Downloaded: true, State: chapterDownloaded, Progress: 1.0}}

	row := view.createChapterRow()
	grid := row.(*fyne.Container)
	rightCell := grid.Objects[2].(*fyne.Container)
	if rightCell.Layout == nil {
		t.Fatal("right column should have a layout")
	}
	if layoutType := fmt.Sprintf("%T", rightCell.Layout); !strings.Contains(strings.ToLower(layoutType), "borderlayout") {
		t.Fatalf("right column should use a Border layout to right-align the controls, got %s", layoutType)
	}
}

// TestDeleteChapterRequiresConfirmation verifies that deleting a chapter shows a
// confirmation dialog and does not remove the file until the user confirms.
func TestDeleteChapterRequiresConfirmation(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "ch001.cbz")
	if err := os.WriteFile(filePath, []byte("cbz"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, view, w := newChapterListViewTest(t, dir)
	view.chapters = []*ChapterItem{{Name: "ch001.cbz", Downloaded: true, State: chapterDownloaded, Progress: 1.0}}

	overlaysBefore := len(w.Canvas().Overlays().List())
	view.deleteChapterFile(0)

	if _, err := os.Stat(filePath); err != nil {
		t.Fatal("file should NOT be deleted before the user confirms")
	}
	if len(w.Canvas().Overlays().List()) <= overlaysBefore {
		t.Fatal("expected a confirmation dialog to be shown")
	}
}

// TestBuildChapterItemsMergesLocalAndRemote verifies that buildChapterItems
// merges on-disk chapters with cached remote chapters, keeping the download
// URL on remote-only chapters.
func TestBuildChapterItemsMergesLocalAndRemote(t *testing.T) {
	local := []string{"ch001.cbz", "ch002.cbz"}
	remote := map[string]string{"ch002.cbz": "http://example.com/ch2", "ch003.cbz": "http://example.com/ch3"}

	items := buildChapterItems(local, remote)
	if len(items) != 3 {
		t.Fatalf("expected 3 merged chapters, got %d", len(items))
	}

	byName := make(map[string]*ChapterItem, len(items))
	for _, it := range items {
		byName[it.Name] = it
	}

	if !byName["ch001.cbz"].Downloaded || byName["ch001.cbz"].State != chapterDownloaded {
		t.Error("ch001 exists on disk so it must be downloaded")
	}
	if !byName["ch002.cbz"].Downloaded {
		t.Error("ch002 exists on disk so it must be downloaded")
	}
	if byName["ch002.cbz"].URL != remote["ch002.cbz"] {
		t.Error("a downloaded chapter should keep its remote URL")
	}
	if byName["ch003.cbz"].Downloaded {
		t.Error("ch003 is remote-only so it must NOT be downloaded")
	}
	if byName["ch003.cbz"].URL != remote["ch003.cbz"] {
		t.Error("a remote-only chapter should keep its URL")
	}
}

// TestRefreshedChaptersAreCachedAndPersisted verifies that successful refresh
// results accumulate in the per-manga cache and update the database snapshot.
func TestRefreshedChaptersAreCachedAndPersisted(t *testing.T) {
	state, view, _ := newChapterListViewTest(t, "")
	manga := &state.MangaData.Manga[0]
	key := mangaSourceKey(manga)

	view.applyRefreshedChapters(manga, map[string]string{"ch001.cbz": "u1"}, view.loadGeneration)
	if len(view.chapters) != 1 {
		t.Fatalf("expected 1 chapter in the list, got %d", len(view.chapters))
	}
	if view.chapters[0].Name != "ch001.cbz" || view.chapters[0].URL != "u1" {
		t.Fatalf("unexpected chapter: %+v", view.chapters[0])
	}
	if len(view.remoteChapters[key]) != 1 {
		t.Fatalf("expected the remote cache to hold 1 chapter for the manga")
	}

	view.applyRefreshedChapters(manga, map[string]string{"ch002.cbz": "u2"}, view.loadGeneration)
	if len(view.remoteChapters[key]) != 2 {
		t.Fatalf("expected the cache to accumulate chapters, got %d", len(view.remoteChapters[key]))
	}
	if len(view.chapters) != 2 {
		t.Fatalf("expected 2 chapters in the list, got %d", len(view.chapters))
	}
	stored, err := testChapterStatsStore(view).LookupChapterStats(key)
	if err != nil {
		t.Fatal(err)
	}
	if stored == nil || stored.Total != 2 || stored.Downloaded != 0 || stored.NotDownloaded != 2 {
		t.Fatalf("unexpected stored counts: %+v", stored)
	}
}

// TestChapterListPersistsRemoteChaptersAcrossSelection verifies that remote
// chapters cached for a manga remain visible when switching away and back.
func TestChapterListPersistsRemoteChaptersAcrossSelection(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ch001.cbz"), []byte("cbz"), 0o644); err != nil {
		t.Fatal(err)
	}

	state, view, _ := newChapterListViewTest(t, dir)
	key := mangaSourceKey(&state.MangaData.Manga[0])

	// Simulate the user having refreshed this manga once: its remote chapters
	// are cached and must survive switching manga.
	view.remoteChapters[key] = map[string]string{
		"ch002.cbz": "http://example.com/ch2",
		"ch003.cbz": "http://example.com/ch3",
	}

	view.onMangaSelected(0)
	if len(view.chapters) != 3 {
		t.Fatalf("selecting the manga should show local + cached remote chapters, got %d", len(view.chapters))
	}

	byName := make(map[string]*ChapterItem, len(view.chapters))
	for _, ch := range view.chapters {
		byName[ch.Name] = ch
	}
	if byName["ch001.cbz"] == nil || !byName["ch001.cbz"].Downloaded {
		t.Error("ch001 exists on disk and must be downloaded")
	}
	if byName["ch002.cbz"] == nil || byName["ch002.cbz"].Downloaded || byName["ch002.cbz"].URL == "" {
		t.Error("ch002 must be a remote-only chapter with a URL")
	}

	// Switch away and back: the cached remote chapters must still be shown.
	view.showNoSelection()
	view.onMangaSelected(0)
	if len(view.chapters) != 3 {
		t.Fatalf("remote chapters should persist after switching away and back, got %d", len(view.chapters))
	}
}

// TestLoadingIndicatorIsBashSpinner verifies that the loading indicator is the
// bash-style |/-\ spinner (the same animation as the main status bar) rather
// than a progress bar.
func TestLoadingIndicatorIsBashSpinner(t *testing.T) {
	_, view, _ := newChapterListViewTest(t, "")
	if view.loadingIndicator == nil {
		t.Fatal("loading indicator should be set")
	}
	if _, ok := interface{}(view.loadingIndicator).(*bashSpinner); !ok {
		t.Fatalf("loading indicator should be a bashSpinner, got %T", view.loadingIndicator)
	}
}

// TestBashSpinnerStartStopCyclesFrames verifies the spinner shows a frame
// glyph while running and clears it again when stopped, matching the status
// bar's spinner behaviour.
func TestBashSpinnerStartStopCyclesFrames(t *testing.T) {
	_, view, _ := newChapterListViewTest(t, "")
	spinner := view.loadingIndicator

	if spinner.ticker != nil {
		t.Fatal("spinner should be off initially")
	}
	if got := spinner.label.Text; got != "" {
		t.Fatalf("spinner label should be empty initially, got %q", got)
	}

	spinner.Start()
	defer spinner.Stop()
	if spinner.ticker == nil {
		t.Fatal("spinner should run after Start")
	}
	if got := spinner.label.Text; got != refreshSpinnerFrames[0] {
		t.Fatalf("spinner should show the first frame immediately, got %q", got)
	}

	spinner.Stop()
	if spinner.ticker != nil {
		t.Error("spinner ticker should be cleared after Stop")
	}
	if got := spinner.label.Text; got != "" {
		t.Errorf("spinner should clear its glyph when stopped, got %q", got)
	}
}

// TestChapterListDoesNotShowRemoteForNeverRefreshedManga verifies that a manga
// that has never been refreshed shows only its local chapters.
func TestChapterListDoesNotShowRemoteForNeverRefreshedManga(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ch001.cbz"), []byte("cbz"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, view, _ := newChapterListViewTest(t, dir)
	view.onMangaSelected(0)

	if len(view.chapters) != 1 {
		t.Fatalf("never-refreshed manga must show only local chapters, got %d", len(view.chapters))
	}
	if view.chapters[0].Name != "ch001.cbz" || !view.chapters[0].Downloaded {
		t.Fatalf("unexpected chapter: %+v", view.chapters[0])
	}
}

// TestRefreshAfterTaskChangeMarksDownloadedOnDisk verifies that a chapter whose
// download just finished (its completed task has already been removed from the
// queue) is still shown as downloaded as soon as its CBZ file exists on disk.
// This covers the case where the UI processes the task removal before the
// "completed" update, so the queue no longer holds the task.
func TestRefreshAfterTaskChangeMarksDownloadedOnDisk(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ch001.cbz"), []byte("cbz"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, view, _ := newChapterListViewTest(t, dir)
	view.chapters = []*ChapterItem{{Name: "ch001.cbz", Downloaded: false, State: chapterDownloading, Progress: 1.0}}

	view.refreshAfterTaskChange()

	if len(view.chapters) != 1 {
		t.Fatalf("expected 1 chapter, got %d", len(view.chapters))
	}
	ch := view.chapters[0]
	if !ch.Downloaded {
		t.Error("chapter exists on disk so it must be marked downloaded")
	}
	if ch.State != chapterDownloaded {
		t.Errorf("expected state chapterDownloaded, got %v", ch.State)
	}
	if ch.Progress != 1.0 {
		t.Errorf("expected progress 1.0, got %v", ch.Progress)
	}
}

// TestRefreshAfterTaskChangeKeepsNotDownloadedWhenNotOnDisk verifies that a
// chapter that is downloading but has not yet written its CBZ file to disk is
// not falsely marked as downloaded after a queue update.
func TestRefreshAfterTaskChangeKeepsNotDownloadedWhenNotOnDisk(t *testing.T) {
	dir := t.TempDir()

	_, view, _ := newChapterListViewTest(t, dir)
	view.chapters = []*ChapterItem{{Name: "ch001.cbz", Downloaded: false, State: chapterDownloading, Progress: 0.5}}

	view.refreshAfterTaskChange()

	ch := view.chapters[0]
	if ch.Downloaded {
		t.Error("chapter is not on disk so it must not be marked downloaded")
	}
}

// TestDeleteChapterFileOnDisk verifies that deleteChapterFileOnDisk removes the
// CBZ file and treats a missing file as success.
func TestDeleteChapterFileOnDisk(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "ch002.cbz")
	if err := os.WriteFile(filePath, []byte("cbz"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := deleteChapterFileOnDisk(dir, "ch002.cbz"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Fatal("file should have been removed")
	}

	if err := deleteChapterFileOnDisk(dir, "ch002.cbz"); err != nil {
		t.Fatalf("deleting a missing file should succeed, got: %v", err)
	}
}

// TestSelectedMangaTitleIsBold verifies that the chapter list pane shows the
// selected manga's title in bold.
func TestSelectedMangaTitleIsBold(t *testing.T) {
	_, view, _ := newChapterListViewTest(t, "")

	view.onMangaSelected(0)

	if view.selectedMangaLabel.Text != "Test Manga" {
		t.Fatalf("selected title should be shown in the chapter list pane, got %q", view.selectedMangaLabel.Text)
	}
	if !view.selectedMangaLabel.TextStyle.Bold {
		t.Error("selected manga title should be bold")
	}
}

// TestStatusBarLoadsCompleteChapterCountsFromDatabase verifies that the status
// bar shows no counts until a complete database snapshot exists, then renders
// downloaded, total, and not-downloaded values from that snapshot.
func TestStatusBarLoadsCompleteChapterCountsFromDatabase(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ch001.cbz"), []byte("cbz"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ch002.cbz"), []byte("cbz"), 0o644); err != nil {
		t.Fatal(err)
	}

	state, view, _ := newChapterListViewTest(t, dir)
	state.SelectedMangaID = -1
	bar := NewMainStatusBar()
	view.SetStatusBar(bar)
	if bar.message.Text != "No manga selected" || bar.badge.Text != "" {
		t.Fatalf("status bar should start idle, got badge=%q message=%q", bar.badge.Text, bar.message.Text)
	}

	state.SelectedMangaID = 0
	view.onMangaSelected(0)
	if bar.badge.Text != "test" {
		t.Errorf("badge should show the manga's site, got %q", bar.badge.Text)
	}
	if bar.message.Text != "" {
		t.Errorf("chapter counts should be hidden without a database snapshot, got %q", bar.message.Text)
	}

	testChapterStatsStore(view).seed(&state.MangaData.Manga[0], 4, 2)
	view.onMangaSelected(0)
	if bar.message.Text != "2 of 4 chapters downloaded · 2 to download" {
		t.Errorf("status bar should render the database counts, got %q", bar.message.Text)
	}
}

func TestChapterCountsReloadAfterMangaSwitch(t *testing.T) {
	state, view, _ := newChapterListViewTest(t, "")
	first := &state.MangaData.Manga[0]
	view.applyRefreshedChapters(first, map[string]string{
		"ch001.cbz": "u1",
		"ch002.cbz": "u2",
		"ch003.cbz": "u3",
	}, view.loadGeneration)

	state.MangaData.Manga = append(state.MangaData.Manga, config.Bookmarks{
		Title: "Other Manga",
		Url:   "http://example.com/other",
		Site:  "test",
	})
	state.SelectedMangaID = 1
	view.onMangaSelected(1)
	state.SelectedMangaID = 0
	view.onMangaSelected(0)

	if view.chapterStats == nil || view.chapterStats.Total != 3 || view.chapterStats.NotDownloaded != 3 {
		t.Fatalf("stored counts were not restored after switching manga: %+v", view.chapterStats)
	}
}

func TestStaleRefreshPersistsWithoutReplacingSelectedManga(t *testing.T) {
	state, view, _ := newChapterListViewTest(t, "")
	first := &state.MangaData.Manga[0]
	state.MangaData.Manga = append(state.MangaData.Manga, config.Bookmarks{
		Title: "Other Manga",
		Url:   "http://example.com/other",
		Site:  "test",
	})
	state.SelectedMangaID = 1
	view.onMangaSelected(1)

	view.applyRefreshedChapters(first, map[string]string{
		"ch001.cbz": "u1",
	}, 0)

	if len(view.chapters) != 0 {
		t.Fatalf("stale refresh replaced the selected manga's list: %+v", view.chapters)
	}
	if view.chapterStats != nil {
		t.Fatalf("stale refresh changed the selected manga's counts: %+v", view.chapterStats)
	}
	stored, err := testChapterStatsStore(view).LookupChapterStats(mangaSourceKey(first))
	if err != nil {
		t.Fatal(err)
	}
	if stored == nil || stored.Total != 1 || stored.NotDownloaded != 1 {
		t.Fatalf("stale refresh was not persisted for its manga: %+v", stored)
	}
}

func TestLocalChapterReadErrorDoesNotOverwriteKnownCounts(t *testing.T) {
	state, view, _ := newChapterListViewTest(t, filepath.Join(t.TempDir(), "missing"))
	manga := &state.MangaData.Manga[0]
	testChapterStatsStore(view).seed(manga, 3, 2)
	view.onMangaSelected(0)
	view.updateStoredDownloadedCount(manga)

	stored, err := testChapterStatsStore(view).LookupChapterStats(mangaSourceKey(manga))
	if err != nil {
		t.Fatal(err)
	}
	if stored == nil || stored.Total != 3 || stored.Downloaded != 2 || stored.NotDownloaded != 1 {
		t.Fatalf("local read error overwrote known counts: %+v", stored)
	}
}

func TestStatusBarUpdatesOnTaskChange(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ch001.cbz"), []byte("cbz"), 0o644); err != nil {
		t.Fatal(err)
	}

	state, view, _ := newChapterListViewTest(t, dir)
	testChapterStatsStore(view).seed(&state.MangaData.Manga[0], 3, 1)
	bar := NewMainStatusBar()
	view.SetStatusBar(bar)
	view.onMangaSelected(0)
	if bar.message.Text != "1 of 3 chapters downloaded · 2 to download" {
		t.Fatalf("unexpected initial message: %q", bar.message.Text)
	}

	// Simulate a second chapter appearing on disk (a finished download).
	if err := os.WriteFile(filepath.Join(dir, "ch002.cbz"), []byte("cbz"), 0o644); err != nil {
		t.Fatal(err)
	}
	view.chapters = append(view.chapters, &ChapterItem{Name: "ch002.cbz"})
	view.refreshAfterTaskChange()

	if bar.message.Text != "2 of 3 chapters downloaded · 1 to download" {
		t.Errorf("bar should reflect the newly downloaded chapter, got %q", bar.message.Text)
	}
}

// TestStatusBarShowsRefreshPoolStatus verifies the right edge of the main
// status bar reflects the chapter-list refresh worker pool: an idle readout
// initially, then a bash-style spinner plus running/queued counts while the
// pool is busy, and back to idle once it drains.
func TestStatusBarShowsRefreshPoolStatus(t *testing.T) {
	bar := NewMainStatusBar()
	if bar.poolStatus.Text != "⟳ Chapter Refresh: idle" {
		t.Fatalf("pool readout should start idle, got %q", bar.poolStatus.Text)
	}
	if bar.spinner.Text != "" || bar.spinTicker != nil {
		t.Fatalf("spinner should be off initially, got %q ticker=%v", bar.spinner.Text, bar.spinTicker)
	}

	busy := refreshpool.Status{Running: 2, Queued: 3}
	bar.SetRefreshPoolStatus(busy)
	if bar.poolStatus.Text != "Chapter Refresh: 2 running · 3 queued" {
		t.Errorf("unexpected busy pool readout: %q", bar.poolStatus.Text)
	}

	// The spinner must show exactly one of the bash-style frames.
	frame := bar.spinner.Text
	if !slices.Contains([]string{"|", "/", "-", "\\"}, frame) {
		t.Errorf("expected a spinner frame glyph, got %q", frame)
	}

	// Repeated busy updates reuse the same ticker instead of stacking more.
	first := bar.spinTicker
	bar.SetRefreshPoolStatus(busy)
	if bar.spinTicker != first {
		t.Error("busy updates must not restart the spinner")
	}

	bar.SetRefreshPoolStatus(refreshpool.Status{})
	if bar.poolStatus.Text != "⟳ Chapter Refresh: idle" {
		t.Errorf("pool readout should return to idle, got %q", bar.poolStatus.Text)
	}
	if bar.spinner.Text != "" || bar.spinTicker != nil {
		t.Errorf("spinner should stop and clear when idle, got %q ticker=%v", bar.spinner.Text, bar.spinTicker)
	}
}
