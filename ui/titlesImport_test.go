package ui

import (
	"strings"
	"testing"
)

func TestParseTitleImportCSVAndTrimFields(t *testing.T) {
	data := []byte("Title, Location, MangaDex Link, Bookmark Type, Site, URL\n" +
		"\"  One Piece  \",\" /mnt/manga/One Piece \",\" https://mangadex.org/title/abc-123/slug \",\"Manga\",\"mangadex\",\" https://example.test/one \"\n")

	entries, err := parseTitleImportEntries("titles.csv", data)
	if err != nil {
		t.Fatalf("parseTitleImportEntries: %v", err)
	}
	entries, problems := prepareTitleImportEntries(entries)
	if len(problems) != 0 {
		t.Fatalf("prepareTitleImportEntries problems = %v", problems)
	}
	if len(entries) != 1 {
		t.Fatalf("parsed %d entries, want 1", len(entries))
	}

	entry := entries[0]
	if entry.Title != "One Piece" {
		t.Errorf("title = %q, want %q", entry.Title, "One Piece")
	}
	if entry.Location != "/mnt/manga/One Piece" {
		t.Errorf("location = %q", entry.Location)
	}
	if entry.MangaDexID != "abc-123" {
		t.Errorf("MangaDexID = %q, want abc-123", entry.MangaDexID)
	}
	if entry.Type != "manga" || entry.Site != "mangadex" || entry.URL != "https://example.test/one" {
		t.Errorf("entry = %+v", entry)
	}
}

func TestParseTitleImportYAML(t *testing.T) {
	data := []byte(strings.TrimSpace(`
titles:
  - title: "  Webtoon Title  "
    location: " /mnt/webtoons/Webtoon Title "
    mangadex_id: "def-456"
    bookmark_type: Webtoon
  - name: "Plain Title"
    path: "/mnt/manga/Plain Title"
    type: manga
`))

	entries, err := parseTitleImportEntries("titles.yaml", data)
	if err != nil {
		t.Fatalf("parseTitleImportEntries: %v", err)
	}
	entries, problems := prepareTitleImportEntries(entries)
	if len(problems) != 0 {
		t.Fatalf("prepareTitleImportEntries problems = %v", problems)
	}
	if len(entries) != 2 {
		t.Fatalf("parsed %d entries, want 2", len(entries))
	}
	if entries[0].Title != "Webtoon Title" || entries[0].MangaDexID != "def-456" || entries[0].Type != "webtoon" {
		t.Errorf("first entry = %+v", entries[0])
	}
	if entries[1].Title != "Plain Title" || entries[1].Location != "/mnt/manga/Plain Title" || entries[1].Type != "manga" {
		t.Errorf("second entry = %+v", entries[1])
	}
}

func TestPrepareTitleImportEntriesReportsProblems(t *testing.T) {
	entries := []titleImportEntry{
		{Row: 1, Title: "No Type", Location: "/tmp/a"},
		{Row: 2, Title: "Bad Type", Location: "/tmp/b", Type: "novel"},
		{Row: 3, Title: "Bad MangaDex", Location: "/tmp/c", Type: "manga", MangaDex: "https://example.com/title/1"},
		{Row: 4, Title: "Duplicate", Location: "/tmp/d", Type: "manga"},
		{Row: 5, Title: " duplicate ", Location: "/tmp/other", Type: "manga"},
	}

	_, problems := prepareTitleImportEntries(entries)
	joined := strings.Join(problems, "\n")
	for _, want := range []string{
		"row 1 (No Type): type is required",
		"row 2 (Bad Type): unknown type",
		"row 3 (Bad MangaDex): invalid MangaDex link/ID",
		"row 5 (duplicate): duplicates row 4 (same title)",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("problems missing %q:\n%s", want, joined)
		}
	}
}

func TestParseTitleImportRejectsUnsupportedExtension(t *testing.T) {
	if _, err := parseTitleImportEntries("titles.txt", []byte("data")); err == nil {
		t.Fatal("expected unsupported extension error")
	}
}
