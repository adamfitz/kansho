package ui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"kansho/config"
	"kansho/mangadex"
)

func TestManualEntryFitsTitleLookupDialog(t *testing.T) {
	app := test.NewApp()
	window := test.NewWindow(nil)
	window.Resize(fyne.NewSize(1000, 800))
	t.Cleanup(func() {
		window.Close()
		app.Quit()
	})

	lookup := newTitleLookupDialog(config.Bookmarks{Title: "Test title"}, window,
		func(*mangadex.MangaInfo, string) {})
	lookup.dlg.Show()
	lookup.openManualEntry()

	minSize := lookup.dlg.MinSize()
	if minSize.Width > titleLookupDialogWidth || minSize.Height > titleLookupDialogHeight {
		t.Fatalf("manual editor minimum size %v exceeds lookup dialog %v",
			minSize, fyne.NewSize(titleLookupDialogWidth, titleLookupDialogHeight))
	}
}
