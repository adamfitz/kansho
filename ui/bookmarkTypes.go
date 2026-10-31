package ui

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"kansho/mangadex"
)

type bookmarkTypeStore interface {
	LoadBookmarkTypeEntries() ([]mangadex.BookmarkTypeEntry, error)
	ApplyBookmarkTypeUpdates([]mangadex.BookmarkTypeUpdate, func(completed, total int)) (int, error)
}

type bookmarkTypeRow struct {
	entry        mangadex.BookmarkTypeEntry
	original     string
	selected     string
	mangaRadio   *widget.RadioGroup
	webtoonRadio *widget.RadioGroup
}

type bookmarkTypeEditor struct {
	parent           fyne.Window
	store            bookmarkTypeStore
	originalContent  fyne.CanvasObject
	content          fyne.CanvasObject
	rows             []*bookmarkTypeRow
	mangaSelectAll   *widget.Check
	webtoonSelectAll *widget.Check
	massType         string
	updatingChecks   bool
}

func ShowBookmarkTypeView(parent fyne.Window) {
	store, err := mangadex.GetStore()
	if err != nil {
		dialog.ShowError(fmt.Errorf("cannot open Kansho database: %w", err), parent)
		return
	}
	showBookmarkTypeView(parent, store)
}

func showBookmarkTypeView(parent fyne.Window, store bookmarkTypeStore) *bookmarkTypeEditor {
	entries, err := store.LoadBookmarkTypeEntries()
	if err != nil {
		dialog.ShowError(fmt.Errorf("cannot load bookmark types: %w", err), parent)
		return nil
	}

	editor := &bookmarkTypeEditor{
		parent:          parent,
		store:           store,
		originalContent: parent.Content(),
	}
	editor.updatingChecks = true
	for _, entry := range entries {
		row := &bookmarkTypeRow{entry: entry, original: entry.Type, selected: entry.Type}
		row.mangaRadio = widget.NewRadioGroup([]string{" "}, func(string) {
			editor.selectRowType(row, "manga")
		})
		row.webtoonRadio = widget.NewRadioGroup([]string{" "}, func(string) {
			editor.selectRowType(row, "webtoon")
		})
		row.mangaRadio.Horizontal = true
		row.webtoonRadio.Horizontal = true
		switch row.selected {
		case "manga":
			row.mangaRadio.SetSelected(" ")
		case "webtoon":
			row.webtoonRadio.SetSelected(" ")
		}
		editor.rows = append(editor.rows, row)
	}
	editor.updatingChecks = false

	editor.mangaSelectAll = widget.NewCheck("Select all", func(checked bool) {
		if !editor.updatingChecks && checked {
			editor.selectAll("manga")
		} else if !editor.updatingChecks && editor.massType == "manga" {
			editor.massType = ""
		}
	})
	editor.webtoonSelectAll = widget.NewCheck("Select all", func(checked bool) {
		if !editor.updatingChecks && checked {
			editor.selectAll("webtoon")
		} else if !editor.updatingChecks && editor.massType == "webtoon" {
			editor.massType = ""
		}
	})

	header := container.NewBorder(nil, nil, nil,
		container.NewGridWithColumns(2,
			container.NewCenter(NewBoldLabel("Manga")),
			container.NewCenter(NewBoldLabel("Webtoon"))),
		NewBoldLabel("Title"))
	selectAllRow := container.NewBorder(nil, nil, nil,
		container.NewGridWithColumns(2,
			container.NewCenter(editor.mangaSelectAll),
			container.NewCenter(editor.webtoonSelectAll)),
		widget.NewLabel(""))
	rows := container.NewVBox()
	for _, row := range editor.rows {
		rows.Add(container.NewBorder(nil, nil, nil,
			container.NewGridWithColumns(2,
				container.NewCenter(row.mangaRadio),
				container.NewCenter(row.webtoonRadio)),
			bookmarkTypeTitle(row.entry.Title)))
		rows.Add(widget.NewSeparator())
	}

	cancelButton := widget.NewButton("Cancel", func() {
		parent.SetContent(editor.originalContent)
		parent.RequestFocus()
	})
	saveButton := widget.NewButton("Save", editor.save)
	footer := container.NewBorder(nil, nil, nil, container.NewHBox(cancelButton, saveButton), nil)
	editor.content = container.NewBorder(
		container.NewVBox(header, selectAllRow, widget.NewSeparator()),
		footer,
		nil,
		nil,
		container.NewVScroll(rows),
	)
	parent.SetContent(editor.content)
	return editor
}

func bookmarkTypeTitle(title string) fyne.CanvasObject {
	label := widget.NewLabel(title)
	label.Truncation = fyne.TextTruncateEllipsis
	return label
}

func (editor *bookmarkTypeEditor) selectAll(kind string) {
	editor.massType = kind
	editor.updatingChecks = true
	editor.mangaSelectAll.SetChecked(kind == "manga")
	editor.webtoonSelectAll.SetChecked(kind == "webtoon")
	editor.updatingChecks = false
	for _, row := range editor.rows {
		editor.setRowType(row, kind)
	}
}

func (editor *bookmarkTypeEditor) selectRowType(row *bookmarkTypeRow, kind string) {
	if editor.updatingChecks {
		return
	}
	editor.massType = ""
	editor.updatingChecks = true
	editor.mangaSelectAll.SetChecked(false)
	editor.webtoonSelectAll.SetChecked(false)
	editor.updatingChecks = false
	editor.setRowType(row, kind)
}

func (editor *bookmarkTypeEditor) setRowType(row *bookmarkTypeRow, kind string) {
	row.selected = kind
	wasUpdating := editor.updatingChecks
	editor.updatingChecks = true
	if kind == "manga" {
		row.mangaRadio.SetSelected(" ")
		row.webtoonRadio.SetSelected("")
	} else {
		row.mangaRadio.SetSelected("")
		row.webtoonRadio.SetSelected(" ")
	}
	editor.updatingChecks = wasUpdating
	row.mangaRadio.Refresh()
	row.webtoonRadio.Refresh()
}

func (editor *bookmarkTypeEditor) updates() []mangadex.BookmarkTypeUpdate {
	updates := make([]mangadex.BookmarkTypeUpdate, 0, len(editor.rows))
	for _, row := range editor.rows {
		if editor.massType != "" || row.selected != row.original {
			updates = append(updates, mangadex.BookmarkTypeUpdate{ID: row.entry.ID, Type: row.selected})
		}
	}
	return updates
}

func (editor *bookmarkTypeEditor) save() {
	updates := editor.updates()
	if len(updates) == 0 {
		editor.parent.SetContent(editor.originalContent)
		return
	}
	apply := func(confirmed bool) {
		if confirmed {
			editor.apply(updates)
		}
	}
	if editor.massType != "" {
		dialog.ShowConfirm("Update All Bookmark Types",
			fmt.Sprintf("ALL %d bookmark types will be updated to %s. Continue?", len(editor.rows), editor.massType),
			apply, editor.parent)
	} else if len(updates) > 1 {
		dialog.ShowConfirm("Update Bookmark Types",
			fmt.Sprintf("%d bookmark types have been selected for update. Continue?", len(updates)),
			apply, editor.parent)
	} else {
		editor.apply(updates)
	}
}

func (editor *bookmarkTypeEditor) apply(updates []mangadex.BookmarkTypeUpdate) {
	progress := widget.NewProgressBar()
	status := widget.NewLabel(fmt.Sprintf("0 / %d titles", len(updates)))
	pleaseWait := NewBoldLabel("Please wait...")
	editor.parent.SetContent(container.NewBorder(
		container.NewVBox(NewBoldLabel("Bookmark Type"), pleaseWait, status),
		nil, nil, nil,
		container.NewCenter(progress),
	))

	go func() {
		updated, err := editor.store.ApplyBookmarkTypeUpdates(updates, func(completed, total int) {
			fyne.Do(func() {
				progress.SetValue(float64(completed) / float64(total))
				status.SetText(fmt.Sprintf("%d / %d titles", completed, total))
			})
		})
		fyne.Do(func() {
			if err != nil {
				editor.parent.SetContent(editor.content)
				dialog.ShowError(fmt.Errorf("failed to update bookmark types: %w", err), editor.parent)
				return
			}
			editor.showComplete(updated, updates)
		})
	}()
}

func (editor *bookmarkTypeEditor) showComplete(updated int, updates []mangadex.BookmarkTypeUpdate) {
	mangaCount := 0
	webtoonCount := 0
	blankCount := 0
	for _, update := range updates {
		switch update.Type {
		case "manga":
			mangaCount++
		case "webtoon":
			webtoonCount++
		default:
			blankCount++
		}
	}

	summary := container.NewVBox(
		widget.NewLabel(fmt.Sprintf("Updated: %d", updated)),
		widget.NewLabel(fmt.Sprintf("Manga: %d", mangaCount)),
		widget.NewLabel(fmt.Sprintf("Webtoon: %d", webtoonCount)),
		widget.NewLabel(fmt.Sprintf("Blank: %d", blankCount)),
	)
	okayButton := widget.NewButton("Okay", func() {
		editor.parent.SetContent(BuildMainLayout(editor.parent))
		editor.parent.RequestFocus()
	})
	editor.parent.SetContent(container.NewBorder(
		container.NewVBox(NewBoldLabel("Bookmark Type"), NewBoldLabel("Completed")),
		container.NewBorder(nil, nil, nil, okayButton, nil),
		nil, nil,
		container.NewCenter(summary),
	))
}
