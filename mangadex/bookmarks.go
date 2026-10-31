package mangadex

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path"
	"sort"
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
	prefs, err := preferences.Load()
	if err != nil {
		return fmt.Errorf("load root preferences: %w", err)
	}
	return s.withWriteTx(func(tx *sql.Tx) error {
		table := s.platformBookmarksTable()
		previous, err := readPlatformBookmarkRows(tx, table)
		if err != nil {
			return err
		}
		currentKeys := make(map[string]bool, len(data.Manga))
		for _, bookmark := range data.Manga {
			if key := bookmarkIdentity(bookmark); key != "" {
				currentKeys[key] = true
			}
		}
		removedKeys := make(map[string]bool)
		for _, row := range previous {
			if key := bookmarkIdentity(row.bookmark); key != "" && !currentKeys[key] {
				removedKeys[key] = true
			}
		}

		otherTable := "bookmarks_linux"
		if table == "bookmarks_linux" {
			otherTable = "bookmarks_windows"
		}
		otherRows, err := readPlatformBookmarkRows(tx, otherTable)
		if err != nil {
			return err
		}
		for _, row := range otherRows {
			if removedKeys[bookmarkIdentity(row.bookmark)] {
				if _, err := tx.Exec(`DELETE FROM `+otherTable+` WHERE id = ?`, row.id); err != nil {
					return fmt.Errorf("remove deleted bookmark from other platform: %w", err)
				}
			}
		}

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
		return syncPlatformBookmarksTx(tx, prefs, s.platform, &BookmarkSyncResult{Skipped: []BookmarkSyncSkip{}})
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
	UpdatedToLinux    int
	UpdatedToWindows  int
	Skipped           []BookmarkSyncSkip
}

type platformBookmarkRow struct {
	id        int64
	bookmark  models.Bookmarks
	updatedAt string
}

// SyncPlatformBookmarks keeps title metadata aligned across both OS tables
// while retaining each table's own directory location.
func (s *Store) SyncPlatformBookmarks() (*BookmarkSyncResult, error) {
	prefs, err := preferences.Load()
	if err != nil {
		return nil, fmt.Errorf("load root preferences: %w", err)
	}
	result := &BookmarkSyncResult{Skipped: []BookmarkSyncSkip{}}
	err = s.withWriteTx(func(tx *sql.Tx) error { return syncPlatformBookmarksTx(tx, prefs, s.platform, result) })
	if err != nil {
		return nil, err
	}
	return result, nil
}

func readPlatformBookmarkRows(tx *sql.Tx, table string) ([]platformBookmarkRow, error) {
	rows, err := tx.Query(`SELECT id, title, url, chapters, location, site, shortname, type, mangadex_id, updated_at FROM ` + table + ` ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", table, err)
	}
	defer rows.Close()
	var bookmarks []platformBookmarkRow
	for rows.Next() {
		var row platformBookmarkRow
		if err := rows.Scan(&row.id, &row.bookmark.Title, &row.bookmark.Url, &row.bookmark.Chapters, &row.bookmark.Location,
			&row.bookmark.Site, &row.bookmark.Shortname, &row.bookmark.Type, &row.bookmark.MangaDexID, &row.updatedAt); err != nil {
			return nil, fmt.Errorf("read row from %s: %w", table, err)
		}
		bookmarks = append(bookmarks, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read rows from %s: %w", table, err)
	}
	return bookmarks, nil
}

func bookmarkIdentity(bookmark models.Bookmarks) string {
	if id := strings.ToLower(strings.TrimSpace(bookmark.MangaDexID)); id != "" {
		return "id:" + id
	}
	site := strings.ToLower(strings.Join(strings.Fields(bookmark.Site), " "))
	if url := strings.ToLower(strings.TrimSpace(bookmark.Url)); url != "" {
		return "url:" + site + "\x00" + strings.TrimRight(url, "/")
	}
	shortname := strings.ToLower(strings.TrimSpace(bookmark.Shortname))
	title := strings.ToLower(strings.Join(strings.Fields(bookmark.Title), " "))
	if shortname != "" {
		return "short:" + site + "\x00" + shortname
	}
	if title != "" {
		return "title:" + site + "\x00" + title
	}
	return ""
}

func syncPlatformBookmarksTx(tx *sql.Tx, prefs preferences.Preferences, preferredPlatform string, result *BookmarkSyncResult) error {
	linuxRows, err := readPlatformBookmarkRows(tx, "bookmarks_linux")
	if err != nil {
		return err
	}
	windowsRows, err := readPlatformBookmarkRows(tx, "bookmarks_windows")
	if err != nil {
		return err
	}
	linuxByKey := latestBookmarksByIdentity(linuxRows)
	windowsByKey := latestBookmarksByIdentity(windowsRows)
	keys := make(map[string]bool, len(linuxByKey)+len(windowsByKey))
	for key := range linuxByKey {
		keys[key] = true
	}
	for key := range windowsByKey {
		keys[key] = true
	}
	orderedKeys := make([]string, 0, len(keys))
	for key := range keys {
		orderedKeys = append(orderedKeys, key)
	}
	sort.Strings(orderedKeys)

	for _, key := range orderedKeys {
		linuxRow, hasLinux := linuxByKey[key]
		windowsRow, hasWindows := windowsByKey[key]
		if !hasLinux && !hasWindows {
			continue
		}
		if key == "" {
			continue
		}
		canonical := linuxRow
		if !hasLinux || (hasWindows && (windowsRow.updatedAt > linuxRow.updatedAt ||
			(windowsRow.updatedAt == linuxRow.updatedAt && preferredPlatform == "windows"))) {
			canonical = windowsRow
		}
		canonical.bookmark.Type = bookmarkType(canonical.bookmark.Type)

		for _, target := range []struct {
			platform string
			table    string
			row      platformBookmarkRow
			exists   bool
		}{
			{platform: "linux", table: "bookmarks_linux", row: linuxRow, exists: hasLinux},
			{platform: "windows", table: "bookmarks_windows", row: windowsRow, exists: hasWindows},
		} {
			if !target.exists {
				location, err := bookmarkRootLocation(prefs, target.platform, canonical.bookmark.Type, canonical.bookmark.Title)
				if err != nil {
					return err
				}
				bookmark := canonical.bookmark
				if _, err := tx.Exec(`INSERT INTO `+target.table+`
					(title, url, chapters, location, site, shortname, type, mangadex_id, updated_at)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
					bookmark.Title, bookmark.Url, bookmark.Chapters, location, bookmark.Site,
					bookmark.Shortname, bookmark.Type, strings.TrimSpace(bookmark.MangaDexID)); err != nil {
					return fmt.Errorf("add %q to %s bookmarks: %w", bookmark.Title, target.platform, err)
				}
				if target.platform == "linux" {
					result.InsertedToLinux++
				} else {
					result.InsertedToWindows++
				}
				continue
			}
			if sameBookmarkMetadata(target.row.bookmark, canonical.bookmark) {
				continue
			}
			bookmark := canonical.bookmark
			location := target.row.bookmark.Location
			if bookmarkType(target.row.bookmark.Type) != bookmark.Type {
				oldRootLocation, err := bookmarkRootLocation(
					prefs, target.platform, target.row.bookmark.Type, target.row.bookmark.Title)
				if err != nil {
					return err
				}
				if oldRootLocation != "" && sameBookmarkLocation(target.platform, location, oldRootLocation) {
					newRootLocation, err := bookmarkRootLocation(prefs, target.platform, bookmark.Type, bookmark.Title)
					if err != nil {
						return err
					}
					if newRootLocation != "" {
						location = newRootLocation
					}
				}
			}
			if _, err := tx.Exec(`UPDATE `+target.table+` SET
				title = ?, url = ?, chapters = ?, location = ?, site = ?, shortname = ?, type = ?, mangadex_id = ?, updated_at = CURRENT_TIMESTAMP
				WHERE id = ?`, bookmark.Title, bookmark.Url, bookmark.Chapters, location, bookmark.Site,
				bookmark.Shortname, bookmark.Type, strings.TrimSpace(bookmark.MangaDexID), target.row.id); err != nil {
				return fmt.Errorf("update %q in %s bookmarks: %w", bookmark.Title, target.platform, err)
			}
			if target.platform == "linux" {
				result.UpdatedToLinux++
			} else {
				result.UpdatedToWindows++
			}
		}
	}
	return nil
}

func latestBookmarksByIdentity(rows []platformBookmarkRow) map[string]platformBookmarkRow {
	latest := make(map[string]platformBookmarkRow, len(rows))
	for _, row := range rows {
		key := bookmarkIdentity(row.bookmark)
		if key == "" {
			continue
		}
		current, exists := latest[key]
		if !exists || row.updatedAt > current.updatedAt {
			latest[key] = row
		}
	}
	return latest
}

func sameBookmarkMetadata(left, right models.Bookmarks) bool {
	return left.Title == right.Title && left.Url == right.Url && left.Chapters == right.Chapters &&
		left.Site == right.Site && left.Shortname == right.Shortname &&
		bookmarkType(left.Type) == bookmarkType(right.Type) &&
		strings.TrimSpace(left.MangaDexID) == strings.TrimSpace(right.MangaDexID)
}

func bookmarkRootLocation(prefs preferences.Preferences, platform, kind, title string) (string, error) {
	root := strings.TrimSpace(prefs.RootDirectory(platform, kind))
	if root == "" {
		return "", nil
	}
	if platform == "linux" {
		var err error
		root, err = preferences.ExpandPath(root)
		if err != nil {
			return "", fmt.Errorf("expand %s root %q: %w", platform, root, err)
		}
	}
	return joinBookmarkRoot(root, title, platform), nil
}

func sameBookmarkLocation(platform, left, right string) bool {
	if platform == "windows" {
		left = strings.TrimRight(strings.ReplaceAll(left, "/", `\`), `\`)
		right = strings.TrimRight(strings.ReplaceAll(right, "/", `\`), `\`)
		return strings.EqualFold(left, right)
	}
	return path.Clean(left) == path.Clean(right)
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
