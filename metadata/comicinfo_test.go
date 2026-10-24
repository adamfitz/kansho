package metadata

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"kansho/mangadex"
)

func TestFromMangaInfoUsesSelectedEnglishTitleForSeries(t *testing.T) {
	info := &mangadex.MangaInfo{
		Title:                map[string]string{"ko": "Jeoldae Gunrim"},
		AltTitles:            []map[string]string{{"en": "Absolute Dominion"}, {"en": "Absolute Rule"}},
		SelectedEnglishTitle: "Absolute Rule",
		Author:               "Author Name",
	}

	comicInfo := FromMangaInfo(info)
	if comicInfo.Series != "Absolute Rule" {
		t.Fatalf("Series = %q, want %q", comicInfo.Series, "Absolute Rule")
	}
	if comicInfo.Series == info.Author {
		t.Fatalf("Series incorrectly contains author name %q", info.Author)
	}
}

func TestFromMangaInfoFallsBackToNonEnglishSeriesWithoutEnglishTitle(t *testing.T) {
	info := &mangadex.MangaInfo{
		Title:            map[string]string{"ko": "Jeoldae Gunrim"},
		OriginalLanguage: "ko",
	}

	comicInfo := FromMangaInfo(info)
	if comicInfo.Series != "Jeoldae Gunrim" {
		t.Fatalf("Series = %q, want %q", comicInfo.Series, "Jeoldae Gunrim")
	}
}

func TestVerifyForDirectoryUsesSelectedDirectoryForChapterFiles(t *testing.T) {
	dir := t.TempDir()
	cbzPath := filepath.Join(dir, "ch001.cbz")
	archive, err := os.Create(cbzPath)
	if err != nil {
		t.Fatal(err)
	}
	zipWriter := zip.NewWriter(archive)
	page, err := zipWriter.Create("001.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := page.Write([]byte("page")); err != nil {
		t.Fatal(err)
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}

	info := &mangadex.MangaInfo{Title: map[string]string{"en": "Test Series"}}
	generated := GenerateForDirectory(nil, info, dir)
	if len(generated) != 1 || generated[0].Status != "OK" {
		t.Fatalf("GenerateForDirectory() = %+v, want one OK result", generated)
	}

	verified := VerifyForDirectory(nil, info, dir)
	if len(verified) != 1 || verified[0].Status != "OK" {
		t.Fatalf("VerifyForDirectory() = %+v, want one OK result", verified)
	}
}
