# chapter-refresh-pool Specification

## Purpose
Provide a dedicated worker pool exclusively for refreshing manga chapter lists (remote chapter fetches) so they never compete with the download queue's workers. Scrapes of the same target site are serialized on one dedicated goroutine to avoid triggering rate limiting, different sites are scraped in parallel up to a global cap, and failing scrapes are retried with an exponential backoff that resets after a successful scrape — so slow or rate-limited sites eventually succeed without blocking downloads or the UI. The pool exposes a single status representation that is shown on the right side of the main window's status bar.

## Requirements

### Requirement: Singleton Pool
The system SHALL provide a single, globally accessible refresh pool instance used by all UI chapter-list refreshes.

#### Scenario: Get pool instance
- GIVEN the application is running
- WHEN `refreshpool.Get()` is called
- THEN the same singleton instance SHALL always be returned

### Requirement: Per-Site Worker Affinity
Each target site SHALL have exactly one dedicated worker goroutine, and all tasks for that site SHALL be executed sequentially by it.

#### Scenario: Same-site tasks are serialized
- GIVEN multiple refresh tasks targeting the same site
- WHEN they are submitted while earlier tasks are still running
- THEN no two tasks of that site SHALL ever execute concurrently

#### Scenario: Different sites run in parallel
- GIVEN refresh tasks targeting different sites
- WHEN they are submitted simultaneously
- THEN they MAY execute concurrently, limited by the global parallelism cap

### Requirement: Global Parallelism Cap
At most 10 scrape attempts SHALL run in parallel across all sites; additional running-eligible tasks SHALL wait until a slot frees up.

#### Scenario: Cap is enforced
- GIVEN more than 10 sites have runnable tasks at once
- WHEN 10 tasks are already executing
- THEN further tasks SHALL start only after a running task releases its slot

### Requirement: Retry With Exponential Backoff
A failed scrape attempt SHALL be retried up to 10 times with a delay that doubles after every failure, starting at 5 seconds and capped at 30 minutes. A successful scrape SHALL reset the site's backoff delay to the base value.

#### Scenario: Backoff grows across failures
- GIVEN a site whose scrape keeps failing
- WHEN retries are scheduled
- THEN the wait before each retry SHALL double relative to the previous one (5s, 10s, 20s, ... capped at 30 minutes)
- AND the task SHALL be attempted at most 11 times (initial attempt + 10 retries)

#### Scenario: Backoff resets after success
- GIVEN a site whose backoff delay has grown due to failures
- WHEN any scrape for that site succeeds
- THEN the site's backoff delay SHALL reset to the base value for future failures

#### Scenario: Rate limiting tolerated
- GIVEN a site responding with rate-limit errors or timeouts (`context deadline exceeded`)
- WHEN the retries exhaust their backoff delays
- THEN the failure SHALL only affect the affected task, logged via OnError, without impacting other sites

#### Scenario: Terminal errors are not retried
- GIVEN a task whose error is marked terminal via its `NoRetry` predicate (e.g. a Cloudflare challenge waiting for user input)
- WHEN Run returns that error
- THEN OnError SHALL fire immediately with no further attempts
- AND no browser window SHALL be reopened for the same failure

### Requirement: Duplicate Submission Rejection
The pool SHALL reject a submission whose dedupe key matches a task that is still queued or running, and SHALL accept the same key again once that task finishes.

#### Scenario: Duplicate refresh rejected
- GIVEN a refresh task for a manga is pending or running
- WHEN the same manga is submitted again using the same dedupe key
- THEN the submission SHALL return false without enqueuing

#### Scenario: Key released after completion
- GIVEN a refresh task finished successfully or terminally failed
- WHEN the same manga is submitted again
- THEN the submission SHALL be accepted

### Requirement: Status Reporting
The pool SHALL report a single snapshot of its state (running count and queued count) through a listener callback whenever the state changes, and once immediately upon registration.

#### Scenario: Status bar shows pool state
- GIVEN the listener is wired to the main window status bar
- WHEN tasks are queued, start running, or finish
- THEN the right edge of the status bar SHALL show "⟳ Refreshes: idle" when nothing is pending
- AND "Refreshes: N running · M queued" next to an animated bash-style spinner (|/-\) while the pool has work in flight

#### Scenario: Listener thread safety
- GIVEN pool callbacks fire on pool goroutines
- WHEN the UI receives a status update
- THEN it SHALL marshal the widget update onto the UI thread before applying it

### Requirement: UI Refresh Routing
The chapter list's Refresh action SHALL submit its fetch to the pool instead of spawning ad-hoc goroutines, preserving existing behaviour for Cloudflare challenges, stale results and button state.

#### Scenario: Refresh routes through the pool
- GIVEN the user presses Refresh on a selected manga's chapter list
- WHEN the submission is accepted
- THEN the Refresh button SHALL stay disabled and the loading indicator shown until the pool reports success or terminal failure

#### Scenario: Stale results discarded
- GIVEN a refresh completes after the user navigated away from the manga
- WHEN the fetched chapters arrive
- THEN the stale result SHALL not modify the displayed list

#### Scenario: Cloudflare challenge preserved
- GIVEN a refresh fails with a Cloudflare challenge error
- WHEN the pool reports terminal failure
- THEN the CF import dialog SHALL appear as before
- AND successfully importing CF data SHALL re-trigger the refresh

### Requirement: Cancellation And Logging
Closing the pool SHALL cancel running scrapes, terminate waiting tasks with `context.Canceled`, reject new submissions, and all notable events (queueing, retries with reasons, successes, give-ups) SHALL be written to the log with a `[RefreshPool]` prefix.

#### Scenario: Close cancels in-flight work
- GIVEN tasks are queued or sleeping in a retry backoff
- WHEN the pool is closed
- THEN waiting tasks SHALL receive OnError(context.Canceled)
- AND subsequent submissions SHALL be rejected
