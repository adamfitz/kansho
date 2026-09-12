# kingofshojo Site Plugin

- **Site name**: `kingofshojo` — **Domain**: `kingofshojo.com` — **File**: `sites/kingofshojo.go`
- **Cloudflare bypass needed**: no
- **Extraction**: `custom` HTML parser for both chapters and images — no browser required (server-rendered pages).

## What is scraped, how, and why

- **Chapter list**: WordPress "mangareader" theme; the manga page server-renders the full chapter list in `<div class="eplister" id="chapterlist"><ul class="clstyle">` with one `<li data-num="...">` per chapter. Why: the entire list is in the static HTML, so a single fetch avoids pagination and JS.
- **Chapter images**: each chapter page embeds the page list as a JSON object inside a `ts_reader.run({...});` call (a common WordPress reader pattern). Why: the reader's image URLs are recoverable from the static HTML without rendering.
- **Image hosts**: served from `cdn.kingofshojo.com` (Cloudflare fronted) with no Referer or CF-clearance requirement, so the plain HTTP image downloader suffices.

## Chapter-list extraction flow

1. `parseKingOfShojoChapters` loads the manga page HTML into goquery.
2. For each `#chapterlist li` it reads the anchor `href`, the `.chapternum` text, and the `data-num` attribute, then emits `filename → normalized URL`. Empty result → "no chapters found in chapter list".

## Chapter download flow

1. `parseKingOfShojoImages` regex-extracts the `ts_reader.run({...})` JSON (dotted-any `.*?`, so it can span multiple lines) and unmarshals it into a `sources[].images` structure.
2. It uses `sources[0].images`, trimming whitespace, keeping only URLs starting with `http`, and de-duplicating.
3. Images are downloaded with the standard plain-HTTP downloader, converted to JPEG, and packaged into the CBZ.

## Normalization

- **URL**: relative or protocol-relative paths become `https://kingofshojo.com{path}`.
- **Filename** (`ch%03d[.frac].cbz`): pulls `(\d+)(?:\.(\d+))?` from `data-num`, else the `.chapternum` text, else the URL. Unparseable labels fall back to a slugified `label.cbz` with a warning.

## Retry & backoff

Default policy (no `GetRetryPolicy` override):

| Operation | Total attempts | Backoff |
|-----------|----------------|---------|
| Chapter-list / image-list fetch | 3 | `2^attempt × 1s` |
| Chapter download | 3 | `2^attempt × 1s` |
| Image download | 3 | `2^attempt × 1s` (adaptive deadline: 2min base, +10s/fail, 30min cap) |

## Debug

Implements `DebugSite` with `SaveHTML: false`, `HTMLPath: "kingofshojo_debug.html"`.