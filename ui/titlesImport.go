package ui

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"
	"gopkg.in/yaml.v3"

	"kansho/mangadex"
	"kansho/models"
)

// titleImportEntry is one row of an existing-titles import file.
type titleImportEntry struct {
	Row        int
	Title      string
	Location   string
	MangaDex   string
	MangaDexID string
	Site       string
	URL        string
	Type       string
}

type titleImportResolution struct {
	Resolved   int
	NoMangaDex int
	Failed     int
	Skipped    int
	Problems   []string
}

// ShowImportExistingTitlesDialog imports existing on-disk titles from a CSV or
// YAML file into the current platform's bookmark table.
func ShowImportExistingTitlesDialog(_ fyne.App, window fyne.Window) {
	openDialog := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
		if err != nil {
			dialog.ShowError(fmt.Errorf("error opening file dialog: %v", err), window)
			return
		}
		if reader == nil {
			return
		}
		defer reader.Close()

		data, err := io.ReadAll(reader)
		if err != nil {
			dialog.ShowError(fmt.Errorf("failed to read import file: %v", err), window)
			return
		}

		entries, err := parseTitleImportEntries(reader.URI().Path(), data)
		if err != nil {
			dialog.ShowError(fmt.Errorf("could not read import file: %v", err), window)
			return
		}
		entries, problems := prepareTitleImportEntries(entries)
		if len(problems) > 0 {
			dialog.ShowError(fmt.Errorf("the import file has %d problem(s):\n\n%s",
				len(problems), strings.Join(problems, "\n")), window)
			return
		}

		dialog.ShowConfirm(
			"Import Existing Titles",
			fmt.Sprintf("Import %d title(s) into the %s bookmark table?\n\n"+
				"MangaDex links and IDs will be looked up after the titles are added.",
				len(entries), titleImportPlatformName()),
			func(confirmed bool) {
				if !confirmed {
					return
				}
				startTitleImport(window, entries)
			},
			window,
		)
	}, window)

	openDialog.SetFilter(storage.NewExtensionFileFilter([]string{".csv", ".yaml", ".yml"}))
	homePath, err := os.UserHomeDir()
	if err == nil {
		if homeDir, listerErr := storage.ListerForURI(storage.NewFileURI(homePath)); listerErr == nil {
			openDialog.SetLocation(homeDir)
		}
	}
	openDialog.Resize(fyne.NewSize(900, 700))
	openDialog.Show()
}

func titleImportPlatformName() string {
	if runtime.GOOS == "windows" {
		return "Windows"
	}
	return "Linux/Unix"
}

// parseTitleImportEntries reads a CSV or YAML import file. The file extension
// selects the format.
func parseTitleImportEntries(path string, data []byte) ([]titleImportEntry, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".csv":
		return parseTitleImportCSV(data)
	case ".yaml", ".yml":
		return parseTitleImportYAML(data)
	default:
		return nil, fmt.Errorf("unsupported import file type %q (use .csv, .yaml, or .yml)", filepath.Ext(path))
	}
}

type titleImportCSVColumns struct {
	title    int
	location int
	mangadex int
	kind     int
	site     int
	url      int
}

func parseTitleImportCSV(data []byte) ([]titleImportEntry, error) {
	reader := csv.NewReader(bytes.NewReader(data))
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse CSV: %w", err)
	}
	if len(records) == 0 {
		return nil, errors.New("CSV file is empty")
	}

	columns := importCSVColumns(records[0])
	if columns.title < 0 || columns.location < 0 || columns.kind < 0 {
		return nil, errors.New("CSV header must include title, location, and type columns")
	}

	entries := make([]titleImportEntry, 0, len(records)-1)
	for i := 1; i < len(records); i++ {
		record := records[i]
		if csvRecordIsBlank(record) {
			continue
		}
		entries = append(entries, titleImportEntry{
			Row:      i + 1,
			Title:    csvField(record, columns.title),
			Location: csvField(record, columns.location),
			MangaDex: csvField(record, columns.mangadex),
			Site:     csvField(record, columns.site),
			URL:      csvField(record, columns.url),
			Type:     csvField(record, columns.kind),
		})
	}
	if len(entries) == 0 {
		return nil, errors.New("CSV file contains no title rows")
	}
	return entries, nil
}

func importCSVColumns(header []string) titleImportCSVColumns {
	columns := titleImportCSVColumns{-1, -1, -1, -1, -1, -1}
	for i, name := range header {
		switch normalizeImportColumn(name) {
		case "title", "name", "manga", "manga_title", "title_name":
			if columns.title < 0 {
				columns.title = i
			}
		case "location", "path", "directory", "folder", "disk_location", "location_on_disk":
			if columns.location < 0 {
				columns.location = i
			}
		case "mangadex", "mangadex_id", "mangadex_url", "mangadex_link", "mangadex_link_or_id", "id":
			if columns.mangadex < 0 {
				columns.mangadex = i
			}
		case "type", "bookmark_type", "kind":
			if columns.kind < 0 {
				columns.kind = i
			}
		case "site", "source", "source_site":
			if columns.site < 0 {
				columns.site = i
			}
		case "url", "source_url", "web_url":
			if columns.url < 0 {
				columns.url = i
			}
		}
	}
	return columns
}

func normalizeImportColumn(name string) string {
	name = strings.TrimPrefix(strings.TrimSpace(name), "\ufeff")
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, "-", "_")
	return strings.Join(strings.Fields(name), "_")
}

func csvField(record []string, index int) string {
	if index < 0 || index >= len(record) {
		return ""
	}
	return strings.TrimSpace(record[index])
}

func csvRecordIsBlank(record []string) bool {
	for _, field := range record {
		if strings.TrimSpace(field) != "" {
			return false
		}
	}
	return true
}

type titleImportYAMLEntry struct {
	Title        string `yaml:"title"`
	Name         string `yaml:"name"`
	Location     string `yaml:"location"`
	Path         string `yaml:"path"`
	MangaDex     string `yaml:"mangadex"`
	MangaDexID   string `yaml:"mangadex_id"`
	MangaDexURL  string `yaml:"mangadex_url"`
	MangaDexLink string `yaml:"mangadex_link"`
	Type         string `yaml:"type"`
	BookmarkType string `yaml:"bookmark_type"`
	Site         string `yaml:"site"`
	URL          string `yaml:"url"`
}

func (entry titleImportYAMLEntry) importEntry(row int) titleImportEntry {
	return titleImportEntry{
		Row:      row,
		Title:    firstNonEmptyImportValue(entry.Title, entry.Name),
		Location: firstNonEmptyImportValue(entry.Location, entry.Path),
		MangaDex: firstNonEmptyImportValue(entry.MangaDex, entry.MangaDexID, entry.MangaDexURL, entry.MangaDexLink),
		Type:     firstNonEmptyImportValue(entry.Type, entry.BookmarkType),
		Site:     entry.Site,
		URL:      entry.URL,
	}
}

func parseTitleImportYAML(data []byte) ([]titleImportEntry, error) {
	var document struct {
		Titles []titleImportYAMLEntry `yaml:"titles"`
	}
	documentErr := yaml.Unmarshal(data, &document)
	if documentErr == nil && len(document.Titles) > 0 {
		entries := make([]titleImportEntry, 0, len(document.Titles))
		for i, entry := range document.Titles {
			entries = append(entries, entry.importEntry(i+1))
		}
		return entries, nil
	}

	var list []titleImportYAMLEntry
	listErr := yaml.Unmarshal(data, &list)
	if listErr == nil && len(list) > 0 {
		entries := make([]titleImportEntry, 0, len(list))
		for i, entry := range list {
			entries = append(entries, entry.importEntry(i+1))
		}
		return entries, nil
	}

	if documentErr != nil && listErr != nil {
		return nil, fmt.Errorf("parse YAML: %w", documentErr)
	}
	return nil, errors.New("YAML file must contain a titles: list or a top-level list of titles")
}

func firstNonEmptyImportValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// prepareTitleImportEntries validates and normalizes every imported row before
// anything is written to the database.
func prepareTitleImportEntries(entries []titleImportEntry) ([]titleImportEntry, []string) {
	prepared := make([]titleImportEntry, 0, len(entries))
	problems := make([]string, 0)
	seen := make(map[string]int, len(entries))

	for _, entry := range entries {
		entry.Title = strings.TrimSpace(entry.Title)
		entry.Location = strings.TrimSpace(entry.Location)
		entry.MangaDex = strings.TrimSpace(entry.MangaDex)
		entry.Site = strings.TrimSpace(entry.Site)
		entry.URL = strings.TrimSpace(entry.URL)
		entry.Type = strings.ToLower(strings.TrimSpace(entry.Type))

		label := fmt.Sprintf("row %d", entry.Row)
		if entry.Title != "" {
			label = fmt.Sprintf("row %d (%s)", entry.Row, entry.Title)
		}
		if entry.Title == "" {
			problems = append(problems, label+": title is required")
		}
		if entry.Location == "" {
			problems = append(problems, label+": location is required")
		}
		switch entry.Type {
		case "manga", "webtoon":
		case "":
			problems = append(problems, label+": type is required (manga or webtoon)")
		default:
			problems = append(problems, fmt.Sprintf("%s: unknown type %q (use manga or webtoon)", label, entry.Type))
		}
		if entry.MangaDex != "" {
			id, err := mangadex.ExtractID(entry.MangaDex)
			if err != nil {
				problems = append(problems, fmt.Sprintf("%s: invalid MangaDex link/ID: %v", label, err))
			} else {
				entry.MangaDexID = id
			}
		}
		if key := titleImportDuplicateKey(entry.Title); key != "" {
			if firstRow, exists := seen[key]; exists {
				problems = append(problems, fmt.Sprintf("%s: duplicates row %d (same title)", label, firstRow))
			} else {
				seen[key] = entry.Row
			}
		}
		prepared = append(prepared, entry)
	}

	return prepared, problems
}

func titleImportDuplicateKey(title string) string {
	return strings.ToLower(strings.Join(strings.Fields(title), " "))
}

func startTitleImport(window fyne.Window, entries []titleImportEntry) {
	store, err := mangadex.GetStore()
	if err != nil {
		dialog.ShowError(fmt.Errorf("cannot open Kansho database: %v", err), window)
		return
	}

	added, skippedDuplicates, err := addImportedTitles(store, entries)
	if err != nil {
		dialog.ShowError(fmt.Errorf("import failed: %v", err), window)
		return
	}

	withMangaDex := make([]titleImportEntry, 0, len(added))
	for _, entry := range added {
		if entry.MangaDexID != "" {
			withMangaDex = append(withMangaDex, entry)
		}
	}
	withoutMangaDex := len(added) - len(withMangaDex)
	if len(withMangaDex) == 0 {
		showTitleImportSummary(window, len(added), skippedDuplicates,
			titleImportResolution{NoMangaDex: withoutMangaDex})
		return
	}

	runImportedMangaDexLookups(window, withMangaDex, func(result titleImportResolution) {
		result.NoMangaDex = withoutMangaDex
		showTitleImportSummary(window, len(added), skippedDuplicates, result)
	})
}

func addImportedTitles(store *mangadex.Store, entries []titleImportEntry) ([]titleImportEntry, int, error) {
	current, err := store.LoadBookmarks()
	if err != nil {
		return nil, 0, fmt.Errorf("load current bookmarks: %w", err)
	}

	keys := make(map[string]bool, len(current.Manga)+len(entries))
	for i := range current.Manga {
		models.NormalizeBookmark(&current.Manga[i])
		if key := titleImportDuplicateKey(current.Manga[i].Title); key != "" {
			keys[key] = true
		}
	}

	added := make([]titleImportEntry, 0, len(entries))
	skipped := 0
	for _, entry := range entries {
		key := titleImportDuplicateKey(entry.Title)
		if keys[key] {
			skipped++
			continue
		}
		keys[key] = true
		current.Manga = append(current.Manga, models.Bookmarks{
			Title:      entry.Title,
			Url:        entry.URL,
			Location:   entry.Location,
			Site:       entry.Site,
			Type:       entry.Type,
			MangaDexID: entry.MangaDexID,
		})
		added = append(added, entry)
	}

	if len(added) == 0 {
		return nil, skipped, nil
	}
	if err := store.SaveBookmarks(current); err != nil {
		return nil, skipped, fmt.Errorf("save bookmarks: %w", err)
	}
	return added, skipped, nil
}

func runImportedMangaDexLookups(window fyne.Window, entries []titleImportEntry, done func(titleImportResolution)) {
	client := mangadex.NewClient()
	status := widget.NewLabel("")
	status.Wrapping = fyne.TextWrapWord
	progress := widget.NewProgressBar()
	progress.Max = float64(len(entries))

	progressDialog := dialog.NewCustomWithoutButtons(
		"MangaDex Metadata",
		container.NewVBox(status, progress),
		window,
	)
	progressDialog.Resize(fyne.NewSize(560, 180))
	progressDialog.Show()

	result := titleImportResolution{}
	index := 0
	var step func()
	step = func() {
		if index >= len(entries) {
			progressDialog.Hide()
			done(result)
			return
		}
		entry := entries[index]
		index++
		status.SetText(fmt.Sprintf("Looking up %q (%d/%d)...", entry.Title, index, len(entries)))

		go func(entry titleImportEntry) {
			info, err := client.GetManga(context.Background(), entry.MangaDexID)
			fyne.Do(func() {
				if err != nil || info == nil {
					result.Failed++
					problem := "MangaDex returned no data"
					if err != nil {
						problem = err.Error()
					}
					result.Problems = append(result.Problems,
						fmt.Sprintf("%s: MangaDex lookup failed: %s", entry.Title, problem))
					progress.SetValue(float64(index))
					step()
					return
				}
				storeImportedMangaDex(info, entry.Title, window, func(stored bool, problem string) {
					if stored {
						result.Resolved++
					} else {
						result.Skipped++
						if problem != "" {
							result.Problems = append(result.Problems, entry.Title+": "+problem)
						}
					}
					progress.SetValue(float64(index))
					step()
				})
			})
		}(entry)
	}
	step()
}

func storeImportedMangaDex(info *mangadex.MangaInfo, importTitle string, window fyne.Window, done func(bool, string)) {
	apply := func() {
		if err := storeFetchedInfo(info, importTitle); err != nil {
			done(false, fmt.Sprintf("metadata could not be stored: %v", err))
			return
		}
		done(true, "")
	}

	candidates := info.EnglishTitleCandidates()
	if len(candidates) == 0 {
		apply()
		return
	}
	if info.SelectedEnglishTitle != "" && info.PrimaryEnglishTitle() != "" {
		apply()
		return
	}
	if len(candidates) == 1 {
		info.SelectedEnglishTitle = candidates[0]
		apply()
		return
	}

	titleSelect := widget.NewSelect(candidates, nil)
	titleSelect.SetSelected(candidates[0])
	content := container.NewVBox(
		widget.NewLabel(fmt.Sprintf("Importing %q.\n\nChoose the English title to use for ComicInfo metadata.", importTitle)),
		titleSelect,
	)
	dlg := dialog.NewCustomConfirm("Choose English Manga Title", "Use Title", "Skip", content, func(confirm bool) {
		if !confirm {
			done(false, "English title selection skipped")
			return
		}
		info.SelectedEnglishTitle = titleSelect.Selected
		apply()
	}, window)
	dlg.Show()
}

func showTitleImportSummary(window fyne.Window, added, skippedDuplicates int, result titleImportResolution) {
	lines := []string{
		fmt.Sprintf("Titles imported: %d", added),
		fmt.Sprintf("Duplicate titles skipped: %d", skippedDuplicates),
		fmt.Sprintf("MangaDex metadata linked: %d", result.Resolved),
		fmt.Sprintf("No MangaDex link/ID: %d", result.NoMangaDex),
		fmt.Sprintf("MangaDex lookups failed: %d", result.Failed),
		fmt.Sprintf("MangaDex selections skipped: %d", result.Skipped),
	}
	if len(result.Problems) > 0 {
		lines = append(lines, "", "Details:")
		lines = append(lines, result.Problems...)
	}

	summary := widget.NewLabel(strings.Join(lines, "\n"))
	summary.Wrapping = fyne.TextWrapWord
	summary.Selectable = true

	var dlg *dialog.CustomDialog
	okayButton := widget.NewButton("OK", func() {
		dlg.Hide()
		reloadMainWindow(window)
	})
	dlg = dialog.NewCustom("Import Complete", "", container.NewScroll(summary), window)
	dlg.SetButtons([]fyne.CanvasObject{okayButton})
	dlg.Resize(fyne.NewSize(620, 420))
	dlg.Show()
}
