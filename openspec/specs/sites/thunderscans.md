# thunderscans Site Plugin

- **Site name**: `thunderscans` — **Domain**: `en-thunderscans.com` — **File**: `sites/thunderscans.go`
- **Cloudflare bypass needed**: no
- **Extraction**: `custom` HTML parser for both chapters and images — no browser required (server-rendered pages).

## What is scraped, how, and why

- **Chapter list**: WordPress "mangareader" theme; the manga page (e.g. `/comics/{slug}/`) server-renders the full chapter list in `<div class="eplister" id="chapterlist"><ul>` with one `<li data-num="...">` per chapter (note: unlike KingOfShojo/arenascan the `<ul>` has no `clstyle` class, but the `#chapterlist li` selector matches regardless). Why: the entire list is in the static HTML, so a single fetch avoids pagination and JS.
- **Chapter images**: each chapter page embeds the page list as a JSON object inside a `ts_reader.run({...});` call (the same reader pattern as KingOfShojo). The `#readerarea` div is empty in the static HTML — the images are only in the reader JSON. Why: the image URLs are recoverable from the static HTML without rendering.
- **Image hosts**: served from `en-thunderscans.com/wp-content/uploads/manga/...` (same domain as the pages, Cloudflare-fronted). The domain answers a plain HTTP request with a normal 200 page (no CF clearance cookie required), so the plain HTTP image downloader suffices. Any CF challenge is still detected automatically and handled by the downloader.

## Chapter-list extraction flow

1. `parseThunderscansChapters` loads the manga page HTML into goquery.
2. For each `#chapterlist li` it reads the anchor `href`, the `.chapternum` text, and the `data-num` attribute, then emits `filename → normalized URL`. Empty result → "no chapters found in chapter list".

## Chapter download flow

1. `parseThunderscansImages` regex-extracts the `ts_reader.run({...})` JSON (dotted-any `.*?`, so it can span multiple lines) and unmarshals it into a `sources[].images` structure.
2. It uses `sources[0].images`, trimming whitespace, keeping only URLs starting with `http`, and de-duplicating.
3. Images are downloaded with the standard plain-HTTP downloader, converted to JPEG, and packaged into the CBZ.

## Normalization

- **URL**: relative or protocol-relative paths become `https://en-thunderscans.com{path}`. Chapter URLs are absolute in practice (e.g. `https://en-thunderscans.com/this-assassin-is-honest-and-upright-chapter-21/`).
- **Filename** (`ch%03d[.frac].cbz`): pulls `(\d+)(?:\.(\d+))?` from `data-num`, else the `.chapternum` text, else the URL. Unparseable labels fall back to a slugified `label.cbz` with a warning.

## Retry & backoff

Default policy (no `GetRetryPolicy` override):

| Operation | Total attempts | Backoff |
|-----------|----------------|---------|
| Chapter-list / image-list fetch | 3 | `2^attempt × 1s` |
| Chapter download | 3 | `2^attempt × 1s` |
| Image download | 3 | `2^attempt × 1s` (adaptive deadline: 2min base, +10s/fail, 30min cap) |

## Debug

Implements `DebugSite` with `SaveHTML: false`, `HTMLPath: "thunderscans_debug.html"`.