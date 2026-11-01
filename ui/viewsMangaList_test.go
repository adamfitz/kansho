package ui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"

	"kansho/config"
)

func TestMangaListScrollbarKeepsFullIdleWidth(t *testing.T) {
	app := test.NewApp()
	view := NewMangaListView(&KanshoAppState{MangaData: config.Manga{Manga: []config.Bookmarks{{Title: "Alpha"}}}})
	window := test.NewWindow(view.Card)
	window.Resize(fyne.NewSize(500, 500))
	window.Show()
	t.Cleanup(func() {
		window.Close()
		app.Quit()
	})
	idleWidth := theme.SizeForWidget(theme.SizeNameScrollBarSmall, view.List)
	activeWidth := theme.SizeForWidget(theme.SizeNameScrollBar, view.List)
	if idleWidth != activeWidth {
		t.Fatalf("idle scrollbar width = %v, active width = %v", idleWidth, activeWidth)
	}
}
