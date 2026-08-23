package ui

import (
	"slices"
	"testing"

	"kansho/refreshpool"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// TestMainStatusBarIsSlim verifies the bar is a one-line strip, not an
// oversized card (the standard card component forces a 100px minimum height).
func TestMainStatusBarIsSlim(t *testing.T) {
	app := test.NewApp()
	w := test.NewWindow(nil)
	t.Cleanup(func() {
		w.Close()
		app.Quit()
	})

	bar := NewMainStatusBar()
	min := bar.Bar.MinSize()
	if min.Height > 60 {
		t.Errorf("status bar should be a slim strip, got min height %v", min.Height)
	}

	// Laid out in a bottom slot like the main layout does, it must stay short.
	layout := container.NewBorder(nil, bar.Bar, nil, nil, widget.NewLabel("filler"))
	w.SetContent(layout)
	w.Resize(fyne.NewSize(1250, 850))
	if h := bar.Bar.Size().Height; h > 60 {
		t.Errorf("status bar should stay slim when laid out at the bottom, got height %v", h)
	}
}

// TestRefreshAreaClickableOnlyWhileBusy verifies the bottom-right refresh
// readout only becomes a click target while the pool has work in flight.
func TestRefreshAreaClickableOnlyWhileBusy(t *testing.T) {
	bar := NewMainStatusBar()
	if bar.refreshArea.clickable {
		t.Fatal("refresh area must not be clickable while idle")
	}

	bar.SetRefreshPoolStatus(refreshpool.Status{Running: 2, Queued: 3,
		Sites: []string{"a", "b"}})
	if !bar.refreshArea.clickable {
		t.Error("refresh area should be clickable while the pool is busy")
	}

	bar.SetRefreshPoolStatus(refreshpool.Status{})
	if bar.refreshArea.clickable {
		t.Error("refresh area should stop being clickable when idle")
	}
}

// TestRefreshDialogListsSitesAndAutoCloses verifies that tapping the busy
// readout opens a dialog with one row per domain (name left, dot indicator on
// the same line), that repeated taps do not stack dialogs, and that the
// dialog closes (and its animation stops) once the pool drains.
func TestRefreshDialogListsSitesAndAutoCloses(t *testing.T) {
	app := test.NewApp()
	w := test.NewWindow(nil)
	t.Cleanup(func() {
		w.Close()
		app.Quit()
	})

	bar := NewMainStatusBar()
	bar.SetWindow(w)

	// Taps while idle do nothing.
	bar.refreshArea.Tapped(nil)
	if bar.refreshDialog != nil {
		t.Fatal("no dialog should open while the pool is idle")
	}

	busy := refreshpool.Status{Running: 1, Queued: 1, Sites: []string{"alpha", "zeta"}}
	bar.SetRefreshPoolStatus(busy)

	bar.refreshArea.Tapped(nil)
	if bar.refreshDialog == nil {
		t.Fatal("tapping the busy readout should open the refresh dialog")
	}

	// One row per domain: name on the left, dots indicator beside it.
	if got, want := domainTexts(bar.domainLabels), []string{"alpha", "zeta"}; !slices.Equal(got, want) {
		t.Errorf("dialog domains = %v, want %v", got, want)
	}
	if len(bar.dotsLabels) != len(bar.domainLabels) {
		t.Fatalf("dot indicators = %d, want one per domain row (%d)",
			len(bar.dotsLabels), len(bar.domainLabels))
	}
	for i, dots := range bar.dotsLabels {
		if !slices.Contains(refreshDotsFrames, dots.Text) {
			t.Errorf("row %d shows %q, want a dots frame %v", i, dots.Text, refreshDotsFrames)
		}
	}
	if bar.dotsTicker == nil {
		t.Error("the dialog's dot indicator should be animating")
	}

	// The dialog must be large enough to show the rows without truncation.
	if size := bar.dialogSize; size.Width < 380 || size.Height <= 0 {
		t.Errorf("dialog too small to fit its content: %v", size)
	}

	// A second tap must reuse the open dialog instead of stacking another.
	first := bar.refreshDialog
	bar.refreshArea.Tapped(nil)
	if bar.refreshDialog != first {
		t.Error("repeated taps must not open a second dialog")
	}

	// Status changes while open are reflected in the domain list.
	bar.SetRefreshPoolStatus(refreshpool.Status{Running: 1, Sites: []string{"alpha", "mid", "zeta"}})
	if got, want := domainTexts(bar.domainLabels), []string{"alpha", "mid", "zeta"}; !slices.Equal(got, want) {
		t.Errorf("dialog domains after update = %v, want %v", got, want)
	}

	// Draining the pool closes the dialog and stops the animation.
	bar.SetRefreshPoolStatus(refreshpool.Status{})
	if bar.refreshDialog != nil {
		t.Error("the refresh dialog should close once the pool is idle")
	}
	if bar.dotsTicker != nil {
		t.Error("the dot animation should stop when the dialog closes")
	}
}

// domainTexts collects the text of each domain label in display order.
func domainTexts(labels []*widget.Label) []string {
	out := make([]string, len(labels))
	for i, l := range labels {
		out[i] = l.Text
	}
	return out
}
