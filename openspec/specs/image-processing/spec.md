# image-processing Specification

## Purpose
Download manga page images, convert them to JPEG format, and package them into CBZ (ZIP) archives suitable for comic readers.

## Requirements

### Requirement: Image Download
The system SHALL download images from URLs with support for multiple transport methods.

#### Scenario: Standard HTTP image download
- GIVEN a direct image URL on a non-CF-protected site
- WHEN `DownloadConvertToJPGRename(ctx, filename, imageURL, targetDir)` is called
- THEN the context SHALL be passed to `http.NewRequestWithContext` for cancellation support
- AND the image SHALL be downloaded via HTTP GET
- AND converted to JPEG if needed
- AND saved with a zero-padded 3-digit filename (e.g., "001.jpg")
- AND up to 3 retries SHALL be attempted on failure

#### Scenario: Image download with CF bypass
- GIVEN an image URL on a CF-protected site
- WHEN `DownloadConvertToJPGRenameCf(ctx, filename, imageURL, targetDir, domain)` is called
- THEN a Colly collector with CF bypass cookies SHALL be created
- AND the context SHALL be checked for cancellation before and after the Colly Visit call
- AND the image SHALL be downloaded with the bypass applied
- AND the MaxBodySize SHALL be set to 0 (unlimited) to handle large images
- AND the response Content-Length SHALL be validated against actual bytes received
- AND converted to JPEG and saved with zero-padded filename

#### Scenario: Image download with shared Colly collector
- GIVEN a pre-configured Colly collector (with CF bypass already applied)
- WHEN `DownloadConvertToJPGRenameCfWithCollector(c, filename, imageURL, targetDir)` is called
- THEN the provided collector SHALL be used directly without creating a new one
- AND the image SHALL be downloaded, converted to JPEG, and saved with zero-padded filename
- NOTE: This variant does NOT support context cancellation (no ctx parameter)

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
