# mangadex Specification

## Purpose
Integrate with the MangaDex API to fetch chapter lists and download chapter images, using the official API rather than HTML scraping.

## Requirements

### Requirement: API-Based Extraction
The system SHALL use the MangaDex REST API for all data retrieval.

#### Scenario: Chapter list via API
- GIVEN a MangaDex manga ID
- WHEN chapters are fetched
- THEN the system SHALL call `GET https://api.mangadex.org/manga/{id}/feed`
- AND SHALL paginate with offset up to the total chapter count
- AND SHALL filter for `translatedLanguage[]=en`
- AND SHALL include content ratings: safe, suggestive, and erotica
- AND SHALL order by `order[chapter]=asc`
- AND SHALL enforce a 250ms delay between paginated requests

#### Scenario: Image URLs via @Home API
- GIVEN a chapter ID
- WHEN image URLs are requested
- THEN the system SHALL call `GET https://api.mangadex.org/at-home/server/{chapterID}`
- AND SHALL construct full image URLs from the returned `baseUrl`, `hash`, and each `data` filename

### Requirement: User-Agent Policy
The MangaDex API Terms of Service require that all API clients identify themselves with a non-spoofed, unique User-Agent string. Using a generic browser User-Agent (spoofing) MAY result in the request being blocked or rate-limited.

#### Scenario: API requests use application User-Agent
- GIVEN an API request is made to `api.mangadex.org`
- WHEN the HTTP client sends the request
- THEN the `User-Agent` header SHALL be set to `kansho/1.0` (or another identifiable application string)
- AND SHALL NOT use a generic browser User-Agent (e.g., `Mozilla/5.0 ...`)
- AND the User-Agent SHALL be unique enough for MangaDex to identify and contact the application maintainer if needed

#### Scenario: Non-API requests use browser User-Agent
- GIVEN a request is made to a non-API domain (e.g., `mangadex.org` for user-facing pages)
- WHEN the request does not target `api.mangadex.org`
- THEN the system MAY use a standard browser User-Agent
- AND the API User-Agent policy SHALL NOT apply

### Requirement: URL Parsing
The system SHALL extract the manga ID from user-provided MangaDex URLs.

#### Scenario: Extract manga ID from URL
- GIVEN a URL like `https://mangadex.org/title/a1b2c3d4/manga-name`
- WHEN `extractMangaDexID` is called
- THEN it SHALL return `a1b2c3d4`
- WHEN the URL is `https://mangadex.org/title/a1b2c3d4`
- THEN it SHALL return `a1b2c3d4`
- WHEN no `/title/` segment exists
- THEN an error SHALL be returned

### Requirement: Chapter Filtering
The system SHALL skip unavailable or invalid chapters.

#### Scenario: Skip zero-page chapters
- GIVEN a chapter has `pages = 0`
- WHEN processing the chapter list
- THEN that chapter SHALL be skipped with a warning log

#### Scenario: Skip chapters without a number
- GIVEN a chapter has a null chapter attribute
- WHEN processing the chapter list
- THEN that chapter SHALL be skipped with a warning log

### Requirement: No CF Bypass Needed
The MangaDex API does not use CF protection, unlike many HTML-based manga sites.

#### Scenario: CF bypass disabled
- GIVEN a MangaDex site plugin instance
- WHEN `NeedsCFBypass()` is called
- THEN it SHALL return `false`

### Requirement: Filename Normalization
The system SHALL produce consistent CBZ filenames from MangaDex chapter numbers.

#### Scenario: Normalize integer chapter
- GIVEN chapter number `"91"`
- WHEN `NormalizeChapterFilename` is called
- THEN the filename SHALL be `ch091.cbz`

#### Scenario: Normalize decimal chapter
- GIVEN chapter number `"91.5"`
- WHEN `NormalizeChapterFilename` is called
- THEN the filename SHALL be `ch091.5.cbz`
