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
	for _, status := range []string{"queued", "downloading", "waiting_cf", "completed", "cancelled", "failed"} {
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
	for _, status := range []string{"failed", "cancelled", "waiting_cf"} {
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
