# manga-bookmarks Specification

## Purpose
Manage the user's manga library through persistent bookmark storage, supporting CRUD operations and import/export functionality.

## Requirements

### Requirement: Bookmark Storage
The system SHALL persist manga bookmarks to disk as a JSON file.

#### Scenario: Load bookmarks on startup
- GIVEN the application starts
- WHEN the config directory exists at `~/.config/kansho/`
- THEN the system SHALL read `bookmarks.json` from that directory
- AND SHALL unmarshal it into the Manga data structure
- WHEN the bookmarks file does not exist
- THEN the system SHALL create an empty bookmarks structure (`{"manga": []}`)

#### Scenario: Save bookmarks
- GIVEN a manga is added or modified
- WHEN `SaveBookmarks` is called
- THEN the in-memory bookmarks SHALL be marshalled to indented JSON
- AND written to `~/.config/kansho/bookmarks.json`

### Requirement: Bookmark Data Structure
Each bookmark SHALL track essential manga metadata.

#### Scenario: Bookmark fields
- GIVEN a manga bookmark is stored
- WHEN its data is serialized
- THEN it SHALL contain: title, url, chapters, location, site, and shortname fields

### Requirement: Config Directory
The system SHALL ensure the config directory exists before any read/write operations.

#### Scenario: Create config directory
- GIVEN the application launches
- WHEN `~/.config/kansho/` does not exist
- THEN the system SHALL create the directory with 0755 permissions

### Requirement: Export Bookmarks
The system SHALL support exporting bookmarks to a user-selected file.

#### Scenario: Export bookmarks to file
- GIVEN the user selects "Export Bookmarks" from the menu
- WHEN a file save dialog is shown
- THEN the user SHALL choose a destination file path
- AND the current bookmarks JSON SHALL be written to that file

### Requirement: Import Bookmarks
The system SHALL support importing bookmarks from an external JSON file.

#### Scenario: Import bookmarks from file
- GIVEN the user selects "Import Bookmarks" from the menu
- WHEN a file open dialog is shown
- THEN the user SHALL select a JSON file
- AND the file contents SHALL be parsed as a Manga structure
- AND the imported entries SHALL be merged into the existing bookmarks
- AND duplicates SHALL be avoided

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
- THEN the Cloudflare debug logger SHALL be closed
