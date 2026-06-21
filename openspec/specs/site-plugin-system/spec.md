# site-plugin-system Specification

## Purpose
Define the plugin architecture for integrating manga source websites, allowing new sites to be added without modifying core download logic.

## Requirements

### Requirement: Site Plugin Interface
The system SHALL define a `SitePlugin` interface that all manga source sites must implement.

#### Scenario: Required interface methods
- GIVEN a site plugin implementation
- WHEN the system validates the plugin
- THEN it MUST implement `GetSiteName() string`
- AND it MUST implement `GetDomain() string`
- AND it MUST implement `NeedsCFBypass() bool`
- AND it MUST implement `GetChapterExtractionMethod() *ChapterExtractionMethod`
- AND it MUST implement `GetImageExtractionMethod() *ImageExtractionMethod`
- AND it MUST implement `NormalizeChapterURL(rawURL, baseURL string) string`
- AND it MUST implement `NormalizeChapterFilename(chapterData map[string]string) string`

#### Scenario: Compile-time interface check
- GIVEN a site plugin struct
- WHEN the developer declares the interface conformance
- THEN `var _ downloader.SitePlugin = (*MySite)(nil)` SHALL compile without error

### Requirement: Site Registration
The system SHALL provide a registry for site download functions.

#### Scenario: Register a site
- GIVEN the `RegisterSite` function is called during package initialization
- WHEN a site name and download function are provided
- THEN the site SHALL be stored in a global registry map
- AND subsequent downloads for that site name SHALL dispatch to the registered function

#### Scenario: Dispatch to registered site
- GIVEN a download task for site "stonescape"
- WHEN the dispatcher looks up "stonescape" in the registry
- THEN it SHALL call the registered download function with the manga data and progress callback
- WHEN the site is not registered
- THEN an error SHALL be returned indicating the site is not supported

### Requirement: Extraction Methods
The system SHALL support multiple chapter and image extraction strategies.

#### Scenario: JavaScript extraction type
- GIVEN a site configured with `extraction.Type = "javascript"`
- WHEN the downloader processes chapters or images
- THEN it SHALL use chromedp to navigate the page and evaluate the provided JavaScript code

#### Scenario: HTML selector extraction type
- GIVEN a site configured with `extraction.Type = "html_selector"`
- WHEN the downloader processes chapters or images
- THEN it SHALL fetch the page HTML via HTTP or browser
- AND it SHALL use goquery with the provided CSS selector to extract data

#### Scenario: Custom parser extraction type
- GIVEN a site configured with `extraction.Type = "custom"`
- WHEN the downloader processes chapters or images
- THEN it SHALL fetch the raw HTML content
- AND it SHALL invoke the provided CustomParser function to extract data from the HTML

#### Scenario: API extraction type
- GIVEN a site configured with `extraction.Type = "api"`
- WHEN the downloader processes chapters or images
- THEN it SHALL create an APIClient with CF bypass support
- AND it SHALL invoke the provided APIFunc to make API requests and extract data

### Requirement: Chapter Filename Normalization
The system SHALL normalize chapter data into standardized CBZ filenames.

#### Scenario: Normalize integer chapter number
- GIVEN a chapter with number "1"
- WHEN `NormalizeChapterFilename` is called for a site that pads to 3 digits
- THEN the filename SHALL be "ch001.cbz"

#### Scenario: Normalize decimal chapter number
- GIVEN a chapter with number "1.5"
- WHEN `NormalizeChapterFilename` is called for a site that pads to 3 digits
- THEN the filename SHALL be "ch001.5.cbz"

### Requirement: API Client User Agent
API-based extraction (Type="api") SHALL use a non-spoofed, identifiable User-Agent string rather than a generic browser User-Agent.

#### Scenario: API client uses application User-Agent
- GIVEN an `APIClient` is created for a domain
- WHEN the client is constructed
- THEN the Colly collector's User-Agent SHALL be set to `kansho/1.0` (or another identifiable application string)
- AND it SHALL NOT use a generic browser User-Agent like `Mozilla/5.0 ...`
- AND this SHALL satisfy API provider requirements for identifying the client application

### Requirement: Debug Support
The system SHALL support optional debugging per site plugin via the `DebugSite` interface.

#### Scenario: Site implements debug interface
- GIVEN a site plugin that implements `DebugSite`
- WHEN `Debugger()` is called
- THEN it SHALL return a `*Debugger` struct with `SaveHTML` and `HTMLPath` fields
- AND the debugger MAY be nil if debugging is not enabled
- WHEN `SaveHTML` is true and `HTMLPath` is set
- THEN the downloader SHALL save fetched HTML to the specified path

### Requirement: Site Configuration
The system SHALL embed a site configuration file that specifies which fields are required when adding manga from each source.

#### Scenario: Load sites config
- GIVEN the application starts
- WHEN the embedded `sites.json` is loaded
- THEN each site entry SHALL provide name, display_name, and required_fields
- AND required_fields SHALL specify whether URL, shortname, title, and location are mandatory

#### Scenario: Validate manga input against site config
- GIVEN a user adds manga from site "stonescape"
- WHEN all required fields for "stonescape" are provided
- THEN validation SHALL pass
- WHEN any required field is missing
- THEN validation SHALL return an error indicating which field is required
