package mangadex

import (
	"os"
	"path/filepath"
	"testing"

	"kansho/models"
	"kansho/preferences"
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

func TestListCoverCache(t *testing.T) {
	store := tempStore(t)
	info := sampleInfo()
	if err := store.Upsert(info); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	uncachedInfo := &MangaInfo{ID: "uncached-cover", Title: map[string]string{"en": "Uncached Manga"}, CoverURL: "https://example.com/uncached.jpg"}
	if err := store.Upsert(uncachedInfo); err != nil {
		t.Fatalf("Upsert uncached title: %v", err)
	}
	missingURLInfo := &MangaInfo{ID: "missing-cover-url", Title: map[string]string{"ja": "題名のないカバー"}}
	if err := store.Upsert(missingURLInfo); err != nil {
		t.Fatalf("Upsert title without cover URL: %v", err)
	}
	coverBytes := []byte("jpeg data")
	if err := store.SetCoverCache(info.ID, coverBytes, info.CoverURL); err != nil {
		t.Fatalf("SetCoverCache: %v", err)
	}

	covers, err := store.ListCoverCache()
	if err != nil {
		t.Fatalf("ListCoverCache: %v", err)
	}
	if len(covers) != 3 {
		t.Fatalf("ListCoverCache returned %d covers, want 3", len(covers))
	}
	var cached, uncached *CachedCover
	var missingURL *CachedCover
	for i := range covers {
		switch covers[i].MangaID {
		case info.ID:
			cached = &covers[i]
		case uncachedInfo.ID:
			uncached = &covers[i]
		case missingURLInfo.ID:
			missingURL = &covers[i]
		}
	}
	if cached == nil || cached.Title != "One Piece" || string(cached.JPEG) != string(coverBytes) {
		t.Errorf("ListCoverCache cached cover = %+v, want the cached One Piece JPEG", cached)
	}
	if uncached == nil || uncached.Title != "Uncached Manga" || uncached.CoverURL != uncachedInfo.CoverURL || len(uncached.JPEG) != 0 {
		t.Errorf("ListCoverCache uncached cover = %+v, want metadata with no JPEG", uncached)
	}
	if missingURL == nil || missingURL.Title != "題名のないカバー" || missingURL.CoverURL != "" || len(missingURL.JPEG) != 0 {
		t.Errorf("ListCoverCache missing-URL title = %+v, want metadata with no URL or JPEG", missingURL)
	}
}

func TestStoredCoverTitlePrefersEnglishOverPrimary(t *testing.T) {
	got := storedCoverTitle("Selected English title", `{"ja":"日本語タイトル","en":"English title"}`, `[{"en":"Alternate English title"}]`)
	if got != "English title" {
		t.Fatalf("storedCoverTitle() = %q, want English title", got)
	}

	got = storedCoverTitle("Selected primary title", `{"ja":"日本語タイトル"}`, `[]`)
	if got != "Selected primary title" {
		t.Fatalf("storedCoverTitle() fallback = %q, want primary title", got)
	}
}

func TestEnglishTitleCandidatesIncludeAlternates(t *testing.T) {
	info := &MangaInfo{
		Title: map[string]string{"ko": "Jeoldae Gunrim"},
		AltTitles: []map[string]string{
			{"en": "Absolute Dominion"},
			{"en": "Absolute Rule"},
			{"en": "Absolute Dominion"},
		},
	}

	want := []string{"Absolute Dominion", "Absolute Rule"}
	got := info.EnglishTitleCandidates()
	if len(got) != len(want) {
		t.Fatalf("EnglishTitleCandidates() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("EnglishTitleCandidates() = %v, want %v", got, want)
		}
	}
	if got := info.PrimaryEnglishTitle(); got != "" {
		t.Fatalf("unresolved PrimaryEnglishTitle() = %q, want empty", got)
	}
}

func TestSelectedEnglishTitlePersistsAcrossUpsert(t *testing.T) {
	store := tempStore(t)
	info := &MangaInfo{
		ID:        "english-title-selection",
		Title:     map[string]string{"ko": "Jeoldae Gunrim"},
		AltTitles: []map[string]string{{"en": "Absolute Dominion"}, {"en": "Absolute Rule"}},
	}
	if err := store.Upsert(info); err != nil {
		t.Fatalf("initial Upsert: %v", err)
	}

	info.SelectedEnglishTitle = "Absolute Rule"
	if err := store.Upsert(info); err != nil {
		t.Fatalf("store selected title: %v", err)
	}

	refreshed := &MangaInfo{
		ID:        info.ID,
		Title:     map[string]string{"ko": "Jeoldae Gunrim"},
		AltTitles: []map[string]string{{"en": "Absolute Dominion"}, {"en": "Absolute Rule"}},
	}
	if err := store.Upsert(refreshed); err != nil {
		t.Fatalf("refresh Upsert: %v", err)
	}

	got, err := store.LookupByID(info.ID)
	if err != nil {
		t.Fatalf("LookupByID: %v", err)
	}
	if got.SelectedEnglishTitle != "Absolute Rule" || got.PrimaryEnglishTitle() != "Absolute Rule" {
		t.Fatalf("saved title = %q, resolved title = %q; want Absolute Rule", got.SelectedEnglishTitle, got.PrimaryEnglishTitle())
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

func TestChapterStatsRoundTripAndReopen(t *testing.T) {
	store := tempStore(t)
	key := "source-key"
	stats := ChapterStats{
		Title:         "One Piece",
		Site:          "mangadex",
		URL:           "https://mangadex.org/title/a1",
		Total:         12,
		Downloaded:    4,
		NotDownloaded: 8,
		LastRefresh:   "2026-09-26",
	}

	missing, err := store.LookupChapterStats(key)
	if err != nil {
		t.Fatal(err)
	}
	if missing != nil {
		t.Fatalf("expected no initial chapter stats, got %+v", missing)
	}
	if err := store.UpsertChapterStats(key, stats); err != nil {
		t.Fatalf("UpsertChapterStats: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	reopened, err := OpenPath(store.path)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	t.Cleanup(func() { reopened.Close() })
	got, err := reopened.LookupChapterStats(key)
	if err != nil {
		t.Fatalf("LookupChapterStats: %v", err)
	}
	if got == nil || *got != stats {
		t.Fatalf("chapter stats after reopen = %+v, want %+v", got, stats)
	}

	stats.Downloaded = 5
	stats.NotDownloaded = 7
	if err := reopened.UpsertChapterStats(key, stats); err != nil {
		t.Fatalf("update chapter stats: %v", err)
	}
	got, err = reopened.LookupChapterStats(key)
	if err != nil || got == nil || got.Downloaded != 5 || got.NotDownloaded != 7 {
		t.Fatalf("updated chapter stats = %+v, err = %v", got, err)
	}
}

func TestEnsureSchemaMigratesExistingDatabase(t *testing.T) {
	store := tempStore(t)
	if _, err := store.conn.Exec(`DROP TABLE manga_chapter_stats; PRAGMA user_version = 2;`); err != nil {
		t.Fatalf("prepare version 2 database: %v", err)
	}
	if err := store.EnsureSchema(); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	var version int
	if err := store.conn.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if version != 8 {
		t.Fatalf("user_version = %d, want 8", version)
	}

	stats := ChapterStats{Title: "One Piece", Total: 2, NotDownloaded: 2}
	if err := store.UpsertChapterStats("source-key", stats); err != nil {
		t.Fatalf("UpsertChapterStats after migration: %v", err)
	}
}

func TestMigrateLegacyDatabase(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, legacyDBFileName)
	store, err := OpenPath(legacy)
	if err != nil {
		t.Fatalf("open legacy store: %v", err)
	}
	if err := store.Upsert(sampleInfo()); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	stats := ChapterStats{Title: "One Piece", Total: 12, Downloaded: 4, NotDownloaded: 8}
	if err := store.UpsertChapterStats("source-key", stats); err != nil {
		t.Fatalf("UpsertChapterStats: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close legacy store: %v", err)
	}

	newPath := filepath.Join(dir, "kansho.db")
	if err := migrateLegacyDatabase(newPath); err != nil {
		t.Fatalf("migrateLegacyDatabase: %v", err)
	}

	if _, err := os.Stat(newPath); os.IsNotExist(err) {
		t.Fatal("new database was not created")
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatalf("legacy database was not retained: %v", err)
	}

	migrated, err := OpenPath(newPath)
	if err != nil {
		t.Fatalf("open migrated database: %v", err)
	}
	defer migrated.Close()
	got, err := migrated.LookupByID(sampleInfo().ID)
	if err != nil || got == nil {
		t.Fatalf("migrated title lookup: %v (got %v)", err, got)
	}
	gotStats, err := migrated.LookupChapterStats("source-key")
	if err != nil || gotStats == nil || *gotStats != stats {
		t.Fatalf("migrated chapter stats = %+v, err = %v", gotStats, err)
	}
	legacyStore, err := OpenPath(legacy)
	if err != nil {
		t.Fatalf("open retained legacy database: %v", err)
	}
	defer legacyStore.Close()
	got, err = legacyStore.LookupByID(sampleInfo().ID)
	if err != nil || got == nil {
		t.Fatalf("retained legacy title lookup: %v (got %v)", err, got)
	}
}

func TestMigrateLegacyDatabaseSkipsCleanConfig(t *testing.T) {
	dir := t.TempDir()
	newPath := filepath.Join(dir, "kansho.db")
	if err := migrateLegacyDatabase(newPath); err != nil {
		t.Fatalf("migrateLegacyDatabase on clean config: %v", err)
	}
	if _, err := os.Stat(newPath); !os.IsNotExist(err) {
		t.Errorf("new database should not be created without a legacy file")
	}
}

func TestMigrateLegacyDatabaseKeepsLegacyWhenNewExists(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, legacyDBFileName)
	newPath := filepath.Join(dir, "kansho.db")

	legacyStore, err := OpenPath(legacy)
	if err != nil {
		t.Fatalf("open legacy store: %v", err)
	}
	if err := legacyStore.Close(); err != nil {
		t.Fatalf("close legacy store: %v", err)
	}
	newStore, err := OpenPath(newPath)
	if err != nil {
		t.Fatalf("open new store: %v", err)
	}
	if err := newStore.Close(); err != nil {
		t.Fatalf("close new store: %v", err)
	}

	if err := migrateLegacyDatabase(newPath); err != nil {
		t.Fatalf("migrateLegacyDatabase with both files: %v", err)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatalf("legacy database should be left untouched: %v", err)
	}
}

func TestUpsertChapterStatsRejectsInvalidCounts(t *testing.T) {
	store := tempStore(t)
	err := store.UpsertChapterStats("source-key", ChapterStats{
		Total:         3,
		Downloaded:    2,
		NotDownloaded: 2,
	})
	if err == nil {
		t.Fatal("expected invalid chapter counts to be rejected")
	}
}

func TestBackup(t *testing.T) {
	store := tempStore(t)
	if err := store.Upsert(sampleInfo()); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	stats := ChapterStats{Title: "One Piece", Total: 12, Downloaded: 4, NotDownloaded: 8}
	if err := store.UpsertChapterStats("source-key", stats); err != nil {
		t.Fatalf("UpsertChapterStats: %v", err)
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
	statsGot, err := backup.LookupChapterStats("source-key")
	if err != nil || statsGot == nil || *statsGot != stats {
		t.Fatalf("chapter stats in backup = %+v, err = %v", statsGot, err)
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
	stats := ChapterStats{Title: "One Piece", Total: 12, Downloaded: 4, NotDownloaded: 8}
	if err := store.UpsertChapterStats("source-key", stats); err != nil {
		t.Fatalf("UpsertChapterStats: %v", err)
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
	statsGot, err := store.LookupChapterStats("source-key")
	if err != nil || statsGot == nil || *statsGot != stats {
		t.Fatalf("restored chapter stats = %+v, err = %v", statsGot, err)
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

func TestBookmarksRoundTripAndReplace(t *testing.T) {
	store := tempStore(t)

	first := models.Manga{Manga: []models.Bookmarks{
		{Title: "One Piece", Url: "https://example.test/one", Site: "Example", Location: "/tmp/one"},
		{Title: "Berserk", Url: "https://example.test/berserk", Site: "Example", Location: "/tmp/berserk"},
	}}
	if err := store.SaveBookmarks(first); err != nil {
		t.Fatalf("SaveBookmarks: %v", err)
	}

	loaded, err := store.LoadBookmarks()
	if err != nil {
		t.Fatalf("LoadBookmarks: %v", err)
	}
	if len(loaded.Manga) != 2 {
		t.Fatalf("loaded %d bookmarks, want 2", len(loaded.Manga))
	}

	second := models.Manga{Manga: []models.Bookmarks{{Title: "Berserk"}}}
	if err := store.SaveBookmarks(second); err != nil {
		t.Fatalf("SaveBookmarks replace: %v", err)
	}
	loaded, err = store.LoadBookmarks()
	if err != nil {
		t.Fatalf("LoadBookmarks after replace: %v", err)
	}
	if len(loaded.Manga) != 1 || loaded.Manga[0].Title != "Berserk" {
		t.Fatalf("bookmarks after replace = %+v", loaded.Manga)
	}
}

func TestApplyRootToBookmarksByPlatformAndType(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if err := preferences.Save(preferences.Preferences{DatabaseDirectory: filepath.Join(home, "db")}); err != nil {
		t.Fatalf("save isolated preferences: %v", err)
	}
	path := filepath.Join(t.TempDir(), "roots.db")
	linux, err := openPathForPlatform(path, "linux")
	if err != nil {
		t.Fatalf("open Linux store: %v", err)
	}
	defer linux.Close()
	windows, err := openPathForPlatform(path, "windows")
	if err != nil {
		t.Fatalf("open Windows store: %v", err)
	}
	defer windows.Close()

	if err := linux.SaveBookmarks(models.Manga{Manga: []models.Bookmarks{
		{Title: "Manga One", Type: "manga", Location: "/old/manga"},
		{Title: "Webtoon One", Type: "webtoon", Location: "/old/webtoon"},
	}}); err != nil {
		t.Fatalf("save Linux bookmarks: %v", err)
	}

	updated, err := linux.ApplyRootToBookmarks("linux", "manga", "/mnt/library/manga")
	if err != nil || updated != 1 {
		t.Fatalf("ApplyRootToBookmarks() = %d, %v; want 1, nil", updated, err)
	}
	linuxRows, err := linux.LoadBookmarks()
	if err != nil {
		t.Fatalf("load Linux bookmarks: %v", err)
	}
	if len(linuxRows.Manga) != 2 || linuxRows.Manga[0].Location != "/mnt/library/manga/Manga One" || linuxRows.Manga[1].Location != "/old/webtoon" {
		t.Fatalf("Linux locations after root update = %+v", linuxRows.Manga)
	}
	windowsRows, err := windows.LoadBookmarks()
	if err != nil || len(windowsRows.Manga) != 2 || windowsRows.Manga[0].Location != "" {
		t.Fatalf("Windows rows changed after Linux root update: %+v, %v", windowsRows.Manga, err)
	}
}

func TestSetBookmarkMangaDexIDOnlyUpdatesCurrentPlatform(t *testing.T) {
	path := filepath.Join(t.TempDir(), "links.db")
	linux, err := openPathForPlatform(path, "linux")
	if err != nil {
		t.Fatalf("open Linux store: %v", err)
	}
	defer linux.Close()
	windows, err := openPathForPlatform(path, "windows")
	if err != nil {
		t.Fatalf("open Windows store: %v", err)
	}
	defer windows.Close()
	bookmarks := models.Manga{Manga: []models.Bookmarks{{
		Title: "Confirmed Title", Url: "https://mangadex.org/title/confirmed-id",
	}}}
	if err := linux.SaveBookmarks(bookmarks); err != nil {
		t.Fatalf("save Linux bookmark: %v", err)
	}
	if err := windows.SaveBookmarks(bookmarks); err != nil {
		t.Fatalf("save Windows bookmark: %v", err)
	}
	linuxRows, err := linux.LoadBookmarks()
	if err != nil || linuxRows.Manga[0].MangaDexID != "" {
		t.Fatalf("MangaDex URL alone assigned an ID: %+v, %v", linuxRows.Manga, err)
	}
	if err := linux.SetBookmarkMangaDexID("Confirmed Title", "confirmed-id"); err != nil {
		t.Fatalf("SetBookmarkMangaDexID: %v", err)
	}

	linuxRows, err = linux.LoadBookmarks()
	if err != nil || linuxRows.Manga[0].MangaDexID != "confirmed-id" {
		t.Fatalf("Linux bookmark link = %+v, %v", linuxRows.Manga, err)
	}
	windowsRows, err := windows.LoadBookmarks()
	if err != nil || windowsRows.Manga[0].MangaDexID != "" {
		t.Fatalf("Windows bookmark changed before sync: %+v, %v", windowsRows.Manga, err)
	}
}

func TestSyncPlatformBookmarksSharesTitlesAndPreservesLocations(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if err := preferences.Save(preferences.Preferences{
		DatabaseDirectory:  filepath.Join(home, "db"),
		WindowsMangaRoot:   `D:\Manga`,
		WindowsWebtoonRoot: `D:\Webtoons`,
		LinuxMangaRoot:     "/mnt/manga",
		LinuxWebtoonRoot:   "/mnt/webtoons",
	}); err != nil {
		t.Fatalf("save preferences: %v", err)
	}

	path := filepath.Join(t.TempDir(), "sync.db")
	linux, err := openPathForPlatform(path, "linux")
	if err != nil {
		t.Fatalf("open Linux store: %v", err)
	}
	defer linux.Close()
	windows, err := openPathForPlatform(path, "windows")
	if err != nil {
		t.Fatalf("open Windows store: %v", err)
	}
	defer windows.Close()

	if err := linux.SaveBookmarks(models.Manga{Manga: []models.Bookmarks{
		{Title: "Linux Only", Type: "manga", Location: "/linux/source", MangaDexID: "id-linux"},
		{Title: "Shared Linux", Type: "manga", Location: "/linux/shared", MangaDexID: "id-shared"},
		{Title: "Unlinked Linux", Url: "https://example.test/linux", Site: "Example", Type: "webtoon", Location: "/linux/no-id"},
	}}); err != nil {
		t.Fatalf("save Linux bookmarks: %v", err)
	}
	windowsBookmarks, err := windows.LoadBookmarks()
	if err != nil {
		t.Fatalf("load Windows bookmarks before adding: %v", err)
	}
	windowsBookmarks.Manga = append(windowsBookmarks.Manga,
		models.Bookmarks{Title: "Windows Only", Type: "webtoon", Location: `C:\windows\source`, MangaDexID: "id-windows"},
		models.Bookmarks{Title: "Unlinked Windows", Url: "https://example.test/windows", Site: "Example", Type: "manga", Location: `C:\windows\no-id`},
	)
	for i := range windowsBookmarks.Manga {
		if windowsBookmarks.Manga[i].MangaDexID == "id-shared" {
			windowsBookmarks.Manga[i].Title = "Shared Windows"
			windowsBookmarks.Manga[i].Location = `C:\windows\shared`
		}
	}
	if err := windows.SaveBookmarks(windowsBookmarks); err != nil {
		t.Fatalf("save Windows bookmarks: %v", err)
	}

	result, err := linux.SyncPlatformBookmarks()
	if err != nil {
		t.Fatalf("SyncPlatformBookmarks: %v", err)
	}
	if result.InsertedToLinux != 0 || result.InsertedToWindows != 0 || len(result.Skipped) != 0 {
		t.Fatalf("sync result = %+v", result)
	}
	linuxRows, err := linux.LoadBookmarks()
	if err != nil {
		t.Fatalf("load Linux after sync: %v", err)
	}
	windowsRows, err := windows.LoadBookmarks()
	if err != nil {
		t.Fatalf("load Windows after sync: %v", err)
	}
	if len(linuxRows.Manga) != 5 || len(windowsRows.Manga) != 5 {
		t.Fatalf("row counts after sync: Linux=%d Windows=%d", len(linuxRows.Manga), len(windowsRows.Manga))
	}
	linuxByID := make(map[string]models.Bookmarks)
	windowsByID := make(map[string]models.Bookmarks)
	for _, bookmark := range linuxRows.Manga {
		linuxByID[bookmark.MangaDexID] = bookmark
	}
	for _, bookmark := range windowsRows.Manga {
		windowsByID[bookmark.MangaDexID] = bookmark
	}
	if linuxByID["id-linux"].Title != windowsByID["id-linux"].Title || windowsByID["id-linux"].Location != `D:\Manga\Linux Only` {
		t.Fatalf("Linux title was not shared with a Windows-specific root: Linux=%+v Windows=%+v", linuxByID["id-linux"], windowsByID["id-linux"])
	}
	if linuxByID["id-windows"].Title != windowsByID["id-windows"].Title || linuxByID["id-windows"].Location != "/mnt/webtoons/Windows Only" {
		t.Fatalf("Windows title was not shared with a Linux-specific root: Linux=%+v Windows=%+v", linuxByID["id-windows"], windowsByID["id-windows"])
	}
	if linuxByID["id-shared"].Title != "Shared Windows" || windowsByID["id-shared"].Title != "Shared Windows" ||
		linuxByID["id-shared"].Location != "/linux/shared" || windowsByID["id-shared"].Location != `C:\windows\shared` {
		t.Fatalf("sync should share title metadata but preserve per-OS locations: Linux=%+v Windows=%+v", linuxByID["id-shared"], windowsByID["id-shared"])
	}
	for _, title := range []string{"Unlinked Linux", "Unlinked Windows"} {
		var linuxFound, windowsFound bool
		for _, bookmark := range linuxRows.Manga {
			linuxFound = linuxFound || bookmark.Title == title
		}
		for _, bookmark := range windowsRows.Manga {
			windowsFound = windowsFound || bookmark.Title == title
		}
		if !linuxFound || !windowsFound {
			t.Fatalf("unlinked title %q missing from both tables: Linux=%+v Windows=%+v", title, linuxRows.Manga, windowsRows.Manga)
		}
	}
}

func TestLegacyBookmarksMigrationRenamesFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	configDir := filepath.Join(home, ".config", "kansho")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("create config dir: %v", err)
	}
	legacy := filepath.Join(configDir, "bookmarks.json")
	if err := os.WriteFile(legacy, []byte(`{"manga":[{"title":"Migrated","url":"u","chapters":"","location":"","site":"s","shortname":""}]}`), 0644); err != nil {
		t.Fatalf("write legacy bookmarks: %v", err)
	}

	store, err := Open()
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	loaded, err := store.LoadBookmarks()
	if err != nil {
		t.Fatalf("LoadBookmarks: %v", err)
	}
	if len(loaded.Manga) != 1 || loaded.Manga[0].Title != "Migrated" {
		t.Fatalf("migrated bookmarks = %+v", loaded.Manga)
	}
	for _, platform := range []string{"linux", "windows"} {
		platformBookmarks, err := store.LoadPlatformBookmarks(platform)
		if err != nil || len(platformBookmarks.Manga) != 1 || platformBookmarks.Manga[0].Title != "Migrated" {
			t.Fatalf("%s bookmarks after open sync = %+v, %v", platform, platformBookmarks.Manga, err)
		}
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy bookmarks file still exists: %v", err)
	}
	if _, err := os.Stat(legacy + ".bak"); err != nil {
		t.Fatalf("legacy bookmarks backup missing: %v", err)
	}
}

func TestWriteLockBlocksWritesAndCanBeCleared(t *testing.T) {
	store := tempStore(t)
	owner, err := store.acquireWriteLock()
	if err != nil {
		t.Fatalf("acquireWriteLock: %v", err)
	}
	defer store.releaseWriteLock(owner)

	err = store.SaveBookmarks(models.Manga{Manga: []models.Bookmarks{{Title: "Blocked"}}})
	if err == nil {
		t.Fatal("SaveBookmarks succeeded while write lock was held")
	}
	if _, ok := err.(*LockedError); !ok {
		t.Fatalf("SaveBookmarks error = %T %v, want *LockedError", err, err)
	}

	count, err := store.LockCount()
	if err != nil || count != 1 {
		t.Fatalf("LockCount = %d, %v; want 1, nil", count, err)
	}
	cleared, err := store.ClearLocks()
	if err != nil || cleared != 1 {
		t.Fatalf("ClearLocks = %d, %v; want 1, nil", cleared, err)
	}

	if err := store.SaveBookmarks(models.Manga{Manga: []models.Bookmarks{{Title: "Allowed"}}}); err != nil {
		t.Fatalf("SaveBookmarks after ClearLocks: %v", err)
	}
}

func TestCompareAndMergeMissingRows(t *testing.T) {
	source := tempStore(t)
	targetPath := filepath.Join(t.TempDir(), "target.db")
	target, err := OpenPath(targetPath)
	if err != nil {
		t.Fatalf("open target: %v", err)
	}
	defer target.Close()

	sourceBookmarks := models.Manga{Manga: []models.Bookmarks{
		{Title: "Shared", Url: "u1", Site: "s"},
		{Title: "Only In Source", Url: "u2", Site: "s"},
	}}
	if err := source.SaveBookmarks(sourceBookmarks); err != nil {
		t.Fatalf("source SaveBookmarks: %v", err)
	}
	targetBookmarks := models.Manga{Manga: []models.Bookmarks{
		{Title: "Shared", Url: "u1", Site: "s"},
		{Title: "Only In Target", Url: "u3", Site: "s"},
	}}
	if err := target.SaveBookmarks(targetBookmarks); err != nil {
		t.Fatalf("target SaveBookmarks: %v", err)
	}

	diff, err := target.CompareWith(source.Path())
	if err != nil {
		t.Fatalf("CompareWith: %v", err)
	}
	if diff.TotalMissing != 2 {
		t.Fatalf("TotalMissing = %d, want 2 platform rows (%+v)", diff.TotalMissing, diff)
	}

	result, err := target.MergeMissingFrom(source.Path(), nil)
	if err != nil {
		t.Fatalf("MergeMissingFrom: %v", err)
	}
	if result.TotalInserted != 2 {
		t.Fatalf("TotalInserted = %d, want 2 platform rows", result.TotalInserted)
	}
	if _, err := os.Stat(result.BackupPath); err != nil {
		t.Fatalf("target backup missing: %v", err)
	}

	merged, err := target.LoadBookmarks()
	if err != nil {
		t.Fatalf("LoadBookmarks after merge: %v", err)
	}
	if len(merged.Manga) != 3 {
		t.Fatalf("merged bookmarks = %+v, want 3 rows", merged.Manga)
	}
}

func TestMergeLegacyBookmarksIntoActivePlatformTable(t *testing.T) {
	source := tempStore(t)
	if _, err := source.conn.Exec(`DROP TABLE bookmarks_linux; DROP TABLE bookmarks_windows`); err != nil {
		t.Fatalf("remove platform bookmark tables: %v", err)
	}
	if _, err := source.conn.Exec(`INSERT INTO bookmarks (title, url, location, site)
		VALUES ('Legacy Merge', 'https://example.test/legacy', '/old/location', 'Example')`); err != nil {
		t.Fatalf("insert legacy bookmark: %v", err)
	}
	target := tempStore(t)
	if _, err := target.MergeMissingFrom(source.Path(), nil); err != nil {
		t.Fatalf("MergeMissingFrom legacy database: %v", err)
	}
	loaded, err := target.LoadBookmarks()
	if err != nil {
		t.Fatalf("LoadBookmarks after merge: %v", err)
	}
	if len(loaded.Manga) != 1 || loaded.Manga[0].Title != "Legacy Merge" {
		t.Fatalf("active platform bookmarks after legacy merge = %+v", loaded.Manga)
	}
}
