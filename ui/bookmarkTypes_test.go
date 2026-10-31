package ui

import (
	"testing"

	"kansho/mangadex"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

type bookmarkTypeTestStore struct {
	entries []mangadex.BookmarkTypeEntry
}

func (store *bookmarkTypeTestStore) LoadBookmarkTypeEntries() ([]mangadex.BookmarkTypeEntry, error) {
	return store.entries, nil
}

func (store *bookmarkTypeTestStore) ApplyBookmarkTypeUpdates(updates []mangadex.BookmarkTypeUpdate, progress func(int, int)) (int, error) {
	return len(updates), nil
}

func TestBookmarkTypeEditorBulkAndPerRowSelection(t *testing.T) {
	app := test.NewApp()
	window := test.NewWindow(nil)
	t.Cleanup(func() {
		window.Close()
		app.Quit()
	})

	store := &bookmarkTypeTestStore{entries: []mangadex.BookmarkTypeEntry{
		{ID: 1, Title: "Alpha", Type: "manga"},
		{ID: 2, Title: "Beta", Type: "webtoon"},
		{ID: 3, Title: "Gamma", Type: ""},
	}}
	editor := showBookmarkTypeView(window, store)
	if editor == nil {
		t.Fatal("showBookmarkTypeView() returned nil")
	}
	if _, ok := findScroll(editor.content); !ok {
		t.Fatal("bookmark rows should be scrollable")
	}
	if got := countBookmarkTypeSeparators(editor.content); got != len(store.entries)+1 {
		t.Fatalf("table has %d separators, want one header rule and one per title", got)
	}

	editor.mangaSelectAll.OnChanged(true)
	if !editor.mangaSelectAll.Checked || editor.webtoonSelectAll.Checked {
		t.Fatal("select-all controls should be mutually exclusive")
	}
	if editor.massType != "manga" || len(editor.updates()) != 3 {
		t.Fatalf("bulk selection = %q with %d updates; want manga with 3", editor.massType, len(editor.updates()))
	}
	for _, row := range editor.rows {
		if row.selected != "manga" || row.mangaRadio.Selected != " " || row.webtoonRadio.Selected != "" {
			t.Errorf("row %q did not show Manga selected: %+v", row.entry.Title, row)
		}
	}

	editor.selectRowType(editor.rows[1], "webtoon")
	if editor.mangaSelectAll.Checked || editor.webtoonSelectAll.Checked || editor.massType != "" {
		t.Fatal("per-row selection should clear both select-all controls")
	}
	updates := editor.updates()
	if len(updates) != 1 || updates[0] != (mangadex.BookmarkTypeUpdate{ID: 3, Type: "manga"}) {
		t.Fatalf("pending updates = %+v, want only Gamma as manga", updates)
	}
}

func countBookmarkTypeSeparators(root fyne.CanvasObject) int {
	switch object := root.(type) {
	case *widget.Separator:
		return 1
	case *container.Scroll:
		return countBookmarkTypeSeparators(object.Content)
	case *fyne.Container:
		count := 0
		for _, child := range object.Objects {
			count += countBookmarkTypeSeparators(child)
		}
		return count
	default:
		return 0
	}
}
