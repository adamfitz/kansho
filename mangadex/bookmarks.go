package mangadex

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path"
	"strings"
	"time"

	"kansho/models"
	"kansho/preferences"
)

const bookmarksMigratedKey = "bookmarks_migrated_at"
const platformBookmarksMigratedKey = "platform_bookmarks_migrated_at"

func (s *Store) platformBookmarksTable() string {
	if s.platform == "windows" {
		return "bookmarks_windows"
	}
	return "bookmarks_linux"
}

// LoadBookmarks returns every bookmark stored in the database.
func (s *Store) LoadBookmarks() (models.Manga, error) {
	return s.LoadPlatformBookmarks(s.platform)
}

// LoadPlatformBookmarks returns bookmarks from the selected OS table.
func (s *Store) LoadPlatformBookmarks(platform string) (models.Manga, error) {
	table, err := bookmarksTable(platform)
	if err != nil {
		return models.Manga{}, err
	}
	rows, err := s.conn.Query(`SELECT title, url, chapters, location, site, shortname, type, mangadex_id
		FROM ` + table + ` ORDER BY lower(title), id`)
	if err != nil {
		return models.Manga{}, fmt.Errorf("load bookmarks: %w", err)
	}
	defer rows.Close()

	manga := models.Manga{Manga: []models.Bookmarks{}}
	for rows.Next() {
		var bookmark models.Bookmarks
		if err := rows.Scan(
			&bookmark.Title,
			&bookmark.Url,
			&bookmark.Chapters,
			&bookmark.Location,
			&bookmark.Site,
			&bookmark.Shortname,
			&bookmark.Type,
			&bookmark.MangaDexID,
		); err != nil {
			return models.Manga{}, fmt.Errorf("read bookmark row: %w", err)
		}
		manga.Manga = append(manga.Manga, bookmark)
	}
	if err := rows.Err(); err != nil {
		return models.Manga{}, fmt.Errorf("read bookmarks: %w", err)
	}
	return manga, nil
}

// SaveBookmarks replaces the stored bookmark set with data in one transaction.
// Kansho's UI edits bookmarks in memory and saves the complete list, so a full
// replacement keeps the existing behaviour while still being atomic.
func (s *Store) SaveBookmarks(data models.Manga) error {
	return s.withWriteTx(func(tx *sql.Tx) error {
		table := s.platformBookmarksTable()
		if _, err := tx.Exec(`DELETE FROM ` + table); err != nil {
			return fmt.Errorf("clear bookmarks: %w", err)
		}
		stmt, err := tx.Prepare(`INSERT INTO ` + table + `
				(title, url, chapters, location, site, shortname, type, mangadex_id, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`)
		if err != nil {
			return fmt.Errorf("prepare bookmark insert: %w", err)
		}
		defer stmt.Close()

		for _, bookmark := range data.Manga {
			mangaDexID := strings.TrimSpace(bookmark.MangaDexID)
			if _, err := stmt.Exec(
				bookmark.Title,
				bookmark.Url,
				bookmark.Chapters,
				bookmark.Location,
				bookmark.Site,
				bookmark.Shortname,
				bookmarkType(bookmark.Type),
				mangaDexID,
			); err != nil {
				return fmt.Errorf("insert bookmark %q: %w", bookmark.Title, err)
			}
		}
		return nil
	})
}

func bookmarkType(kind string) string {
	if kind == "webtoon" {
		return kind
	}
	return "manga"
}

// ApplyRootToBookmarks rebases matching bookmark locations without moving files.
func (s *Store) ApplyRootToBookmarks(platform, kind, root string) (int, error) {
	return s.ApplyBookmarkRootUpdates([]BookmarkRootUpdate{{Platform: platform, Type: kind, Root: root}})
}

type BookmarkRootUpdate struct {
	Platform string
	Type     string
	Root     string
}

// ApplyBookmarkRootUpdates rebases all selected root groups atomically.
func (s *Store) ApplyBookmarkRootUpdates(updates []BookmarkRootUpdate) (int, error) {
	for _, update := range updates {
		if _, err := bookmarksTable(update.Platform); err != nil {
			return 0, err
		}
		if update.Type != "manga" && update.Type != "webtoon" {
			return 0, fmt.Errorf("unknown bookmark type %q", update.Type)
		}
		if strings.TrimSpace(update.Root) == "" {
			return 0, fmt.Errorf("root directory is empty")
		}
	}

	updated := 0
	err := s.withWriteTx(func(tx *sql.Tx) error {
		for _, update := range updates {
			table, _ := bookmarksTable(update.Platform)
			rows, err := tx.Query(`SELECT id, title FROM `+table+` WHERE type = ?`, update.Type)
			if err != nil {
				return fmt.Errorf("list %s bookmarks: %w", update.Type, err)
			}
			type row struct {
				id    int64
				title string
			}
			var bookmarks []row
			for rows.Next() {
				var bookmark row
				if err := rows.Scan(&bookmark.id, &bookmark.title); err != nil {
					rows.Close()
					return fmt.Errorf("read bookmark for root update: %w", err)
				}
				bookmarks = append(bookmarks, bookmark)
			}
			if err := rows.Err(); err != nil {
				rows.Close()
				return fmt.Errorf("read bookmarks for root update: %w", err)
			}
			if err := rows.Close(); err != nil {
				return fmt.Errorf("close bookmarks for root update: %w", err)
			}

			for _, bookmark := range bookmarks {
				if _, err := tx.Exec(`UPDATE `+table+` SET location = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
					joinBookmarkRoot(strings.TrimSpace(update.Root), bookmark.title, update.Platform), bookmark.id); err != nil {
					return fmt.Errorf("update bookmark %q location: %w", bookmark.title, err)
				}
				updated++
			}
		}
		return nil
	})
	return updated, err
}

type BookmarkSyncSkip struct {
	Platform string
	Title    string
}

type BookmarkSyncResult struct {
	InsertedToLinux   int
	InsertedToWindows int
	Skipped           []BookmarkSyncSkip
}

// SyncPlatformBookmarks copies missing MangaDex IDs in both directions.
func (s *Store) SyncPlatformBookmarks() (*BookmarkSyncResult, error) {
	prefs, err := preferences.Load()
	if err != nil {
		return nil, fmt.Errorf("load root preferences: %w", err)
	}
	linuxTable, _ := bookmarksTable("linux")
	windowsTable, _ := bookmarksTable("windows")
	result := &BookmarkSyncResult{Skipped: []BookmarkSyncSkip{}}
	err = s.withWriteTx(func(tx *sql.Tx) error {
		linuxBookmarks, err := readPlatformBookmarks(tx, linuxTable)
		if err != nil {
			return err
		}
		windowsBookmarks, err := readPlatformBookmarks(tx, windowsTable)
		if err != nil {
			return err
		}
		linuxIDs := bookmarkIDSet(linuxBookmarks)
		windowsIDs := bookmarkIDSet(windowsBookmarks)

		if err := copyMissingPlatformBookmarks(tx, windowsTable, "linux", linuxBookmarks, windowsIDs, prefs, result); err != nil {
			return err
		}
		if err := copyMissingPlatformBookmarks(tx, linuxTable, "windows", windowsBookmarks, linuxIDs, prefs, result); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func readPlatformBookmarks(tx *sql.Tx, table string) ([]models.Bookmarks, error) {
	rows, err := tx.Query(`SELECT title, url, chapters, location, site, shortname, type, mangadex_id FROM ` + table + ` ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", table, err)
	}
	defer rows.Close()
	var bookmarks []models.Bookmarks
	for rows.Next() {
		var bookmark models.Bookmarks
		if err := rows.Scan(&bookmark.Title, &bookmark.Url, &bookmark.Chapters, &bookmark.Location,
			&bookmark.Site, &bookmark.Shortname, &bookmark.Type, &bookmark.MangaDexID); err != nil {
			return nil, fmt.Errorf("read row from %s: %w", table, err)
		}
		bookmarks = append(bookmarks, bookmark)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read rows from %s: %w", table, err)
	}
	return bookmarks, nil
}

func bookmarkIDSet(bookmarks []models.Bookmarks) map[string]bool {
	ids := make(map[string]bool, len(bookmarks))
	for _, bookmark := range bookmarks {
		if id := strings.TrimSpace(bookmark.MangaDexID); id != "" {
			ids[id] = true
		}
	}
	return ids
}

func copyMissingPlatformBookmarks(tx *sql.Tx, targetTable, sourcePlatform string,
	bookmarks []models.Bookmarks, targetIDs map[string]bool, prefs preferences.Preferences, result *BookmarkSyncResult,
) error {
	targetPlatform := "linux"
	if sourcePlatform == "linux" {
		targetPlatform = "windows"
	}
	for _, bookmark := range bookmarks {
		id := strings.TrimSpace(bookmark.MangaDexID)
		if id == "" {
			result.Skipped = append(result.Skipped, BookmarkSyncSkip{Platform: sourcePlatform, Title: bookmark.Title})
			continue
		}
		if targetIDs[id] {
			continue
		}
		bookmark.Type = bookmarkType(bookmark.Type)
		if root := strings.TrimSpace(prefs.RootDirectory(targetPlatform, bookmark.Type)); root != "" {
			if targetPlatform == "linux" {
				var err error
				root, err = preferences.ExpandPath(root)
				if err != nil {
					return fmt.Errorf("expand %s root %q: %w", targetPlatform, root, err)
				}
			}
			bookmark.Location = joinBookmarkRoot(root, bookmark.Title, targetPlatform)
		}
		if _, err := tx.Exec(`INSERT INTO `+targetTable+`
			(title, url, chapters, location, site, shortname, type, mangadex_id, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
			bookmark.Title, bookmark.Url, bookmark.Chapters, bookmark.Location, bookmark.Site,
			bookmark.Shortname, bookmark.Type, id); err != nil {
			return fmt.Errorf("copy %q from %s to %s: %w", bookmark.Title, sourcePlatform, targetPlatform, err)
		}
		targetIDs[id] = true
		if targetPlatform == "linux" {
			result.InsertedToLinux++
		} else {
			result.InsertedToWindows++
		}
	}
	return nil
}

// SetBookmarkMangaDexID links the current platform's exact bookmark title to a user-confirmed ID.
func (s *Store) SetBookmarkMangaDexID(title, id string) error {
	title = strings.TrimSpace(title)
	id = strings.TrimSpace(id)
	if title == "" || id == "" {
		return fmt.Errorf("bookmark title and MangaDex ID are required")
	}
	table := s.platformBookmarksTable()
	return s.withWriteTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`UPDATE `+table+` SET mangadex_id = ?, updated_at = CURRENT_TIMESTAMP
			WHERE title = ? COLLATE NOCASE`, id, title); err != nil {
			return fmt.Errorf("link bookmark %q to MangaDex ID %s: %w", title, id, err)
		}
		return nil
	})
}

func bookmarksTable(platform string) (string, error) {
	switch platform {
	case "linux":
		return "bookmarks_linux", nil
	case "windows":
		return "bookmarks_windows", nil
	default:
		return "", fmt.Errorf("unknown bookmark platform %q", platform)
	}
}

func joinBookmarkRoot(root, title, platform string) string {
	if platform == "windows" {
		root = strings.TrimRight(strings.ReplaceAll(root, "/", `\`), `\`)
		title = strings.Trim(title, `/\`)
		if root == "" {
			return title
		}
		return root + `\` + title
	}
	return path.Join(root, title)
}

func (s *Store) migrateLegacyBookmarksToPlatform() error {
	if marker, err := s.getMeta(platformBookmarksMigratedKey); err != nil {
		return err
	} else if marker != "" {
		return nil
	}

	table := s.platformBookmarksTable()
	return s.withWriteTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO ` + table + `
			(title, url, chapters, location, site, shortname, type, mangadex_id, created_at, updated_at)
			SELECT b.title, b.url, b.chapters, b.location, b.site, b.shortname, 'manga', '', b.created_at, b.updated_at
			FROM bookmarks b
			WHERE NOT EXISTS (
				SELECT 1 FROM ` + table + ` p
				WHERE p.title = b.title AND p.url = b.url AND p.chapters = b.chapters
				  AND p.location = b.location AND p.site = b.site AND p.shortname = b.shortname
			)`)
		if err != nil {
			return fmt.Errorf("copy legacy bookmark rows: %w", err)
		}
		if _, err := tx.Exec(`INSERT INTO kansho_meta (key, value) VALUES (?, CURRENT_TIMESTAMP)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`, platformBookmarksMigratedKey); err != nil {
			return fmt.Errorf("mark legacy bookmark migration: %w", err)
		}
		return nil
	})
}

// insertMissingBookmarks adds only bookmark rows that are not already present.
// It is used by the one-time legacy-file migration and by database merges.
func (s *Store) insertMissingBookmarks(bookmarks []models.Bookmarks) error {
	if len(bookmarks) == 0 {
		return nil
	}
	return s.withWriteTx(func(tx *sql.Tx) error {
		stmt, err := tx.Prepare(`
			INSERT INTO bookmarks
				(title, url, chapters, location, site, shortname, updated_at)
			SELECT ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP
			WHERE NOT EXISTS (
				SELECT 1 FROM bookmarks
				WHERE title = ? AND url = ? AND chapters = ?
				  AND location = ? AND site = ? AND shortname = ?
			)`)
		if err != nil {
			return fmt.Errorf("prepare bookmark migration insert: %w", err)
		}
		defer stmt.Close()

		for _, bookmark := range bookmarks {
			if _, err := stmt.Exec(
				bookmark.Title,
				bookmark.Url,
				bookmark.Chapters,
				bookmark.Location,
				bookmark.Site,
				bookmark.Shortname,
				bookmark.Title,
				bookmark.Url,
				bookmark.Chapters,
				bookmark.Location,
				bookmark.Site,
				bookmark.Shortname,
			); err != nil {
				return fmt.Errorf("migrate bookmark %q: %w", bookmark.Title, err)
			}
		}
		return nil
	})
}

// migrateLegacyBookmarks imports bookmarks.json into the database once, then
// renames the old file to bookmarks.json.bak.
func (s *Store) migrateLegacyBookmarks() error {
	marker, err := s.getMeta(bookmarksMigratedKey)
	if err != nil {
		return err
	}
	if marker != "" {
		return nil
	}

	path, err := preferences.LegacyBookmarksPath()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s.setMeta(bookmarksMigratedKey, "no-legacy-file")
	}
	if err != nil {
		log.Printf("[mangadex] cannot read legacy bookmarks %s: %v", path, err)
		return nil
	}

	var manga models.Manga
	if err := json.Unmarshal(data, &manga); err != nil {
		log.Printf("[mangadex] cannot parse legacy bookmarks %s: %v", path, err)
		return nil
	}

	if err := s.insertMissingBookmarks(manga.Manga); err != nil {
		return err
	}

	backup := legacyBookmarkBackupPath(path)
	if err := os.Rename(path, backup); err != nil {
		log.Printf("[mangadex] migrated bookmarks but could not rename %s to %s: %v", path, backup, err)
	} else {
		log.Printf("[mangadex] migrated %d bookmarks from %s to the database; old file is now %s",
			len(manga.Manga), path, backup)
	}

	return s.setMeta(bookmarksMigratedKey, time.Now().UTC().Format(time.RFC3339))
}

func legacyBookmarkBackupPath(path string) string {
	backup := path + ".bak"
	if _, err := os.Stat(backup); os.IsNotExist(err) {
		return backup
	}
	return fmt.Sprintf("%s.%s.bak", path, time.Now().UTC().Format("20060102-150405"))
}

func (s *Store) getMeta(key string) (string, error) {
	var value string
	err := s.conn.QueryRow(`SELECT value FROM kansho_meta WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read Kansho metadata %q: %w", key, err)
	}
	return value, nil
}

func (s *Store) setMeta(key, value string) error {
	return s.withWriteTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`
			INSERT INTO kansho_meta (key, value) VALUES (?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
		if err != nil {
			return fmt.Errorf("write Kansho metadata %q: %w", key, err)
		}
		return nil
	})
}
