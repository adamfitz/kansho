package mangadex

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "modernc.org/sqlite"

	"kansho/parser"
)

// dbFileName is the name of the SQLite database inside the kansho config
// directory. It stores MangaDex title information and generic per-manga
// chapter count snapshots; Kansho's bookmarks live separately in bookmarks.json.
const dbFileName = "kansho.db"

// legacyDBFileName is the old database file name used before the database
// became a general Kansho store. Open migrates it to dbFileName once so
// existing local title data and chapter stats are not lost.
const legacyDBFileName = "mangadex.db"

// PreRestoreFileName is the file the current database is preserved as just
// before a restore swaps it out for the user's backup.
const PreRestoreFileName = "kansho.pre-restore.db"

// GetStore returns the process-wide default local database, opening it (and its
// schema) on first use.
func GetStore() (*Store, error) {
	defaultStoreOnce.Do(func() {
		defaultStore, defaultStoreErr = Open()
	})
	return defaultStore, defaultStoreErr
}

var (
	defaultStoreOnce sync.Once
	defaultStore     *Store
	defaultStoreErr  error
)

// Store is the local SQLite database of MangaDex title information and generic
// manga chapter count snapshots. It is stored at ~/.config/kansho/kansho.db
// and is completely separate from the bookmarks JSON.
type Store struct {
	conn *sql.DB
	path string
}

// DefaultDBPath returns the database path inside the kansho config directory.
func DefaultDBPath() (string, error) {
	dir, err := parser.ExpandPath("~/.config/kansho")
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, dbFileName), nil
}

// migrateLegacyDatabase moves an existing mangadex.db to kansho.db on first
// startup after the rename so existing local title data and chapter counts are
// not lost. It only acts when the legacy file exists and the new one does not.
func migrateLegacyDatabase(newPath string) error {
	legacyPath := filepath.Join(filepath.Dir(newPath), legacyDBFileName)
	if _, err := os.Stat(legacyPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if _, err := os.Stat(newPath); err == nil {
		return nil // new database already in place; leave the legacy file alone
	} else if !os.IsNotExist(err) {
		return err
	}

	if err := ValidateSchema(legacyPath); err != nil {
		log.Printf("[mangadex] skip migration of %s: not a Kansho database (%v)", legacyPath, err)
		return nil
	}

	conn, err := sql.Open("sqlite", legacyPath+"?_busy_timeout=5000")
	if err != nil {
		return fmt.Errorf("open legacy database: %w", err)
	}
	if err := conn.Ping(); err != nil {
		conn.Close()
		return fmt.Errorf("ping legacy database: %w", err)
	}
	quoted := strings.ReplaceAll(newPath, "'", "''")
	if _, err := conn.Exec(fmt.Sprintf("VACUUM INTO '%s'", quoted)); err != nil {
		conn.Close()
		return fmt.Errorf("migrate legacy database: %w", err)
	}
	if err := conn.Close(); err != nil {
		return fmt.Errorf("close legacy database: %w", err)
	}
	if err := ValidateSchema(newPath); err != nil {
		return fmt.Errorf("migrated database failed validation: %w", err)
	}

	for _, p := range []string{legacyPath, legacyPath + "-wal", legacyPath + "-shm"} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove legacy database %s: %w", p, err)
		}
	}
	return nil
}

// Open opens (or creates) the local database at the default location and
// ensures the schema exists. A legacy mangadex.db is migrated to kansho.db
// automatically on first use.
func Open() (*Store, error) {
	path, err := DefaultDBPath()
	if err != nil {
		return nil, fmt.Errorf("kansho db path: %w", err)
	}
	if err := migrateLegacyDatabase(path); err != nil {
		return nil, fmt.Errorf("migrate legacy database: %w", err)
	}
	return OpenPath(path)
}

// OpenPath opens (or creates) the database at an explicit path.
func OpenPath(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("create kansho database directory: %w", err)
	}

	conn, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("open Kansho database: %w", err)
	}
	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("ping Kansho database: %w", err)
	}

	s := &Store{conn: conn, path: path}
	if err := s.EnsureSchema(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("ensure Kansho schema: %w", err)
	}
	return s, nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	return s.conn.Close()
}

// Path returns the database file path.
func (s *Store) Path() string {
	return s.path
}

// EnsureSchema creates the title, alias, and chapter statistic tables and their
// indexes if they do not exist.
func (s *Store) EnsureSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS manga_title (
		id                     INTEGER PRIMARY KEY AUTOINCREMENT,
		mangadex_id            TEXT NOT NULL UNIQUE,
		primary_title          TEXT NOT NULL,
		title_json             TEXT NOT NULL DEFAULT '{}',
		alt_titles_json        TEXT NOT NULL DEFAULT '[]',
		description_json       TEXT NOT NULL DEFAULT '{}',
		original_language      TEXT NOT NULL DEFAULT '',
		publication_demographic TEXT NOT NULL DEFAULT '',
		year                   INTEGER,
		content_rating         TEXT NOT NULL DEFAULT '',
		status                 TEXT NOT NULL DEFAULT '',
		tags_json              TEXT NOT NULL DEFAULT '[]',
		author                 TEXT NOT NULL DEFAULT '',
		artist                 TEXT NOT NULL DEFAULT '',
		cover_url              TEXT NOT NULL DEFAULT '',
		url                    TEXT NOT NULL DEFAULT '',
		updated_at             DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_manga_title_primary
		ON manga_title (primary_title);
	CREATE INDEX IF NOT EXISTS idx_manga_title_updated
		ON manga_title (updated_at);
	CREATE TABLE IF NOT EXISTS manga_title_lookup (
		lookup_title TEXT PRIMARY KEY,
		mangadex_id  TEXT NOT NULL,
		updated_at   DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_manga_title_lookup_id
		ON manga_title_lookup (mangadex_id);
	CREATE TABLE IF NOT EXISTS manga_chapter_stats (
		manga_key             TEXT PRIMARY KEY,
		title                 TEXT NOT NULL DEFAULT '',
		site                  TEXT NOT NULL DEFAULT '',
		url                   TEXT NOT NULL DEFAULT '',
		total_chapters         INTEGER NOT NULL CHECK (total_chapters >= 0),
		downloaded_chapters    INTEGER NOT NULL CHECK (downloaded_chapters >= 0),
		not_downloaded_chapters INTEGER NOT NULL CHECK (not_downloaded_chapters >= 0),
		updated_at             DATETIME DEFAULT CURRENT_TIMESTAMP,
		CHECK (downloaded_chapters <= total_chapters),
		CHECK (downloaded_chapters + not_downloaded_chapters = total_chapters)
	);
	CREATE INDEX IF NOT EXISTS idx_manga_chapter_stats_title
		ON manga_chapter_stats (title);
	PRAGMA user_version = 3;
	`
	_, err := s.conn.Exec(schema)
	return err
}

type ChapterStats struct {
	Title         string
	Site          string
	URL           string
	Total         int
	Downloaded    int
	NotDownloaded int
}

// rowMeta is the persistent form of a MangaInfo record inside the database.
type rowMeta struct {
	ID                     int64
	MangaDexID             string
	PrimaryTitle           string
	TitleJSON              string
	AltTitlesJSON          string
	DescriptionJSON        string
	OriginalLanguage       string
	PublicationDemographic string
	Year                   *int
	ContentRating          string
	Status                 string
	TagsJSON               string
	Author                 string
	Artist                 string
	CoverURL               string
	URL                    string
}

const rowColumns = `id, mangadex_id, primary_title, title_json, alt_titles_json,
	description_json, original_language, publication_demographic, year, content_rating,
	status, tags_json, author, artist, cover_url, url`

func scanRow(row interface {
	Scan(dest ...interface{}) error
}) (*rowMeta, error) {
	var m rowMeta
	if err := row.Scan(&m.ID, &m.MangaDexID, &m.PrimaryTitle, &m.TitleJSON,
		&m.AltTitlesJSON, &m.DescriptionJSON, &m.OriginalLanguage,
		&m.PublicationDemographic, &m.Year, &m.ContentRating, &m.Status,
		&m.TagsJSON, &m.Author, &m.Artist, &m.CoverURL, &m.URL); err != nil {
		return nil, err
	}
	return &m, nil
}

func rowToInfo(m *rowMeta) (*MangaInfo, error) {
	info := &MangaInfo{
		ID:                     m.MangaDexID,
		OriginalLanguage:       m.OriginalLanguage,
		PublicationDemographic: m.PublicationDemographic,
		Year:                   m.Year,
		ContentRating:          m.ContentRating,
		Status:                 m.Status,
		Author:                 m.Author,
		Artist:                 m.Artist,
		CoverURL:               m.CoverURL,
		URL:                    m.URL,
		Title:                  map[string]string{},
		Description:            map[string]string{},
		AltTitles:              []map[string]string{},
		Tags:                   []TagInfo{},
	}
	_ = json.Unmarshal([]byte(m.TitleJSON), &info.Title)
	_ = json.Unmarshal([]byte(m.AltTitlesJSON), &info.AltTitles)
	_ = json.Unmarshal([]byte(m.DescriptionJSON), &info.Description)
	_ = json.Unmarshal([]byte(m.TagsJSON), &info.Tags)
	if info.Title == nil {
		info.Title = map[string]string{}
	}
	if info.Description == nil {
		info.Description = map[string]string{}
	}
	return info, nil
}

// marshalledInfo converts a MangaInfo into its persistent row form.
func marshalledInfo(info *MangaInfo) *rowMeta {
	titleJSON, _ := json.Marshal(info.Title)
	altJSON, _ := json.Marshal(info.AltTitles)
	descJSON, _ := json.Marshal(info.Description)
	tagsJSON, _ := json.Marshal(info.Tags)
	return &rowMeta{
		MangaDexID:             info.ID,
		PrimaryTitle:           info.EnglishTitle(),
		TitleJSON:              string(titleJSON),
		AltTitlesJSON:          string(altJSON),
		DescriptionJSON:        string(descJSON),
		OriginalLanguage:       info.OriginalLanguage,
		PublicationDemographic: info.PublicationDemographic,
		Year:                   info.Year,
		ContentRating:          info.ContentRating,
		Status:                 info.Status,
		TagsJSON:               string(tagsJSON),
		Author:                 info.Author,
		Artist:                 info.Artist,
		CoverURL:               info.CoverURL,
		URL:                    info.URL,
	}
}

// SetTitleAlias records that a bookmarked manga title corresponds to a given
// MangaDex title ID. The mapping lets later lookups by the exact bookmarked
// title succeed even when that title differs from the stored primary (English)
// title, so a title resolved once is always served from the local database
// afterwards.
func (s *Store) SetTitleAlias(title, id string) error {
	title = strings.TrimSpace(title)
	if title == "" || id == "" {
		return fmt.Errorf("cannot alias an empty title or id")
	}
	_, err := s.conn.Exec(`INSERT INTO manga_title_lookup (lookup_title, mangadex_id, updated_at)
		VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(lookup_title) DO UPDATE SET
			mangadex_id = excluded.mangadex_id,
			updated_at = CURRENT_TIMESTAMP`, title, id)
	if err != nil {
		return fmt.Errorf("store title alias %q -> %s: %w", title, id, err)
	}
	return nil
}

// LookupByTitle finds the best-matching local MangaDex record for a manga
// title. It matches case-insensitively against the exact bookmarked title map
// first, then the primary (English) title, then falls back to any stored title /
// alt title. Returns nil when no local record matches.
func (s *Store) LookupByTitle(title string) (*MangaInfo, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, fmt.Errorf("empty title for mangadex lookup")
	}

	var id string
	err := s.conn.QueryRow(`SELECT mangadex_id FROM manga_title_lookup
		WHERE lookup_title = ? COLLATE NOCASE`, title).Scan(&id)
	if err == nil {
		return s.LookupByID(id)
	}
	if err != sql.ErrNoRows {
		return nil, fmt.Errorf("lookup %q in mangadex alias table: %w", title, err)
	}

	query := fmt.Sprintf(`SELECT %s FROM manga_title
		WHERE lower(primary_title) = lower(?) OR lower(title_json) LIKE lower(?)
		   OR lower(alt_titles_json) LIKE lower(?)
		ORDER BY CASE WHEN lower(primary_title) = lower(?) THEN 0 ELSE 1 END
		LIMIT 1`, rowColumns)
	like := "%" + title + "%"
	row := s.conn.QueryRow(query, title, like, like, title)

	m, err := scanRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lookup %q in mangadex db: %w", title, err)
	}
	return rowToInfo(m)
}

// LookupByID finds a local record by its MangaDex manga ID.
func (s *Store) LookupByID(id string) (*MangaInfo, error) {
	query := fmt.Sprintf(`SELECT %s FROM manga_title WHERE mangadex_id = ?`, rowColumns)
	row := s.conn.QueryRow(query, id)

	m, err := scanRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lookup mangadex id %s: %w", id, err)
	}
	return rowToInfo(m)
}

// Upsert stores (or updates) MangaDex title information keyed by manga ID.
func (s *Store) Upsert(info *MangaInfo) error {
	if info == nil || info.ID == "" {
		return fmt.Errorf("cannot store mangadex info without an ID")
	}
	m := marshalledInfo(info)

	_, err := s.conn.Exec(`INSERT INTO manga_title
		(mangadex_id, primary_title, title_json, alt_titles_json, description_json,
		 original_language, publication_demographic, year, content_rating, status,
		 tags_json, author, artist, cover_url, url, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(mangadex_id) DO UPDATE SET
			primary_title = excluded.primary_title,
			title_json = excluded.title_json,
			alt_titles_json = excluded.alt_titles_json,
			description_json = excluded.description_json,
			original_language = excluded.original_language,
			publication_demographic = excluded.publication_demographic,
			year = excluded.year,
			content_rating = excluded.content_rating,
			status = excluded.status,
			tags_json = excluded.tags_json,
			author = excluded.author,
			artist = excluded.artist,
			cover_url = excluded.cover_url,
			url = excluded.url,
			updated_at = CURRENT_TIMESTAMP`,
		m.MangaDexID, m.PrimaryTitle, m.TitleJSON, m.AltTitlesJSON, m.DescriptionJSON,
		m.OriginalLanguage, m.PublicationDemographic, m.Year, m.ContentRating, m.Status,
		m.TagsJSON, m.Author, m.Artist, m.CoverURL, m.URL)
	if err != nil {
		return fmt.Errorf("upsert mangadex info %s: %w", info.ID, err)
	}
	return nil
}

func (s *Store) UpsertChapterStats(key string, stats ChapterStats) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("cannot store chapter stats without a manga key")
	}
	if stats.Total < 0 || stats.Downloaded < 0 || stats.NotDownloaded < 0 {
		return fmt.Errorf("chapter counts cannot be negative")
	}
	if stats.Downloaded > stats.Total || stats.Downloaded+stats.NotDownloaded != stats.Total {
		return fmt.Errorf("chapter counts must add up to the total")
	}

	_, err := s.conn.Exec(`INSERT INTO manga_chapter_stats
		(manga_key, title, site, url, total_chapters, downloaded_chapters,
		 not_downloaded_chapters, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(manga_key) DO UPDATE SET
			title = excluded.title,
			site = excluded.site,
			url = excluded.url,
			total_chapters = excluded.total_chapters,
			downloaded_chapters = excluded.downloaded_chapters,
			not_downloaded_chapters = excluded.not_downloaded_chapters,
			updated_at = CURRENT_TIMESTAMP`,
		key, stats.Title, stats.Site, stats.URL, stats.Total,
		stats.Downloaded, stats.NotDownloaded)
	if err != nil {
		return fmt.Errorf("store chapter stats for %s: %w", key, err)
	}
	return nil
}

func (s *Store) LookupChapterStats(key string) (*ChapterStats, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, nil
	}

	var stats ChapterStats
	err := s.conn.QueryRow(`SELECT title, site, url, total_chapters,
		downloaded_chapters, not_downloaded_chapters
		FROM manga_chapter_stats WHERE manga_key = ?`, key).Scan(
		&stats.Title,
		&stats.Site,
		&stats.URL,
		&stats.Total,
		&stats.Downloaded,
		&stats.NotDownloaded,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lookup chapter stats for %s: %w", key, err)
	}
	return &stats, nil
}

// Count returns the number of stored title records.
func (s *Store) Count() (int, error) {
	var n int
	err := s.conn.QueryRow(`SELECT COUNT(*) FROM manga_title`).Scan(&n)
	return n, err
}

// Backup writes a consistent snapshot of the database to dest using
// SQLite's VACUUM INTO.
func (s *Store) Backup(dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return fmt.Errorf("create backup directory: %w", err)
	}
	quoted := strings.ReplaceAll(dest, "'", "''")
	if _, err := s.conn.Exec(fmt.Sprintf("VACUUM INTO '%s'", quoted)); err != nil {
		return fmt.Errorf("backup Kansho database: %w", err)
	}
	return nil
}

// IntegrityCheck runs PRAGMA integrity_check and returns its result.
func (s *Store) IntegrityCheck() (string, error) {
	var result string
	err := s.conn.QueryRow("PRAGMA integrity_check").Scan(&result)
	if err != nil {
		return "", fmt.Errorf("integrity check: %w", err)
	}
	return result, nil
}

// Compact optimizes and shrinks the database file.
func (s *Store) Compact() error {
	if _, err := s.conn.Exec("PRAGMA optimize"); err != nil {
		return fmt.Errorf("optimize Kansho database: %w", err)
	}
	if _, err := s.conn.Exec("VACUUM"); err != nil {
		return fmt.Errorf("vacuum Kansho database: %w", err)
	}
	return nil
}

// ValidateSchema checks that a SQLite file has the expected manga_title schema
// and passes an integrity check. This is used before a restore to avoid
// importing a corrupt or incompatible file.
func ValidateSchema(dbPath string) error {
	conn, err := sql.Open("sqlite", dbPath+"?mode=ro&_busy_timeout=5000")
	if err != nil {
		return fmt.Errorf("open candidate database: %w", err)
	}
	defer conn.Close()

	var integ string
	if err := conn.QueryRow("PRAGMA integrity_check").Scan(&integ); err != nil || integ != "ok" {
		if err == nil {
			err = fmt.Errorf("integrity check returned %q", integ)
		}
		return fmt.Errorf("candidate database failed integrity check: %w", err)
	}

	rows, err := conn.Query("PRAGMA table_info(manga_title)")
	if err != nil {
		return fmt.Errorf("candidate database has no manga_title table: %w", err)
	}
	defer rows.Close()

	expected := map[string]bool{
		"primary_title": false, "title_json": false, "alt_titles_json": false,
		"description_json": false, "mangadex_id": false,
	}
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt interface{}
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return fmt.Errorf("read candidate schema: %w", err)
		}
		if _, ok := expected[name]; ok {
			expected[name] = true
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read candidate schema: %w", err)
	}

	for col, present := range expected {
		if !present {
			return fmt.Errorf("candidate database is missing column %q in manga_title (incompatible schema)", col)
		}
	}
	return nil
}

// Restore replaces the current database with the file at src. The source must
// pass schema validation and an integrity check first. The current database is
// backed up to a timestamped file inside the config directory before the swap
// so a bad restore is never destructive.
func (s *Store) Restore(src string) error {
	if err := ValidateSchema(src); err != nil {
		return fmt.Errorf("restore rejected: %w", err)
	}

	safetyBackup := filepath.Join(filepath.Dir(s.path), PreRestoreFileName)
	_ = s.Backup(safetyBackup)

	tempPath := s.path + ".restore"
	if err := os.MkdirAll(filepath.Dir(tempPath), 0755); err != nil {
		return fmt.Errorf("prepare restore directory: %w", err)
	}
	if err := s.conn.Close(); err != nil {
		return fmt.Errorf("close current database: %w", err)
	}
	defer func() {
		s.conn, _ = sql.Open("sqlite", s.path+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on")
		_ = s.conn.Ping()
	}()

	srcContent, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read restore source: %w", err)
	}
	if err := os.RemoveAll(tempPath); err != nil {
		return fmt.Errorf("clear temp restore path: %w", err)
	}
	if err := os.WriteFile(tempPath, srcContent, 0644); err != nil {
		return fmt.Errorf("stage restored database: %w", err)
	}
	if err := os.Rename(tempPath, s.path); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("install restored database: %w", err)
	}

	// Re-open and validate the restored database.
	s.conn, err = sql.Open("sqlite", s.path+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on")
	if err != nil {
		return fmt.Errorf("reopen restored database: %w", err)
	}
	if err := s.conn.Ping(); err != nil {
		return fmt.Errorf("ping restored database: %w", err)
	}
	result, err := s.IntegrityCheck()
	if err != nil {
		return fmt.Errorf("validate restored database: %w", err)
	}
	if result != "ok" {
		return fmt.Errorf("restored database failed integrity check: %s", result)
	}
	// Keep older backups usable: add any newer tables (e.g. the title alias
	// table) that the restored file may be missing.
	if err := s.EnsureSchema(); err != nil {
		return fmt.Errorf("upgrade restored database schema: %w", err)
	}
	return nil
}
