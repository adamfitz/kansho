# arenascan Site Plugin

- **Site name**: `arenascan` — **Domain**: `arenascan.com` — **File**: `sites/arenascans.go`
- **Cloudflare bypass needed**: no
- **Extraction**: `custom` HTML parser for both chapters and images — no browser/JS required.

## What is scraped, how, and why

- **Chapter list**: the manga page `/manga/{slug}/` (WordPress "mangareader" theme) server-renders the *complete* chapter list in `<div class="eplister" id="chapterlist"><ul class="clstyle">` with one `<li data-num="...">` per chapter. Because everything is in the static HTML, the paginated category pages (`/category/{slug}/page/N/`) are never fetched — why: one HTTP fetch yields every chapter.
- **Chapter images**: each chapter page embeds its page images inside `<div id="readerarea">`. In the static (HTTP-fetched) HTML the `<img>` elements sit inside a `<noscript>` fallback block; `x/net/html` treats the noscript content as one raw-text node, so the parser re-parses that text as an HTML fragment to recover the `<img src>` values in order. On a browser-rendered page the images are direct children of `#readerarea` and are preferred. Why: extract everything from one static fetch without a browser.
- **Image hosts**: served from `cdn.arenascan.com` with no Referer or CF-clearance requirement, so the plain HTTP image downloader suffices.

## Chapter-list extraction flow

1. `FetchChapterURLs` fetches the manga page HTML through the request executor (HTTP first, browser only as a last resort) and calls `parsearenascanChapters`.
2. `parsearenascanChapters` uses goquery: for each `#chapterlist li` it reads the anchor `href`, the `.chapternum` text, and the `data-num` attribute, then builds the map `filename → normalized URL`.
3. An empty result returns "no chapters found in #chapterlist".

## Chapter download flow

1. `FetchChapterImages` fetches the chapter URL's HTML through the executor and calls `parsearenascanImages`.
2. `parsearenascanImages` starts from `#readerarea`: collects direct `img[src]` descendants (http only, de-duplicated preserving order); if none found, grabs the `noscript` text and re-parses it as HTML to collect its images the same way.
3. Image URLs are downloaded by the standard HTTP image downloader (1500ms spacing), converted to JPEG, and packaged into a CBZ via `parser.CreateCbzFromDir`.

## Normalization

- **URL**: relative or protocol-relative paths are made absolute: `https://arenascan.com{path}`.
- **Filename** (`ch%03d[.frac].cbz`): extracts `(\d+)(?:\.(\d+))?` from `data-num`, then the `.chapternum` text, then the URL. An unparseable label falls back to a lowercased, slugified `label.cbz` with a warning.

## Retry & backoff

Default policy (no `GetRetryPolicy` override):

| Operation | Total attempts | Backoff |
|-----------|----------------|---------|
| Chapter-list / image-list fetch | 3 | `2^attempt × 1s` |
| Chapter download | 3 | `2^attempt × 1s` |
| Image download | 3 | `2^attempt × 1s` (adaptive deadline: 2min base, +10s/fail, 30min cap) |

## Debug

Implements `DebugSite` with `SaveHTML: false`, `HTMLPath: "arenascan_debug.html"`.