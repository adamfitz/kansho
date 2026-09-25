# mangadex-title-info Specification

## Purpose
Provide MangaDex title information (title, alt titles, description, author, artist, status, demographic, year, tags, cover, URL) for any bookmarked manga through an info (i) button on each manga list row. Title info is fetched from the MangaDex API, stored in a local SQLite database, and displayed in the main window's chapter list pane (in place of the chapter list) with copyable fields, a clickable MangaDex URL, and a language selector. The same SQLite file also stores per-source manga chapter-count snapshots for all supported sites; remote chapter URLs and chapter rows remain separate from these aggregates.

## Requirements

### Requirement: Info Button
The manga list SHALL show an info (i) button to the right of each manga title row.

#### Scenario: Info button on every row
- GIVEN the user views the manga list
- WHEN the info (i) icon on a row is clicked
- THEN Kansho SHALL open the MangaDex title information flow for that manga
- AND the info icon SHALL be rendered without button chrome (mirroring the per-chapter action icons)

### Requirement: Local Title Database
MangaDex title information and generic per-manga chapter-count snapshots SHALL be persisted in a SQLite database stored in the kansho config directory (`~/.config/kansho/mangadex.db`).

#### Scenario: Database location and schema
- GIVEN the user invokes the info button
- WHEN the database does not exist yet
- THEN the database SHALL be created on first use with `manga_title`, `manga_title_lookup`, and `manga_chapter_stats` tables
- AND `manga_chapter_stats` SHALL key records by a stable manga source identity and store title, site, URL, total chapters, downloaded chapters, not-downloaded chapters, and the update timestamp
- AND the database SHALL be a separate artifact from `bookmarks.json` (bookmarks remain JSON; title info and aggregate chapter counts live in the database)

#### Scenario: Detail fields stored
- GIVEN a MangaDex title is fetched
- WHEN the dashboard is saved locally
- THEN the stored record SHALL include at least: MangaDex ID, title (all languages), alt titles (all languages), description (all languages), original language, publication demographic, year, content rating, status, tags (all languages), author, artist, cover URL and MangaDex URL
- AND records SHALL be keyed on the unique MangaDex ID so re-fetching updates rather than duplicates

### Requirement: Lookup Sequence
The user SHALL be shown local database content when available, and a lookup dialog otherwise.

#### Scenario: Local information exists
- GIVEN a manga title exists in the local MangaDex database
- WHEN the info button is clicked for that title
- THEN the information pane SHALL open directly from local database content in the chapter list card
- AND no MangaDex network request SHALL be required to open the pane

#### Scenario: MangaDex URL known on the bookmark
- GIVEN the selected manga bookmark is itself a MangaDex title (its URL contains a `/title/<id>` segment)
- WHEN the info button is clicked and no local record exists
- THEN Kansho SHALL fetch that ID directly from MangaDex
- AND SHALL NOT show the title lookup dialog

#### Scenario: No local information
- GIVEN a bookmarked manga has no local MangaDex information and its URL is not a MangaDex title URL
- WHEN the info button is clicked
- THEN a MangaDex title lookup dialog SHALL appear
- AND the dialog SHALL be pre-filled with the bookmarked manga title as the search query

### Requirement: Persistence
Every completed lookup outcome SHALL be written to the local database so the title can be served locally from then on.

#### Scenario: Manual URL lookup is saved
- GIVEN the user enters a MangaDex URL or title ID and the lookup succeeds
- WHEN the title information is shown
- THEN the fetched title SHALL be stored in the database
- AND the bookmarked manga title SHALL be mapped to the fetched MangaDex title ID

#### Scenario: Search result selection is saved
- GIVEN the user selects a title from the search results
- WHEN the title information is shown
- THEN the fetched title SHALL be stored in the database
- AND the bookmarked manga title SHALL be mapped to the selected MangaDex title ID

#### Scenario: Local lookup afterwards
- GIVEN a bookmarked title has already been resolved to a MangaDex title once
- WHEN the info button is clicked again for the same bookmark
- THEN Kansho SHALL serve the information from the local database (via the stored title mapping)
- AND SHALL NOT show the lookup dialog again
- AND SHALL NOT require a MangaDex network request to open the window

### Requirement: Title Lookup Dialog
The lookup dialog SHALL let the user pick the correct MangaDex title for a bookmark.

#### Scenario: Search results
- GIVEN the user searches MangaDex titles
- WHEN results are returned
- THEN the first page SHALL show up to 50 matching titles
- AND each result row SHALL display the English title (with the Japanese title in parentheses when available)

#### Scenario: Infinite scroll pagination
- GIVEN a search returns more than one page of results
- WHEN the user scrolls near the bottom of the result list
- THEN the next page SHALL be loaded in the background and appended
- AND a status label SHALL report how many matching titles have been shown

#### Scenario: Selecting a result
- GIVEN the user taps a search result row
- WHEN the row is clicked
- THEN the corresponding MangaDex title SHALL be fetched, stored locally, and shown in the information pane

#### Scenario: Manual URL / title ID field
- GIVEN the user enters a MangaDex URL (`https://mangadex.org/title/<id>`) or a bare title ID in the manual field
- WHEN the fetch action is triggered
- THEN Kansho SHALL fetch that title directly, ignoring the search results
- AND a manual entry SHALL take precedence over any tapped search result until it is cleared

### Requirement: Information Pane
The information pane SHALL display every stored detail for a MangaDex title inside the main window's chapter list card.

#### Scenario: Pane display
- GIVEN the information pane replaces the chapter list in the chapter list card
- THEN it SHALL show the title, MangaDex ID, original language, status, demographic, year, authors, artists, content rating, tags, alt titles, description, and the MangaDex URL
- AND the chapter list header/footer SHALL be replaced by an info header with a "Back to Chapter List" button so the user can return to the chapters

#### Scenario: Back to chapter list
- GIVEN the information pane is open
- WHEN the user clicks the "Back to Chapter List" button
- THEN the chapter list SHALL be shown again with the previous manga selection and its chapters restored

#### Scenario: Selecting a manga leaves the pane
- GIVEN the information pane is open
- WHEN the user selects a different manga in the manga list
- THEN the pane SHALL close and the chapter list for the newly selected manga SHALL be shown

#### Scenario: Copyable fields
- GIVEN the information pane is open
- WHEN the user interacts with any field
- THEN every field SHALL be highlightable and copyable (drag-select, Ctrl+C, right-click Copy)

#### Scenario: Clickable MangaDex link
- GIVEN the information pane is open
- WHEN the user clicks the MangaDex URL itself
- THEN the MangaDex title page SHALL open in the default browser
- AND a Copy button SHALL copy the full raw URL to the clipboard

#### Scenario: Refresh button
- GIVEN the information pane is open
- WHEN the user clicks the Refresh button
- THEN Kansho SHALL re-query MangaDex for the title ID
- AND SHALL show a notice that the title is being refreshed
- AND SHALL store the fresh record in the database
- AND SHALL redraw the pane with the refreshed title data
- AND the current language selection SHALL be preserved

#### Scenario: Refresh with no MangaDex result
- GIVEN the user clicks the Refresh button
- WHEN the MangaDex query fails or returns nothing
- THEN Kansho SHALL prompt the user to enter a MangaDex URL or title ID
- WHEN the user enters a valid URL or title ID
- THEN Kansho SHALL fetch that title
- AND SHALL store it in the database (updating the title mapping)
- AND SHALL redraw the pane with the fetched title data
- WHEN the entered value is not a valid MangaDex URL or the fetch fails
- THEN Kansho SHALL show the error inside the prompt and keep it open

### Requirement: Language Selection
The information pane SHALL default to English and let the user choose from any language the MangaDex title is actually published in.

#### Scenario: English by default
- GIVEN the information pane opens
- THEN English SHALL be the initially displayed language when English data is available

#### Scenario: Language dropdown
- GIVEN the information pane is open
- WHEN the user opens the language dropdown
- THEN the options SHALL be derived from the languages MangaDex publishes for the title (the union of title / alt-title / description / tag languages)

#### Scenario: Language switch
- GIVEN the user picks a different language from the dropdown
- THEN Kansho SHALL inform the user that the requested language is being queried
- AND SHALL re-query MangaDex for the title
- AND SHALL redraw the pane fields in that language
- AND SHALL persist the fetched record to the local database

### Requirement: Database Menu
The application menu bar SHALL include a "Database" menu next to Bookmarks with Backup, Restore, and Compact options for the local database containing MangaDex title information and manga chapter-count snapshots.

#### Scenario: Backup database
- GIVEN the user selects Database → Backup
- WHEN a destination file is chosen
- THEN a consistent VACUUM snapshot of the database SHALL be written to the chosen path

#### Scenario: Restore database
- GIVEN the user selects Database → Restore and picks a backup file
- WHEN the file passes schema validation and an integrity check
- THEN the current database SHALL be replaced by the chosen file
- AND the previous database SHALL be preserved as `mangadex.pre-restore.db` before the swap
- WHEN the file does not validate (missing schema or failed integrity check)
- THEN the restore SHALL be rejected

#### Scenario: Compact database
- GIVEN the user selects Database → Compact
- WHEN compression executes
- THEN the database SHALL be optimised (`PRAGMA optimize`) and shrunk (`VACUUM`)
- AND an integrity check SHALL be run afterwards