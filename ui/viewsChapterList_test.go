package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kansho/config"

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
func chapterRowParts(item fyne.CanvasObject) (tick *widget.Icon, downloadBtn, xBtn *widget.Button) {
	grid := item.(*fyne.Container)
	rightCell := grid.Objects[2].(*fyne.Container)
	rightColumn := rightCell.Objects[0].(*fyne.Container)
	return rightColumn.Objects[0].(*widget.Icon), rightColumn.Objects[1].(*widget.Button), rightColumn.Objects[2].(*widget.Button)
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
