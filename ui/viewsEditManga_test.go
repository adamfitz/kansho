package ui

import (
	"path/filepath"
	"runtime"
	"testing"

	"kansho/models"
	"kansho/preferences"
)

func TestSelectedBookmarkTypeRequiresExplicitChoice(t *testing.T) {
	if _, err := selectedBookmarkType(""); err == nil {
		t.Fatal("empty type selection should be rejected")
	}
	for selection, want := range map[string]string{"Manga": "manga", "Webtoon": "webtoon"} {
		got, err := selectedBookmarkType(selection)
		if err != nil || got != want {
			t.Errorf("selectedBookmarkType(%q) = %q, %v; want %q", selection, got, err, want)
		}
	}
}

func TestBookmarkRootLocationUsesPlatformAndType(t *testing.T) {
	prefs := preferences.Preferences{
		WindowsMangaRoot:   `D:\Manga`,
		WindowsWebtoonRoot: `D:\Webtoons`,
		LinuxMangaRoot:     "/mnt/manga",
		LinuxWebtoonRoot:   "/mnt/webtoons",
	}
	platform := "linux"
	root := prefs.LinuxWebtoonRoot
	if runtime.GOOS == "windows" {
		platform = "windows"
		root = prefs.WindowsWebtoonRoot
	}

	got, err := bookmarkRootLocation(prefs, platform, "webtoon", "Title")
	if err != nil {
		t.Fatalf("bookmarkRootLocation: %v", err)
	}
	want := filepath.Join(root, "Title")
	if got != want {
		t.Fatalf("bookmarkRootLocation() = %q, want %q", got, want)
	}
}

func TestSiteSelectionMapsLocalLabelToLocalSite(t *testing.T) {
	if got := siteSelectionValue(localSiteDisplay); got != models.LocalSiteName {
		t.Fatalf("siteSelectionValue(%q) = %q, want %q", localSiteDisplay, got, models.LocalSiteName)
	}
	if got := siteSelectionValue(""); got != models.LocalSiteName {
		t.Fatalf("siteSelectionValue(empty) = %q, want %q", got, models.LocalSiteName)
	}
	if got := siteSelectionValue("mangadex"); got != "mangadex" {
		t.Fatalf("siteSelectionValue(mangadex) = %q", got)
	}
	if got := siteSelectionDisplay(""); got != localSiteDisplay {
		t.Fatalf("siteSelectionDisplay(empty) = %q, want %q", got, localSiteDisplay)
	}
	if got := siteSelectionDisplay(models.LocalSiteName); got != localSiteDisplay {
		t.Fatalf("siteSelectionDisplay(local) = %q, want %q", got, localSiteDisplay)
	}
	if got := siteSelectionDisplay("mangadex"); got != "mangadex" {
		t.Fatalf("siteSelectionDisplay(mangadex) = %q", got)
	}
}
