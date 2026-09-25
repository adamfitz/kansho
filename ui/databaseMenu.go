package ui

import (
	"fmt"
	"os"
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"

	"kansho/mangadex"
	"kansho/parser"
)

// defaultDBPath returns the current Kansho database path.
func defaultDBPath() string {
	configDir, err := parser.ExpandPath("~/.config/kansho")
	if err != nil {
		return filepath.Join("kansho", "mangadex.db")
	}
	return filepath.Join(configDir, "mangadex.db")
}

// ShowDatabaseBackupDialog writes a VACUUM snapshot of the local title and
// chapter-statistics database to a user-chosen file.
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
			fmt.Sprintf("local title and chapter-statistics database backed up to %s", dest), window)
	}, window)

	saveDialog.SetFileName("mangadex-backup.db")
	homePath, err := os.UserHomeDir()
	if err == nil {
		if homeDir, listerErr := storage.ListerForURI(storage.NewFileURI(homePath)); listerErr == nil {
			saveDialog.SetLocation(homeDir)
		}
	}
	saveDialog.Resize(fyne.NewSize(900, 700))
	saveDialog.Show()
}

// ShowDatabaseRestoreDialog replaces the local title and chapter-statistics
// database with the file chosen by the user. The candidate file must pass
// schema validation and an integrity check; the current database is kept as
// mangadex.pre-restore.db.
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
			"Restore MangaDex Database",
			fmt.Sprintf("Replace the current local title and chapter-statistics database with %s?\nThe current database is saved as mangadex.pre-restore.db before the swap.", path),
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
	return fmt.Sprintf("local title and chapter-statistics database restored successfully (%d titles). The previous database is at %s",
		count, filepath.Join(filepath.Dir(defaultDBPath()), "mangadex.pre-restore.db")), nil
}

// ShowDatabaseCompactDialog optimises and shrinks the local title and
// chapter-statistics database.
func ShowDatabaseCompactDialog(_ fyne.App, window fyne.Window) {
	store, err := mangadex.GetStore()
	if err != nil {
		dialog.ShowError(fmt.Errorf("cannot open Kansho database: %v", err), window)
		return
	}

	dialog.ShowConfirm(
		"Compact MangaDex Database",
		"Compact the local title and chapter-statistics database (PRAGMA optimize + VACUUM)? This frees unused space and is safe to run at any time.",
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
			dialog.ShowInformation("Database Compacted", "local title and chapter-statistics database compacted successfully.", window)
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
