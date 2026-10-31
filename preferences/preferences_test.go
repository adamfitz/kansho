package preferences

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCreatesDefaultFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	prefs, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if prefs.DatabaseDirectory != DefaultDirectory {
		t.Fatalf("DatabaseDirectory = %q, want %q", prefs.DatabaseDirectory, DefaultDirectory)
	}

	path, err := FilePath()
	if err != nil {
		t.Fatalf("FilePath: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("preferences file not created: %v", err)
	}
}

func TestSaveAndLoadDatabaseDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	want := filepath.Join(home, "cloud", "kansho")
	if err := Save(Preferences{DatabaseDirectory: want}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.DatabaseDirectory != want {
		t.Fatalf("DatabaseDirectory = %q, want %q", got.DatabaseDirectory, want)
	}

	dbPath, err := got.DatabasePath()
	if err != nil {
		t.Fatalf("DatabasePath: %v", err)
	}
	if dbPath != filepath.Join(want, DatabaseFileName) {
		t.Fatalf("DatabasePath = %q", dbPath)
	}
}

func TestSaveAndLoadPlatformRoots(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	want := Preferences{
		DatabaseDirectory:  filepath.Join(home, "db"),
		WindowsMangaRoot:   `W:\Manga`,
		WindowsWebtoonRoot: `W:\Webtoons`,
		LinuxMangaRoot:     "/mnt/library/manga",
		LinuxWebtoonRoot:   "/mnt/library/webtoons",
	}
	if err := Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != want {
		t.Fatalf("Load() = %+v, want %+v", got, want)
	}
	if got.RootDirectory("windows", "webtoon") != want.WindowsWebtoonRoot {
		t.Fatalf("Windows webtoon root = %q", got.RootDirectory("windows", "webtoon"))
	}
	if got.RootDirectory("linux", "manga") != want.LinuxMangaRoot {
		t.Fatalf("Linux manga root = %q", got.RootDirectory("linux", "manga"))
	}
}
