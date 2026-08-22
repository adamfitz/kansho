package ui

import (
	"testing"

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
