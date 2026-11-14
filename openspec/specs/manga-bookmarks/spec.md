# manga-bookmarks Specification

## Purpose
Manage the user's shared title library through persistent SQLite storage, with one title catalog represented in both OS tables and a separate directory location for each operating system.

## Requirements

### Requirement: Bookmark Storage
The system SHALL persist manga bookmarks in the local SQLite database.

#### Scenario: Load bookmarks on startup
- GIVEN the application starts
- WHEN the database is opened
- THEN Linux SHALL load bookmarks from `bookmarks_linux` and Windows SHALL load bookmarks from `bookmarks_windows`
- AND SHALL expose them through the shared `models.Manga` structure
- AND SHALL synchronize shared title metadata between the OS tables while preserving custom locations
- AND SHALL update a platform location to the new type's configured root when its old location matched the old type's configured root
- WHEN the database has no bookmark rows
- THEN the system SHALL return an empty bookmarks structure (`{"manga": []}`)

#### Scenario: Save bookmarks
- GIVEN a manga is added, edited, or deleted
- WHEN `SaveBookmarks` is called
- THEN the complete in-memory bookmark list SHALL replace the contents of the current OS bookmark table inside one immediate write transaction
- AND the previous bookmark rows SHALL be removed in the same transaction
- AND title metadata SHALL be upserted into the other OS table while preserving its location unless its type changes and it uses the old type's configured root
- AND the write SHALL acquire Kansho's advisory write lock before changing the database

#### Scenario: Trim bookmark whitespace
- GIVEN a bookmark title or URL has leading or trailing whitespace
- WHEN the bookmark is added, imported, saved, or loaded
- THEN the title and URL SHALL be trimmed
- AND the stored and displayed title SHALL match the trimmed value

#### Scenario: Bookmark ordering
- GIVEN bookmarks are saved
- WHEN they are loaded again
- THEN they SHALL be ordered by title and row ID

### Requirement: Bookmark Data Structure
Each bookmark SHALL track essential manga metadata.

#### Scenario: Bookmark fields
- GIVEN a manga bookmark is stored
- WHEN its data is loaded
- THEN it SHALL contain: title, url, chapters, location, site, shortname, type, and optional MangaDex ID fields
- AND `type` SHALL be `manga` or `webtoon`

#### Scenario: Choose bookmark type
- GIVEN the user adds a bookmark
- WHEN the user explicitly selects Manga or Webtoon in the required radio group
- THEN the bookmark SHALL be stored in the selected OS table with that type
- AND the selection SHALL be stored in both OS tables
- AND no type SHALL be selected by default

#### Scenario: Change type for a bookmark using configured roots
- GIVEN a bookmark uses the configured root for its current type
- AND a configured root exists for the other type on each platform
- WHEN the user changes the bookmark type
- THEN its location SHALL use the configured root for the new type on the current platform
- AND the other platform's location SHALL be rebased to its configured root for the new type
- AND custom locations SHALL remain unchanged

### Requirement: Config Directory
The system SHALL ensure the config directory exists before any read/write operations.

#### Scenario: Create config directory
- GIVEN the application launches
- WHEN `~/.config/kansho/` does not exist
- THEN the system SHALL create the directory with 0755 permissions

### Requirement: Database Location Preferences
The system SHALL store the database directory in a YAML preferences file.

#### Scenario: Preferences file
- GIVEN the application launches
- WHEN preferences are loaded
- THEN the preferences file SHALL be `~/.config/kansho/kansho.conf`
- AND it SHALL contain a `database_directory` value
- AND the default database directory SHALL be `~/.config/kansho`
- AND the database file name SHALL be `kansho.db`
- AND optional roots SHALL be stored for Linux manga, Linux webtoons, Windows manga, and Windows webtoons
- AND all four roots SHALL be empty by default

#### Scenario: Apply a root to existing bookmarks
- GIVEN the user changes a root and checks its apply option
- WHEN the user clicks Apply Selected Roots
- THEN Kansho SHALL create a database snapshot in a `backup` subdirectory before changing paths
- AND SHALL update locations only for matching OS and bookmark type rows to `<root>/<title>`
- AND SHALL reload the library from the updated database
- AND SHALL NOT move files

#### Scenario: Save a root without applying it
- GIVEN the user changes a root and leaves its apply option unchecked
- WHEN the user saves preferences without applying the root
- THEN Kansho SHALL save the root for future bookmarks
- AND existing bookmark locations SHALL remain unchanged

#### Scenario: Add bookmark with a configured root
- GIVEN a root exists for the current OS and selected bookmark type
- WHEN the user adds a bookmark without selecting the custom-directory override
- THEN its location SHALL be `<root>/<title>`
- WHEN the user selects the custom-directory override
- THEN the selected directory SHALL be used instead

#### Scenario: Edit bookmark type with a configured root
- GIVEN the bookmark's current location matches the configured root for its current type
- WHEN the user changes its type
- THEN the directory field SHALL update to `<new-type-root>/<title>`
- AND saving SHALL use that updated location

#### Scenario: Apply a new database directory
- GIVEN the user selects a new database directory in File → Preferences
- WHEN the user saves the preferences
- THEN Kansho SHALL persist the directory, reopen the database at the new location, and reload the main interface
- AND Kansho SHALL NOT keep two live databases at the same time

### Requirement: Legacy Bookmark Migration
The system SHALL migrate an existing `bookmarks.json` file into the database once.

#### Scenario: Migrate legacy JSON bookmarks
- GIVEN `~/.config/kansho/bookmarks.json` exists
- AND the database does not yet contain the `bookmarks_migrated_at` marker
- WHEN the database is opened
- THEN every bookmark from the JSON file SHALL be inserted into the `bookmarks` table
- AND the legacy table rows SHALL be copied into the current OS table without deleting the legacy table
- AND the migration marker SHALL be recorded in `kansho_meta`
- AND the JSON file SHALL be renamed to `bookmarks.json.bak`
- WHEN a file named `bookmarks.json.bak` already exists
- THEN the legacy file SHALL be renamed with a timestamp suffix instead

#### Scenario: Migration failure
- GIVEN the legacy file cannot be parsed or renamed
- WHEN migration fails
- THEN the failure SHALL be logged
- AND the application SHALL continue with an empty or existing bookmark table
- AND the legacy file SHALL NOT be deleted

#### Scenario: Migration runs once
- GIVEN the `bookmarks_migrated_at` marker exists
- WHEN the database is opened again
- THEN Kansho SHALL NOT import the legacy JSON file again

### Requirement: Export Bookmarks
The system SHALL support exporting bookmarks to a user-selected file.

#### Scenario: Export bookmarks to file
- GIVEN the user selects "Export Bookmarks" from the menu
- WHEN a file save dialog is shown
- THEN the user SHALL choose a destination file path
- AND the current bookmarks from the database SHALL be written to that file as JSON

### Requirement: Import Bookmarks
The system SHALL support importing bookmarks from an external JSON file.

#### Scenario: Import bookmarks from file
- GIVEN the user selects "Import Bookmarks" from the menu
- WHEN a file open dialog is shown
- THEN the user SHALL select a JSON file
- AND the file contents SHALL be parsed as a Manga structure
- AND the imported entries SHALL be merged into the existing bookmarks
- AND duplicates SHALL be avoided
- AND the merged bookmark list SHALL be saved to the database

### Requirement: Bulk Import Existing Titles
The system SHALL import existing on-disk titles from a CSV or YAML file into the current OS bookmark table.

#### Scenario: Select an import file
- GIVEN the user selects Database → Import Existing Titles
- WHEN the file open dialog is shown
- THEN the dialog SHALL accept `.csv`, `.yaml`, and `.yml` files
- AND the selected file SHALL be parsed according to its extension

#### Scenario: Validate the complete import
- GIVEN the user selects a CSV or YAML file
- WHEN the file is parsed
- THEN every row SHALL require a non-empty title, location, and type
- AND type SHALL be `manga` or `webtoon`
- AND an optional MangaDex link or ID SHALL resolve to a MangaDex title ID
- AND any validation problem SHALL list the row and title
- AND the import SHALL NOT write any bookmark when problems exist

#### Scenario: Normalize imported values
- GIVEN an import row is valid
- WHEN it is prepared for storage
- THEN title, location, MangaDex value, site, and URL SHALL have leading and trailing whitespace removed
- AND type SHALL be lowercased
- AND the current operating system SHALL select `bookmarks_linux` or `bookmarks_windows`; the file SHALL NOT contain an OS column

#### Scenario: Skip duplicate titles
- GIVEN the current bookmark table already contains a title
- WHEN an import row has the same title, ignoring case and surrounding whitespace
- THEN that row SHALL be skipped and counted as a duplicate
- AND duplicate titles inside the import file SHALL be reported as validation problems

#### Scenario: Save imported titles
- GIVEN the import file has no validation problems
- WHEN the user confirms the import
- THEN the new bookmarks SHALL be appended and saved through the normal bookmark write path
- AND the normal cross-platform sync SHALL create counterparts in the other OS table
- AND no files SHALL be moved or downloaded
- AND the summary SHALL report imported, duplicate, resolved, no-MangaDex, failed, and skipped counts

#### Scenario: Resolve MangaDex metadata
- GIVEN an imported bookmark has a MangaDex link or ID
- WHEN the bookmark is saved
- THEN Kansho SHALL look up each title sequentially after the save
- AND the MangaDex ID SHALL already be linked to the bookmark before lookup
- AND when MangaDex offers multiple English titles, the prompt SHALL name the title being imported
- AND skipping the prompt SHALL leave the bookmark imported with its MangaDex ID but without newly stored metadata

### Import File Format

Imported locations are stored as provided; Kansho does not create, move, or verify the directories during import.

Leave `site` empty or set it to `local` for Local titles. Kansho stores both as `local`, and the Add/Edit form shows `Local`. Scraping actions require a real site and URL. A Local title MAY still use a MangaDex link or ID so MangaDex metadata can be fetched and applied to its volumes and chapters.

The first CSV row is the header. Header names are case-insensitive; spaces, hyphens, and underscores are treated the same. Empty data rows are ignored.

| Field | Required | Accepted header aliases |
|-------|----------|-------------------------|
| Title | Yes | `title`, `name`, `manga`, `manga_title`, `title_name` |
| Location | Yes | `location`, `path`, `directory`, `folder`, `disk_location`, `location_on_disk` |
| Type | Yes | `type`, `bookmark_type`, `kind` |
| MangaDex | No | `mangadex`, `mangadex_id`, `mangadex_url`, `mangadex_link`, `mangadex_link_or_id`, `id` |
| Site | No | `site`, `source`, `source_site` |
| URL | No | `url`, `source_url`, `web_url` |

CSV example:

```csv
Title,Location,MangaDex Link or ID,Bookmark Type,Site,URL
One Piece,/home/user/manga/One Piece,https://mangadex.org/title/abc-123,manga,mangadex,https://example.test/one-piece
"Title, With Comma",/home/user/manga/Title With Comma,,manga,,
```

Quote a CSV field when it contains a comma, quote, or newline.

A YAML file MAY use a `titles:` list or a top-level list. The `type` field is required; all other fields are optional except `title` and `location`.

```yaml
titles:
  - title: One Piece
    location: /home/user/manga/One Piece
    mangadex: https://mangadex.org/title/abc-123
    type: manga
    site: mangadex
    url: https://example.test/one-piece
  - title: Solo Leveling
    location: /home/user/webtoons/Solo Leveling
    type: webtoon
```

Top-level list form:

```yaml
- title: Solo Leveling
  location: /home/user/webtoons/Solo Leveling
  type: webtoon
```

YAML aliases:

| Field | Accepted keys |
|-------|---------------|
| Title | `title`, `name` |
| Location | `location`, `path` |
| Type | `type`, `bookmark_type` |
| MangaDex | `mangadex`, `mangadex_id`, `mangadex_url`, `mangadex_link` |
| Site | `site` |
| URL | `url` |

### Requirement: Cross-Platform Bookmark Sync
The system SHALL keep title metadata synchronized between Linux and Windows bookmark tables while preserving distinct platform locations.

#### Scenario: Match shared titles
- GIVEN a bookmark exists in one OS table
- WHEN Kansho saves bookmarks, opens the database, or the user selects Database → Sync Linux/Windows Bookmarks
- THEN Kansho SHALL create or update its counterpart in the other OS table
- AND SHALL match by MangaDex ID when present, otherwise by source URL or site and title
- AND SHALL copy title, URL, chapters, site, shortname, type, and MangaDex ID
- AND SHALL preserve the existing location in each OS table
- AND SHALL use the destination OS root for a new counterpart when configured
- AND SHALL NOT copy a source OS location into the destination OS table

#### Scenario: Update shared title metadata
- GIVEN both OS tables contain the same title identity with different metadata
- WHEN Kansho syncs bookmarks
- THEN Kansho SHALL update the older title metadata from the more recently updated row
- AND SHALL leave both platform locations unchanged

#### Scenario: Sync a title without MangaDex ID
- GIVEN a bookmark has a source URL or a site and title but no MangaDex ID
- WHEN the user syncs bookmarks
- THEN Kansho SHALL synchronize it using the available title identity

### Requirement: Transaction Locks
The system SHALL protect bookmark writes with an advisory database lock.

#### Scenario: Concurrent write protection
- GIVEN another Kansho instance holds the advisory write lock
- WHEN a bookmark write is attempted
- THEN the write SHALL fail with a lock error that identifies the lock owner and age

#### Scenario: Same-instance writes queue
- GIVEN one Kansho operation is writing to the database
- WHEN another operation in the same Kansho process writes at the same time (for example a metadata Repair advancing its cursor while a chapter download saves bookmark state)
- THEN the second write SHALL wait for the first write to finish and SHALL then succeed
- AND no `LockedError` SHALL be reported for a lock held by the same process
- AND single-statement cursor updates SHALL use the same in-process write queue as transactional writes
- AND readers SHALL continue under SQLite WAL while writes are serialized

#### Scenario: Clear stale locks
- GIVEN the user opens Database → Clear Transaction Locks
- WHEN the user confirms the warning
- THEN all rows in `kansho_locks` SHALL be removed

### Requirement: Logging
The system SHALL maintain a rotating log file.

#### Scenario: Log file setup
- GIVEN the application initializes
- WHEN the config directory is verified
- THEN a log file SHALL be created at `~/.config/kansho/kansho.log`
- AND log rotation SHALL be configured with 10MB max file size
- AND up to 2 compressed backups SHALL be kept

#### Scenario: Close loggers on exit
- GIVEN the application is quitting
- WHEN `CloseLoggers` is called
- THEN the CF debug logger SHALL be closed
