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
	verified := VerifyForDirectory(nil, info, dir)
	if len(verified) != 1 || verified[0].Status != "Fixed" {
		t.Fatalf("VerifyForDirectory() = %+v, want one Fixed result", verified)
	}
}

func TestListChapterFilesSortsByChapterNumber(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"ch010.cbz", "ch2.cbz", "extra.cbz", "Series v02 (2024).cbz", "Series Vol. 1.cbz"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files, err := ListChapterFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 5 {
		t.Fatalf("ListChapterFiles() returned %d files, want 5", len(files))
	}
	wantOrder := []string{"ch2.cbz", "ch010.cbz", "Series Vol. 1.cbz", "Series v02 (2024).cbz", "extra.cbz"}
	for i, want := range wantOrder {
		if files[i].Name != want {
			t.Fatalf("ListChapterFiles() order[%d] = %v, want %v", i, files[i].Name, want)
		}
	}
	if !files[0].HasNumber || files[0].Number != 2 {
		t.Fatalf("ch2 parsed as %+v, want number 2", files[0])
	}
	if files[2].HasNumber || !files[2].IsVolume || files[2].VolumeNumber != 1 {
		t.Fatalf("Series Vol. 1 parsed as %+v, want volume 1", files[2])
	}
	if files[3].HasNumber || !files[3].IsVolume || files[3].VolumeNumber != 2 {
		t.Fatalf("Series v02 parsed as %+v, want volume 2", files[3])
	}
	if files[4].HasNumber || files[4].HasVolume {
		t.Fatalf("extra should not have a parsed number: %+v", files[4])
	}
}

func TestChapterAndVolumeNumberValuesDoNotMix(t *testing.T) {
	for _, name := range []string{"Series v01.cbz", "Series Vol. 2.cbz", "Series Volume_3.cbz", "Series V4.cbz"} {
		if _, ok := ChapterNumberValue(name); ok {
			t.Fatalf("ChapterNumberValue(%q) parsed a volume as a chapter", name)
		}
		if _, ok := VolumeNumberValue(name); !ok {
			t.Fatalf("VolumeNumberValue(%q) did not parse a volume", name)
		}
	}
	chapter, ok := ChapterNumberValue("Series v01 ch005.cbz")
	if !ok || chapter != 5 {
		t.Fatalf("ChapterNumberValue() = %v, %v; want 5, true", chapter, ok)
	}
	volume, ok := VolumeNumberValue("Series v01 ch005.cbz")
	if !ok || volume != 1 {
		t.Fatalf("VolumeNumberValue() = %v, %v; want 1, true", volume, ok)
	}
}

func TestChapterAndVolumeZeroAreNotDropped(t *testing.T) {
	chapter, ok := ChapterNumberValue("ch000.cbz")
	if !ok || chapter != 0 {
		t.Fatalf("ChapterNumberValue(ch000) = %v, %v; want 0, true", chapter, ok)
	}
	volume, ok := VolumeNumberValue("Series Vol 0.cbz")
	if !ok || volume != 0 {
		t.Fatalf("VolumeNumberValue(Vol 0) = %v, %v; want 0, true", volume, ok)
	}
	if got := ExtractChapterNumber("ch003.5.cbz"); got != "3.5" {
		t.Fatalf("ExtractChapterNumber(ch003.5) = %q, want 3.5", got)
	}
	if got := ExtractChapterNumber("ch000.5.cbz"); got != "0.5" {
		t.Fatalf("ExtractChapterNumber(ch000.5) = %q, want 0.5", got)
	}
}

func TestCheckCBZRequiresNonEmptyComicInfo(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.cbz")
	writeTestCBZ(t, missing, map[string]string{"001.jpg": "page"})
	if err := CheckCBZ(missing); err == nil {
		t.Fatal("CheckCBZ() succeeded without ComicInfo.xml")
	}

	empty := filepath.Join(dir, "empty.cbz")
	writeTestCBZ(t, empty, map[string]string{"ComicInfo.xml": ""})
	if err := CheckCBZ(empty); err == nil {
		t.Fatal("CheckCBZ() succeeded with an empty ComicInfo.xml")
	}

	present := filepath.Join(dir, "present.cbz")
	writeTestCBZ(t, present, map[string]string{"ComicInfo.xml": "<ComicInfo/>"})
	if err := CheckCBZ(present); err != nil {
		t.Fatalf("CheckCBZ() failed with ComicInfo.xml: %v", err)
	}
}

func TestDirectoryFullyCheckedUsesCursor(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"ch001.cbz", "ch002.cbz"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if !DirectoryFullyChecked(dir, 2, 0) {
		t.Fatal("DirectoryFullyChecked() = false at chapter 2")
	}
	if DirectoryFullyChecked(dir, 1, 0) {
		t.Fatal("DirectoryFullyChecked() = true at chapter 1")
	}
	if err := os.WriteFile(filepath.Join(dir, "Series v03.cbz"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if DirectoryFullyChecked(dir, 2, 0) {
		t.Fatal("DirectoryFullyChecked() = true with an unchecked volume")
	}
	if DirectoryFullyChecked(dir, 2, 2) {
		t.Fatal("DirectoryFullyChecked() = true with a volume above the volume cursor")
	}
	if !DirectoryFullyChecked(dir, 2, 3) {
		t.Fatal("DirectoryFullyChecked() = false with chapter and volume cursors")
	}
	if err := os.WriteFile(filepath.Join(dir, "extra.cbz"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if DirectoryFullyChecked(dir, 2, 3) {
		t.Fatal("DirectoryFullyChecked() = true with an unnumbered file")
	}
}

func writeTestCBZ(t *testing.T, path string, files map[string]string) {
	t.Helper()
	archive, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zipWriter := zip.NewWriter(archive)
	for name, content := range files {
		entry, err := zipWriter.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
}
