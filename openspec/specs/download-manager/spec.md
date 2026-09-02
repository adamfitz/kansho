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
The system SHALL automatically retry failed downloads with exponential backoff, governed by a per-site retry policy. The default policy retries each operation up to 3 times with a 1 second base backoff. A site opts into custom behavior by implementing the `RetryPolicySite` interface (a `GetRetryPolicy() SiteRetryPolicy` method); zero-valued fields in a site's policy fall back to the package defaults.

`SiteRetryPolicy` exposes `MaxChapterRetries`/`ChapterBackoff` (chapter downloads), `MaxImageRetries`/`ImageBackoff` (image downloads), `MaxFetchRetries`/`FetchBackoff` (chapter-list and image-list fetches), and the decay knobs `DecayBackoff`, `DecayStep`, `DecayMax`.

#### Scenario: Retry failed chapter download
- GIVEN a chapter download fails
- WHEN the error is not a CF challenge
- THEN the system SHALL retry up to `MaxChapterRetries` times (default 3)
- AND SHALL wait `2^attempt * ChapterBackoff` between retries (default base 1s → 2, 4, 8 seconds)
- AND SHALL use `SleepCtx(ctx, backoff)` so the wait is cancelled immediately if the context is cancelled
- WHEN all retries are exhausted
- THEN the system SHALL log the failure and continue to the next chapter

#### Scenario: Retry failed image download
- GIVEN an image download fails
- WHEN retrying
- THEN the system SHALL retry up to `MaxImageRetries` times (default 3)
- AND SHALL use an exponential backoff of `2^attempt * ImageBackoff` between retries
- AND SHALL use `SleepCtx(ctx, backoff)` so the wait is cancelled immediately if the context is cancelled
- AND each attempt SHALL be a single HTTP request at the download layer — the parser helpers SHALL NOT retry internally, avoiding nested retries that could stall on one image for minutes
- AND every attempt, stall, and backoff phase SHALL be pushed through the progress callback and logged, so the status bar indicator updates live for all downloads

#### Scenario: Retry failed chapter-list or image-list fetch
- GIVEN `FetchChapterURLs` or `FetchChapterImages` fails to extract from the site
- WHEN the error is not a CF challenge
- THEN the system SHALL retry up to `MaxFetchRetries` times (default 3)
- AND SHALL use an exponential backoff of `2^attempt * FetchBackoff` between retries, or the decay controller's effective base when the site enables decay
- AND SHALL use `SleepCtx(ctx, backoff)` so the wait is cancelled immediately if the context is cancelled
- WHEN a CF challenge is detected at any attempt
- THEN the fetch SHALL return the CF challenge error immediately to the queue with no further retries

#### Scenario: Per-site retry policy overrides
- GIVEN a site implements `RetryPolicySite`
- WHEN its `GetRetryPolicy()` is resolved, with zero-valued fields replaced by the package defaults
- THEN comix SHALL be configured with `MaxImageRetries` 9 and `ImageBackoff` 3s (legacy per-image path)
- AND FlameComics SHALL be configured with `MaxImageRetries` 9 and `ImageBackoff` 3s (single-request attempts)
- AND manhuaus SHALL be configured with `MaxChapterRetries` 5 / `ChapterBackoff` 2s, `MaxImageRetries` 8 / `ImageBackoff` 2s, `MaxFetchRetries` 8 / `FetchBackoff` 2s, plus `DecayBackoff` enabled with `DecayStep` 2s and `DecayMax` 60s
- AND all other sites SHALL keep the default policy

#### Scenario: Decaying backoff (recovery without resetting to scratch)
- GIVEN a site has `DecayBackoff` enabled
- AND the manager SHALL build one decay controller per manga download at construction time, resting on the site's `ImageBackoff` (so a site enabling decay SHALL set `FetchBackoff` equal to `ImageBackoff` for consistent fetch waits), with `DecayStep` defaulting to the image backoff and `DecayMax` defaulting to 30s
- WHEN `FetchChapterURLs` or `FetchChapterImages` exhausts all of its retries for a chapter
- THEN the effective backoff base SHALL double (capped at `DecayMax`), applying to subsequent fetch retries
- WHEN a fetch later succeeds
- THEN the effective base SHALL decrement by one `DecayStep` instead of resetting to the configured base, draining back to the base over successive successful chapters like a queue
- AND no individual retry wait SHALL exceed `DecayMax`, so a fully-down site can never stall the download indefinitely
- AND while the effective base is elevated, each retry wait SHALL still grow exponentially from it (`2^attempt * effectiveBase`, capped at `DecayMax`)

#### Scenario: Adaptive image attempt timeout
- GIVEN an image attempt carries a context deadline (base: 2 minutes)
- WHEN an image attempt of that manga's download fails
- THEN the next image attempt's deadline SHALL grow by 10 seconds (capped at 30 minutes), so a struggling site gets progressively longer per request
- AND any successful image download SHALL reset the deadline to the 2 minute base

#### Scenario: Referer-protected image download (comix)
- GIVEN a comix.to chapter download
- WHEN each image is downloaded
- THEN the image SHALL be fetched through `DownloadConvertToJPGRenameWithReferer` with a browser User-Agent and a Referer of `https://comix.to/`, because the static CDN 403s requests without one
- AND a shared keep-alive client SHALL be reused across the batch
- AND the image SHALL be converted to JPEG and saved with a zero-padded filename
- AND comix SHALL be selected by site name before the generic CF-bypass and plain paths

#### Scenario: Stalled image download (FlameComics only)
- GIVEN a server accepts a connection but sends no body bytes (Cloudflare throttling)
- WHEN a FlameComics image download stalls
- THEN `DownloadFlameComicsImage` SHALL abort the request after 20 seconds of no data and return a `parser.StalledError`
- AND a STALLED log line SHALL be emitted identifying the URL
- AND the manager's `downloadImageWithRetry` SHALL retry the image with exponential backoff up to the site's `MaxImageRetries` (FlameComics configures 9)
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
