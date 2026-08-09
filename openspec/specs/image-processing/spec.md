# image-processing Specification

## Purpose
Download manga page images, convert them to JPEG format, and package them into CBZ (ZIP) archives suitable for comic readers.

## Requirements

### Requirement: Image Download
The system SHALL download images from URLs with support for multiple transport methods.

#### Scenario: Shared client image download (FlameComics only)
- GIVEN a FlameComics download with multiple chapters of images on the same domain
- WHEN the FlameComics download path uses the shared keep-alive client for each image
- THEN one shared `http.Client`/`http.Transport` SHALL live for the whole FlameComics session, so TCP/TLS connections are reused across every image of every chapter instead of opening a fresh connection per image
- AND the request context SHALL be derived from the parent `ctx` via `context.WithTimeout` so parent cancellation aborts an in-flight download immediately
- AND the image SHALL be converted to JPEG if needed and saved with a zero-padded 3-digit filename (e.g., "001.jpg")
- AND retry/backoff SHALL be handled by the caller at exactly one level (no nested retries inside the download function)
- AND all other sites SHALL keep their legacy per-image download paths unchanged

#### Scenario: Referer-protected image download (comix)
- GIVEN an image URL on a static CDN that rejects requests without a Referer from the reading site (e.g. comix.to)
- WHEN the manager uses `DownloadConvertToJPGRenameWithReferer`
- THEN the request SHALL carry a browser User-Agent and the configured Referer (e.g. `https://comix.to/`)
- AND a shared keep-alive `http.Client` SHALL be reused across the whole batch so connections are not opened fresh per image
- AND the image SHALL be converted to JPEG if needed and saved with a zero-padded 3-digit filename
- AND retries SHALL be handled by the caller at exactly one level

#### Scenario: Image download with CF bypass
- GIVEN an image URL on a CF-protected site
- WHEN a CF-protected image is downloaded through the legacy `DownloadConvertToJPGRenameCf` path (or the FlameComics shared path)
- THEN the CF bypass cookie for `domain` SHALL be loaded and applied to the request
- AND the cookie domain SHALL be dot-prefixed so it applies to the CDN subdomain (e.g. `cdn.flamecomics.xyz`)
- AND the CF bypass User-Agent SHALL be applied
- AND the image SHALL be downloaded, converted to JPEG, and saved with zero-padded filename

#### Scenario: No-data stall detection
- GIVEN a server accepts a connection but sends no body bytes (Cloudflare throttling)
- WHEN `readBodyWithStallDetect` reads the response body
- THEN the download SHALL abort the request after `noDataTimeout` (20s) of zero bytes received
- AND a STALLED log line SHALL be emitted identifying the URL
- AND a `parser.StalledError` SHALL be returned so the caller can detect the stall and retry, instead of blocking silently for the full request timeout

### Requirement: Image Format Conversion
The system SHALL convert WebP, PNG, and GIF images to JPEG format.

#### Scenario: Detect image format from magic bytes
- GIVEN raw image bytes
- WHEN `detectImageFormat` is called
- THEN JPEG SHALL be detected by FF D8 FF header
- AND PNG SHALL be detected by 89 50 4E 47 header
- AND GIF SHALL be detected by GIF87a/GIF89a header
- AND WebP SHALL be detected by RIFF...WEBP header

#### Scenario: Convert WebP to JPEG
- GIVEN a WebP image is downloaded
- WHEN `ConvertImageToJPEG` is called
- THEN the image SHALL be decoded using golang.org/x/image/webp
- AND saved as JPEG with quality 90

#### Scenario: PNG/GIF to JPEG conversion
- GIVEN a PNG or GIF image is downloaded
- WHEN `ConvertImageToJPEG` is called
- THEN the image SHALL be decoded using the standard library
- AND saved as JPEG with quality 90

#### Scenario: JPEG passthrough
- GIVEN a JPEG image is downloaded
- WHEN `ConvertImageToJPEG` is called
- THEN the raw bytes SHALL be saved directly without re-encoding

### Requirement: CBZ Archive Creation
The system SHALL package downloaded images into CBZ files.

#### Scenario: Create CBZ from directory
- GIVEN a directory containing sequentially numbered image files
- WHEN `CreateCbzFromDir` is called
- THEN all image files SHALL be sorted alphabetically
- AND added to a ZIP archive with .cbz extension
- AND the archive SHALL be written to the specified output path

#### Scenario: Empty directory handling
- GIVEN a directory with no image files
- WHEN `CreateCbzFromDir` is called
- THEN an empty CBZ file SHALL be created

### Requirement: Rate Limiting
The system SHALL rate-limit sequential downloads to avoid overwhelming servers.

#### Scenario: Rate-limited image downloads
- GIVEN multiple images need to be downloaded sequentially from the same site
- WHEN the download loop processes each image
- THEN a 1500ms delay SHALL be enforced between each image download
- AND the rate limiter SHALL be stopped after all downloads complete
- AND `WaitCtx(ctx)` SHALL be used instead of `Wait()` to allow immediate cancellation during the wait

### Requirement: Context-Aware Sleep
The system SHALL provide context-aware sleep utilities for cancellation during wait periods.

#### Scenario: Sleep with context cancellation
- GIVEN a goroutine needs to sleep for a fixed duration
- WHEN `SleepCtx(ctx, duration)` is called
- THEN the goroutine SHALL sleep for the specified duration
- OR SHALL return immediately if the context is cancelled
- AND SHALL return true if the sleep completed normally, false if cancelled
