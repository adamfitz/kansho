package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"

	"kansho/mangadex"
	"kansho/models"
	"kansho/parser"
)

// defaultDBPath returns the current Kansho database path.
func defaultDBPath() string {
	configDir, err := parser.ExpandPath("~/.config/kansho")
	if err != nil {
		return filepath.Join("kansho", "kansho.db")
	}
	return filepath.Join(configDir, "kansho.db")
}

// ShowDatabaseBackupDialog writes a VACUUM snapshot of the local Kansho database to a user-chosen file.
func ShowDatabaseBackupDialog(_ fyne.App, window fyne.Window) {
	store, err := mangadex.GetStore()
	if err != nil {
		dialog.ShowError(fmt.Errorf("cannot open Kansho database: %v", err), window)
		return
	}

	saveDialog := dialog.NewFileSave(func(writer fyne.URIWriteCloser, err error) {
		if err != nil {
			dialog.ShowError(fmt.Errorf("error opening save dialog: %v", err), window)
			return
		}
		if writer == nil {
			return // user cancelled
		}
		defer writer.Close()
		dest := writer.URI().Path()
		if err := store.Backup(dest); err != nil {
			dialog.ShowError(fmt.Errorf("backup failed: %v", err), window)
			return
		}
		dialog.ShowInformation("Backup Complete",
			fmt.Sprintf("Kansho database backed up to %s", dest), window)
	}, window)

	saveDialog.SetFileName("kansho-backup.db")
	homePath, err := os.UserHomeDir()
	if err == nil {
		if homeDir, listerErr := storage.ListerForURI(storage.NewFileURI(homePath)); listerErr == nil {
			saveDialog.SetLocation(homeDir)
		}
	}
	saveDialog.Resize(fyne.NewSize(900, 700))
	saveDialog.Show()
}

type platformBookmarkGroup struct {
	key     string
	title   string
	id      string
	linux   []models.Bookmarks
	windows []models.Bookmarks
}

// ShowSyncBookmarksView compares platform bookmark tables and lets the user sync them.
func ShowSyncBookmarksView(parent fyne.Window) {
	store, err := mangadex.GetStore()
	if err != nil {
		dialog.ShowError(fmt.Errorf("cannot open Kansho database: %v", err), parent)
		return
	}

	originalContent := parent.Content()

	var groups []platformBookmarkGroup
	selectedKey := ""
	windowsDetails := widget.NewLabel("Select a bookmark to inspect both tables.")
	windowsDetails.Wrapping = fyne.TextWrapWord
	linuxDetails := widget.NewLabel("Select a bookmark to inspect both tables.")
	linuxDetails.Wrapping = fyne.TextWrapWord
	status := widget.NewLabel("")
	status.Wrapping = fyne.TextWrapWord
	statusScroll := container.NewVScroll(status)

	platformList := widget.NewList(
		func() int { return len(groups) },
		func() fyne.CanvasObject {
			label := widget.NewLabel("template")
			label.Truncation = fyne.TextTruncateEllipsis
			return label
		},
		func(id widget.ListItemID, item fyne.CanvasObject) {
			if id < 0 || int(id) >= len(groups) {
				return
			}
			item.(*widget.Label).SetText(platformBookmarkGroupLabel(groups[id]))
		},
	)
	platformList.OnSelected = func(id widget.ListItemID) {
		if id < 0 || int(id) >= len(groups) {
			return
		}
		group := groups[id]
		selectedKey = group.key
		windowsDetails.SetText(formatPlatformBookmarkDetails("Windows", group.windows))
		linuxDetails.SetText(formatPlatformBookmarkDetails("Linux", group.linux))
	}

	windowsPane := container.NewBorder(
		container.NewVBox(widget.NewLabel("Windows"), widget.NewSeparator()),
		nil, nil, nil, container.NewScroll(windowsDetails),
	)
	linuxPane := container.NewBorder(
		container.NewVBox(widget.NewLabel("Linux"), widget.NewSeparator()),
		nil, nil, nil, container.NewScroll(linuxDetails),
	)
	details := container.NewVSplit(windowsPane, linuxPane)
	details.SetOffset(0.5)
	comparison := container.NewHSplit(container.NewScroll(platformList), details)
	comparison.SetOffset(0.28)

	loadGroups := func(preserveKey string) error {
		windows, err := store.LoadPlatformBookmarks("windows")
		if err != nil {
			return fmt.Errorf("load Windows bookmarks: %w", err)
		}
		linux, err := store.LoadPlatformBookmarks("linux")
		if err != nil {
			return fmt.Errorf("load Linux bookmarks: %w", err)
		}
		groups = buildPlatformBookmarkGroups(linux.Manga, windows.Manga)
		platformList.Refresh()
		if len(groups) == 0 {
			selectedKey = ""
			windowsDetails.SetText("No Windows bookmarks.")
			linuxDetails.SetText("No Linux bookmarks.")
			return nil
		}
		selectedIndex := 0
		for i := range groups {
			if groups[i].key == preserveKey {
				selectedIndex = i
				break
			}
		}
		platformList.Select(selectedIndex)
		return nil
	}

	refreshButton := widget.NewButton("Refresh", func() {
		if err := loadGroups(selectedKey); err != nil {
			status.SetText(err.Error())
		}
	})
	backButton := widget.NewButton("Back to Library", func() {
		parent.SetContent(originalContent)
		parent.RequestFocus()
	})
	syncButton := widget.NewButton("Sync by MangaDex ID", func() {
		result, err := store.SyncPlatformBookmarks()
		if err != nil {
			status.SetText(fmt.Sprintf("Bookmark sync failed: %v", err))
			return
		}
		var summary strings.Builder
		fmt.Fprintf(&summary, "Added to Linux: %d    Added to Windows: %d",
			result.InsertedToLinux, result.InsertedToWindows)
		if len(result.Skipped) == 0 {
			summary.WriteString("\nNo bookmarks skipped.")
		} else {
			fmt.Fprintf(&summary, "\nSkipped %d bookmark(s) without MangaDex IDs. Add MangaDex information, then sync again:", len(result.Skipped))
			for _, skipped := range result.Skipped {
				fmt.Fprintf(&summary, "\n%s: %s", skipped.Platform, skipped.Title)
			}
		}
		status.SetText(summary.String())
		if err := loadGroups(selectedKey); err != nil {
			status.SetText(fmt.Sprintf("%s\nRefresh failed: %v", summary.String(), err))
		}
	})
	content := container.NewBorder(container.NewBorder(nil, nil, nil, backButton,
		widget.NewLabel("Select an entry. Rows with IDs group by MangaDex ID; rows without IDs remain separate.")),
		container.NewVBox(container.NewHBox(refreshButton, syncButton), statusScroll),
		nil, nil, comparison)
	if err := loadGroups(""); err != nil {
		status.SetText(err.Error())
	}
	parent.SetContent(content)
}

func buildPlatformBookmarkGroups(linux, windows []models.Bookmarks) []platformBookmarkGroup {
	groups := make([]platformBookmarkGroup, 0, len(linux)+len(windows))
	indices := make(map[string]int, len(linux)+len(windows))
	appendBookmarks := func(platform string, bookmarks []models.Bookmarks) {
		for index, bookmark := range bookmarks {
			id := strings.TrimSpace(bookmark.MangaDexID)
			key := "id:" + id
			if id == "" {
				key = fmt.Sprintf("%s:no-id:%d", platform, index)
			}
			groupIndex, exists := indices[key]
			if !exists {
				groupIndex = len(groups)
				indices[key] = groupIndex
				groups = append(groups, platformBookmarkGroup{key: key, title: bookmark.Title, id: id})
			}
			if platform == "linux" {
				groups[groupIndex].linux = append(groups[groupIndex].linux, bookmark)
			} else {
				groups[groupIndex].windows = append(groups[groupIndex].windows, bookmark)
			}
		}
	}
	appendBookmarks("linux", linux)
	appendBookmarks("windows", windows)
	sort.SliceStable(groups, func(i, j int) bool {
		return strings.ToLower(groups[i].title) < strings.ToLower(groups[j].title)
	})
	return groups
}

func platformBookmarkGroupLabel(group platformBookmarkGroup) string {
	if group.id == "" {
		platform := "Linux"
		if len(group.windows) > 0 {
			platform = "Windows"
		}
		return fmt.Sprintf("%s: %s (no MangaDex ID)", platform, group.title)
	}
	return fmt.Sprintf("%s  [Windows %d | Linux %d]", group.title, len(group.windows), len(group.linux))
}

func formatPlatformBookmarkDetails(platform string, bookmarks []models.Bookmarks) string {
	if len(bookmarks) == 0 {
		return fmt.Sprintf("No %s bookmark for this entry.", platform)
	}
	var details strings.Builder
	for index, bookmark := range bookmarks {
		if index > 0 {
			details.WriteString("\n\n")
		}
		fmt.Fprintf(&details, "Title: %s\nType: %s\nMangaDex ID: %s\nSite: %s\nURL: %s\nLocation: %s\nChapters: %s\nShortname: %s",
			bookmark.Title, bookmark.Type, bookmark.MangaDexID, bookmark.Site, bookmark.Url,
			bookmark.Location, bookmark.Chapters, bookmark.Shortname)
	}
	return details.String()
}

// ShowDatabaseRestoreDialog replaces the local Kansho database with the file chosen by the user. The candidate file must pass
// schema validation and an integrity check; the current database is kept as
// kansho.pre-restore.db.
func ShowDatabaseRestoreDialog(_ fyne.App, window fyne.Window) {
	store, err := mangadex.GetStore()
	if err != nil {
		dialog.ShowError(fmt.Errorf("cannot open Kansho database: %v", err), window)
		return
	}

	openDialog := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
		if err != nil {
			dialog.ShowError(fmt.Errorf("error opening file dialog: %v", err), window)
			return
		}
		if reader == nil {
			return // user cancelled
		}
		_ = reader.Close()
		path := reader.URI().Path()

		if err := mangadex.ValidateSchema(path); err != nil {
			dialog.ShowError(fmt.Errorf("restore rejected: %v", err), window)
			return
		}

		dialog.ShowConfirm(
			"Restore Kansho Database",
			fmt.Sprintf("Replace the current Kansho database with %s?\nThe current database is saved as %s before the swap.", path, mangadex.PreRestoreFileName),
			func(confirmed bool) {
				if !confirmed {
					return
				}
				result, err := doRestore(store, path)
				if err != nil {
					dialog.ShowError(err, window)
					return
				}
				dialog.ShowInformation("Restore Complete", result, window)
			},
			window,
		)
	}, window)

	openDialog.Resize(fyne.NewSize(900, 700))
	openDialog.Show()
}

// doRestore performs the actual restore and returns a human-readable summary.
func doRestore(store databaseStore, src string) (string, error) {
	if err := store.Restore(src); err != nil {
		return "", fmt.Errorf("restore failed: %v", err)
	}
	count, err := store.Count()
	if err != nil {
		return "", fmt.Errorf("restore succeeded but count failed: %v", err)
	}
	return fmt.Sprintf("Kansho database restored successfully (%d title metadata records). The previous database is at %s",
		count, filepath.Join(filepath.Dir(defaultDBPath()), mangadex.PreRestoreFileName)), nil
}

// ShowDatabaseCompactDialog optimises and shrinks the local Kansho database.
func ShowDatabaseCompactDialog(_ fyne.App, window fyne.Window) {
	store, err := mangadex.GetStore()
	if err != nil {
		dialog.ShowError(fmt.Errorf("cannot open Kansho database: %v", err), window)
		return
	}

	dialog.ShowConfirm(
		"Compact Kansho Database",
		"Compact the local Kansho database (PRAGMA optimize + VACUUM)? This frees unused space and is safe to run at any time.",
		func(confirmed bool) {
			if !confirmed {
				return
			}
			if err := store.Compact(); err != nil {
				dialog.ShowError(fmt.Errorf("compact failed: %v", err), window)
				return
			}
			integrity, intErr := store.IntegrityCheck()
			if intErr == nil && integrity != "ok" {
				dialog.ShowError(fmt.Errorf("integrity check returned %q after compact", integrity), window)
				return
			}
			dialog.ShowInformation("Database Compacted", "Kansho database compacted successfully.", window)
		},
		window,
	)
}

// ShowClearTransactionLocksDialog removes Kansho's advisory database write
// locks. It is intentionally guarded by a warning because clearing a lock that
// another running instance is using can allow two writers at once.
func ShowClearTransactionLocksDialog(_ fyne.App, window fyne.Window) {
	store, err := mangadex.GetStore()
	if err != nil {
		dialog.ShowError(fmt.Errorf("cannot open Kansho database: %v", err), window)
		return
	}

	count, err := store.LockCount()
	if err != nil {
		dialog.ShowError(err, window)
		return
	}

	warning := fmt.Sprintf(
		"WARNING: Only use this when Kansho reports a database lock error that you cannot clear, and you are sure no other Kansho instance is writing to the database.\n\n"+
			"Clearing a lock that is really in use can allow two writers to change the database at the same time.\n\n"+
			"Clear %d stored transaction lock(s)?",
		count,
	)

	dialog.ShowConfirm(
		"Clear Transaction Locks",
		warning,
		func(confirmed bool) {
			if !confirmed {
				return
			}
			cleared, err := store.ClearLocks()
			if err != nil {
				dialog.ShowError(err, window)
				return
			}
			dialog.ShowInformation("Transaction Locks Cleared",
				fmt.Sprintf("Cleared %d transaction lock(s).", cleared), window)
		},
		window,
	)
}

// databaseStore is the small slice of mangadex.Store used by the database menu,
// kept as a narrow interface so the handlers are easy to test.
type databaseStore interface {
	Restore(src string) error
	Compact() error
	Count() (int, error)
	IntegrityCheck() (string, error)
}

var _ databaseStore = (*mangadex.Store)(nil)
