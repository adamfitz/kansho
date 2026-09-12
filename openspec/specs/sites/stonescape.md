# stonescape Site Plugin

- **Site name**: `stonescape` — **Domain**: `stonescape.xyz` — **File**: `sites/stonescape.go`
- **Cloudflare bypass needed**: no
- **Extraction**: `api` for both chapters and images, via the site's official JSON API.

## What is scraped, how, and why

- **Series → chapters**: the API exposes a clean resolution chain:
  1. `GET /api/series/by-slug/{slug}` → `seriesId`.
  2. `GET /api/series/{seriesId}/chapters` → `[{chapterId, chapterNumber}]`.
  Why use the API: chapter metadata (including UUID chapter ids) is only reliably available from these endpoints, and no HTML parsing or rendering is needed.
- **Chapter pages**: `GET /api/chapters/{chapterId}/pages` → `[{pageNumber, url}]`. Why: the API returns the exact page order; the parser insertion-sorts by `pageNumber` and prefixes the *relative* `url` (`/pub/manhwa/...`) with `https://stonescape.xyz` to make absolute image URLs.
- The chapter id (a UUID) is carried in the `url` field and passed straight to the image API — `NormalizeChapterURL` leaves it unchanged.

## Chapter-list extraction flow

1. `GetChapterExtractionMethod`'s `APIFunc` extracts the slug from `/series/{slug}`, fetches `seriesId`, then fetches the chapters list.
2. Each chapter becomes `{num: chapterNumber, url: chapterId}`. Empty series/chapter responses error out.

## Chapter download flow

1. `GetImageExtractionMethod`'s `APIFunc` fetches `/api/chapters/{chapterId}/pages` and builds absolute image URLs.
2. The page URLs are downloaded with the standard plain-HTTP image downloader, converted to JPEG, and packaged into the CBZ.

## Normalization

- **URL**: not modified — the "URL" is the chapter UUID destined for the pages API.
- **Filename** (`ch%03d[.frac].cbz`): regex `^([0-9]+)(?:\.([0-9]+))?$` over the API `chapterNumber` (e.g. `"1.00"`, `"30.00"`, `"1.50"`). The whole part pads to 3 digits; a decimal part whose trimmed value is non-empty is appended with trailing zeros stripped (`1.50` → `ch001.5.cbz`); a `.00` decimal is dropped (`30.00` → `ch030.cbz`).

## Retry & backoff

Default policy (no `GetRetryPolicy` override):

| Operation | Total attempts | Backoff |
|-----------|----------------|---------|
| Chapter-list / image-list fetch | 3 | `2^attempt × 1s` |
| Chapter download | 3 | `2^attempt × 1s` |
| Image download | 3 | `2^attempt × 1s` (adaptive deadline: 2min base, +10s/fail, 30min cap) |