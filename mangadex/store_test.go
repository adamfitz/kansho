package mangadex

import (
	"os"
	"path/filepath"
	"testing"
)

func tempStore(t *testing.T) *Store {
	t.Helper()
	store, err := OpenPath(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open temp store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func sampleInfo() *MangaInfo {
	year := 1997
	return &MangaInfo{
		ID:                     "a96676e5-8ae2-425e-b549-7f15dd34a6d8",
		Title:                  map[string]string{"en": "One Piece", "ja": "ワンピース"},
		AltTitles:              []map[string]string{{"ja": "ワンピース"}, {"en": "Wan Pīsu"}},
		OriginalLanguage:       "ja",
		Description:            map[string]string{"en": "Pirate adventure.", "fr": "Aventure de pirates."},
		Status:                 "ongoing",
		PublicationDemographic: "shounen",
		Year:                   &year,
		ContentRating:          "safe",
		Tags:                   []TagInfo{{ID: "t1", Name: map[string]string{"en": "Action"}}},
		Author:                 "Eiichiro Oda",
		Artist:                 "Eiichiro Oda",
		CoverURL:               "https://uploads.mangadex.org/covers/a96676e5/cover.512.jpg",
		URL:                    "https://mangadex.org/title/a96676e5-8ae2-425e-b549-7f15dd34a6d8",
	}
}

func TestUpsertAndLookupByID(t *testing.T) {
	store := tempStore(t)

	if err := store.Upsert(sampleInfo()); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := store.LookupByID(sampleInfo().ID)
	if err != nil {
		t.Fatalf("LookupByID: %v", err)
	}
	if got == nil {
		t.Fatal("expected record, got nil")
	}
	if got.Title["en"] != "One Piece" {
		t.Errorf("title en = %q, want %q", got.Title["en"], "One Piece")
	}
	if got.Description["fr"] != "Aventure de pirates." {
		t.Errorf("description fr = %q, want %q", got.Description["fr"], "Aventure de pirates.")
	}
	if got.Author != "Eiichiro Oda" {
		t.Errorf("author = %q, want %q", got.Author, "Eiichiro Oda")
	}
	if got.Year == nil || *got.Year != 1997 {
		t.Errorf("year = %v, want 1997", got.Year)
	}
	if got.EnglishTitle() != "One Piece" {
		t.Errorf("EnglishTitle() = %q", got.EnglishTitle())
	}
}

func TestUpsertUpdatesExisting(t *testing.T) {
	store := tempStore(t)

	if err := store.Upsert(sampleInfo()); err != nil {
		t.Fatalf("first Upsert: %v", err)
	}
	info := sampleInfo()
	info.Status = "completed"
	if err := store.Upsert(info); err != nil {
		t.Fatalf("second Upsert: %v", err)
	}

	got, err := store.LookupByID(sampleInfo().ID)
	if err != nil {
		t.Fatalf("LookupByID: %v", err)
	}
	if got.Status != "completed" {
		t.Errorf("status = %q, want %q", got.Status, "completed")
	}
	count, err := store.Count()
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
}

func TestLookupByTitle(t *testing.T) {
	store := tempStore(t)
	if err := store.Upsert(sampleInfo()); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := store.LookupByTitle("one piece")
	if err != nil {
		t.Fatalf("LookupByTitle: %v", err)
	}
	if got == nil {
		t.Fatal("expected match for 'one piece', got nil")
	}
	if got.ID != sampleInfo().ID {
		t.Errorf("matched id = %s, want %s", got.ID, sampleInfo().ID)
	}

	if _, err := store.LookupByTitle("no such title here"); err != nil {
		t.Fatalf("LookupByTitle missing: %v", err)
	}
	missing, err := store.LookupByTitle("no such title here")
	if err != nil {
		t.Fatalf("LookupByTitle missing: %v", err)
	}
	if missing != nil {
		t.Errorf("expected nil for missing title, got %v", missing)
	}
}

func TestLookupByTitleAltTitleMatch(t *testing.T) {
	store := tempStore(t)
	info := sampleInfo()
	info.Title = map[string]string{"ja": "ワンピース"}
	if err := store.Upsert(info); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := store.LookupByTitle("Wan Pīsu")
	if err != nil {
		t.Fatalf("LookupByTitle: %v", err)
	}
	if got == nil {
		t.Fatal("expected match via alt title, got nil")
	}
}

func TestSetTitleAlias(t *testing.T) {
	store := tempStore(t)
	info := sampleInfo()
	info.Title = map[string]string{"en": "One Piece", "ja": "ワンピース"}
	if err := store.Upsert(info); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	// The bookmarked title differs from every stored title, so only the alias
	// can resolve it.
	alias := "One Piece (Polish Scanlation Name)"
	if err := store.SetTitleAlias(alias, info.ID); err != nil {
		t.Fatalf("SetTitleAlias: %v", err)
	}

	got, err := store.LookupByTitle(alias)
	if err != nil {
		t.Fatalf("LookupByTitle via alias: %v", err)
	}
	if got == nil || got.ID != info.ID {
		t.Fatalf("expected alias lookup to return %s, got %v", info.ID, got)
	}
}

func TestSetTitleAliasUpdatesMapping(t *testing.T) {
	store := tempStore(t)
	first := sampleInfo()
	if err := store.Upsert(first); err != nil {
		t.Fatalf("Upsert first: %v", err)
	}

	second := sampleInfo()
	second.ID = "2c267bf0-b7f5-4f3e-b1b1-7e5d1a1a1a1a"
	second.Title = map[string]string{"en": "A Completely Different Manga"}
	if err := store.Upsert(second); err != nil {
		t.Fatalf("Upsert second: %v", err)
	}

	title := "My Bookmarked Title"
	if err := store.SetTitleAlias(title, first.ID); err != nil {
		t.Fatalf("SetTitleAlias initial: %v", err)
	}
	if err := store.SetTitleAlias(title, second.ID); err != nil {
		t.Fatalf("SetTitleAlias update: %v", err)
	}

	got, err := store.LookupByTitle(title)
	if err != nil {
		t.Fatalf("LookupByTitle: %v", err)
	}
	if got == nil || got.ID != second.ID {
		t.Fatalf("expected alias to resolve to %s after update, got %v", second.ID, got)
	}

	if got.Title["en"] != second.Title["en"] {
		t.Errorf("resolved title = %q, want %q", got.Title["en"], second.Title["en"])
	}
}

func TestBackup(t *testing.T) {
	store := tempStore(t)
	if err := store.Upsert(sampleInfo()); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	dest := filepath.Join(t.TempDir(), "backup.db")
	if err := store.Backup(dest); err != nil {
		t.Fatalf("Backup: %v", err)
	}
	if _, err := os.Stat(dest); os.IsNotExist(err) {
		t.Fatal("backup file does not exist")
	}

	backup, err := OpenPath(dest)
	if err != nil {
		t.Fatalf("open backup: %v", err)
	}
	defer backup.Close()

	got, err := backup.LookupByID(sampleInfo().ID)
	if err != nil || got == nil {
		t.Fatalf("lookup in backup: %v (got %v)", err, got)
	}
}

func TestIntegrityCheck(t *testing.T) {
	store := tempStore(t)
	result, err := store.IntegrityCheck()
	if err != nil {
		t.Fatalf("IntegrityCheck: %v", err)
	}
	if result != "ok" {
		t.Errorf("integrity check = %q, want ok", result)
	}
}

func TestCompact(t *testing.T) {
	store := tempStore(t)
	if err := store.Upsert(sampleInfo()); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := store.Compact(); err != nil {
		t.Fatalf("Compact: %v", err)
	}
}

func TestValidateSchemaRejectsBogusFile(t *testing.T) {
	dir := t.TempDir()
	bogus := filepath.Join(dir, "bogus.db")
	if err := os.WriteFile(bogus, []byte("this is not a sqlite database"), 0644); err != nil {
		t.Fatalf("write bogus: %v", err)
	}
	if err := ValidateSchema(bogus); err == nil {
		t.Fatal("expected error for bogus database, got nil")
	}
}

func TestRestore(t *testing.T) {
	store := tempStore(t)
	if err := store.Upsert(sampleInfo()); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	backup := filepath.Join(t.TempDir(), "restore-source.db")
	if err := store.Backup(backup); err != nil {
		t.Fatalf("Backup: %v", err)
	}

	// Add a second record that will be wiped by the restore.
	other := sampleInfo()
	other.ID = "2c267bf0-b7f5-4f3e-b1b1-7e5d1a1a1a1a"
	other.Title = map[string]string{"en": "Marker"}
	if err := store.Upsert(other); err != nil {
		t.Fatalf("Upsert marker: %v", err)
	}

	if err := store.Restore(backup); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	if _, err := store.LookupByID(other.ID); err != nil {
		t.Fatalf("LookupByID after restore: %v", err)
	}
	found, _ := store.LookupByID(other.ID)
	if found != nil {
		t.Errorf("expected marker record to be gone after restore, got %v", found)
	}

	got, err := store.LookupByID(sampleInfo().ID)
	if err != nil || got == nil {
		t.Fatalf("restored record lookup: %v (got %v)", err, got)
	}
	if got.Title["en"] != "One Piece" {
		t.Errorf("restored title = %q, want One Piece", got.Title["en"])
	}

	result, err := store.IntegrityCheck()
	if err != nil || result != "ok" {
		t.Fatalf("post-restore integrity check = %q, %v", result, err)
	}
}

func TestRestoreRejectsInvalidSchema(t *testing.T) {
	store := tempStore(t)
	bogus := filepath.Join(t.TempDir(), "bogus.db")
	if err := os.WriteFile(bogus, []byte("garbage"), 0644); err != nil {
		t.Fatalf("write bogus: %v", err)
	}
	if err := store.Restore(bogus); err == nil {
		t.Fatal("expected restore to reject bogus file")
	}
}

func TestExtractID(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "https://mangadex.org/title/a1b2c3d4/manga-name", want: "a1b2c3d4"},
		{in: "https://mangadex.org/title/a1b2c3d4", want: "a1b2c3d4"},
		{in: "a1b2c3d4", want: "a1b2c3d4"},
		{in: "  a1b2c3d4  ", want: "a1b2c3d4"},
		{in: "https://mangadex.org/notatitle/a1b2c3d4", wantErr: true},
		{in: "https://other.com/title/a1b2c3d4", wantErr: true},
		{in: "", wantErr: true},
		{in: "https://mangadex.org/title/", wantErr: true},
	}
	for _, tc := range cases {
		got, err := ExtractID(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ExtractID(%q): expected error, got %q", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ExtractID(%q): unexpected error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ExtractID(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestAvailableLanguages(t *testing.T) {
	info := sampleInfo()
	langs := info.AvailableLanguages()
	if len(langs) != 3 {
		t.Fatalf("expected 3 languages (en/fr/ja via alt titles), got %v", langs)
	}
	if langs[0] != "en" || langs[1] != "fr" && langs[0] != "fr" {
		t.Errorf("unexpected languages: %v", langs)
	}
}

func TestTitleInFallback(t *testing.T) {
	info := sampleInfo()
	if got := info.TitleIn("en"); got != "One Piece" {
		t.Errorf("TitleIn(en) = %q", got)
	}
	if got := info.TitleIn("ja"); got != "ワンピース" {
		t.Errorf("TitleIn(ja) = %q", got)
	}
	if got := info.TitleIn("zz"); got != "One Piece" {
		t.Errorf("TitleIn(zz) fallback = %q", got)
	}
}
