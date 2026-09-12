# mangadex Site Plugin

- **Site name**: `mangadex` — **Domain**: `api.mangadex.org` — **File**: `sites/managdex.go`
- **Cloudflare bypass needed**: no
- **Extraction**: `api` for both chapters and images. The API client uses a **non-spoofed, identifiable** User-Agent (`kansho/1.0`) per API-provider policy — never a browser User-Agent.

## What is scraped, how, and why

- **Chapter list**: MangaDex exposes a feed endpoint (`/manga/{id}/feed`) that returns every chapter in JSON. Why use the API: it is the official, stable source of chapter data (number, id, page count) and avoids HTML scraping entirely. Filters: `translatedLanguage[]=en`, `order[chapter]=asc`, and `contentRating[]` safe/suggestive/erotica; chapters with a missing number or **0 pages** (deleted/unavailable) are skipped.
- **Chapter images**: MangaDex@Home provides the image host for a chapter via `/at-home/server/{chapterID}` (a `baseUrl` + data `hash` + filename list). Why: this is the sanctioned way to obtain all page URLs without rendering a reader, and images are plain bytes thereafter.
- The manga ID is extracted from the URL `https://mangadex.org/title/{ID}[/title-name]` (matched on the `title` path segment).

## Chapter-list extraction flow

1. `GetChapterExtractionMethod`'s `APIFunc` derives the manga ID from the base URL (so a plugin built by `GetSitePlugin` for UI refresh works too) and calls `getAllChaptersAPI`.
2. `getAllChaptersAPI` paginates `limit=100` offets through `/manga/{id}/feed`, sleeping **250ms** between pages.
3. Each chapter becomes `{num, id}` where the id is stored in the `url` field (the manager passes `chapterURL` straight through); the map keys are `ch%03d[.frac].cbz`.

## Chapter download flow

1. `GetImageExtractionMethod`'s `APIFunc` treats `chapterURL` as the chapter ID and calls `/at-home/server/{chapterID}`.
2. Full image URLs are built as `{baseUrl}/data/{hash}/{filename}` from the chapter's `data` list.
3. These are downloaded with the standard plain-HTTP image downloader (no CF headers — MangaDex doesn't use Cloudflare), converted to JPEG, and packaged into the CBZ.

## Normalization

- **URL**: not modified — the chapter "URL" is the chapter ID.
- **Filename** (`ch%03s[.frac].cbz`): pads the whole part to 3 digits and appends any `.` decimal part (`91.5` → `ch091.5.cbz`).

## Retry & backoff

Default policy (no `GetRetryPolicy` override):

| Operation | Total attempts | Backoff |
|-----------|----------------|---------|
| Chapter-list / image-list fetch | 3 | `2^attempt × 1s` |
| Chapter download | 3 | `2^attempt × 1s` |
| Image download | 3 | `2^attempt × 1s` (adaptive deadline: 2min base, +10s/fail, 30min cap) |

Plus the API's own 250ms inter-page sleep and the 1500ms inter-image rate limit.