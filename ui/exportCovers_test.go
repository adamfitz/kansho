package ui

import (
	"os"
	"path/filepath"
	"testing"

	"kansho/mangadex"
)

func TestExportCachedCoversSelectedAndAll(t *testing.T) {
	covers := []mangadex.CachedCover{
		{MangaID: "manga-one", Title: "Manga One", JPEG: []byte("cover one")},
		{MangaID: "manga-two", Title: "Manga Two", JPEG: []byte("cover two")},
	}

	selectedDir := t.TempDir()
	count, err := exportCachedCovers(covers, "manga-two", selectedDir)
	if err != nil {
		t.Fatalf("export selected cover: %v", err)
	}
	if count != 1 {
		t.Fatalf("selected export count = %d, want 1", count)
	}
	if data, err := os.ReadFile(filepath.Join(selectedDir, "Manga Two.jpg")); err != nil || string(data) != "cover two" {
		t.Fatalf("selected cover output = %q, err = %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(selectedDir, "Manga One.jpg")); !os.IsNotExist(err) {
		t.Fatalf("unselected cover should not be exported, stat err = %v", err)
	}

	allDir := t.TempDir()
	count, err = exportCachedCovers(covers, "", allDir)
	if err != nil {
		t.Fatalf("export all covers: %v", err)
	}
	if count != 2 {
		t.Fatalf("all export count = %d, want 2", count)
	}
}
