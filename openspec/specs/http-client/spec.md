# http-client Specification

## Purpose
Provide a unified HTTP client with automatic retry, Cloudflare bypass, response decompression, and debugging support for all network requests.

## Requirements

### Requirement: Unified HTTP Client
The system SHALL provide a single HTTP client that handles CF bypass, retries, and decompression.

#### Scenario: Create HTTP client for domain
- GIVEN a domain and CF bypass flag
- WHEN `NewHTTPClient` is called
- THEN a client SHALL be created with a 30-second timeout and 5 max retries
- AND if CF bypass is needed, bypass data SHALL be loaded from the stored file (if available)
- AND cookie expiry SHALL NOT be validated upfront — validity SHALL be determined empirically by request outcome

#### Scenario: Fetch HTML with retries
- GIVEN a target URL
- WHEN `FetchHTML` is called
- THEN the client SHALL make a GET request with CF bypass headers if data is available
- AND SHALL decompress the response if Content-Encoding indicates compression
- AND SHALL detect Cloudflare challenges in the response
- AND SHALL retry on timeout errors up to 5 times with increasing timeouts (10s, 15s, 20s, 25s, 30s)
- AND SHALL not retry on non-timeout errors (return immediately)

### Requirement: CF Challenge Detection on Responses
The system SHALL inspect HTTP responses for Cloudflare challenge indicators.

#### Scenario: Detect CF from real response
- GIVEN an HTTP response is received
- WHEN `Detectcf` is called with the response
- THEN it SHALL check for cf-browser-html, jschl_vc, or other CF indicators in the body
- AND SHALL return the detection result and challenge information

#### Scenario: Handle CF challenge
- GIVEN a CF challenge is detected during an HTTP fetch
- WHEN the response contains challenge indicators
- THEN any existing bypass data for the domain SHALL be marked as failed and deleted
- AND the challenge URL SHALL be opened in the user's default browser for manual solving
- AND a `CfChallengeError` SHALL be returned

### Requirement: Response Decompression
The system SHALL decompress gzip, deflate, and brotli-encoded responses.

#### Scenario: Decompress response
- GIVEN a response with Content-Encoding: gzip
- WHEN `DecompressResponseBody` is called
- THEN the body SHALL be decompressed using gzip
- AND the decompressed bytes SHALL be returned
- WHEN Content-Encoding is br (brotli)
- THEN the body SHALL be decompressed using brotli
- WHEN Content-Encoding is deflate
- THEN the body SHALL be decompressed using zlib

### Requirement: Debug Support
The system SHALL support saving raw HTML responses to disk for debugging.

#### Scenario: Save debug HTML
- GIVEN the HTTP client has DebugSaveHTML enabled and a DebugSaveHTMLPath set
- WHEN a successful HTTP response is received
- THEN the full response body SHALL be written to the configured debug file path
