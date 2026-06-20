# request-executor Specification

## Purpose
Provide an HTTP-first content fetching strategy with automatic browser fallback, optimizing for speed while ensuring JavaScript-rendered pages are still accessible.

## Requirements

### Requirement: HTTP-First Strategy
The system SHALL attempt HTTP fetching before resorting to browser automation.

#### Scenario: HTTP fetch succeeds
- GIVEN a target URL that renders content server-side
- WHEN `FetchHTML` is called
- THEN the system SHALL first attempt an HTTP GET via `HTTPClient.FetchHTML`
- AND if successful, return the HTML directly without launching a browser

#### Scenario: HTTP fails with non-CF error, fall back to browser
- GIVEN a target URL that requires JavaScript rendering
- WHEN `FetchHTML` is called and the HTTP attempt fails with a non-CF error
- THEN the system SHALL create a `BrowserSession`
- AND SHALL navigate to the URL using Chromium
- AND SHALL wait for the optional CSS selector
- AND SHALL return the rendered HTML

#### Scenario: CF challenge stops execution
- GIVEN a target URL behind CF protection
- WHEN `FetchHTML` is called and HTTP detects a CF challenge
- THEN the system SHALL NOT attempt browser fallback
- AND SHALL return the `CfChallengeError` immediately

### Requirement: Domain Resolution
The system SHALL resolve the domain from the actual target URL, not from hardcoded site plugin values.

#### Scenario: Derive domain from URL
- GIVEN a target URL "https://www.example.com/manga/title"
- WHEN `NewRequestExecutor` is called
- THEN the domain SHALL be extracted as "www.example.com"
- AND the HTTP client SHALL be created for that domain

### Requirement: Debug Support
The system SHALL support saving fetched HTML to disk for debugging.

#### Scenario: Debug HTML saving
- GIVEN a `Debugger` with `SaveHTML = true` and a valid `HTMLPath`
- WHEN `NewRequestExecutor` is called
- THEN the HTTP client's debug flags SHALL be set to save HTML responses to the specified path
