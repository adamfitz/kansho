package ui

import (
	"os"
	"path/filepath"
	"testing"

	"kansho/mangadex"
)

func TestBackupDatabaseWithoutOverwrite(t *testing.T) {
	store, err := mangadex.OpenPath(filepath.Join(t.TempDir(), "source.db"))
	if err != nil {
		t.Fatalf("open source database: %v", err)
	}
	defer store.Close()

	t.Run("creates absent destination", func(t *testing.T) {
		destination := filepath.Join(t.TempDir(), "kansho.db")
		if err := backupDatabaseWithoutOverwrite(store, destination); err != nil {
			t.Fatalf("copy database: %v", err)
		}
		copy, err := mangadex.OpenPath(destination)
		if err != nil {
			t.Fatalf("open copied database: %v", err)
		}
		if err := copy.Close(); err != nil {
			t.Fatalf("close copied database: %v", err)
		}
	})

	t.Run("preserves existing destination", func(t *testing.T) {
		destination := filepath.Join(t.TempDir(), "kansho.db")
		original := []byte("existing database must remain untouched")
		if err := os.WriteFile(destination, original, 0644); err != nil {
			t.Fatalf("create existing destination: %v", err)
		}
		if err := backupDatabaseWithoutOverwrite(store, destination); err == nil {
			t.Fatal("copy unexpectedly replaced the existing destination")
		}
		got, err := os.ReadFile(destination)
		if err != nil {
			t.Fatalf("read existing destination: %v", err)
		}
		if string(got) != string(original) {
			t.Fatalf("existing destination changed: got %q, want %q", got, original)
		}
	})
}