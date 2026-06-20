# validation Specification

## Purpose
Validate user input when adding manga to the bookmarks, ensuring all required fields for the selected site are provided before submission.

## Requirements

### Requirement: Field Validation
The system SHALL validate that all required fields for a given manga source site are present.

#### Scenario: Validate all required fields present
- GIVEN a site named "stonescape" with `required_fields: { url: true, title: true, location: true, shortname: false }`
- WHEN `ValidateAddManga` is called with a URL, title, and location
- THEN validation SHALL pass (return nil)

#### Scenario: Missing required field returns error
- GIVEN a site with `url` marked as required
- WHEN `ValidateAddManga` is called with an empty URL
- THEN an error SHALL be returned with message "URL is required"

#### Scenario: Missing title returns error
- GIVEN a site with `title` marked as required
- WHEN `ValidateAddManga` is called with an empty title
- THEN an error SHALL be returned with message "title is required"

#### Scenario: Missing location returns error
- GIVEN a site with `location` marked as required
- WHEN `ValidateAddManga` is called with an empty location
- THEN an error SHALL be returned with message "location is required"

#### Scenario: Missing shortname returns error
- GIVEN a site with `shortname` marked as required
- WHEN `ValidateAddManga` is called with an empty shortname
- THEN an error SHALL be returned with message "shortname is required"

### Requirement: Site Selection Validation
The system SHALL require a site to be selected before validating fields.

#### Scenario: No site selected
- GIVEN no site is selected (empty site name)
- WHEN `ValidateAddManga` is called
- THEN an error SHALL be returned with message "please select a site"

### Requirement: Unknown Site Handling
The system SHALL reject unknown site names.

#### Scenario: Unknown site name
- GIVEN a site name that does not exist in the SitesConfig
- WHEN `ValidateAddManga` is called
- THEN an error SHALL be returned with message "unknown site: {name}"

### Requirement: Independence from UI Framework
The validation logic SHALL operate on raw string values only, avoiding any dependency on the Fyne UI toolkit.

#### Scenario: Validation uses plain strings
- GIVEN the validation function signature
- WHEN it accepts siteName, title, shortname, url, and location as strings
- THEN it SHALL NOT import or reference any Fyne types
- AND it SHALL be usable from any context (UI, CLI, tests)
