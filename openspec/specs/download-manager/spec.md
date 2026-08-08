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
- AND the progress fraction SHALL advance in steps of `1/newChaptersToDownload` per chapter, starting from `0` for the first chapter
- AND during retry backoff, the callback SHALL report the retry status (e.g., "Retrying chapter 5 in 4s (attempt 2/3)...")
- AND on cancellation, the callback SHALL report "Cancelling..." before returning

### Requirement: Single Chapter Download
The Manager SHALL support downloading a single chapter to produce one CBZ archive, without fetching or filtering the full chapter list.

#### Scenario: Download a single chapter
- GIVEN a configured DownloadConfig with manga data and a site plugin
- WHEN `Manager.DownloadSingleChapter(ctx, chapterURL, cbzName)` is called
- THEN the system SHALL download the images of the given chapter URL only
- AND SHALL package them into the given CBZ filename in the manga's configured location
- AND SHALL report progress through the config's ProgressCallback using a total of 1 chapter
- AND SHALL return without error on success

#### Scenario: Cancel a single chapter download
- GIVEN a single chapter download is in progress
- WHEN the context is cancelled
- THEN the download SHALL abort with the context error
- AND SHALL NOT create a partial CBZ archive

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
- THEN the system SHALL retry up to 3 times for all sites
- AND SHALL use 2, 4, and 8 second exponential backoff
- AND SHALL use `SleepCtx(ctx, backoff)` so the wait is cancelled immediately if the context is cancelled
- AND FlameComics SHALL retry up to 5 times (2, 4, 8, 16, and 32 second backoff) to ride out CDN throttling
- AND for the FlameComics path, each attempt SHALL be a single HTTP request — `DownloadFlameComicsImage` SHALL NOT retry internally, avoiding nested retries that could stall on one image for minutes
- AND all other sites SHALL keep their legacy retry behavior unchanged
- AND every attempt, stall, and backoff phase SHALL be pushed through the progress callback and logged, so the status bar indicator updates live for all downloads

#### Scenario: Stalled image download (FlameComics only)
- GIVEN a server accepts a connection but sends no body bytes (Cloudflare throttling)
- WHEN a FlameComics image download stalls
- THEN `DownloadFlameComicsImage` SHALL abort the request after 20 seconds of no data and return a `parser.StalledError`
- AND a STALLED log line SHALL be emitted identifying the URL
- AND the manager's `downloadImageWithRetry` SHALL retry the image with exponential backoff (up to 5 attempts)
- AND the stall SHALL never block the download for more than the no-data timeout plus the retry backoff

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

#### Scenario: Cancel an in-flight image download
- GIVEN an individual image is being downloaded
- WHEN the parent context is cancelled mid-download
- THEN the FlameComics request SHALL abort immediately because its timeout SHALL be derived from the parent context via `context.WithTimeout(ctx, ...)`
- AND the non-CF legacy request SHALL abort immediately because it SHALL use the parent context directly
- AND the context cancellation error SHALL propagate to the caller

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

### Requirement: Encrypted Image Sites
The system SHALL support sites where images are encrypted in transit and require client-side decryption.

#### Scenario: Download encrypted images
- GIVEN a chapter URL on a site that implements `ImageDecryptorSite`
- WHEN the standard HTTP extraction returns 0 images
- THEN the manager SHALL create a browser session for the site's domain
- AND SHALL call `DownloadCanvasImages` with the site's `TransformImage` method as the transform function
- AND SHALL write each decrypted image to the chapter directory with a zero-padded filename
- AND SHALL use the extension detected from the decrypted image's magic bytes

#### Scenario: Encrypted extraction failure falls back to HTTP
- GIVEN `DownloadCanvasImages` fails or returns 0 images
- WHEN the encrypted extraction path is exhausted
- THEN the manager SHALL log the failure and fall back to standard HTTP extraction
