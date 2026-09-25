package ui

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"kansho/config"
	"kansho/mangadex"
)

// DisplayMangaInfoFunc renders a fetched MangaDex title record. It is supplied
// by the chapter list pane so title info is shown in the main window instead of
// a separate dialog window.
type DisplayMangaInfoFunc func(info *mangadex.MangaInfo, bookmarkTitle string)

// ShowMangaTitleInfo is the entry point for the (i) info button on a manga
// list row. It looks the manga up in the local Kansho database first. When a
// local record exists it renders the information pane immediately; otherwise a
// title lookup dialog is shown so the user can pick the correct MangaDex
// title (or enter a MangaDex URL / title ID manually).
func ShowMangaTitleInfo(manga config.Bookmarks, window fyne.Window, display DisplayMangaInfoFunc) {
	if display == nil {
		dialog.ShowError(fmt.Errorf("MangaDex information display is not available"), window)
		return
	}

	store, err := mangadex.GetStore()
	if err != nil {
		dialog.ShowError(fmt.Errorf("cannot open Kansho database: %v", err), window)
		return
	}

	// If the bookmark is itself a MangaDex title we already know the ID: use it
	// directly (no search dialog needed).
	if id, parseErr := mangadex.ExtractID(manga.Url); parseErr == nil {
		local, localErr := store.LookupByID(id)
		if localErr != nil {
			dialog.ShowError(fmt.Errorf("lookup %s: %v", id, localErr), window)
			return
		}
		if local != nil {
			display(local, manga.Title)
			return
		}
		fetchAndShow(window, id, manga.Title, display)
		return
	}

	local, err := store.LookupByTitle(manga.Title)
	if err != nil {
		dialog.ShowError(fmt.Errorf("lookup %q: %v", manga.Title, err), window)
		return
	}
	if local != nil {
		display(local, manga.Title)
		return
	}

	showMangaLookupDialog(manga.Title, window, display)
}

// storeFetchedInfo persists a fetched MangaDex title and records the bookmark
// title that maps to it, so every lookup outcome is written to the database and
// the same title is served locally from then on.
func storeFetchedInfo(info *mangadex.MangaInfo, bookmarkTitle string) error {
	if info == nil || info.ID == "" {
		return fmt.Errorf("nothing to store: no MangaDex title data")
	}
	store, err := mangadex.GetStore()
	if err != nil {
		return err
	}
	if err := store.Upsert(info); err != nil {
		return err
	}
	if strings.TrimSpace(bookmarkTitle) != "" {
		if aliasErr := store.SetTitleAlias(bookmarkTitle, info.ID); aliasErr != nil {
			log.Printf("[UI] Failed to alias %q -> %s: %v", bookmarkTitle, info.ID, aliasErr)
		}
	}
	return nil
}

// fetchAndShow fetches a single MangaDex title by ID, stores it locally (also
// mapping the bookmarked title to it), and renders the information pane.
func fetchAndShow(window fyne.Window, id, bookmarkTitle string, display DisplayMangaInfoFunc) {
	status := dialog.NewInformation("Looking Up MangaDex Title",
		fmt.Sprintf("Looking up MangaDex title %q...", id), window)
	fyne.Do(func() { status.Show() })

	go func() {
		client := mangadex.NewClient()
		info, err := client.GetManga(context.Background(), id)
		fyne.Do(func() {
			status.Hide()
			if err != nil || info == nil {
				dialog.ShowError(fmt.Errorf("could not fetch MangaDex title %s: %v", id, err), window)
				return
			}
			if storeErr := storeFetchedInfo(info, bookmarkTitle); storeErr != nil {
				dialog.ShowError(fmt.Errorf("could not store MangaDex info for %s: %v", id, storeErr), window)
				return
			}
			display(info, bookmarkTitle)
		})
	}()
}

// showMangaLookupDialog opens the MangaDex title selection dialog. It shows a
// searchable, scrollable list of matching titles (50 at a time, loading more in
// the background as the user scrolls) plus a manual field for a MangaDex URL or
// title ID.
func showMangaLookupDialog(query string, window fyne.Window, display DisplayMangaInfoFunc) {
	d := newTitleLookupDialog(query, window, display)
	d.show()
}

// --- Info window ---

// makeSelectableLabel creates a label whose text can be highlighted and copied
// (Ctrl+C or right-click copy), as required for every info window field.
func makeSelectableLabel(text string) *widget.Label {
	l := widget.NewLabel(text)
	l.Wrapping = fyne.TextWrapWord
	l.Selectable = true
	return l
}

// infoFieldLabel is a bold field name used in the info window rows.
func infoFieldLabel(text string) *widget.Label {
	l := widget.NewLabel(text)
	l.TextStyle = fyne.TextStyle{Bold: true}
	return l
}

// buildMangaInfoContent lays out the MangaDex title information as a pane that
// fills the chapter list card of the main window. The pane is rebuilt (via
// replace) after a refresh so a newer (or different) title record is shown with
// its own language set.
//
// English is the default language; a dropdown lists every language MangaDex
// actually publishes for the title and switching language re-queries MangaDex
// (with a visible notice) and redraws the fields. The Refresh button re-queries
// MangaDex and, when that returns nothing, prompts the user for a MangaDex URL
// or title ID.
func buildMangaInfoContent(parentWindow fyne.Window, replace func(fyne.CanvasObject), info *mangadex.MangaInfo, bookmarkTitle, initialLang string) fyne.CanvasObject {
	langs := info.AvailableLanguages()
	displayNames := make([]string, 0, len(langs)+1)
	codeOfDisplay := make(map[string]string)
	for _, code := range langs {
		display := languageDisplayName(code)
		codeOfDisplay[display] = code
		displayNames = append(displayNames, display)
	}
	if len(displayNames) == 0 {
		displayNames = append(displayNames, languageDisplayName("en"))
		codeOfDisplay[languageDisplayName("en")] = "en"
	}

	currentLang := initialLang
	if !stringInSlice(langs, currentLang) {
		if len(langs) > 0 {
			currentLang = langs[0]
		} else {
			currentLang = "en"
		}
	}

	titleLabel := widget.NewLabel(info.EnglishTitle())
	titleLabel.TextStyle = fyne.TextStyle{Bold: true}
	titleLabel.Wrapping = fyne.TextWrapWord
	titleLabel.Selectable = true

	notifyLabel := widget.NewLabel("")
	notifyLabel.Wrapping = fyne.TextWrapWord

	// The scrollable field area; swapped in place when the language changes.
	fieldScroll := container.NewVScroll(container.NewVBox())
	fieldScroll.SetMinSize(fyne.NewSize(680, 460))

	rebuildFields := func(lang string) {
		box := container.NewVBox(
			infoRow("MangaDex ID", info.ID),
			infoRow("Original Language", languageDisplayName(info.OriginalLanguage)),
			infoRow("Status", prettifyStatus(info.Status)),
			infoRow("Demographic", prettifyDemographic(info.PublicationDemographic)),
			infoRow("Year", intString(info.Year)),
			infoRow("Author(s)", info.Author),
			infoRow("Artist(s)", info.Artist),
			infoRow("Content Rating", prettifyRating(info.ContentRating)),
			infoRow("Tags", strings.Join(info.TagList(lang), ", ")),
			infoRow("Alt Titles", strings.Join(info.AltTitleList(), "\n")),
			buildURLRow(info.URL),
			NewSeparator(),
			infoRow("Description", strings.TrimSpace(info.DescriptionIn(lang))),
		)
		fieldScroll.Content = box
		fieldScroll.Refresh()
	}

	rebuildFields(currentLang)

	langSelect := widget.NewSelect(displayNames, func(display string) {
		newLang := codeOfDisplay[display]
		if newLang == "" || newLang == currentLang {
			return
		}
		currentLang = newLang
		notifyLabel.SetText(fmt.Sprintf("Requested language '%s' is being queried for this title...", display))
		rebuildFields(newLang)
		// Re-query MangaDex for this title so the shown language data is
		// verified against the latest remote record, then store the result.
		fetchLang := newLang
		go func() {
			client := mangadex.NewClient()
			fresh, err := client.GetManga(context.Background(), info.ID)
			fyne.Do(func() {
				if err != nil || fresh == nil {
					notifyLabel.SetText(fmt.Sprintf("%s — showing cached data (%v).", display, err))
					return
				}
				if storeErr := storeFetchedInfo(fresh, bookmarkTitle); storeErr != nil {
					notifyLabel.SetText(fmt.Sprintf("%s — fetched but not stored (%v).", display, storeErr))
				}
				info = fresh
				rebuildFields(fetchLang)
				notifyLabel.SetText(fmt.Sprintf("Showing %s for this title.", display))
			})
		}()
	})

	// Default the dropdown to the current language.
	langSelect.SetSelected(languageDisplayName(currentLang))

	// Refresh re-queries MangaDex for the current title. If the query returns
	// nothing the user is prompted for a MangaDex URL or title ID instead.
	refreshButton := widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), nil)
	refreshButton.OnTapped = func() {
		notifyLabel.SetText("Refreshing from MangaDex...")
		id := info.ID
		langAtTap := currentLang
		go func() {
			client := mangadex.NewClient()
			fresh, err := client.GetManga(context.Background(), id)
			fyne.Do(func() {
				if err != nil || fresh == nil {
					promptForMangaDexURL(parentWindow, replace, info, bookmarkTitle, langAtTap, err)
					return
				}
				if storeErr := storeFetchedInfo(fresh, bookmarkTitle); storeErr != nil {
					log.Printf("[UI] Refresh of %s not stored: %v", id, storeErr)
				}
				replace(buildMangaInfoContent(parentWindow, replace, fresh, bookmarkTitle, langAtTap))
			})
		}()
	}

	titleBlock := container.NewVBox(
		titleLabel,
		container.NewHBox(infoFieldLabel("Language:"), langSelect, refreshButton),
	)
	header := container.NewVBox(titleBlock, notifyLabel, NewSeparator())

	return container.NewBorder(header, nil, nil, nil, fieldScroll)
}

// promptForMangaDexURL asks the user for a MangaDex URL or title ID. It is
// shown by the Refresh button when the automatic MangaDex query returns no
// title, and saves whatever the user enters before redrawing the pane via
// replace.
func promptForMangaDexURL(parentWindow fyne.Window, replace func(fyne.CanvasObject), info *mangadex.MangaInfo, bookmarkTitle, lang string, cause error) {
	status := widget.NewLabel("")
	status.Wrapping = fyne.TextWrapWord
	if cause != nil {
		status.SetText(fmt.Sprintf("The MangaDex lookup failed: %v", cause))
	} else {
		status.SetText("The MangaDex lookup returned nothing for this title.")
	}

	entry := widget.NewEntry()
	entry.SetPlaceHolder("https://mangadex.org/title/<id>  or  <id>")
	if info != nil && info.URL != "" {
		entry.SetText(info.URL)
	} else if info != nil && info.ID != "" {
		entry.SetText(info.ID)
	}

	fetchButton := widget.NewButton("Fetch", nil)
	content := container.NewVBox(
		widget.NewLabel("Enter the MangaDex URL or title ID to fetch it directly."),
		container.NewBorder(nil, nil, nil, fetchButton, entry),
		NewSeparator(),
		status,
	)

	dlg := dialog.NewCustom("MangaDex Title Not Found", "Cancel", content, parentWindow)
	dlg.Resize(fyne.NewSize(640, 320))

	fetchButton.OnTapped = func() {
		raw := strings.TrimSpace(entry.Text)
		if raw == "" {
			status.SetText("Enter a MangaDex URL or title ID.")
			return
		}
		id, err := mangadex.ExtractID(raw)
		if err != nil {
			status.SetText(err.Error())
			return
		}
		fetchButton.Disable()
		status.SetText(fmt.Sprintf("Fetching MangaDex title %s...", id))
		go func() {
			client := mangadex.NewClient()
			fresh, err := client.GetManga(context.Background(), id)
			fyne.Do(func() {
				if err != nil || fresh == nil {
					fetchButton.Enable()
					status.SetText(fmt.Sprintf("Could not fetch MangaDex title %s: %v", id, err))
					return
				}
				if storeErr := storeFetchedInfo(fresh, bookmarkTitle); storeErr != nil {
					status.SetText(fmt.Sprintf("Could not store MangaDex info: %v", storeErr))
					fetchButton.Enable()
					return
				}
				dlg.Hide()
				replace(buildMangaInfoContent(parentWindow, replace, fresh, bookmarkTitle, lang))
			})
		}()
	}

	dlg.Show()
}

// stringInSlice reports whether s is present in the string slice.
func stringInSlice(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

// infoRow builds a single "<field>: <value>" row with a bold field label on the
// left and a selectable, wrapping value filling the remaining width.
func infoRow(field, value string) fyne.CanvasObject {
	if value == "" {
		value = "—"
	}
	valueLabel := makeSelectableLabel(value)
	return container.NewBorder(nil, nil, infoFieldLabel(field+":"), nil, valueLabel)
}

// buildURLRow shows the MangaDex page URL as a clickable hyperlink (the URL
// text itself opens the page in the default browser) plus a Copy button so the
// raw URL can still be copied to the clipboard.
func buildURLRow(rawURL string) fyne.CanvasObject {
	parsed, _ := url.Parse(rawURL)
	link := widget.NewHyperlink(rawURL, parsed)
	link.Truncation = fyne.TextTruncateEllipsis
	copyButton := widget.NewButton("Copy", func() {
		if fyne.CurrentApp() != nil && fyne.CurrentApp().Clipboard() != nil {
			fyne.CurrentApp().Clipboard().SetContent(rawURL)
		}
	})
	return container.NewBorder(nil, nil, infoFieldLabel("MangaDex:"), copyButton, link)
}

func intString(p *int) string {
	if p == nil {
		return ""
	}
	return fmt.Sprintf("%d", *p)
}

func prettifyStatus(status string) string {
	switch strings.ToLower(status) {
	case "ongoing":
		return "Ongoing"
	case "completed":
		return "Completed"
	case "hiatus":
		return "Hiatus"
	case "cancelled":
		return "Cancelled"
	}
	return capitalize(status)
}

func prettifyDemographic(dem string) string {
	switch strings.ToLower(dem) {
	case "shounen":
		return "Shounen"
	case "shoujo":
		return "Shoujo"
	case "seinen":
		return "Seinen"
	case "josei":
		return "Josei"
	case "kids":
		return "Kids"
	}
	return capitalize(dem)
}

func prettifyRating(rating string) string {
	switch strings.ToLower(rating) {
	case "safe":
		return "Safe"
	case "suggestive":
		return "Suggestive"
	case "erotica":
		return "Erotica"
	case "pornographic":
		return "Pornographic"
	}
	return capitalize(rating)
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// languageDisplayName maps an ISO 639-1 (or MangaDex) language code to a
// human-readable name for the language dropdown. Unknown codes fall back to the
// code itself.
func languageDisplayName(code string) string {
	if name, ok := knownLanguages[strings.ToLower(code)]; ok {
		return fmt.Sprintf("%s (%s)", name, code)
	}
	if code == "" {
		return "Unknown"
	}
	return code
}

var knownLanguages = map[string]string{
	"en": "English", "ja": "Japanese", "fr": "French", "es": "Spanish",
	"es-la": "Spanish (Latin American)", "pt": "Portuguese", "pt-br": "Portuguese (Brazilian)",
	"de": "German", "it": "Italian", "nl": "Dutch", "ru": "Russian",
	"zh": "Chinese", "zh-hk": "Chinese (Traditional)", "ko": "Korean",
	"id": "Indonesian", "th": "Thai", "vi": "Vietnamese", "ar": "Arabic",
	"tr": "Turkish", "pl": "Polish", "el": "Greek", "hi": "Hindi",
	"fil": "Filipino", "bg": "Bulgarian", "cs": "Czech", "da": "Danish",
	"fi": "Finnish", "hu": "Hungarian", "ro": "Romanian", "sv": "Swedish",
	"uk": "Ukrainian", "he": "Hebrew", "bn": "Bengali", "ms": "Malay",
	"no": "Norwegian", "az": "Azerbaijani", "fa": "Persian", "ka": "Georgian",
	"mk": "Macedonian", "sr": "Serbian", "sk": "Slovak", "sl": "Slovenian",
	"et": "Estonian", "lv": "Latvian", "lt": "Lithuanian", "hr": "Croatian",
	"is": "Icelandic", "cy": "Welsh", "ga": "Irish", "eu": "Basque",
	"gl": "Galician", "kk": "Kazakh", "ky": "Kyrgyz", "la": "Latin",
	"lb": "Luxembourgish", "mi": "Maori", "mn": "Mongolian", "ne": "Nepali",
	"pa": "Punjabi", "si": "Sinhala", "ta": "Tamil", "te": "Telugu",
	"tl": "Tagalog", "ur": "Urdu", "uz": "Uzbek", "yo": "Yoruba",
	"zu": "Zulu", "af": "Afrikaans", "sq": "Albanian", "am": "Amharic",
	"be": "Belarusian", "ca": "Catalan", "eo": "Esperanto", "fy": "Frisian",
	"sw": "Swahili", "hy": "Armenian", "ml": "Malayalam", "mr": "Marathi",
	"my": "Burmese", "km": "Khmer", "lo": "Lao", "si-li": "Sinhala",
	"ku": "Kurdish", "su": "Sundanese", "ceb": "Cebuano", "jv": "Javanese",
}
