# user-interface Specification

## Purpose
Provide a graphical desktop interface for managing manga bookmarks, initiating downloads, and monitoring progress.

## Requirements

### Requirement: Main Layout
The system SHALL display a two-column layout with a gradient background.

#### Scenario: Main layout structure
- GIVEN the application is running
- WHEN the main window is displayed
- THEN a purple gradient background (45 degrees, RGB 115/103/240 to 136/84/208) SHALL fill the window
- AND a header SHALL appear at the top with the application title
- AND a left column SHALL contain the manga list (top 50%) and edit/add manga form (bottom 50%)
- AND a right column SHALL contain the chapter list (100% height)
- AND a footer SHALL appear at the bottom with attribution text

### Requirement: Application State
The system SHALL use a centralized state object to coordinate UI components.

#### Scenario: Centralized state
- GIVEN the application initializes
- WHEN `NewKanshoAppState` is created
- THEN it SHALL load bookmarks from disk
- AND initialize with no manga selected (SelectedMangaID = -1)
- AND provide observer-style callbacks for manga selection, addition, and deletion

#### Scenario: Select manga triggers callbacks
- GIVEN a user clicks on a manga in the list
- WHEN `SelectManga(id)` is called
- THEN the selected manga ID SHALL be updated
- AND all registered OnMangaSelected callbacks SHALL be invoked

### Requirement: Manga List View
The system SHALL display all bookmarked manga in a scrollable list.

#### Scenario: Display manga list
- GIVEN the user has bookmarked manga titles
- WHEN the manga list view is rendered
- THEN each manga SHALL display its title and site name
- AND clicking a manga SHALL select it and trigger the chapter list update
- AND "Edit Manga" and "Delete Manga" buttons SHALL be available per entry

### Requirement: Add/Edit Manga Form
The system SHALL provide a form for adding new manga or editing existing ones.

#### Scenario: Dynamic form fields
- GIVEN the user selects a site from the dropdown
- WHEN the site selection changes
- THEN the form SHALL dynamically show or hide fields based on the selected site's RequiredFields config
- AND validation SHALL check that all required fields for the selected site are filled

#### Scenario: Add manga to bookmarks
- GIVEN all required fields are filled for the selected site
- WHEN the user clicks "Add Manga"
- THEN the manga SHALL be added to the bookmarks data
- AND persisted to disk immediately
- AND listed in the download queue (if auto-download is configured)
- AND the manga list view SHALL refresh to show the new entry

### Requirement: Chapter List View
The system SHALL display the chapters of the currently selected manga.

#### Scenario: Show chapters for selected manga
- GIVEN a manga is selected in the manga list
- WHEN the chapter list view receives a selection callback
- THEN it SHALL list all locally downloaded CBZ files for that manga
- AND display chapter numbers extracted from filenames
- AND show the download progress if a download is active

### Requirement: Download Queue View
The system SHALL display the current download queue with progress information.

#### Scenario: Show download queue
- GIVEN there are active or queued downloads
- WHEN the download queue view is rendered
- THEN each task SHALL display: manga title, status, progress bar, and status message
- AND cancel buttons SHALL be available for active and queued tasks

#### Scenario: Progress updates in real-time
- GIVEN a download is in progress
- WHEN the progress callback updates the task state
- THEN the queue view SHALL reflect the updated progress bar value
- AND the status message SHALL update with current chapter and image information

### Requirement: Keyboard Shortcuts and Menus
The system SHALL provide menus and keyboard shortcuts for common operations.

#### Scenario: Menu items
- GIVEN the application menu is visible
- WHEN the user opens the File menu
- THEN "Logs" SHALL open the log display window
- WHEN the user opens the Bookmarks menu
- THEN "Bookmarks" SHALL open the bookmarks window
- AND "Export Bookmarks" SHALL open a save dialog
- AND "Import Bookmarks" SHALL open a file picker
- WHEN the user opens the Help menu
- THEN "About" SHALL show an about dialog with version information

#### Scenario: Config window
- GIVEN the user presses Ctrl+Shift+C
- WHEN the config window opens
- THEN it SHALL display application configuration including version, git commit, build time, and rlv (rlv companion tool) version
