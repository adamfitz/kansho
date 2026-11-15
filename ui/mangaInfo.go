package ui

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"kansho/config"
	"kansho/mangadex"
	"kansho/metadata"
)

// DisplayMangaInfoFunc renders title metadata in the chapter list pane.
type DisplayMangaInfoFunc func(info *mangadex.MangaInfo, bookmarkTitle string)

// ShowMangaTitleInfo opens metadata for the selected bookmark. Manual metadata
// is checked before any MangaDex ID or title mapping.
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

	manual, err := store.LookupManualMetadata(metadata.MangaSourceKey(manga))
	if err != nil {
		dialog.ShowError(fmt.Errorf("cannot look up manual metadata: %v", err), window)
		return
	}
	if manual != nil {
		display(manual, manga.Title)
		return
	}

	// If the bookmark already carries a MangaDex ID, use it directly (no
	// search dialog needed). This also covers bulk imports that store the ID
	// before the metadata lookup runs.
	if id := strings.TrimSpace(manga.MangaDexID); id != "" {
		local, localErr := store.LookupByID(id)
		if localErr != nil {
			dialog.ShowError(fmt.Errorf("lookup %s: %v", id, localErr), window)
			return
		}
		if local != nil {
			displayWithEnglishTitle(local, manga.Title, window, display)
			return
		}
		fetchAndShow(window, id, manga.Title, display)
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
			displayWithEnglishTitle(local, manga.Title, window, display)
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
		displayWithEnglishTitle(local, manga.Title, window, display)
		return
	}

	showMangaLookupDialog(manga, window, display)
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
	if info.SelectedEnglishTitle == "" {
		existing, lookupErr := store.LookupByID(info.ID)
		if lookupErr != nil {
			return lookupErr
		}
		if existing != nil {
			for _, candidate := range info.EnglishTitleCandidates() {
				if candidate == existing.SelectedEnglishTitle {
					info.SelectedEnglishTitle = candidate
					break
				}
			}
		}
	}
	if err := store.Upsert(info); err != nil {
		return err
	}
	if strings.TrimSpace(bookmarkTitle) != "" {
		if aliasErr := store.SetTitleAlias(bookmarkTitle, info.ID); aliasErr != nil {
			log.Printf("[UI] Failed to alias %q -> %s: %v", bookmarkTitle, info.ID, aliasErr)
		}
		if linkErr := store.SetBookmarkMangaDexID(bookmarkTitle, info.ID); linkErr != nil {
			return fmt.Errorf("link bookmark to MangaDex ID: %w", linkErr)
		}
	}
	return nil
}

func chooseEnglishTitle(info *mangadex.MangaInfo, window fyne.Window, contextTitle string, chosen func()) {
	candidates := info.EnglishTitleCandidates()
	if len(candidates) == 0 {
		dialog.ShowError(fmt.Errorf("MangaDex has no English title for %q", info.EnglishTitle()), window)
		return
	}
	if info.SelectedEnglishTitle != "" && info.PrimaryEnglishTitle() != "" {
		chosen()
		return
	}
	if len(candidates) == 1 {
		info.SelectedEnglishTitle = candidates[0]
		chosen()
		return
	}

	prompt := "Choose the English title to use for ComicInfo metadata."
	if strings.TrimSpace(contextTitle) != "" {
		prompt = fmt.Sprintf("Importing %q. Choose the English title to use for ComicInfo metadata.", contextTitle)
	}
	titleSelect := widget.NewSelect(candidates, nil)
	titleSelect.SetSelected(candidates[0])
	content := container.NewVBox(
		widget.NewLabel(prompt),
		titleSelect,
	)
	dlg := dialog.NewCustomConfirm("Choose English Manga Title", "Use Title", "Cancel", content, func(confirm bool) {
		if !confirm {
			return
		}
		info.SelectedEnglishTitle = titleSelect.Selected
		chosen()
	}, window)
	dlg.Show()
}

func retainEnglishTitleChoice(current, fresh *mangadex.MangaInfo) {
	if current == nil || fresh == nil || current.SelectedEnglishTitle == "" || fresh.SelectedEnglishTitle != "" {
		return
	}
	for _, candidate := range fresh.EnglishTitleCandidates() {
		if candidate == current.SelectedEnglishTitle {
			fresh.SelectedEnglishTitle = candidate
			return
		}
	}
}

func displayWithEnglishTitle(info *mangadex.MangaInfo, bookmarkTitle string, window fyne.Window, display DisplayMangaInfoFunc) {
	chooseEnglishTitle(info, window, "", func() {
		if err := storeFetchedInfo(info, bookmarkTitle); err != nil {
			dialog.ShowError(fmt.Errorf("could not store MangaDex info for %s: %v", info.ID, err), window)
			return
		}
		display(info, bookmarkTitle)
	})
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
			displayWithEnglishTitle(info, bookmarkTitle, window, display)
		})
	}()
}

// showMangaLookupDialog opens the MangaDex title selection dialog. It shows a
// searchable, scrollable list of matching titles (50 at a time, loading more in
// the background as the user scrolls) plus a manual field for a MangaDex URL or
// title ID.
func showMangaLookupDialog(manga config.Bookmarks, window fyne.Window, display DisplayMangaInfoFunc) {
	d := newTitleLookupDialog(manga, window, display)
	d.show()
}

// ShowMangaTitleLookup opens the MangaDex picker even when the bookmark already
// has a MangaDex ID. onChanged receives a selected MangaDex ID after it has
// been persisted and before the information pane renders.
func ShowMangaTitleLookup(manga config.Bookmarks, window fyne.Window, display DisplayMangaInfoFunc, onChanged func(id string)) {
	if display == nil {
		display = func(*mangadex.MangaInfo, string) {}
	}
	wrapped := func(info *mangadex.MangaInfo, bookmarkTitle string) {
		if info != nil && info.ID != "" {
			store, err := mangadex.GetStore()
			if err != nil {
				dialog.ShowError(fmt.Errorf("cannot open Kansho database: %v", err), window)
				return
			}
			if err := store.DeleteManualMetadata(metadata.MangaSourceKey(manga)); err != nil {
				dialog.ShowError(fmt.Errorf("cannot switch to MangaDex metadata: %v", err), window)
				return
			}
		}
		if onChanged != nil && info != nil && info.ID != "" {
			onChanged(info.ID)
		}
		display(info, bookmarkTitle)
	}
	showMangaLookupDialog(manga, window, wrapped)
}

func buildManualMetadataForm(window fyne.Window, initial *mangadex.MangaInfo, scrollHeight float32, cancel func(), save func(*mangadex.MangaInfo) error) fyne.CanvasObject {
	info := &mangadex.MangaInfo{
		Title:       map[string]string{},
		Description: map[string]string{},
		AltTitles:   []map[string]string{},
		Tags:        []mangadex.TagInfo{},
	}
	if initial != nil {
		*info = *initial
		info.Title = cloneStringMap(initial.Title)
		info.Description = cloneStringMap(initial.Description)
		info.AltTitles = make([]map[string]string, 0, len(initial.AltTitles))
		for _, alt := range initial.AltTitles {
			info.AltTitles = append(info.AltTitles, cloneStringMap(alt))
		}
		info.Tags = make([]mangadex.TagInfo, 0, len(initial.Tags))
		for _, tag := range initial.Tags {
			tag.Name = cloneStringMap(tag.Name)
			info.Tags = append(info.Tags, tag)
		}
		if initial.Year != nil {
			year := *initial.Year
			info.Year = &year
		}
	}
	if info.Title == nil {
		info.Title = map[string]string{}
	}
	if info.Description == nil {
		info.Description = map[string]string{}
	}
	info.ID = ""
	info.SelectedEnglishTitle = ""

	languages := info.AvailableLanguages()
	if !stringInSlice(languages, "en") {
		languages = append(languages, "en")
	}
	currentLanguage := "en"
	if !stringInSlice(languages, currentLanguage) && len(languages) > 0 {
		currentLanguage = languages[0]
	}

	languageSelect := widget.NewSelect(languages, nil)
	languageSelect.SetSelected(currentLanguage)
	titleEntry := widget.NewEntry()
	titleEntry.SetPlaceHolder("Title")
	altTitlesEntry := widget.NewMultiLineEntry()
	altTitlesEntry.SetPlaceHolder("One alternative title per line")
	descriptionEntry := widget.NewMultiLineEntry()
	descriptionEntry.SetPlaceHolder("Synopsis")
	tagsEntry := widget.NewMultiLineEntry()
	tagsEntry.SetPlaceHolder("One tag per line")

	originalLanguageEntry := widget.NewEntry()
	statusEntry := widget.NewEntry()
	demographicEntry := widget.NewEntry()
	yearEntry := widget.NewEntry()
	contentRatingEntry := widget.NewEntry()
	authorEntry := widget.NewEntry()
	artistEntry := widget.NewEntry()
	coverEntry := widget.NewEntry()
	urlEntry := widget.NewEntry()

	loadLanguage := func(lang string) {
		titleEntry.SetText(info.Title[lang])
		var altTitles []string
		for _, alt := range info.AltTitles {
			if title := strings.TrimSpace(alt[lang]); title != "" {
				altTitles = append(altTitles, title)
			}
		}
		altTitlesEntry.SetText(strings.Join(altTitles, "\n"))
		descriptionEntry.SetText(info.Description[lang])
		var tags []string
		for _, tag := range info.Tags {
			if name := strings.TrimSpace(tag.Name[lang]); name != "" {
				tags = append(tags, name)
			}
		}
		tagsEntry.SetText(strings.Join(tags, "\n"))
	}

	saveLanguage := func(lang string) {
		setLocalizedValue(info.Title, lang, titleEntry.Text)
		setLocalizedValue(info.Description, lang, descriptionEntry.Text)

		altTitles := stringsToLines(altTitlesEntry.Text)
		keptAltTitles := make([]map[string]string, 0, len(info.AltTitles)+len(altTitles))
		for _, alt := range info.AltTitles {
			delete(alt, lang)
			if len(alt) > 0 {
				keptAltTitles = append(keptAltTitles, alt)
			}
		}
		for _, title := range altTitles {
			keptAltTitles = append(keptAltTitles, map[string]string{lang: title})
		}
		info.AltTitles = keptAltTitles

		tagNames := stringsToLines(tagsEntry.Text)
		keptTags := make([]mangadex.TagInfo, 0, len(info.Tags)+len(tagNames))
		nextName := 0
		for _, tag := range info.Tags {
			delete(tag.Name, lang)
			if nextName < len(tagNames) {
				if tag.Name == nil {
					tag.Name = map[string]string{}
				}
				tag.Name[lang] = tagNames[nextName]
				nextName++
			}
			if len(tag.Name) > 0 {
				keptTags = append(keptTags, tag)
			}
		}
		for ; nextName < len(tagNames); nextName++ {
			keptTags = append(keptTags, mangadex.TagInfo{
				Name: map[string]string{lang: tagNames[nextName]},
			})
		}
		info.Tags = keptTags
	}

	loadLanguage(currentLanguage)
	languageSelect.OnChanged = func(lang string) {
		if lang == "" || lang == currentLanguage {
			return
		}
		saveLanguage(currentLanguage)
		currentLanguage = lang
		loadLanguage(currentLanguage)
	}

	addLanguageButton := widget.NewButton("Add Language", func() {
		codeEntry := widget.NewEntry()
		codeEntry.SetPlaceHolder("Language code, for example en or ja")
		addDialog := dialog.NewCustomConfirm("Add Metadata Language", "Add", "Cancel",
			container.NewVBox(widget.NewLabel("Enter a language code."), codeEntry),
			func(confirm bool) {
				if !confirm {
					return
				}
				code := strings.TrimSpace(codeEntry.Text)
				if code == "" {
					dialog.ShowError(fmt.Errorf("enter a language code"), window)
					return
				}
				saveLanguage(currentLanguage)
				if !stringInSlice(languages, code) {
					languages = append(languages, code)
					languageSelect.Options = languages
					languageSelect.Refresh()
				}
				languageSelect.SetSelected(code)
			}, window)
		addDialog.Show()
	})

	if info.OriginalLanguage != "" {
		originalLanguageEntry.SetText(info.OriginalLanguage)
	}
	statusEntry.SetText(info.Status)
	demographicEntry.SetText(info.PublicationDemographic)
	if info.Year != nil {
		yearEntry.SetText(strconv.Itoa(*info.Year))
	}
	contentRatingEntry.SetText(info.ContentRating)
	authorEntry.SetText(info.Author)
	artistEntry.SetText(info.Artist)
	coverEntry.SetText(info.CoverURL)
	urlEntry.SetText(info.URL)

	field := func(label string, object fyne.CanvasObject) fyne.CanvasObject {
		return container.NewVBox(widget.NewLabel(label), object)
	}
	editorFields := container.NewVBox(
		field("Language", container.NewBorder(nil, nil, nil, addLanguageButton, languageSelect)),
		field("Title", titleEntry),
		field("Alternative Titles", altTitlesEntry),
		field("Synopsis", descriptionEntry),
		field("Original Language", originalLanguageEntry),
		field("Status", statusEntry),
		field("Demographic", demographicEntry),
		field("Year", yearEntry),
		field("Author(s)", authorEntry),
		field("Artist(s)", artistEntry),
		field("Content Rating", contentRatingEntry),
		field("Tags", tagsEntry),
		field("Cover URL", coverEntry),
		field("URL", urlEntry),
	)
	editorScroll := container.NewVScroll(editorFields)
	editorScroll.SetMinSize(fyne.NewSize(680, scrollHeight))
	statusLabel := widget.NewLabel("")
	statusLabel.Wrapping = fyne.TextWrapWord
	cancelButton := widget.NewButton("Cancel", cancel)
	saveButton := widget.NewButton("Save", func() {
		saveLanguage(currentLanguage)
		info.OriginalLanguage = strings.TrimSpace(originalLanguageEntry.Text)
		info.Status = strings.TrimSpace(statusEntry.Text)
		info.PublicationDemographic = strings.TrimSpace(demographicEntry.Text)
		info.ContentRating = strings.TrimSpace(contentRatingEntry.Text)
		info.Author = strings.TrimSpace(authorEntry.Text)
		info.Artist = strings.TrimSpace(artistEntry.Text)
		info.CoverURL = strings.TrimSpace(coverEntry.Text)
		info.URL = strings.TrimSpace(urlEntry.Text)
		info.Year = nil
		if rawYear := strings.TrimSpace(yearEntry.Text); rawYear != "" {
			year, err := strconv.Atoi(rawYear)
			if err != nil {
				statusLabel.SetText(fmt.Sprintf("Year must be a whole number: %v", err))
				return
			}
			info.Year = &year
		}
		if err := save(info); err != nil {
			statusLabel.SetText(fmt.Sprintf("Could not save manual metadata: %v", err))
		}
	})

	return container.NewBorder(nil, container.NewVBox(statusLabel, container.NewHBox(cancelButton, saveButton)),
		nil, nil, editorScroll)
}

func setLocalizedValue(values map[string]string, lang, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		delete(values, lang)
		return
	}
	values[lang] = value
}

func cloneStringMap(values map[string]string) map[string]string {
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func stringsToLines(value string) []string {
	lines := strings.Split(value, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
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
// English is the default language. MangaDex records are re-queried when the
// language changes; manual records redraw from their local values.
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

	// Green tick shown once every local CBZ for this title has passed Check.
	checkIcon := widget.NewIcon(greenTickResource)
	checkIcon.Hide()

	notifyLabel := widget.NewLabel("")
	notifyLabel.Wrapping = fyne.TextWrapWord

	// The scrollable field area; swapped in place when the language changes.
	fieldScroll := container.NewVScroll(container.NewVBox())

	rebuildFields := func(lang string) {
		rows := []fyne.CanvasObject{
			infoRow("Metadata Source", metadataSourceLabel(info)),
		}
		if info.ID != "" {
			rows = append(rows, infoRow("MangaDex ID", info.ID))
		}
		rows = append(rows,
			infoRow("Original Language", languageDisplayName(info.OriginalLanguage)),
			infoRow("Status", prettifyStatus(info.Status)),
			infoRow("Demographic", prettifyDemographic(info.PublicationDemographic)),
			infoRow("Year", intString(info.Year)),
			infoRow("Author(s)", info.Author),
			infoRow("Artist(s)", info.Artist),
			infoRow("Content Rating", prettifyRating(info.ContentRating)),
			infoRow("Tags", strings.Join(info.TagList(lang), ", ")),
			infoRow("Alt Titles", strings.Join(info.AltTitleList(), "\n")),
			buildURLRow(metadataURLLabel(info), info.URL),
			NewSeparator(),
			infoRow("Description", strings.TrimSpace(info.DescriptionIn(lang))),
		)
		box := container.NewVBox(rows...)
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
		if info.ID == "" {
			notifyLabel.SetText(fmt.Sprintf("Showing %s for this title.", display))
			return
		}
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
				retainEnglishTitleChoice(info, fresh)
				chooseEnglishTitle(fresh, parentWindow, "", func() {
					if storeErr := storeFetchedInfo(fresh, bookmarkTitle); storeErr != nil {
						notifyLabel.SetText(fmt.Sprintf("%s — fetched but not stored (%v).", display, storeErr))
					}
					info = fresh
					rebuildFields(fetchLang)
					notifyLabel.SetText(fmt.Sprintf("Showing %s for this title.", display))
				})
			})
		}()
	})

	// Default the dropdown to the current language.
	langSelect.SetSelected(languageDisplayName(currentLang))

	// Refresh re-queries MangaDex for the current title. If the query returns
	// nothing the user is prompted for a MangaDex URL or title ID instead.
	refreshButton := widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), nil)
	if info.ID == "" {
		refreshButton.Hide()
	}
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
				retainEnglishTitleChoice(info, fresh)
				chooseEnglishTitle(fresh, parentWindow, "", func() {
					if storeErr := storeFetchedInfo(fresh, bookmarkTitle); storeErr != nil {
						log.Printf("[UI] Refresh of %s not stored: %v", id, storeErr)
					}
					replace(buildMangaInfoContent(parentWindow, replace, fresh, bookmarkTitle, langAtTap))
				})
			})
		}()
	}

	// Find matching bookmark for this title
	var matchedBookmark *config.Bookmarks
	all := config.Manga{Manga: []config.Bookmarks{}}
	if store, storeErr := mangadex.GetStore(); storeErr != nil {
		log.Printf("[UI] cannot open Kansho database: %v", storeErr)
	} else if loaded, loadErr := store.LoadBookmarks(); loadErr != nil {
		log.Printf("[UI] cannot load bookmarks: %v", loadErr)
	} else {
		all = loaded
	}
	for i := range all.Manga {
		m := all.Manga[i]
		if strings.EqualFold(m.Title, bookmarkTitle) {
			mm := m
			matchedBookmark = &mm
			break
		}
	}
	if matchedBookmark == nil {
		for i := range all.Manga {
			m := all.Manga[i]
			if strings.Contains(strings.ToLower(m.Title), strings.ToLower(bookmarkTitle)) || strings.Contains(strings.ToLower(bookmarkTitle), strings.ToLower(m.Title)) {
				mm := m
				matchedBookmark = &mm
				break
			}
		}
	}

	manualUpdateButton := widget.NewButton("Manual Update", nil)
	manualUpdateButton.OnTapped = func() {
		if matchedBookmark == nil {
			dialog.ShowError(fmt.Errorf("cannot find the bookmark for manual metadata"), parentWindow)
			return
		}
		var initial *mangadex.MangaInfo
		if info.ID == "" {
			initial = info
		}
		var editor *dialog.CustomDialog
		form := buildManualMetadataForm(parentWindow, initial, 420, func() {
			editor.Hide()
		}, func(updated *mangadex.MangaInfo) error {
			store, err := mangadex.GetStore()
			if err != nil {
				return err
			}
			if err := store.UpsertManualMetadata(metadata.MangaSourceKey(*matchedBookmark), updated); err != nil {
				return err
			}
			editor.Hide()
			replace(buildMangaInfoContent(parentWindow, replace, updated, bookmarkTitle, currentLang))
			return nil
		})
		editor = dialog.NewCustom("Manual Metadata", "Close", form, parentWindow)
		editor.Resize(fyne.NewSize(760, 620))
		editor.Show()
	}

	updateCheckIcon := func() {
		show := false
		if matchedBookmark != nil && strings.TrimSpace(matchedBookmark.Location) != "" {
			if store, err := mangadex.GetStore(); err == nil {
				if stats, statsErr := store.LookupChapterStats(mangaSourceKey(matchedBookmark)); statsErr == nil && stats != nil {
					show = metadata.DirectoryFullyChecked(matchedBookmark.Location, stats.LastCheckedChapter, stats.LastCheckedVolume)
				}
			}
		}
		if show {
			checkIcon.Show()
		} else {
			checkIcon.Hide()
		}
		checkIcon.Refresh()
	}
	updateCheckIcon()

	repairButton := widget.NewButton(metadataActionRepair, nil)
	checkButton := widget.NewButton(metadataActionCheck, nil)
	if matchedBookmark == nil || strings.TrimSpace(matchedBookmark.Location) == "" {
		repairButton.Disable()
		checkButton.Disable()
	}
	repairButton.OnTapped = func() {
		if matchedBookmark == nil {
			return
		}
		repairButton.Disable()
		checkButton.Disable()
		notifyLabel.SetText("Queueing metadata repair for all files...")
		bookmark := *matchedBookmark
		infoAtTap := info
		go func() {
			store, err := mangadex.GetStore()
			if err != nil {
				fyne.Do(func() {
					repairButton.Enable()
					checkButton.Enable()
					notifyLabel.SetText(fmt.Sprintf("Cannot open the database: %v", err))
				})
				return
			}
			files, err := metadata.ListChapterFiles(bookmark.Location)
			if err != nil || len(files) == 0 {
				fyne.Do(func() {
					repairButton.Enable()
					checkButton.Enable()
					notifyLabel.SetText("No CBZ files found in directory.")
				})
				return
			}
			submitMetadataRun(metadataActionRepair, store, infoAtTap, &bookmark, files, func(summary metadataRunSummary) {
				fyne.Do(func() {
					repairButton.Enable()
					checkButton.Enable()
					updateCheckIcon()
					notifyLabel.SetText(summary.message())
				})
			})
		}()
	}
	checkButton.OnTapped = func() {
		if matchedBookmark == nil {
			return
		}
		repairButton.Disable()
		checkButton.Disable()
		notifyLabel.SetText("Queueing quick metadata check...")
		bookmark := *matchedBookmark
		infoAtTap := info
		go func() {
			store, err := mangadex.GetStore()
			if err != nil {
				fyne.Do(func() {
					repairButton.Enable()
					checkButton.Enable()
					notifyLabel.SetText(fmt.Sprintf("Cannot open the database: %v", err))
				})
				return
			}
			cursor := 0.0
			volumeCursor := 0.0
			if stats, statsErr := store.LookupChapterStats(mangaSourceKey(&bookmark)); statsErr == nil && stats != nil {
				cursor = stats.LastCheckedChapter
				volumeCursor = stats.LastCheckedVolume
			}
			files, err := uncheckedChapterFiles(bookmark.Location, cursor, volumeCursor)
			if err != nil {
				fyne.Do(func() {
					repairButton.Enable()
					checkButton.Enable()
					notifyLabel.SetText(fmt.Sprintf("Cannot list chapters: %v", err))
				})
				return
			}
			if len(files) == 0 {
				fyne.Do(func() {
					repairButton.Enable()
					checkButton.Enable()
					updateCheckIcon()
					notifyLabel.SetText("All chapters are already checked.")
				})
				return
			}
			submitMetadataRun(metadataActionCheck, store, infoAtTap, &bookmark, files, func(summary metadataRunSummary) {
				fyne.Do(func() {
					repairButton.Enable()
					checkButton.Enable()
					updateCheckIcon()
					notifyLabel.SetText(summary.message())
				})
			})
		}()
	}

	titleRow := container.NewBorder(nil, nil, nil, checkIcon, titleLabel)
	titleBlock := container.NewVBox(
		titleRow,
		container.NewHBox(infoFieldLabel("Language:"), langSelect, refreshButton, checkButton, repairButton, manualUpdateButton),
	)
	statusRow := container.NewBorder(nil, nil, nil, nil, notifyLabel)
	header := container.NewVBox(titleBlock, statusRow, NewSeparator())

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

// buildURLRow shows a clickable URL and a Copy button.
func metadataSourceLabel(info *mangadex.MangaInfo) string {
	if info.ID == "" {
		return "Manual"
	}
	return "MangaDex"
}

func metadataURLLabel(info *mangadex.MangaInfo) string {
	if info.ID == "" {
		return "URL"
	}
	return "MangaDex"
}

func buildURLRow(label, rawURL string) fyne.CanvasObject {
	parsed, _ := url.Parse(rawURL)
	link := widget.NewHyperlink(rawURL, parsed)
	link.Truncation = fyne.TextTruncateEllipsis
	copyButton := widget.NewButton("Copy", func() {
		if fyne.CurrentApp() != nil && fyne.CurrentApp().Clipboard() != nil {
			fyne.CurrentApp().Clipboard().SetContent(rawURL)
		}
	})
	return container.NewBorder(nil, nil, infoFieldLabel(label+":"), copyButton, link)
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
