package mangadex

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"kansho/models"
	"kansho/preferences"
)

const bookmarksMigratedKey = "bookmarks_migrated_at"

// LoadBookmarks returns every bookmark stored in the database.
func (s *Store) LoadBookmarks() (models.Manga, error) {
	rows, err := s.conn.Query(`
		SELECT title, url, chapters, location, site, shortname
		FROM bookmarks
		ORDER BY lower(title), id`)
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
		if _, err := tx.Exec(`DELETE FROM bookmarks`); err != nil {
			return fmt.Errorf("clear bookmarks: %w", err)
		}
		stmt, err := tx.Prepare(`
			INSERT INTO bookmarks
				(title, url, chapters, location, site, shortname, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`)
		if err != nil {
			return fmt.Errorf("prepare bookmark insert: %w", err)
		}
		defer stmt.Close()

		for _, bookmark := range data.Manga {
			if _, err := stmt.Exec(
				bookmark.Title,
				bookmark.Url,
				bookmark.Chapters,
				bookmark.Location,
				bookmark.Site,
				bookmark.Shortname,
			); err != nil {
				return fmt.Errorf("insert bookmark %q: %w", bookmark.Title, err)
			}
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
