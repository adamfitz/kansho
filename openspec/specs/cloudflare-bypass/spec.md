# cloudflare-bypass Specification

## Purpose
Detect Cloudflare protection on manga source websites and provide a manual cookie-based bypass mechanism to enable automated downloading.

## Requirements

### Requirement: CF Challenge Detection
The system SHALL detect when a response is a Cloudflare challenge page.

#### Scenario: Detect CF challenge from HTTP response
- GIVEN an HTTP response is received
- WHEN the response body contains CF challenge indicators (cf-browser-html, jschl_vc, etc.)
- THEN the system SHALL detect it as a Cloudflare challenge
- AND SHALL return the challenge information including status code and indicators

#### Scenario: Detect CF challenge from Colly response
- GIVEN a Colly collector receives a response
- WHEN the response body contains CF challenge indicators
- THEN the system SHALL detect the challenge from the Colly response object

### Requirement: Browser-Based Manual Solving
When a CF challenge is detected, the system SHALL open the user's default browser for manual solving.

#### Scenario: Open challenge URL in browser
- GIVEN a Cloudflare challenge is detected on a URL
- WHEN `OpenInBrowser` is called with the challenge URL
- THEN the system SHALL invoke `xdg-open` (Linux) or the platform equivalent to open the URL in the default browser
- AND SHALL allow the user to manually complete the challenge

### Requirement: Cookie Persistence
The system SHALL persist solved CF cookies to disk for reuse across sessions.

#### Scenario: Save bypass data
- GIVEN a user has solved a Cloudflare challenge via the browser extension
- WHEN the browser extension writes cookie data to the CF data directory
- THEN the system SHALL load the stored BypassData from the file
- AND SHALL use the stored cookies and user-agent for subsequent requests

#### Scenario: Apply bypass data to HTTP requests
- GIVEN BypassData is loaded for a domain
- WHEN the HTTPClient makes a request to that domain
- THEN the cf_clearance cookie SHALL be attached to the request
- AND the captured User-Agent SHALL be used
- AND browser-like headers (sec-ch-ua, Accept-Language, etc.) SHALL be set

#### Scenario: Apply bypass data to Colly collector
- GIVEN BypassData is loaded for a domain
- WHEN a Colly collector is configured for that domain
- THEN the cookies SHALL be applied via the collector's OnRequest callback
- AND the User-Agent SHALL be set on the collector

#### Scenario: Apply bypass data to Chromedp browser
- GIVEN BypassData is loaded for a domain
- WHEN a BrowserSession is created for that domain
- THEN the captured User-Agent SHALL be used in the browser allocator options
- AND cookies SHALL be injected via CDP's Network.setCookies before navigation

### Requirement: Cookie Invalidation
The system SHALL invalidate stored bypass data when CF challenges recur despite having cookies.

#### Scenario: Mark cookies as failed
- GIVEN a request is made with existing bypass data
- WHEN a CF challenge is still returned
- THEN the system SHALL mark the stored cookies as failed
- AND SHALL delete the bypass data file for that domain
- AND SHALL open the browser for re-authentication

### Requirement: Browser Extension Integration
The system SHALL support a companion browser extension that captures CF bypass cookies.

#### Scenario: Import from browser extension
- GIVEN the browser extension has captured cookies
- WHEN `ImportFromChrome` or `ImportFromFirefox` is called
- THEN the system SHALL launch a local HTTP server to receive cookie data from the extension
- AND SHALL parse the received data and store it in the CF data directory

### Requirement: Domain Resolution
The system SHALL derive the domain from the actual request URL, not from hardcoded site hints, to prevent www/non-www mismatches.

#### Scenario: Resolve domain from URL
- GIVEN a URL like "https://www.mgeko.cc/manga/title"
- WHEN `DomainFromURL` is called
- THEN it SHALL return "www.mgeko.cc"
- WHEN parsing fails
- THEN it SHALL fall back to the provided hint domain
