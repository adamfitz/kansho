# ravenscans Site Plugin

- **Site name**: `ravenscans` — **Domain**: `ravenscans.com` — **File**: `sites/ravenscans.go`
- **Cloudflare bypass needed**: no
- **Extraction**: chapters via **browser JavaScript**; images via a **custom** static-HTML regex parser.

## What is scraped, how, and why

- **Chapter list**: the manga page renders chapters as `div.eplister ul li` rows (WordPress mangareader theme), each with an anchor and a `data-num` chapter number. Why browser JS: gives a robust DOM query of a client-rendered list while permitting CF handling if needed in future.
- **Chapter images**: the chapter page's static HTML references CDN images of the form `https://cdn{1..N}.ravenscans.org/.../chapter-{n}/{page}.jpg`. Why a regex over the static HTML: the images are present in the initial document with no rendering. The parser extracts the numeric page component and **sorts by page number** so the CBZ pages land in reading order even if the HTML lists them out of order.

## Chapter-list extraction flow

1. `extractChaptersWithJS` opens a browser session with `WaitSelector: div.eplister ul li`.
2. The JS maps each `li` to `{url: a.href, text: li[data-num], title: div.eph-num span.chapternum text}` and drops rows without a URL or number.
3. Filenames come from `NormalizeChapterFilename` (using `text` = `data-num`), URLs from `NormalizeChapterURL`.

## Chapter download flow

1. `parseRavenScansImages` regex-matches all `https://cdn\d+\.ravenscans\.org/.../chapter-\d+/(\d+)\.jpg` URLs, de-duplicates them, then insertion-sorts by the captured page number.
2. The ordered image URLs are downloaded with the standard plain-HTTP image downloader, converted to JPEG, and packaged into the CBZ.

## Normalization

- **URL**: not modified (already absolute, `https://ravenscans.org/...`).
- **Filename** (`ch%03d[.frac].cbz`): uses the `data-num` text directly (validated as a float); if empty, parses `chapter[-_.]?(\d+)((?:[-_.]\d+)*)` from the URL. Pads the whole part to 3 digits and keeps a decimal part. Unparseable input falls back to `ch{raw}.cbz` or a sanitized-slug `.cbz`.

## Retry & backoff

Default policy (no `GetRetryPolicy` override):

| Operation | Total attempts | Backoff |
|-----------|----------------|---------|
| Chapter-list / image-list fetch | 3 | `2^attempt × 1s` |
| Chapter download | 3 | `2^attempt × 1s` |
| Image download | 3 | `2^attempt × 1s` (adaptive deadline: 2min base, +10s/fail, 30min cap) |