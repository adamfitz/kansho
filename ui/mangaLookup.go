package ui

import (
	"context"
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"kansho/mangadex"
)

// lookupPageSize is the number of MangaDex search results fetched per page.
const lookupPageSize = 50

// tappableLabel is a single-line text row that reacts to a tap. It is used for
// each MangaDex search result in the lookup dialog.
type tappableLabel struct {
	widget.Label
	onTapped func()
}

func (l *tappableLabel) Tapped(_ *fyne.PointEvent) {
	if l.onTapped != nil {
		l.onTapped()
	}
}

// titleLookupDialog presents a searchable, scrollable list of MangaDex title
// candidates plus a manual URL/title-id field. Search results are fetched 50 at
// a time; scrolling near the bottom loads the next page in the background.
type titleLookupDialog struct {
	window fyne.Window

	// display renders the chosen MangaDex title in the chapter list pane.
	display DisplayMangaInfoFunc

	// bookmarkTitle is the bookmarked manga title that triggered the lookup.
	// It is mapped to whichever MangaDex title the user picks (or types), so a
	// later info-button click is served from the local database.
	bookmarkTitle string

	searchEntry  *widget.Entry
	manualEntry  *widget.Entry
	statusLabel  *widget.Label
	resultBox    *fyne.Container
	resultScroll *container.Scroll

	dlg *dialog.CustomDialog

	loading   bool
	queryText string
	offset    int
	total     int
}

func newTitleLookupDialog(initialQuery string, window fyne.Window, display DisplayMangaInfoFunc) *titleLookupDialog {
	d := &titleLookupDialog{
		window:        window,
		display:       display,
		bookmarkTitle: initialQuery,
		queryText:     initialQuery,
	}

	d.statusLabel = widget.NewLabel("")
	d.statusLabel.Wrapping = fyne.TextWrapWord

	d.searchEntry = widget.NewEntry()
	d.searchEntry.SetPlaceHolder("Search MangaDex titles...")
	d.searchEntry.SetText(initialQuery)

	searchButton := widget.NewButtonWithIcon("Search", theme.SearchIcon(), d.doSearch)
	d.searchEntry.OnSubmitted = func(_ string) { d.doSearch() }

	d.manualEntry = widget.NewEntry()
	d.manualEntry.SetPlaceHolder("https://mangadex.org/title/<id>  or  <id>")
	d.manualEntry.Validator = func(text string) error {
		if strings.TrimSpace(text) == "" {
			return nil
		}
		_, err := mangadex.ExtractID(text)
		return err
	}
	fetchButton := widget.NewButton("Fetch", d.doManualFetch)
	d.manualEntry.OnSubmitted = func(_ string) { d.doManualFetch() }

	hint := widget.NewLabel("You can also enter a MangaDex title URL or title ID directly — that always takes precedence over the search results.")
	hint.Wrapping = fyne.TextWrapWord
	hint.Importance = widget.LowImportance

	d.resultBox = container.NewVBox()
	d.resultScroll = container.NewVScroll(d.resultBox)
	d.resultScroll.SetMinSize(fyne.NewSize(680, 360))
	d.resultScroll.OnScrolled = func(pos fyne.Position) {
		d.onScrolled(pos)
	}

	content := container.NewBorder(
		container.NewVBox(
			widget.NewLabel("Search MangaDex:"),
			container.NewBorder(nil, nil, nil, searchButton, d.searchEntry),
			NewSeparator(),
			widget.NewLabel("MangaDex URL or title ID:"),
			container.NewBorder(nil, nil, nil, fetchButton, d.manualEntry),
			NewSeparator(),
			hint,
		),
		container.NewVBox(d.statusLabel),
		nil,
		nil,
		d.resultScroll,
	)

	d.dlg = dialog.NewCustom("MangaDex Title Lookup", "Close", content, window)
	d.dlg.Resize(fyne.NewSize(760, 620))
	return d
}

func (d *titleLookupDialog) show() {
	d.dlg.Show()
	d.doSearch()
}

func (d *titleLookupDialog) setStatus(msg string) {
	d.statusLabel.SetText(msg)
}

// doSearch starts afresh a MangaDex title search with the current search entry
// text, resetting pagination and clearing the result list.
func (d *titleLookupDialog) doSearch() {
	q := strings.TrimSpace(d.searchEntry.Text)
	if q == "" {
		d.setStatus("Enter a title to search.")
		return
	}
	d.queryText = q
	d.offset = 0
	d.total = 0
	d.loading = false
	d.resultBox.RemoveAll()
	d.resultScroll.Refresh()
	d.setStatus(fmt.Sprintf("Searching MangaDex for %q...", q))
	d.fetchPage(0)
}

// fetchPage loads one page of search results starting at offset and appends
// them to the result list.
func (d *titleLookupDialog) fetchPage(offset int) {
	if d.loading {
		return
	}
	d.loading = true
	q := d.queryText
	go func() {
		client := mangadex.NewClient()
		results, total, err := client.SearchManga(context.Background(), q, offset)
		fyne.Do(func() {
			d.loading = false
			if err != nil {
				d.setStatus(fmt.Sprintf("Search failed: %v", err))
				return
			}
			d.total = total
			d.offset = offset + len(results)
			for _, r := range results {
				d.appendResult(r)
			}
			d.resultScroll.Refresh()
			if total == 0 {
				d.setStatus("No titles found on MangaDex.")
				return
			}
			d.setStatus(fmt.Sprintf("Showing %d of %d matching titles — scroll for more.", d.offset, total))
		})
	}()
}

func (d *titleLookupDialog) appendResult(r mangadex.SearchResult) {
	row := &tappableLabel{}
	row.Text = r.DisplayTitle()
	row.Truncation = fyne.TextTruncateEllipsis
	row.ExtendBaseWidget(row)
	capture := r
	row.onTapped = func() {
		d.onResultChosen(capture)
	}
	d.resultBox.Add(container.NewBorder(nil, nil, nil, nil, row))
}

// onScrolled triggers the next page load when the user scrolls near the bottom
// of the result list.
func (d *titleLookupDialog) onScrolled(pos fyne.Position) {
	if d.loading || d.offset <= 0 {
		return
	}
	if d.total > 0 && d.offset >= d.total {
		return
	}
	contentHeight := d.resultScroll.Content.Size().Height
	viewHeight := d.resultScroll.Size().Height
	if pos.Y+viewHeight >= contentHeight-120 {
		d.setStatus(fmt.Sprintf("Loading more titles (%d/%d)...", d.offset, d.total))
		d.fetchPage(d.offset)
	}
}

// onResultChosen is called when the user taps a search result row. A manually
// entered MangaDex URL/ID always takes precedence, so the tap is ignored while
// the manual field is non-empty.
func (d *titleLookupDialog) onResultChosen(r mangadex.SearchResult) {
	if strings.TrimSpace(d.manualEntry.Text) != "" {
		d.setStatus("Manual MangaDex URL/ID takes precedence — clear it to use the search results.")
		return
	}
	d.dlg.Hide()
	fetchAndShow(d.window, r.ID, d.bookmarkTitle, d.display)
}

// doManualFetch fetches the MangaDex title given by the manual URL/ID field.
func (d *titleLookupDialog) doManualFetch() {
	raw := strings.TrimSpace(d.manualEntry.Text)
	if raw == "" {
		d.setStatus("Enter a MangaDex URL or title ID to fetch it directly.")
		return
	}
	id, err := mangadex.ExtractID(raw)
	if err != nil {
		d.statusLabel.SetText(err.Error())
		return
	}
	d.dlg.Hide()
	fetchAndShow(d.window, id, d.bookmarkTitle, d.display)
}
