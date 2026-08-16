package ui

import (
	"fmt"
	"strings"
	"testing"

	"kansho/config"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// TestGroupTasksByManga verifies that tasks are grouped by manga title in
// insertion order.
func TestGroupTasksByManga(t *testing.T) {
	tasks := []*config.DownloadTask{
		{Manga: config.Bookmarks{Title: "Manga A"}, Chapter: "a1.cbz", Status: "queued"},
		{Manga: config.Bookmarks{Title: "Manga B"}, Chapter: "b1.cbz", Status: "queued"},
		{Manga: config.Bookmarks{Title: "Manga A"}, Chapter: "a2.cbz", Status: "downloading"},
	}

	groups := groupTasksByManga(tasks)
	if len(groups) != 2 {
		t.Fatalf("expected 2 manga groups, got %d", len(groups))
	}
	if groups[0].title != "Manga A" || len(groups[0].tasks) != 2 {
		t.Fatalf("unexpected first group: %+v", groups[0])
	}
	if groups[1].title != "Manga B" || len(groups[1].tasks) != 1 {
		t.Fatalf("unexpected second group: %+v", groups[1])
	}
}

// findButton walks a canvas object tree looking for a button whose text
// matches.
func findButton(root fyne.CanvasObject, text string) *widget.Button {
	switch obj := root.(type) {
	case *widget.Button:
		if obj.Text == text {
			return obj
		}
	case *fyne.Container:
		for _, child := range obj.Objects {
			if btn := findButton(child, text); btn != nil {
				return btn
			}
		}
	}
	return nil
}

// findProgressBar walks a canvas object tree looking for a progress bar.
func findProgressBar(root fyne.CanvasObject) (*widget.ProgressBar, bool) {
	switch obj := root.(type) {
	case *widget.ProgressBar:
		return obj, true
	case *fyne.Container:
		for _, child := range obj.Objects {
			if bar, ok := findProgressBar(child); ok {
				return bar, true
			}
		}
	}
	return nil, false
}

// TestDownloadQueuePopupUsesScrollContainer verifies that the queue pop-up is a
// Border layout with a scrollable centre (so the list is constrained and gets a
// scrollbar when too long) and a global "Cancel All" button in the header.
func TestDownloadQueuePopupUsesScrollContainer(t *testing.T) {
	app := test.NewApp()
	w := test.NewWindow(nil)
	t.Cleanup(func() {
		w.Close()
		app.Quit()
	})

	b := NewDownloadQueueButton(&KanshoAppState{Window: w})
	content := b.buildPopup()

	border, ok := content.(*fyne.Container)
	if !ok {
		t.Fatal("popup content should be a container")
	}
	if border.Layout == nil {
		t.Fatal("popup content should have a layout")
	}
	layoutType := fmt.Sprintf("%T", border.Layout)
	if !strings.Contains(strings.ToLower(layoutType), "borderlayout") {
		t.Fatalf("popup should use a Border layout, got %s", layoutType)
	}

	// container.NewBorder stores the centre object first, then the top.
	center := border.Objects[0]
	if _, ok := center.(*container.Scroll); !ok {
		t.Fatalf("popup centre should be a scroll container to constrain the list, got %T", center)
	}

	if btn := findButton(border.Objects[1], "Cancel All"); btn == nil {
		t.Error("popup header should contain a global 'Cancel All' button")
	}
	if btn := findButton(border.Objects[1], "Clear All"); btn == nil {
		t.Error("popup header should contain a 'Clear All' button")
	}
}

// TestDownloadQueueEmptyShowsInfoDialog verifies that clicking the queue button
// with an empty queue shows an informational dialog rather than opening the
// pop-up.
func TestDownloadQueueEmptyShowsInfoDialog(t *testing.T) {
	state, _, w := newChapterListViewTest(t, "")
	b := NewDownloadQueueButton(state)

	overlaysBefore := len(w.Canvas().Overlays().List())
	b.showSummary()
	if len(w.Canvas().Overlays().List()) <= overlaysBefore {
		t.Fatal("expected an info dialog when the queue is empty")
	}
}

// TestGetStatusIcon verifies a status icon exists for every queue status.
func TestGetStatusIcon(t *testing.T) {
	for _, status := range []string{"queued", "downloading", "waiting_cf", "skipped_cf", "completed", "cancelled", "failed"} {
		if getStatusIcon(status) == "" {
			t.Errorf("missing status icon for %q", status)
		}
	}
}

// TestUnfinishedTaskRowHasActionButton verifies that a task that did not finish
// downloading gets an action button — "Start" for a user-stopped (cancelled)
// task, "Retry" for a failed or CF-blocked one — while active tasks do not.
func TestUnfinishedTaskRowHasActionButton(t *testing.T) {
	state, _, _ := newChapterListViewTest(t, "")
	b := NewDownloadQueueButton(state)

	task := &config.DownloadTask{ID: "failed", Manga: config.Bookmarks{Title: "Manga A"}, Chapter: "a1.cbz", Status: "failed"}
	if btn := findButton(b.taskSummaryRow(task), "Retry"); btn == nil {
		t.Error("failed task row should have a Retry button")
	}
	task = &config.DownloadTask{ID: "waiting_cf", Manga: config.Bookmarks{Title: "Manga A"}, Chapter: "a1.cbz", Status: "waiting_cf"}
	if btn := findButton(b.taskSummaryRow(task), "Retry"); btn == nil {
		t.Error("waiting_cf task row should have a Retry button")
	}
	task = &config.DownloadTask{ID: "skipped_cf", Manga: config.Bookmarks{Title: "Manga A"}, Chapter: "a1.cbz", Status: "skipped_cf"}
	if btn := findButton(b.taskSummaryRow(task), "Retry"); btn == nil {
		t.Error("skipped_cf task row should have a Retry button")
	}
	task = &config.DownloadTask{ID: "cancelled", Manga: config.Bookmarks{Title: "Manga A"}, Chapter: "a1.cbz", Status: "cancelled"}
	if btn := findButton(b.taskSummaryRow(task), "Start"); btn == nil {
		t.Error("cancelled task row should have a Start button")
	}

	for _, status := range []string{"queued", "downloading", "completed"} {
		task := &config.DownloadTask{ID: status, Manga: config.Bookmarks{Title: "Manga A"}, Chapter: "a1.cbz", Status: status}
		if btn := findButton(b.taskSummaryRow(task), "Retry"); btn != nil {
			t.Errorf("%s task row should NOT have a Retry button", status)
		}
		if btn := findButton(b.taskSummaryRow(task), "Start"); btn != nil {
			t.Errorf("%s task row should NOT have a Start button", status)
		}
	}
}

// TestRetryRowLabelIsCentreWithButtonRight verifies that a retryable task row
// lays the chapter label out in the centre (so it gets the remaining width and
// does not wrap/break) with the action button hugging the right edge.
func TestRetryRowLabelIsCentreWithButtonRight(t *testing.T) {
	state, _, _ := newChapterListViewTest(t, "")
	b := NewDownloadQueueButton(state)

	task := &config.DownloadTask{ID: "1", Manga: config.Bookmarks{Title: "Manga A"}, Chapter: "a1.cbz", Status: "cancelled"}
	row := b.taskSummaryRow(task)

	border, ok := row.(*fyne.Container)
	if !ok {
		t.Fatal("retryable task row should be a container")
	}

	// container.NewBorder stores the centre object first, then the right.
	label, ok := border.Objects[0].(*widget.Label)
	if !ok {
		t.Fatalf("row centre should be the chapter label, got %T", border.Objects[0])
	}
	btn, ok := border.Objects[1].(*widget.Button)
	if !ok || btn.Text != "Start" {
		t.Fatalf("row right element should be the Start button, got %T", border.Objects[1])
	}

	// After laying out the row, the label must occupy most of the width and the
	// button must sit at the right edge.
	row.Resize(fyne.NewSize(500, 30))
	if label.Size().Width <= 0 {
		t.Fatal("label should have a non-zero width in the row layout")
	}
	if btn.Position().X <= label.Position().X {
		t.Error("Start button should be positioned to the right of the label")
	}
	if btn.Position().X+btn.Size().Width > row.Size().Width {
		t.Error("Start button should hug the right edge of the row")
	}
}

// TestActiveDownloadTask verifies that activeDownloadTask returns the single
// downloading task, or nil when nothing is downloading.
func TestActiveDownloadTask(t *testing.T) {
	tasks := []*config.DownloadTask{
		{Manga: config.Bookmarks{Title: "Manga A"}, Chapter: "a1.cbz", Status: "queued"},
		{Manga: config.Bookmarks{Title: "Manga B"}, Chapter: "b1.cbz", Status: "downloading"},
	}
	active := activeDownloadTask(tasks)
	if active == nil || active.Chapter != "b1.cbz" {
		t.Fatalf("expected the downloading task, got %+v", active)
	}

	if active := activeDownloadTask([]*config.DownloadTask{
		{Manga: config.Bookmarks{Title: "Manga A"}, Chapter: "a1.cbz", Status: "queued"},
	}); active != nil {
		t.Fatalf("expected nil when nothing is downloading, got %+v", active)
	}
}

// TestActiveTaskRowShowsProgressAndStopButton verifies that the currently
// downloading task row is split into a left chapter pane and a right pane with
// a live progress bar and a Stop button.
func TestActiveTaskRowShowsProgressAndStopButton(t *testing.T) {
	state, _, _ := newChapterListViewTest(t, "")
	b := NewDownloadQueueButton(state)

	task := &config.DownloadTask{ID: "1", Manga: config.Bookmarks{Title: "Manga A"}, Chapter: "a1.cbz", Status: "downloading", Progress: 0.42}
	row := b.activeTaskRow(task)

	grid, ok := row.(*fyne.Container)
	if !ok {
		t.Fatal("active task row should be a container")
	}
	if grid.Layout == nil || fmt.Sprintf("%T", grid.Layout) != "*layout.gridLayout" {
		t.Fatalf("active task row should split into two columns, got %T", grid.Layout)
	}
	if len(grid.Objects) != 2 {
		t.Fatalf("expected two panes (left chapter, right progress), got %d", len(grid.Objects))
	}

	if btn := findButton(grid.Objects[1], "Stop"); btn == nil {
		t.Error("right pane should contain a Stop button")
	}
	progress, ok := findProgressBar(grid.Objects[1])
	if !ok {
		t.Fatal("right pane should contain a progress bar")
	}
	if progress.Value != 0.42 {
		t.Errorf("progress bar should reflect task progress, got %f", progress.Value)
	}
}

// TestIsRetryableTaskStatus verifies which statuses are retryable.
func TestIsRetryableTaskStatus(t *testing.T) {
	for _, status := range []string{"failed", "cancelled", "waiting_cf", "skipped_cf"} {
		if !isRetryableTaskStatus(status) {
			t.Errorf("%s should be retryable", status)
		}
	}
	for _, status := range []string{"queued", "downloading", "completed"} {
		if isRetryableTaskStatus(status) {
			t.Errorf("%s should NOT be retryable", status)
		}
	}
}

// TestStatusEmojiForMessage verifies the state glyph reflects what the current
// download is doing: downloading, stalled, in backoff, packaging, or starting.
func TestStatusEmojiForMessage(t *testing.T) {
	cases := map[string]string{
		"Chapter 3/19: Downloading image 8/10":                                                "⬇",
		"Chapter 3/19: Downloading image 8/10 (attempt 1/3)":                                  "⬇",
		"Chapter 3/19: Downloading image 8/10 — STALLED (no data received), waiting to retry": "⚠",
		"Chapter 3/19: Downloading image 8/10 — retry 2/3 in 4s (last error: ...)":            "⏳",
		"Chapter 3/19: Creating CBZ file...":                                                  "📦",
		"Found 2 new chapters to download":                                                    "▶",
		"Starting download...":                                                                "▶",
	}
	for msg, want := range cases {
		if got := statusEmojiForMessage(msg); got != want {
			t.Errorf("statusEmojiForMessage(%q) = %q, want %q", msg, got, want)
		}
	}
}

// TestSiteNameForTask verifies the badge site name prefers the bookmark's site
// field and falls back to the URL hostname.
func TestSiteNameForTask(t *testing.T) {
	task := &config.DownloadTask{Manga: config.Bookmarks{Site: "FlameComics", Url: "https://flamecomics.xyz/manga/xyz"}}
	if got := siteNameForTask(task); got != "FlameComics" {
		t.Errorf("expected bookmark site name, got %q", got)
	}

	task = &config.DownloadTask{Manga: config.Bookmarks{Url: "https://www.example.com/manga/xyz"}}
	if got := siteNameForTask(task); got != "example.com" {
		t.Errorf("expected hostname fallback, got %q", got)
	}

	if got := siteNameForTask(nil); got != "" {
		t.Errorf("expected empty for nil task, got %q", got)
	}
}

// TestTruncateMangaTitle verifies long titles are fixed to a specific length
// with an ellipsis, short titles are unchanged, and the truncation is rune-safe.
func TestTruncateMangaTitle(t *testing.T) {
	short := "One Piece"
	if got := truncateMangaTitle(short, 30); got != short {
		t.Errorf("short title should be unchanged, got %q", got)
	}

	long := "The 100 Girlfriends Who Really, Really, Really, Really, REALLY Love You"
	got := truncateMangaTitle(long, 30)
	if len([]rune(got)) != 30 {
		t.Errorf("truncated title should be exactly 30 runes, got %d (%q)", len([]rune(got)), got)
	}
	if runes := []rune(got); runes[len(runes)-1] != '…' {
		t.Errorf("truncated title should end with an ellipsis, got %q", got)
	}

	if got := truncateMangaTitle("日本語の長いタイトルテスト", 5); got != "日本語の…" {
		t.Errorf("rune-safe truncation failed, got %q", got)
	}
}

// TestRefreshPopupThrottlesStructuralRebuilds verifies that a pure progress tick
// on the task currently being displayed updates the active row and status bar in
// place instead of rebuilding the whole list.
func TestRefreshPopupThrottlesStructuralRebuilds(t *testing.T) {
	state, _, _ := newChapterListViewTest(t, "")
	b := NewDownloadQueueButton(state)
	b.buildPopup()

	task := &config.DownloadTask{ID: "1", Manga: config.Bookmarks{Title: "Manga A"}, Chapter: "a1.cbz", Status: "downloading", Progress: 0.2}
	b.refreshPopup([]*config.DownloadTask{task})
	firstRebuild := b.lastFullRebuild
	if firstRebuild.IsZero() || b.lastStructureKey == "" {
		t.Fatal("first refresh should rebuild and record its structure key")
	}

	// Pure progress tick: same task pointer, same status → in-place update.
	task.Progress = 0.6
	task.StatusMessage = "Chapter 1/1: Downloading image 2/5"
	b.refreshPopup([]*config.DownloadTask{task})

	if !b.lastFullRebuild.Equal(firstRebuild) {
		t.Error("progress tick should not trigger a full list rebuild")
	}
	if b.activeRowProgress == nil || b.activeRowProgress.Value != 0.6 {
		t.Errorf("active row progress should update in place, got %+v", b.activeRowProgress)
	}
	if !strings.Contains(b.statusMessage.Text, "Downloading image 2/5") {
		t.Errorf("status bar should reflect the progress tick, got %q", b.statusMessage.Text)
	}
}

// TestRefreshPopupCoalescesRapidStructuralChanges verifies that structural
// changes arriving inside the throttle window are coalesced into a scheduled
// rebuild instead of rebuilding the list immediately.
func TestRefreshPopupCoalescesRapidStructuralChanges(t *testing.T) {
	state, _, _ := newChapterListViewTest(t, "")
	b := NewDownloadQueueButton(state)
	b.buildPopup()

	task := &config.DownloadTask{ID: "1", Manga: config.Bookmarks{Title: "Manga A"}, Chapter: "a1.cbz", Status: "downloading", Progress: 0.2}
	b.refreshPopup([]*config.DownloadTask{task})
	firstRebuild := b.lastFullRebuild

	task.Status = "completed"
	b.refreshPopup([]*config.DownloadTask{task})

	if b.pendingRebuild == nil {
		t.Error("rapid structural change should schedule a coalesced rebuild")
	}
	if !b.lastFullRebuild.Equal(firstRebuild) {
		t.Error("coalesced rebuild should not run immediately")
	}
	if b.pendingRebuild != nil {
		b.pendingRebuild.Stop()
		b.pendingRebuild = nil
	}
}

// TestUpdateStatusBar verifies the footer status bar shows the site in the
// badge and the truncated manga title plus status message, and shows the idle
// state when nothing is downloading.
func TestUpdateStatusBar(t *testing.T) {
	state, _, _ := newChapterListViewTest(t, "")
	b := NewDownloadQueueButton(state)
	b.buildPopup()

	active := &config.DownloadTask{
		ID:            "1",
		Manga:         config.Bookmarks{Title: "The 100 Girlfriends Who Really, Really, Really, Really, REALLY Love You", Site: "FlameComics"},
		Chapter:       "a1.cbz",
		Status:        "downloading",
		StatusMessage: "Chapter 3/19: Downloading image 8/10 — retry 2/3 in 4s",
	}
	b.updateStatusBar([]*config.DownloadTask{active})
	if b.statusBadge.Text != "FlameComics ⏳" {
		t.Errorf("expected badge with site and backoff glyph, got %q", b.statusBadge.Text)
	}
	if !strings.HasPrefix(b.statusMessage.Text, "The 100 Girlfriends Who Reall…") {
		t.Errorf("expected message to start with truncated manga title, got %q", b.statusMessage.Text)
	}
	if !strings.HasSuffix(b.statusMessage.Text, active.StatusMessage) {
		t.Errorf("expected message to contain the status message, got %q", b.statusMessage.Text)
	}

	b.updateStatusBar([]*config.DownloadTask{
		{Manga: config.Bookmarks{Title: "Manga A"}, Chapter: "a1.cbz", Status: "queued"},
	})
	if b.statusBadge.Text != "⏸ Idle" {
		t.Errorf("expected Idle badge with no active task, got %q", b.statusBadge.Text)
	}

	b.updateStatusBar(nil)
	if b.statusBadge.Text != "⏸ Idle" || b.statusMessage.Text != "No downloads in queue" {
		t.Errorf("expected empty idle message, got badge=%q message=%q", b.statusBadge.Text, b.statusMessage.Text)
	}
}

// TestDownloadQueuePopupHasStatusBar verifies the pop-up is a Border layout with
// a footer status bar (bottom) holding the status badge and message labels.
func TestDownloadQueuePopupHasStatusBar(t *testing.T) {
	app := test.NewApp()
	w := test.NewWindow(nil)
	t.Cleanup(func() {
		w.Close()
		app.Quit()
	})

	b := NewDownloadQueueButton(&KanshoAppState{Window: w})
	content := b.buildPopup()

	border, ok := content.(*fyne.Container)
	if !ok {
		t.Fatal("popup content should be a container")
	}
	if len(border.Objects) < 3 {
		t.Fatalf("popup should have center, header and footer, got %d objects", len(border.Objects))
	}

	// container.NewBorder stores the centre first, then top, then bottom.
	footer := border.Objects[2]
	vbox, ok := footer.(*fyne.Container)
	if !ok {
		t.Fatalf("popup footer should be a container, got %T", footer)
	}
	row := vbox.Objects[len(vbox.Objects)-1]
	if _, ok := row.(*fyne.Container); !ok {
		t.Fatalf("footer status row should be a border container, got %T", row)
	}

	// The status bar should reflect the current download once the popup refreshes.
	b.updateStatusBar([]*config.DownloadTask{
		{ID: "1", Manga: config.Bookmarks{Title: "Manga A"}, Chapter: "a1.cbz", Status: "downloading", StatusMessage: "Chapter 3/19: Downloading image 8/10"},
	})
	if b.statusBadge.Text == "" || b.statusMessage.Text == "" {
		t.Error("status bar labels should be populated after updateStatusBar")
	}
}
