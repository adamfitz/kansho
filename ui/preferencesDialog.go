package ui

import (
	"fmt"
	"log"
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

// ShowPreferencesDialog lets the user choose the directory that holds
// kansho.db. kansho.conf itself always stays in ~/.config/kansho.
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
	moveCheck := widget.NewCheck("Move Files (remove files from old location)", nil)

	configPath, _ := preferences.FilePath()
	content := container.NewVBox(
		widget.NewLabel("Database directory:"),
		dirRow,
		moveCheck,
		widget.NewLabel("Preferences file: "+configPath),
		widget.NewLabel("Bookmarks are stored in the database; there is no separate bookmarks file."),
	)

	prefsDialog := dialog.NewCustom("Preferences", "Cancel", content, window)
	cancelButton := widget.NewButton("Cancel", prefsDialog.Hide)
	saveButton := widget.NewButton("Save", func() {
		prefsDialog.Hide()
		applyPreferences(window, currentDir, dirEntry.Text, moveCheck.Checked)
	})
	prefsDialog.SetButtons([]fyne.CanvasObject{cancelButton, saveButton})
	prefsDialog.Resize(fyne.NewSize(700, 260))
	prefsDialog.Show()
}

func applyPreferences(window fyne.Window, oldDir, rawNewDir string, move bool) {
	newDir, err := preferences.ExpandPath(rawNewDir)
	if err != nil {
		dialog.ShowError(fmt.Errorf("invalid database directory: %v", err), window)
		return
	}
	if err := os.MkdirAll(newDir, 0755); err != nil {
		dialog.ShowError(fmt.Errorf("cannot create database directory: %v", err), window)
		return
	}

	oldDB := filepath.Join(oldDir, preferences.DatabaseFileName)
	newDB := filepath.Join(newDir, preferences.DatabaseFileName)

	if samePath(oldDB, newDB) {
		if err := preferences.Save(preferences.Preferences{DatabaseDirectory: newDir}); err != nil {
			dialog.ShowError(fmt.Errorf("cannot save preferences: %v", err), window)
			return
		}
		dialog.ShowInformation("Preferences", "The database directory is unchanged.", window)
		return
	}

	oldExists := fileExists(oldDB)
	newExists := fileExists(newDB)

	switch {
	case oldExists && newExists:
		showDatabaseConflictDialog(window, oldDir, newDir, oldDB, newDB)
	case oldExists:
		copyDatabaseAndSwitch(window, oldDir, newDir, oldDB, newDB, move)
	default:
		if err := switchToDatabase(window, oldDir, newDir, newExists); err != nil {
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

func copyDatabaseAndSwitch(window fyne.Window, oldDir, newDir, oldDB, newDB string, move bool) {
	store, err := mangadex.GetStore()
	if err != nil {
		dialog.ShowError(fmt.Errorf("cannot open the current database: %v", err), window)
		return
	}
	if err := store.Backup(newDB); err != nil {
		dialog.ShowError(fmt.Errorf("cannot copy database to the new location: %v", err), window)
		return
	}

	if err := switchToDatabase(window, oldDir, newDir, move); err != nil {
		dialog.ShowError(err, window)
		return
	}

	if move {
		dialog.ShowInformation("Preferences",
			fmt.Sprintf("Database moved to %s. The old database was removed.", newDir), window)
		return
	}

	dialog.ShowConfirm(
		"Remove Old Database?",
		fmt.Sprintf("The database was copied to %s.\n\nDelete the old database at %s?\n\nChoosing No keeps both copies.", newDir, oldDB),
		func(remove bool) {
			if !remove {
				return
			}
			if err := deleteDatabaseFiles(oldDB); err != nil {
				dialog.ShowError(fmt.Errorf("could not remove old database: %v", err), window)
				return
			}
			dialog.ShowInformation("Preferences", "The old database was removed.", window)
		},
		window,
	)
}

func showDatabaseConflictDialog(window fyne.Window, oldDir, newDir, oldDB, newDB string) {
	targetStore, err := mangadex.OpenPath(newDB)
	if err != nil {
		dialog.ShowError(fmt.Errorf("cannot open the target database: %v", err), window)
		return
	}

	diff, err := targetStore.CompareWith(oldDB)
	if err != nil {
		_ = targetStore.Close()
		dialog.ShowError(fmt.Errorf("cannot compare databases: %v", err), window)
		return
	}

	var summary strings.Builder
	fmt.Fprintf(&summary, "The target directory already contains a Kansho database:\n%s\n\n", newDB)
	fmt.Fprintf(&summary, "Current database:\n%s\n\n", oldDB)
	if diff.TotalMissing == 0 {
		summary.WriteString("No records are missing from the target database.\n\n")
	} else {
		fmt.Fprintf(&summary, "%d record(s) exist only in the current database:\n\n", diff.TotalMissing)
	}
	for _, table := range diff.Tables {
		fmt.Fprintf(&summary, "%s: current %d row(s), target %d row(s), missing %d\n",
			table.Name, table.SourceCount, table.TargetCount, table.MissingCount)
		for _, row := range table.Missing {
			fmt.Fprintf(&summary, "    - %s\n", row)
		}
	}

	label := widget.NewLabel(summary.String())
	label.Wrapping = fyne.TextWrapWord
	scroll := container.NewScroll(label)
	scroll.SetMinSize(fyne.NewSize(820, 420))

	conflictDialog := dialog.NewCustom("Database Found in Target Directory", "Cancel", scroll, window)
	cancelButton := widget.NewButton("Cancel", func() {
		_ = targetStore.Close()
		conflictDialog.Hide()
	})
	useTargetButton := widget.NewButton("Use Target Database", func() {
		dialog.ShowConfirm(
			"Use Target Database",
			fmt.Sprintf("Use the database at %s and delete the current database at %s?\n\nThe target database becomes Kansho's only database.", newDB, oldDB),
			func(confirmed bool) {
				if !confirmed {
					return
				}
				conflictDialog.Hide()
				if err := backupTargetDatabase(targetStore, newDB); err != nil {
					_ = targetStore.Close()
					dialog.ShowError(err, window)
					return
				}
				_ = targetStore.Close()
				if err := switchToDatabase(window, oldDir, newDir, true); err != nil {
					dialog.ShowError(err, window)
					return
				}
				dialog.ShowInformation("Preferences",
					fmt.Sprintf("Kansho now uses the target database at %s. The old database was removed.", newDB), window)
			},
			window,
		)
	})
	mergeButton := widget.NewButton("Merge Databases", func() {
		conflictDialog.Hide()
		mergeDatabases(window, targetStore, oldDir, newDir, oldDB, newDB)
	})
	conflictDialog.SetButtons([]fyne.CanvasObject{cancelButton, useTargetButton, mergeButton})
	conflictDialog.Resize(fyne.NewSize(900, 620))
	conflictDialog.Show()
}

func mergeDatabases(window fyne.Window, targetStore *mangadex.Store, oldDir, newDir, oldDB, newDB string) {
	progressBar := widget.NewProgressBar()
	progressBar.Min = 0
	progressBar.Max = 1
	progressLabel := widget.NewLabel("Backing up the target database...")
	progressContent := container.NewVBox(progressLabel, progressBar)
	progressDialog := dialog.NewCustomWithoutButtons("Merging Databases", progressContent, window)
	progressDialog.Resize(fyne.NewSize(600, 180))
	progressDialog.Show()

	go func() {
		result, err := targetStore.MergeMissingFrom(oldDB, func(done, total int, table string) {
			fyne.Do(func() {
				progressBar.SetValue(float64(done) / float64(total))
				progressLabel.SetText(fmt.Sprintf("Merged %s (%d of %d tables)...", table, done, total))
			})
		})
		_ = targetStore.Close()

		fyne.Do(func() {
			progressDialog.Hide()
			if err != nil {
				dialog.ShowError(fmt.Errorf("database merge failed: %v", err), window)
				return
			}

			if err := switchToDatabase(window, oldDir, newDir, true); err != nil {
				dialog.ShowError(err, window)
				return
			}

			var summary strings.Builder
			fmt.Fprintf(&summary, "Database merged into %s.\n\n", newDB)
			fmt.Fprintf(&summary, "Target backup: %s\n\n", result.BackupPath)
			fmt.Fprintf(&summary, "Records added: %d\n", result.TotalInserted)
			for _, table := range result.Tables {
				fmt.Fprintf(&summary, "    %s: %d\n", table.Name, table.Inserted)
			}
			summary.WriteString("\nThe old database was removed.")
			dialog.ShowInformation("Merge Complete", summary.String(), window)
		})
	}()
}

func backupTargetDatabase(store *mangadex.Store, targetPath string) error {
	backup := nextMigrationBackupPath(filepath.Dir(targetPath))
	if err := store.Backup(backup); err != nil {
		return fmt.Errorf("cannot back up the target database: %v", err)
	}
	log.Printf("[UI] target database backed up to %s", backup)
	return nil
}

func nextMigrationBackupPath(dir string) string {
	base := filepath.Join(dir, "kansho.pre-migration-"+time.Now().UTC().Format("20060102-150405")+".db")
	candidate := base
	for i := 1; ; i++ {
		if !fileExists(candidate) {
			return candidate
		}
		candidate = strings.TrimSuffix(base, ".db") + fmt.Sprintf("-%d.db", i)
	}
}

func switchToDatabase(window fyne.Window, oldDir, newDir string, removeOld bool) error {
	if err := preferences.Save(preferences.Preferences{DatabaseDirectory: newDir}); err != nil {
		return fmt.Errorf("cannot save preferences: %v", err)
	}
	if err := mangadex.ReopenDefault(); err != nil {
		_ = preferences.Save(preferences.Preferences{DatabaseDirectory: oldDir})
		_ = mangadex.ReopenDefault()
		return fmt.Errorf("cannot open the new database: %v", err)
	}

	reloadMainWindow(window)

	if removeOld {
		oldDB := filepath.Join(oldDir, preferences.DatabaseFileName)
		if err := deleteDatabaseFiles(oldDB); err != nil {
			return fmt.Errorf("new database is active, but the old database could not be removed: %v", err)
		}
	}
	return nil
}

func deleteDatabaseFiles(path string) error {
	for _, candidate := range []string{path, path + "-wal", path + "-shm", path + "-journal"} {
		if err := os.Remove(candidate); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
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
