package ui

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"

	"kansho/mangadex"
	"kansho/metadata"
)

const allCoversOption = "All covers"

// ShowExportCoversDialog lets the user export one cached cover or all cached
// covers to a directory of their choice.
func ShowExportCoversDialog(_ fyne.App, window fyne.Window) {
	store, err := mangadex.GetStore()
	if err != nil {
		dialog.ShowError(fmt.Errorf("cannot open Kansho database: %v", err), window)
		return
	}
	covers, err := store.ListCoverCache()
	if err != nil {
		dialog.ShowError(fmt.Errorf("could not list cached covers: %v", err), window)
		return
	}
	if len(covers) == 0 {
		dialog.ShowInformation("Export Covers", "There are no MangaDex titles available to export.", window)
		return
	}
	client := mangadex.NewClient()

	options := []string{fmt.Sprintf("%s (%d saved titles)", allCoversOption, len(covers))}
	coverByOption := make(map[string]mangadex.CachedCover, len(covers))
	for _, cover := range covers {
		option := fmt.Sprintf("%s (%s)", cover.Title, cover.MangaID)
		options = append(options, option)
		coverByOption[option] = cover
	}
	allOption := options[0]
	selectedOption := allOption
	var destination string

	if homePath, err := os.UserHomeDir(); err == nil {
		destination = homePath
	}
	folderLabel := widget.NewLabel(destination)
	folderLabel.Wrapping = fyne.TextTruncate
	preview := container.NewMax(widget.NewLabel("All available covers will be exported."))

	coverSelect := widget.NewSelect(options, func(option string) {
		selectedOption = option
		preview.RemoveAll()
		if option == allOption {
			preview.Add(widget.NewLabel("All available covers will be exported."))
			return
		}
		cover, ok := coverByOption[option]
		if !ok {
			preview.Add(widget.NewLabel("Cover preview unavailable."))
			return
		}
		preview.Add(widget.NewLabel("Loading cover preview..."))
		go func() {
			resolved, err := resolveExportCover(store, client, cover)
			fyne.Do(func() {
				if selectedOption != option {
					return
				}
				preview.RemoveAll()
				if err != nil || len(resolved.JPEG) == 0 {
					preview.Add(widget.NewLabel("Could not load cover preview."))
					return
				}
				image := canvas.NewImageFromReader(bytes.NewReader(resolved.JPEG), resolved.MangaID+".jpg")
				image.FillMode = canvas.ImageFillContain
				image.SetMinSize(fyne.NewSize(320, 400))
				preview.Add(image)
			})
		}()
	})
	coverSelect.SetSelected(allOption)

	folderButton := widget.NewButton("Choose Folder...", func() {
		folderDialog := dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil {
				dialog.ShowError(fmt.Errorf("could not choose export folder: %v", err), window)
				return
			}
			if uri == nil {
				return
			}
			destination = uri.Path()
			folderLabel.SetText(destination)
		}, window)
		if homePath, err := os.UserHomeDir(); err == nil {
			if homeDir, err := storage.ListerForURI(storage.NewFileURI(homePath)); err == nil {
				folderDialog.SetLocation(homeDir)
			}
		}
		folderDialog.Resize(fyne.NewSize(900, 700))
		folderDialog.Show()
	})

	selection := container.NewVBox(
		widget.NewLabel("Cover to export:"),
		coverSelect,
		widget.NewLabel("Destination folder:"),
		container.NewBorder(nil, nil, folderButton, nil, folderLabel),
	)
	content := container.NewHBox(selection, widget.NewSeparator(), preview)

	var exportDialog dialog.Dialog
	exportDialog = dialog.NewCustomConfirm("Export Covers", "Export", "Cancel", content, func(confirm bool) {
		if !confirm {
			return
		}
		selectedID := ""
		if selectedOption != allOption {
			selectedID = coverByOption[selectedOption].MangaID
		}
		progress := dialog.NewProgressInfinite("Export Covers", "Downloading and exporting covers...", window)
		progress.Show()
		go func() {
			selectedCovers := make([]mangadex.CachedCover, 0, len(covers))
			failed := make([]string, 0)
			for _, cover := range covers {
				if selectedID != "" && cover.MangaID != selectedID {
					continue
				}
				resolved, fetchErr := resolveExportCover(store, client, cover)
				if fetchErr != nil {
					failed = append(failed, cover.Title)
					continue
				}
				selectedCovers = append(selectedCovers, resolved)
			}
			count := 0
			var exportErr error
			if len(selectedCovers) == 0 {
				exportErr = fmt.Errorf("no covers could be downloaded")
			} else {
				count, exportErr = exportCachedCovers(selectedCovers, "", destination)
			}
			fyne.Do(func() {
				progress.Hide()
				if exportErr != nil {
					message := fmt.Sprintf("cover export failed after %d file(s): %v", count, exportErr)
					if len(failed) > 0 {
						message += ". Could not download: " + strings.Join(failed, ", ")
					}
					dialog.ShowError(fmt.Errorf("%s", message), window)
					return
				}
				message := fmt.Sprintf("Exported %d cover(s) to %s", count, destination)
				if len(failed) > 0 {
					message += fmt.Sprintf(". Could not download %d cover(s): %s", len(failed), strings.Join(failed, ", "))
				}
				dialog.ShowInformation("Export Complete", message, window)
			})
		}()
	}, window)
	exportDialog.Resize(fyne.NewSize(900, 560))
	exportDialog.Show()
}

func resolveExportCover(store *mangadex.Store, client *mangadex.Client, cover mangadex.CachedCover) (mangadex.CachedCover, error) {
	if len(cover.JPEG) > 0 {
		return cover, nil
	}
	cached, err := store.GetCoverCache(cover.MangaID)
	if err != nil {
		return cover, fmt.Errorf("read cached cover: %w", err)
	}
	if len(cached) > 0 {
		cover.JPEG = cached
		return cover, nil
	}

	info := &mangadex.MangaInfo{ID: cover.MangaID, CoverURL: cover.CoverURL}
	if info.CoverURL == "" {
		stored, err := store.LookupByID(cover.MangaID)
		if err != nil {
			return cover, fmt.Errorf("read saved MangaDex title: %w", err)
		}
		if stored != nil && stored.CoverURL != "" {
			info = stored
			cover.CoverURL = stored.CoverURL
			if title := stored.PrimaryEnglishTitle(); title != "" {
				cover.Title = title
			}
		}
	}
	if info.CoverURL == "" {
		fresh, err := client.GetManga(context.Background(), cover.MangaID)
		if err != nil {
			return cover, fmt.Errorf("refresh MangaDex title: %w", err)
		}
		if err := store.Upsert(fresh); err != nil {
			return cover, fmt.Errorf("save refreshed MangaDex title: %w", err)
		}
		info = fresh
		cover.CoverURL = fresh.CoverURL
		if title := fresh.PrimaryEnglishTitle(); title != "" {
			cover.Title = title
		}
	}
	data, err := metadata.GetCoverBytes(store, info)
	if err != nil {
		return cover, fmt.Errorf("download cover: %w", err)
	}
	if len(data) == 0 {
		return cover, fmt.Errorf("MangaDex returned no cover image")
	}
	cover.JPEG = data
	return cover, nil
}

func exportCachedCovers(covers []mangadex.CachedCover, selectedID, destination string) (int, error) {
	info, err := os.Stat(destination)
	if err != nil {
		return 0, fmt.Errorf("read destination folder: %w", err)
	}
	if !info.IsDir() {
		return 0, fmt.Errorf("destination is not a folder")
	}

	exported := 0
	for _, cover := range covers {
		if selectedID != "" && cover.MangaID != selectedID {
			continue
		}
		fileName := strings.Map(func(char rune) rune {
			if unicode.IsLetter(char) || unicode.IsDigit(char) || char == ' ' || char == '-' || char == '_' || char == '.' || char == '(' || char == ')' {
				return char
			}
			return '_'
		}, cover.Title)
		fileName = strings.Trim(fileName, " ._-")
		if fileName == "" {
			fileName = cover.MangaID
		}
		if err := os.WriteFile(filepath.Join(destination, fileName+".jpg"), cover.JPEG, 0644); err != nil {
			return exported, fmt.Errorf("write cover %s: %w", cover.MangaID, err)
		}
		exported++
	}
	if exported == 0 {
		return 0, fmt.Errorf("no matching cached covers found")
	}
	return exported, nil
}
