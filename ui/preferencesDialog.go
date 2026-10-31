package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"kansho/mangadex"
	"kansho/preferences"
)

// ShowPreferencesDialog lets the user configure database and bookmark root directories.
func ShowPreferencesDialog(_ fyne.App, window fyne.Window) {
	prefs, err := preferences.Load()
	if err != nil {
		dialog.ShowError(fmt.Errorf("cannot load preferences: %v", err), window)
		return
	}
	currentDir, err := prefs.DatabaseDir()
	if err != nil {
		dialog.ShowError(fmt.Errorf("cannot resolve database directory: %v", err), window)
		return
	}

	dirEntry := widget.NewEntry()
	dirEntry.SetText(prefs.DatabaseDirectory)

	browseButton := widget.NewButton("Browse...", func() {
		dialog.ShowFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil {
				dialog.ShowError(fmt.Errorf("error opening folder dialog: %v", err), window)
				return
			}
			if uri == nil {
				return
			}
			dirEntry.SetText(uri.Path())
		}, window)
	})

	dirRow := container.NewBorder(nil, nil, nil, browseButton, dirEntry)
	rootControls := []rootPreferenceControl{
		newRootPreferenceControl(prefs, "linux", "manga", window),
		newRootPreferenceControl(prefs, "linux", "webtoon", window),
		newRootPreferenceControl(prefs, "windows", "manga", window),
		newRootPreferenceControl(prefs, "windows", "webtoon", window),
	}
	rootRows := make([]fyne.CanvasObject, 0, len(rootControls))
	for _, control := range rootControls {
		rootRows = append(rootRows, control.content)
	}

	configPath, _ := preferences.FilePath()
	content := container.NewVScroll(container.NewVBox(
		widget.NewLabel("Database directory:"),
		dirRow,
		widget.NewLabel("Default bookmark roots:"),
		widget.NewLabel("Applying a root changes saved paths only; it does not move files."),
		container.NewVBox(rootRows...),
		widget.NewLabel("Preferences file: "+configPath),
		widget.NewLabel("Bookmarks are stored in the database; there is no separate bookmarks file."),
	))

	prefsDialog := dialog.NewCustom("Preferences", "Cancel", content, window)
	cancelButton := widget.NewButton("Cancel", prefsDialog.Hide)
	saveButton := widget.NewButton("Save", func() {
		prefsDialog.Hide()
		updatedPrefs := prefs
		updatedPrefs.DatabaseDirectory = dirEntry.Text
		for _, control := range rootControls {
			updatedPrefs.SetRootDirectory(control.platform, control.kind, control.entry.Text)
		}
		applyPreferences(window, currentDir, dirEntry.Text, prefs, updatedPrefs, rootControls)
	})
	prefsDialog.SetButtons([]fyne.CanvasObject{cancelButton, saveButton})
	prefsDialog.Resize(fyne.NewSize(860, 680))
	prefsDialog.Show()
}

type rootPreferenceControl struct {
	platform string
	kind     string
	entry    *widget.Entry
	apply    *widget.Check
	content  fyne.CanvasObject
}

func newRootPreferenceControl(prefs preferences.Preferences, platform, kind string, window fyne.Window) rootPreferenceControl {
	entry := widget.NewEntry()
	entry.SetPlaceHolder("Optional path")
	entry.SetText(prefs.RootDirectory(platform, kind))
	browseButton := widget.NewButton("Browse...", func() {
		dialog.ShowFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil {
				dialog.ShowError(fmt.Errorf("error opening folder dialog: %v", err), window)
				return
			}
			if uri != nil {
				entry.SetText(uri.Path())
			}
		}, window)
	})
	apply := widget.NewCheck(fmt.Sprintf("Apply to all %s %s bookmarks", platform, kind), nil)
	label := fmt.Sprintf("%s %s root:", strings.Title(platform), kind)
	content := container.NewVBox(
		widget.NewLabel(label),
		container.NewBorder(nil, nil, nil, browseButton, entry),
		apply,
	)
	return rootPreferenceControl{platform: platform, kind: kind, entry: entry, apply: apply, content: content}
}

func applyPreferences(window fyne.Window, oldDir, rawNewDir string,
	oldPrefs, newPrefs preferences.Preferences, rootControls []rootPreferenceControl,
) {
	newDir, err := preferences.ExpandPath(rawNewDir)
	if err != nil {
		dialog.ShowError(fmt.Errorf("invalid database directory: %v", err), window)
		return
	}
	if err := os.MkdirAll(newDir, 0755); err != nil {
		dialog.ShowError(fmt.Errorf("cannot create database directory: %v", err), window)
		return
	}

	rootChanged := oldPrefs.LinuxMangaRoot != newPrefs.LinuxMangaRoot ||
		oldPrefs.LinuxWebtoonRoot != newPrefs.LinuxWebtoonRoot ||
		oldPrefs.WindowsMangaRoot != newPrefs.WindowsMangaRoot ||
		oldPrefs.WindowsWebtoonRoot != newPrefs.WindowsWebtoonRoot
	applyRoot := false
	for _, control := range rootControls {
		applyRoot = applyRoot || control.apply.Checked
	}
	backupPath := ""
	updatedBookmarks := 0
	var rootUpdates []mangadex.BookmarkRootUpdate
	if rootChanged || applyRoot {
		for _, control := range rootControls {
			if !control.apply.Checked {
				continue
			}
			root := strings.TrimSpace(newPrefs.RootDirectory(control.platform, control.kind))
			if root == "" {
				dialog.ShowError(fmt.Errorf("set a %s %s root before applying it", control.platform, control.kind), window)
				return
			}
			if control.platform == "linux" {
				root, err = preferences.ExpandPath(root)
				if err != nil {
					dialog.ShowError(fmt.Errorf("invalid Linux root directory: %v", err), window)
					return
				}
			}
			rootUpdates = append(rootUpdates, mangadex.BookmarkRootUpdate{Platform: control.platform, Type: control.kind, Root: root})
		}
		store, err := mangadex.GetStore()
		if err != nil {
			dialog.ShowError(fmt.Errorf("cannot open the current database: %v", err), window)
			return
		}
		backupPath = nextRootSettingsBackupPath(store.Path())
		if err := store.Backup(backupPath); err != nil {
			dialog.ShowError(fmt.Errorf("cannot back up the database before changing roots: %v", err), window)
			return
		}
		if len(rootUpdates) > 0 {
			updatedBookmarks, err = store.ApplyBookmarkRootUpdates(rootUpdates)
			if err != nil {
				dialog.ShowError(fmt.Errorf("cannot update bookmark paths: %v", err), window)
				return
			}
		}
	}
	oldDB := filepath.Join(oldDir, preferences.DatabaseFileName)
	newDB := filepath.Join(newDir, preferences.DatabaseFileName)

	if samePath(oldDB, newDB) {
		newPrefs.DatabaseDirectory = newDir
		if err := preferences.Save(newPrefs); err != nil {
			dialog.ShowError(fmt.Errorf("cannot save preferences: %v", err), window)
			return
		}
		message := fmt.Sprintf("Preferences saved. Updated %d bookmark path(s).", updatedBookmarks)
		if backupPath != "" {
			message += "\nDatabase backup: " + backupPath
		}
		dialog.ShowInformation("Preferences", message, window)
		return
	}
	if rootChanged {
		newPrefs.DatabaseDirectory = oldPrefs.DatabaseDirectory
		if err := preferences.Save(newPrefs); err != nil {
			dialog.ShowError(fmt.Errorf("cannot save root preferences: %v", err), window)
			return
		}
	}

	oldExists := fileExists(oldDB)
	newExists := fileExists(newDB)

	switch {
	case oldExists && newExists:
		if err := switchToDatabase(window, oldDir, newDir); err != nil {
			dialog.ShowError(err, window)
			return
		}
		dialog.ShowInformation("Preferences",
			fmt.Sprintf("Using the existing database at %s. The current database was left untouched.", newDB), window)
	case oldExists:
		copyDatabaseAndSwitch(window, oldDir, newDir, oldDB, newDB)
	default:
		if err := switchToDatabase(window, oldDir, newDir); err != nil {
			dialog.ShowError(err, window)
			return
		}
		if newExists {
			dialog.ShowInformation("Preferences",
				fmt.Sprintf("Kansho now uses the existing database at %s.", newDB), window)
		} else {
			dialog.ShowInformation("Preferences",
				fmt.Sprintf("Kansho will use a new database at %s.", newDB), window)
		}
	}
}

func nextRootSettingsBackupPath(databasePath string) string {
	dir := filepath.Join(filepath.Dir(databasePath), "backup")
	base := filepath.Join(dir, "kansho-root-settings-"+time.Now().UTC().Format("20060102-150405")+".db")
	candidate := base
	for i := 1; fileExists(candidate); i++ {
		candidate = strings.TrimSuffix(base, ".db") + fmt.Sprintf("-%d.db", i)
	}
	return candidate
}

func copyDatabaseAndSwitch(window fyne.Window, oldDir, newDir, oldDB, newDB string) {
	store, err := mangadex.GetStore()
	if err != nil {
		dialog.ShowError(fmt.Errorf("cannot open the current database: %v", err), window)
		return
	}
	if err := backupDatabaseWithoutOverwrite(store, newDB); err != nil {
		dialog.ShowError(fmt.Errorf("cannot copy database to the new location: %v", err), window)
		return
	}

	if err := switchToDatabase(window, oldDir, newDir); err != nil {
		dialog.ShowError(err, window)
		return
	}
}

func backupDatabaseWithoutOverwrite(store *mangadex.Store, destination string) error {
	destinationDir := filepath.Dir(destination)
	if err := os.MkdirAll(destinationDir, 0755); err != nil {
		return fmt.Errorf("create database directory: %w", err)
	}
	tempDir, err := os.MkdirTemp(destinationDir, ".kansho-copy-")
	if err != nil {
		return fmt.Errorf("create temporary database directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	tempDB := filepath.Join(tempDir, preferences.DatabaseFileName)
	if err := store.Backup(tempDB); err != nil {
		return err
	}
	if err := os.Link(tempDB, destination); err != nil {
		return fmt.Errorf("install database copy without overwriting an existing file: %w", err)
	}
	return nil
}

func switchToDatabase(window fyne.Window, oldDir, newDir string) error {
	prefs, err := preferences.Load()
	if err != nil {
		return fmt.Errorf("cannot load preferences: %v", err)
	}
	previousPrefs := prefs
	prefs.DatabaseDirectory = newDir
	if err := preferences.Save(prefs); err != nil {
		return fmt.Errorf("cannot save preferences: %v", err)
	}
	if err := mangadex.ReopenDefault(); err != nil {
		previousPrefs.DatabaseDirectory = oldDir
		_ = preferences.Save(previousPrefs)
		_ = mangadex.ReopenDefault()
		return fmt.Errorf("cannot open the new database: %v", err)
	}

	reloadMainWindow(window)
	return nil
}

func reloadMainWindow(window fyne.Window) {
	window.SetContent(BuildMainLayout(window))
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func samePath(a, b string) bool {
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return absA == absB
}
