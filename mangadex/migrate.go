package mangadex

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// TableDiff describes the rows present in the source database but missing from
// the target database.
type TableDiff struct {
	Name         string
	SourceCount  int
	TargetCount  int
	MissingCount int
	Missing      []string
}

// DatabaseDiff is the comparison shown to the user before a database merge.
type DatabaseDiff struct {
	Tables       []TableDiff
	TotalMissing int
}

// MergeTableResult reports how many rows were copied for one table.
type MergeTableResult struct {
	Name     string
	Inserted int64
}

// MergeResult summarises a database merge.
type MergeResult struct {
	Tables        []MergeTableResult
	TotalInserted int64
	BackupPath    string
}

type tableSpec struct {
	name         string
	sourceTable  string
	sourceCount  string
	targetCount  string
	missingQuery string
	insertQuery  string
	describeRow  func(*sql.Rows) (string, error)
}

// CompareWith compares the target store with the database at sourcePath and
// returns the rows that exist only in the source database.
func (s *Store) CompareWith(sourcePath string) (*DatabaseDiff, error) {
	ctx := context.Background()
	conn, err := s.conn.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("open comparison connection: %w", err)
	}
	defer conn.Close()

	if err := attachSource(conn, sourcePath); err != nil {
		return nil, err
	}
	defer detachSource(conn)

	specs, err := comparisonSpecs(conn, s.platform)
	if err != nil {
		return nil, err
	}

	diff := &DatabaseDiff{Tables: []TableDiff{}}
	for _, spec := range specs {
		var tableDiff TableDiff
		tableDiff.Name = spec.name
		if err := conn.QueryRowContext(ctx, spec.sourceCount).Scan(&tableDiff.SourceCount); err != nil {
			return nil, fmt.Errorf("count source table %s: %w", spec.name, err)
		}
		if err := conn.QueryRowContext(ctx, spec.targetCount).Scan(&tableDiff.TargetCount); err != nil {
			return nil, fmt.Errorf("count target table %s: %w", spec.name, err)
		}

		rows, err := conn.QueryContext(ctx, spec.missingQuery)
		if err != nil {
			return nil, fmt.Errorf("compare table %s: %w", spec.name, err)
		}
		for rows.Next() {
			description, err := spec.describeRow(rows)
			if err != nil {
				rows.Close()
				return nil, fmt.Errorf("read missing row from %s: %w", spec.name, err)
			}
			tableDiff.Missing = append(tableDiff.Missing, description)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("compare table %s: %w", spec.name, err)
		}
		rows.Close()
		tableDiff.MissingCount = len(tableDiff.Missing)
		diff.TotalMissing += tableDiff.MissingCount
		diff.Tables = append(diff.Tables, tableDiff)
	}
	return diff, nil
}

// MergeMissingFrom copies every row that exists in sourcePath but not in this
// store. The target database is backed up before any write. progress, when not
// nil, is called after each table is merged.
func (s *Store) MergeMissingFrom(sourcePath string, progress func(done, total int, table string)) (*MergeResult, error) {
	backupPath := nextAvailablePath(filepath.Join(
		filepath.Dir(s.path),
		"kansho.pre-migration-"+time.Now().UTC().Format("20060102-150405")+".db",
	))
	if err := s.Backup(backupPath); err != nil {
		return nil, fmt.Errorf("backup target database before merge: %w", err)
	}

	owner, err := s.acquireWriteLock()
	if err != nil {
		return nil, err
	}
	defer s.releaseWriteLock(owner)

	ctx := context.Background()
	conn, err := s.conn.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("open merge connection: %w", err)
	}
	defer conn.Close()

	if err := attachSource(conn, sourcePath); err != nil {
		return nil, err
	}
	defer detachSource(conn)

	specs, err := comparisonSpecs(conn, s.platform)
	if err != nil {
		return nil, err
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin merge transaction: %w", err)
	}
	result := &MergeResult{Tables: []MergeTableResult{}, BackupPath: backupPath}
	for i, spec := range specs {
		res, err := tx.ExecContext(ctx, spec.insertQuery)
		if err != nil {
			_ = tx.Rollback()
			return nil, fmt.Errorf("merge table %s: %w", spec.name, err)
		}
		inserted, _ := res.RowsAffected()
		result.Tables = append(result.Tables, MergeTableResult{Name: spec.name, Inserted: inserted})
		result.TotalInserted += inserted
		if progress != nil {
			progress(i+1, len(specs), spec.name)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit merge transaction: %w", err)
	}
	_ = detachSource(conn)
	s.checkpoint()
	return result, nil
}

func attachSource(conn *sql.Conn, sourcePath string) error {
	quoted := strings.ReplaceAll(sourcePath, "'", "''")
	if _, err := conn.ExecContext(context.Background(),
		fmt.Sprintf("ATTACH DATABASE '%s' AS source", quoted)); err != nil {
		return fmt.Errorf("attach source database %s: %w", sourcePath, err)
	}
	return nil
}

func detachSource(conn *sql.Conn) error {
	if _, err := conn.ExecContext(context.Background(), "DETACH DATABASE source"); err != nil {
		return fmt.Errorf("detach source database: %w", err)
	}
	return nil
}

func comparisonSpecs(conn *sql.Conn, platform string) ([]tableSpec, error) {
	linuxBookmarksExist, err := sourceTableExists(conn, "bookmarks_linux")
	if err != nil {
		return nil, err
	}
	windowsBookmarksExist, err := sourceTableExists(conn, "bookmarks_windows")
	if err != nil {
		return nil, err
	}
	all := []tableSpec{legacyBookmarkSpec()}
	if !linuxBookmarksExist && !windowsBookmarksExist {
		all = append(all, legacyPlatformBookmarkSpec(platform))
	}
	if linuxBookmarksExist {
		all = append(all, platformBookmarkSpec("bookmarks_linux"))
	}
	if windowsBookmarksExist {
		all = append(all, platformBookmarkSpec("bookmarks_windows"))
	}
	all = append(all, []tableSpec{
		{
			name:        "manga_title",
			sourceCount: `SELECT COUNT(*) FROM source.manga_title`,
			targetCount: `SELECT COUNT(*) FROM main.manga_title`,
			missingQuery: `SELECT s.mangadex_id, s.primary_title
				FROM source.manga_title s
				WHERE NOT EXISTS (
					SELECT 1 FROM main.manga_title m WHERE m.mangadex_id = s.mangadex_id
				)
				ORDER BY lower(s.primary_title)`,
			insertQuery: `INSERT INTO main.manga_title
					(mangadex_id, primary_title, selected_english_title, title_json, alt_titles_json,
					 description_json, original_language, publication_demographic, year, content_rating,
					 status, tags_json, author, artist, cover_url, url)
				SELECT s.mangadex_id, s.primary_title, s.selected_english_title, s.title_json,
					s.alt_titles_json, s.description_json, s.original_language,
					s.publication_demographic, s.year, s.content_rating, s.status, s.tags_json,
					s.author, s.artist, s.cover_url, s.url
				FROM source.manga_title s
				WHERE NOT EXISTS (
					SELECT 1 FROM main.manga_title m WHERE m.mangadex_id = s.mangadex_id
				)`,
			describeRow: func(rows *sql.Rows) (string, error) {
				var id, title string
				if err := rows.Scan(&id, &title); err != nil {
					return "", err
				}
				return fmt.Sprintf("%s (MangaDex ID %s)", title, id), nil
			},
		},
		{
			name:        "manga_title_lookup",
			sourceCount: `SELECT COUNT(*) FROM source.manga_title_lookup`,
			targetCount: `SELECT COUNT(*) FROM main.manga_title_lookup`,
			missingQuery: `SELECT s.lookup_title, s.mangadex_id
				FROM source.manga_title_lookup s
				WHERE NOT EXISTS (
					SELECT 1 FROM main.manga_title_lookup m WHERE m.lookup_title = s.lookup_title
				)
				ORDER BY lower(s.lookup_title)`,
			insertQuery: `INSERT INTO main.manga_title_lookup (lookup_title, mangadex_id)
				SELECT s.lookup_title, s.mangadex_id
				FROM source.manga_title_lookup s
				WHERE NOT EXISTS (
					SELECT 1 FROM main.manga_title_lookup m WHERE m.lookup_title = s.lookup_title
				)`,
			describeRow: func(rows *sql.Rows) (string, error) {
				var title, id string
				if err := rows.Scan(&title, &id); err != nil {
					return "", err
				}
				return fmt.Sprintf("%s -> %s", title, id), nil
			},
		},
		{
			name:        "manga_chapter_stats",
			sourceCount: `SELECT COUNT(*) FROM source.manga_chapter_stats`,
			targetCount: `SELECT COUNT(*) FROM main.manga_chapter_stats`,
			missingQuery: `SELECT s.manga_key, s.title, s.site
				FROM source.manga_chapter_stats s
				WHERE NOT EXISTS (
					SELECT 1 FROM main.manga_chapter_stats m WHERE m.manga_key = s.manga_key
				)
				ORDER BY lower(s.title)`,
			insertQuery: `INSERT INTO main.manga_chapter_stats
					(manga_key, title, site, url, total_chapters, downloaded_chapters,
					 not_downloaded_chapters, last_refresh)
				SELECT s.manga_key, s.title, s.site, s.url, s.total_chapters,
					s.downloaded_chapters, s.not_downloaded_chapters, s.last_refresh
				FROM source.manga_chapter_stats s
				WHERE NOT EXISTS (
					SELECT 1 FROM main.manga_chapter_stats m WHERE m.manga_key = s.manga_key
				)`,
			describeRow: func(rows *sql.Rows) (string, error) {
				var key, title, site string
				if err := rows.Scan(&key, &title, &site); err != nil {
					return "", err
				}
				return fmt.Sprintf("%s | site=%s | key=%s", title, site, key), nil
			},
		},
		{
			name:        "manga_metadata_prefs",
			sourceCount: `SELECT COUNT(*) FROM source.manga_metadata_prefs`,
			targetCount: `SELECT COUNT(*) FROM main.manga_metadata_prefs`,
			missingQuery: `SELECT s.manga_key, s.prompt_mangadex_metadata
				FROM source.manga_metadata_prefs s
				WHERE NOT EXISTS (
					SELECT 1 FROM main.manga_metadata_prefs m WHERE m.manga_key = s.manga_key
				)
				ORDER BY lower(s.manga_key)`,
			insertQuery: `INSERT INTO main.manga_metadata_prefs (manga_key, prompt_mangadex_metadata)
				SELECT s.manga_key, s.prompt_mangadex_metadata
				FROM source.manga_metadata_prefs s
				WHERE NOT EXISTS (
					SELECT 1 FROM main.manga_metadata_prefs m WHERE m.manga_key = s.manga_key
				)`,
			describeRow: func(rows *sql.Rows) (string, error) {
				var key string
				var allowed int
				if err := rows.Scan(&key, &allowed); err != nil {
					return "", err
				}
				return fmt.Sprintf("%s | metadata prompt allowed=%d", key, allowed), nil
			},
		},
		{
			name:        "manga_cover_cache",
			sourceCount: `SELECT COUNT(*) FROM source.manga_cover_cache`,
			targetCount: `SELECT COUNT(*) FROM main.manga_cover_cache`,
			missingQuery: `SELECT s.mangadex_id, s.cover_url
				FROM source.manga_cover_cache s
				WHERE NOT EXISTS (
					SELECT 1 FROM main.manga_cover_cache m WHERE m.mangadex_id = s.mangadex_id
				)
				ORDER BY s.mangadex_id`,
			insertQuery: `INSERT INTO main.manga_cover_cache (mangadex_id, cover_jpeg, cover_url)
				SELECT s.mangadex_id, s.cover_jpeg, s.cover_url
				FROM source.manga_cover_cache s
				WHERE NOT EXISTS (
					SELECT 1 FROM main.manga_cover_cache m WHERE m.mangadex_id = s.mangadex_id
				)`,
			describeRow: func(rows *sql.Rows) (string, error) {
				var id, url string
				if err := rows.Scan(&id, &url); err != nil {
					return "", err
				}
				return fmt.Sprintf("%s | cover=%s", id, url), nil
			},
		},
	}...)

	specs := make([]tableSpec, 0, len(all))
	for _, spec := range all {
		sourceTable := spec.sourceTable
		if sourceTable == "" {
			sourceTable = spec.name
		}
		exists, err := sourceTableExists(conn, sourceTable)
		if err != nil {
			return nil, err
		}
		if exists {
			specs = append(specs, spec)
		}
	}
	return specs, nil
}

func legacyPlatformBookmarkSpec(platform string) tableSpec {
	table, _ := bookmarksTable(platform)
	return tableSpec{
		name:        "legacy_bookmarks_to_" + table,
		sourceTable: "bookmarks",
		sourceCount: `SELECT COUNT(*) FROM source.bookmarks`,
		targetCount: `SELECT COUNT(*) FROM main.` + table,
		missingQuery: `SELECT b.title, b.url, b.chapters, b.location, b.site, b.shortname
			FROM source.bookmarks b
			WHERE NOT EXISTS (
				SELECT 1 FROM main.` + table + ` m
				WHERE m.title = b.title AND m.url = b.url AND m.chapters = b.chapters
				  AND m.location = b.location AND m.site = b.site AND m.shortname = b.shortname
			)
			ORDER BY lower(b.title), b.id`,
		insertQuery: `INSERT INTO main.` + table + `
				(title, url, chapters, location, site, shortname, type, mangadex_id, updated_at)
			SELECT b.title, b.url, b.chapters, b.location, b.site, b.shortname, 'manga', '', CURRENT_TIMESTAMP
			FROM source.bookmarks b
			WHERE NOT EXISTS (
				SELECT 1 FROM main.` + table + ` m
				WHERE m.title = b.title AND m.url = b.url AND m.chapters = b.chapters
				  AND m.location = b.location AND m.site = b.site AND m.shortname = b.shortname
			)`,
		describeRow: func(rows *sql.Rows) (string, error) {
			var title, url, chapters, location, site, shortname string
			if err := rows.Scan(&title, &url, &chapters, &location, &site, &shortname); err != nil {
				return "", err
			}
			return fmt.Sprintf("%s | site=%s | url=%s | location=%s", title, site, url, location), nil
		},
	}
}

func legacyBookmarkSpec() tableSpec {
	return tableSpec{
		name:        "bookmarks",
		sourceCount: `SELECT COUNT(*) FROM source.bookmarks`,
		targetCount: `SELECT COUNT(*) FROM main.bookmarks`,
		missingQuery: `SELECT b.title, b.url, b.chapters, b.location, b.site, b.shortname
			FROM source.bookmarks b
			WHERE NOT EXISTS (
				SELECT 1 FROM main.bookmarks m
				WHERE m.title = b.title AND m.url = b.url AND m.chapters = b.chapters
				  AND m.location = b.location AND m.site = b.site AND m.shortname = b.shortname
			)
			ORDER BY lower(b.title), b.id`,
		insertQuery: `INSERT INTO main.bookmarks
				(title, url, chapters, location, site, shortname, updated_at)
			SELECT b.title, b.url, b.chapters, b.location, b.site, b.shortname, CURRENT_TIMESTAMP
			FROM source.bookmarks b
			WHERE NOT EXISTS (
				SELECT 1 FROM main.bookmarks m
				WHERE m.title = b.title AND m.url = b.url AND m.chapters = b.chapters
				  AND m.location = b.location AND m.site = b.site AND m.shortname = b.shortname
			)`,
		describeRow: func(rows *sql.Rows) (string, error) {
			var title, url, chapters, location, site, shortname string
			if err := rows.Scan(&title, &url, &chapters, &location, &site, &shortname); err != nil {
				return "", err
			}
			return fmt.Sprintf("%s | site=%s | url=%s | chapters=%s | location=%s | shortname=%s",
				title, site, url, chapters, location, shortname), nil
		},
	}
}

func platformBookmarkSpec(table string) tableSpec {
	return tableSpec{
		name:        table,
		sourceCount: `SELECT COUNT(*) FROM source.` + table,
		targetCount: `SELECT COUNT(*) FROM main.` + table,
		missingQuery: `SELECT b.title, b.url, b.chapters, b.location, b.site, b.shortname, b.type, b.mangadex_id
			FROM source.` + table + ` b
			WHERE NOT EXISTS (
				SELECT 1 FROM main.` + table + ` m
				WHERE m.title = b.title AND m.url = b.url AND m.chapters = b.chapters
				  AND m.location = b.location AND m.site = b.site AND m.shortname = b.shortname
			)
			ORDER BY lower(b.title), b.id`,
		insertQuery: `INSERT INTO main.` + table + `
				(title, url, chapters, location, site, shortname, type, mangadex_id, updated_at)
			SELECT b.title, b.url, b.chapters, b.location, b.site, b.shortname, b.type, b.mangadex_id, CURRENT_TIMESTAMP
			FROM source.` + table + ` b
			WHERE NOT EXISTS (
				SELECT 1 FROM main.` + table + ` m
				WHERE m.title = b.title AND m.url = b.url AND m.chapters = b.chapters
				  AND m.location = b.location AND m.site = b.site AND m.shortname = b.shortname
			)`,
		describeRow: func(rows *sql.Rows) (string, error) {
			var title, url, chapters, location, site, shortname, kind, id string
			if err := rows.Scan(&title, &url, &chapters, &location, &site, &shortname, &kind, &id); err != nil {
				return "", err
			}
			return fmt.Sprintf("%s | type=%s | MangaDex ID=%s | site=%s | location=%s", title, kind, id, site, location), nil
		},
	}
}

func sourceTableExists(conn *sql.Conn, table string) (bool, error) {
	var name string
	err := conn.QueryRowContext(context.Background(),
		`SELECT name FROM source.sqlite_master WHERE type = 'table' AND name = ?`, table,
	).Scan(&name)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect source table %s: %w", table, err)
	}
	return true, nil
}

func nextAvailablePath(path string) string {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path
	}
	base := strings.TrimSuffix(path, filepath.Ext(path))
	ext := filepath.Ext(path)
	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s-%d%s", base, i, ext)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}
