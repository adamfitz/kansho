# download-manager Specification

## Purpose
Orchestrate the end-to-end download process for manga chapters, from fetching chapter lists to producing CBZ archives with retry logic and context-aware cancellation.

## Requirements

### Requirement: Download Lifecycle
The Manager SHALL execute the full download workflow for a single manga title.

#### Scenario: Full download workflow
- GIVEN a configured DownloadConfig with manga data and a site plugin
- WHEN `Manager.Download(ctx)` is called
- THEN the system SHALL fetch all chapter URLs from the manga's page
- AND SHALL query the local filesystem for already-downloaded chapter CBZ files
- AND SHALL filter out already-downloaded chapters
- AND SHALL sort remaining chapters in ascending order
- AND SHALL download each chapter sequentially

#### Scenario: No new chapters
- GIVEN all chapters are already downloaded locally
- WHEN the manager processes the chapter list
- THEN it SHALL report "No new chapters to download"
- AND SHALL return without error

#### Scenario: Download progress reporting
- GIVEN a download is in progress
- WHEN a ProgressCallback is provided in the config
- THEN the callback SHALL be invoked with: status message, progress fraction (0.0 to 1.0), actual chapter number, current download index, and total chapters found
- AND during retry backoff, the callback SHALL report the retry status (e.g., "Retrying chapter 5 in 4s (attempt 2/3)...")
- AND on cancellation, the callback SHALL report "Cancelling..." before returning

### Requirement: Chapter Download
Each chapter download SHALL fetch page images, convert them to JPEG, and package them as a CBZ (ZIP) archive.

#### Scenario: Download chapter images
- GIVEN a chapter URL and a site plugin
- WHEN `FetchChapterImages` is called
- THEN the system SHALL use the site's image extraction method to get image URLs
- AND SHALL download each image with retry logic
- AND SHALL convert non-JPEG images (WebP, PNG, GIF) to JPEG at quality 90
- AND SHALL save images as zero-padded filenames (001.jpg, 002.jpg, etc.)

#### Scenario: Create CBZ archive
- GIVEN downloaded images exist in a temporary directory
- WHEN all images for a chapter are downloaded
- THEN the system SHALL create a CBZ (ZIP) file containing all images in sorted order
- AND SHALL place the CBZ in the manga's configured location directory
- AND SHALL clean up the temporary directory

#### Scenario: Empty chapter rejected
- GIVEN a chapter page is fetched
- WHEN no images are found on the page
- THEN the download SHALL return an error indicating no images found

### Requirement: Retry Logic
The system SHALL automatically retry failed downloads with exponential backoff.

#### Scenario: Retry failed chapter download
- GIVEN a chapter download fails
- WHEN the error is not a CF challenge
- THEN the system SHALL retry up to 3 times
- AND SHALL wait 2, 4, and 8 seconds between retries (exponential backoff)
- AND SHALL use `SleepCtx(ctx, backoff)` so the wait is cancelled immediately if the context is cancelled
- WHEN all retries are exhausted
- THEN the system SHALL log the failure and continue to the next chapter

#### Scenario: Retry failed image download
- GIVEN an image download fails
- WHEN retrying
- THEN the system SHALL retry up to 3 times
- AND SHALL use 2, 4, and 8 second exponential backoff
- AND SHALL use `SleepCtx(ctx, backoff)` so the wait is cancelled immediately if the context is cancelled

### Requirement: Cancellation
The system SHALL support context-based cancellation of downloads at all levels.

#### Scenario: Cancel an active download
- GIVEN a download is in progress
- WHEN the context is cancelled
- THEN the downloader SHALL check `ctx.Done()` between chapter iterations
- AND SHALL check `ctx.Done()` before each individual image download
- AND SHALL check `ctx.Done()` before CBZ archive creation
- AND SHALL return the context error immediately
- AND SHALL not start new downloads for subsequent chapters

#### Scenario: Cancellation during extraction
- GIVEN an extraction operation (chapter listing or image URL fetching) is in progress
- WHEN the parent context is cancelled
- THEN internal timeouts SHALL derive from the parent context (not `context.Background()`)
- AND the extraction SHALL abort within the timeout granularity
- AND the context cancellation error SHALL propagate to the caller

#### Scenario: Cancellation during rate limit wait
- GIVEN images are being downloaded with rate limiting
- WHEN the context is cancelled during the 1500ms rate limit wait
- THEN `WaitCtx(ctx)` SHALL return immediately instead of waiting for the next tick
- AND the downloader SHALL return the context error
