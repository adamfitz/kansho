# browser-automation Specification

## Purpose
Provide headless Chromium browser automation for fetching JavaScript-rendered content and executing custom JavaScript on manga source pages.

## Requirements

### Requirement: Browser Session Management
The system SHALL manage chromedp browser sessions with configurable options.

#### Scenario: Create headless browser session
- GIVEN a domain and CF bypass flag
- WHEN `NewBrowserSession` is called
- THEN a new chromedp allocator context SHALL be created with headless mode
- AND the AutomationControlled flag SHALL be disabled to avoid detection
- AND if CF bypass data is available, the captured User-Agent SHALL be applied

#### Scenario: Close browser session
- GIVEN an active browser session
- WHEN `Close` is called
- THEN both the browser context and allocator context SHALL be cancelled

### Requirement: Navigation and JavaScript Evaluation
The system SHALL support navigating to pages and executing JavaScript for content extraction.

#### Scenario: Navigate and evaluate JavaScript
- GIVEN a URL and JavaScript code
- WHEN `NavigateAndEvaluate` is called
- THEN the browser SHALL navigate to the URL
- AND wait for the page body to load
- AND check for CF challenge pages after navigation
- AND if a CF challenge is detected, open the browser for manual solving and return a CfChallengeError
- AND wait for the specified CSS selector (if provided)
- AND evaluate the JavaScript code, storing results in the provided output variable

#### Scenario: Navigate with wait selector
- GIVEN a URL and a wait selector
- WHEN `Navigate` is called
- THEN the browser SHALL navigate to the URL
- AND if a wait selector is provided, wait for that selector to be visible
- AND if no wait selector is provided, wait for the body element
- AND check for CF challenges after successful navigation

### Requirement: Cookie Injection
The system SHALL inject CF bypass cookies into the browser before navigation.

#### Scenario: Inject CF cookies into browser
- GIVEN a BrowserSession with loaded bypass data
- WHEN `injectCookies` is called before navigation
- THEN the cf_clearance cookie SHALL be added via CDP Network.setCookies
- AND all other stored cookies SHALL be injected
- AND the number of injected cookies SHALL be logged

### Requirement: Batched HTML Fetching
The system SHALL support fetching rendered HTML from JavaScript-heavy pages using a single batched chromedp operation.

#### Scenario: FetchHTMLBatched navigation
- GIVEN a URL for a JavaScript-rendered page (e.g., Next.js/React)
- WHEN `FetchHTMLBatched` is called
- THEN a BrowserSession SHALL be created for the domain
- AND CF cookies SHALL be injected if available
- AND navigation, WaitReady("body"), and OuterHTML("html") SHALL be batched into a single chromedp.Run call
- AND a single 60-second timeout SHALL cover the entire operation
- AND the rendered HTML SHALL be returned

#### Scenario: Debug HTML saving in batched fetch
- GIVEN a `Debugger` with `SaveHTML = true` and a valid `HTMLPath`
- WHEN `FetchHTMLBatched` completes
- THEN the rendered HTML SHALL be written to the configured debug path

### Requirement: HTTP with Browser Fallback
The system SHALL attempt HTTP fetching first, falling back to browser automation on failure.

#### Scenario: HTTP-first with browser fallback
- GIVEN a target URL
- WHEN `RequestExecutor.FetchHTML` is called
- THEN the executor SHALL first attempt an HTTP GET request
- AND if HTTP succeeds, return the HTML directly
- AND if HTTP fails with a non-CF error, fall back to a headless browser session
- AND if a CF challenge is detected at any point, return it as a CfChallengeError
