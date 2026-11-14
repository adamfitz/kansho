package ui

import (
	"slices"
	"testing"

	"kansho/metadataqueue"
	"kansho/refreshpool"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
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
func TestMainStatusBarShowsLastRefreshWhenAvailable(t *testing.T) {
	bar := NewMainStatusBar()
	bar.ShowManga("test", &chapterCounts{Total: 4, Downloaded: 2, NotDownloaded: 2, LastRefresh: "2026-09-26"})
	if got, want := bar.message.Text, "2 of 4 chapters downloaded · 2 to download · Last Refresh: 2026-09-26"; got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
}

// TestMetadataRefreshReadoutMatchesChapterRefresh verifies the metadata
// readout uses the same idle label, busy label and spinner as the chapter
// refresh readout.
func TestMetadataRefreshReadoutMatchesChapterRefresh(t *testing.T) {
	bar := NewMainStatusBar()
	if got, want := bar.metaStatus.Text, "⟳ Metadata Refresh: idle"; got != want {
		t.Fatalf("metadata idle text = %q, want %q", got, want)
	}
	if got, want := bar.metaSpinner.Text, ""; got != want {
		t.Fatalf("metadata idle spinner = %q, want %q", got, want)
	}

	bar.SetMetadataQueueStatus(metadataqueue.Status{Running: 2, Queued: 3})
	if got, want := bar.metaStatus.Text, "Metadata Refresh: 2 running · 3 queued"; got != want {
		t.Fatalf("metadata busy text = %q, want %q", got, want)
	}
	if got, want := bar.metaSpinner.Text, refreshSpinnerFrames[0]; got != want {
		t.Fatalf("metadata busy spinner = %q, want %q", got, want)
	}
	if !bar.metaArea.clickable {
		t.Error("metadata refresh area should be clickable while busy")
	}

	bar.SetMetadataQueueStatus(metadataqueue.Status{})
	if got, want := bar.metaStatus.Text, "⟳ Metadata Refresh: idle"; got != want {
		t.Fatalf("metadata idle text after drain = %q, want %q", got, want)
	}
	if got, want := bar.metaSpinner.Text, ""; got != want {
		t.Fatalf("metadata spinner after drain = %q, want %q", got, want)
	}
}

// TestStatusBarReadoutsKeepRightPadding verifies the idle metadata readout is
// inset from the right edge and separated from the chapter refresh readout,
// instead of sitting flush against the status bar.
func TestStatusBarReadoutsKeepRightPadding(t *testing.T) {
	app := test.NewApp()
	w := test.NewWindow(nil)
	t.Cleanup(func() {
		w.Close()
		app.Quit()
	})

	bar := NewMainStatusBar()
	w.SetContent(container.NewBorder(nil, bar.Bar, nil, nil, widget.NewLabel("filler")))
	w.Resize(fyne.NewSize(1250, 850))

	abs := func(obj fyne.CanvasObject) fyne.Position {
		return app.Driver().AbsolutePositionForObject(obj)
	}
	barRight := abs(bar.Bar).X + bar.Bar.Size().Width
	metaRight := abs(bar.metaArea).X + bar.metaArea.Size().Width
	if inset := barRight - metaRight; inset < theme.Padding()*2 {
		t.Errorf("metadata readout right inset = %v, want at least %v", inset, theme.Padding()*2)
	}

	refreshRight := abs(bar.refreshArea).X + bar.refreshArea.Size().Width
	if gap := abs(bar.metaArea).X - refreshRight; gap < theme.Padding()*2 {
		t.Errorf("gap between readouts = %v, want at least %v", gap, theme.Padding()*2)
	}
}

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
