package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"kansho/config"
	"kansho/refreshpool"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

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

// TestMergeRemoteChaptersCachesForManga verifies that mergeRemoteChapters
// persists fetched remote chapters in the per-manga cache and accumulates them
// across refreshes.
func TestMergeRemoteChaptersCachesForManga(t *testing.T) {
	_, view, _ := newChapterListViewTest(t, "")

	view.mergeRemoteChapters("Test Manga", map[string]string{"ch001.cbz": "u1"})
	if len(view.chapters) != 1 {
		t.Fatalf("expected 1 chapter in the list, got %d", len(view.chapters))
	}
	if view.chapters[0].Name != "ch001.cbz" || view.chapters[0].URL != "u1" {
		t.Fatalf("unexpected chapter: %+v", view.chapters[0])
	}
	if len(view.remoteChapters["Test Manga"]) != 1 {
		t.Fatalf("expected the remote cache to hold 1 chapter for the manga")
	}

	view.mergeRemoteChapters("Test Manga", map[string]string{"ch002.cbz": "u2"})
	if len(view.remoteChapters["Test Manga"]) != 2 {
		t.Fatalf("expected the cache to accumulate chapters, got %d", len(view.remoteChapters["Test Manga"]))
	}
	if len(view.chapters) != 2 {
		t.Fatalf("expected 2 chapters in the list, got %d", len(view.chapters))
	}
}

// TestChapterListPersistsRemoteChaptersAcrossSelection verifies that remote
// chapters cached for a manga remain visible when switching away and back.
func TestChapterListPersistsRemoteChaptersAcrossSelection(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ch001.cbz"), []byte("cbz"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, view, _ := newChapterListViewTest(t, dir)

	// Simulate the user having refreshed this manga once: its remote chapters
	// are cached and must survive switching manga.
	view.remoteChapters["Test Manga"] = map[string]string{
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

// TestLoadingIndicatorIsAJAXSpinner verifies that the loading indicator is the
// custom AJAX-style rotating spinner rather than a progress bar.
func TestLoadingIndicatorIsAJAXSpinner(t *testing.T) {
	_, view, _ := newChapterListViewTest(t, "")
	if view.loadingIndicator == nil {
		t.Fatal("loading indicator should be set")
	}
	if _, ok := interface{}(view.loadingIndicator).(*ajaxSpinner); !ok {
		t.Fatalf("loading indicator should be an ajaxSpinner, got %T", view.loadingIndicator)
	}
}

// TestAJAXSpinnerRendersRing verifies that the AJAX spinner creates a full ring
// of radial ticks when it is laid out.
func TestAJAXSpinnerRendersRing(t *testing.T) {
	_, view, _ := newChapterListViewTest(t, "")
	spinner := view.loadingIndicator

	renderer := test.WidgetRenderer(spinner)
	segments := renderer.Objects()
	if len(segments) != spinnerSegments {
		t.Fatalf("expected %d spinner segments, got %d", spinnerSegments, len(segments))
	}

	// Layout the spinner and check every tick is drawn within bounds.
	spinner.Resize(fyne.NewSize(40, 40))
	renderer.Layout(fyne.NewSize(40, 40))
	for _, obj := range segments {
		line, ok := obj.(*canvas.Line)
		if !ok {
			t.Fatalf("expected canvas.Line segment, got %T", obj)
		}
		if line.Position1.X == 0 && line.Position1.Y == 0 && line.Position2.X == 0 && line.Position2.Y == 0 {
			t.Error("a spinner segment was not positioned")
		}
	}

	spinner.Start()
	spinner.Stop()
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

// TestStatusBarShowsSiteAndDownloadedCountOnlyUntilRefresh verifies that the
// main status bar starts idle, then shows the download site and downloaded
// chapter count on selection, and only includes the not-downloaded count once
// the manga's chapter list has been refreshed.
func TestStatusBarShowsSiteAndDownloadedCountOnlyUntilRefresh(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ch001.cbz"), []byte("cbz"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ch002.cbz"), []byte("cbz"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, view, _ := newChapterListViewTest(t, dir)

	bar := NewMainStatusBar()
	view.SetStatusBar(bar)
	if bar.message.Text != "No manga selected" || bar.badge.Text != "" {
		t.Fatalf("status bar should start idle, got badge=%q message=%q", bar.badge.Text, bar.message.Text)
	}

	// Selecting the manga shows site + downloaded count; the not-downloaded
	// count is hidden until the user presses Refresh.
	view.onMangaSelected(0)
	if bar.badge.Text != "test" {
		t.Errorf("badge should show the manga's site, got %q", bar.badge.Text)
	}
	if bar.message.Text != "2 chapters downloaded" {
		t.Errorf("before a refresh the bar should show only the downloaded count, got %q", bar.message.Text)
	}

	// A refresh fetches remote chapters: afterwards the not-downloaded count
	// is shown as well (simulated here via the per-manga remote cache).
	view.remoteChapters["Test Manga"] = map[string]string{
		"ch003.cbz": "http://example.com/ch3",
		"ch004.cbz": "http://example.com/ch4",
	}
	view.onMangaSelected(0)
	if bar.message.Text != "2 chapters downloaded · 2 to download" {
		t.Errorf("after a refresh the bar should include the not-downloaded count, got %q", bar.message.Text)
	}
}

// TestStatusBarUpdatesOnTaskChange verifies that the status bar counts stay in
// sync when the queue state changes (e.g. a chapter finishes downloading).
func TestStatusBarUpdatesOnTaskChange(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ch001.cbz"), []byte("cbz"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, view, _ := newChapterListViewTest(t, dir)
	bar := NewMainStatusBar()
	view.SetStatusBar(bar)
	view.onMangaSelected(0)
	if bar.message.Text != "1 chapters downloaded" {
		t.Fatalf("unexpected initial message: %q", bar.message.Text)
	}

	// Simulate a second chapter appearing on disk (a finished download).
	if err := os.WriteFile(filepath.Join(dir, "ch002.cbz"), []byte("cbz"), 0o644); err != nil {
		t.Fatal(err)
	}
	view.chapters = append(view.chapters, &ChapterItem{Name: "ch002.cbz"})
	view.refreshAfterTaskChange()

	if bar.message.Text != "2 chapters downloaded" {
		t.Errorf("bar should reflect the newly downloaded chapter, got %q", bar.message.Text)
	}
}

// TestStatusBarShowsRefreshPoolStatus verifies the right edge of the main
// status bar reflects the chapter-list refresh worker pool: an idle readout
// initially, then a bash-style spinner plus running/queued counts while the
// pool is busy, and back to idle once it drains.
func TestStatusBarShowsRefreshPoolStatus(t *testing.T) {
	bar := NewMainStatusBar()
	if bar.poolStatus.Text != "⟳ Refreshes: idle" {
		t.Fatalf("pool readout should start idle, got %q", bar.poolStatus.Text)
	}
	if bar.spinner.Text != "" || bar.spinTicker != nil {
		t.Fatalf("spinner should be off initially, got %q ticker=%v", bar.spinner.Text, bar.spinTicker)
	}

	busy := refreshpool.Status{Running: 2, Queued: 3}
	bar.SetRefreshPoolStatus(busy)
	if bar.poolStatus.Text != "Refreshes: 2 running · 3 queued" {
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
	if bar.poolStatus.Text != "⟳ Refreshes: idle" {
		t.Errorf("pool readout should return to idle, got %q", bar.poolStatus.Text)
	}
	if bar.spinner.Text != "" || bar.spinTicker != nil {
		t.Errorf("spinner should stop and clear when idle, got %q ticker=%v", bar.spinner.Text, bar.spinTicker)
	}
}
