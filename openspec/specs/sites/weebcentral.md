# weebcentral Site Plugin

- **Site name**: `weebcentral` — **Domain**: `weebcentral.com` — **File**: `sites/weebcentral.go`
- **Cloudflare bypass needed**: yes
- **Extraction**: `custom` parsers driven by WeebCentral's own **HTMX fragments** — no browser rendering required.

## What is scraped, how, and why

- **Chapter list**: the series page shows only a handful of recent chapters but embeds a "Show All Chapters" button whose `hx-get` targets `https://weebcentral.com/series/{ID}/full-chapter-list`. Why scrape that endpoint: it returns a plain HTML fragment containing **every** chapter `<a href="/chapters/{ID}">…<span>Chapter N</span>` link in one request, avoiding pagination.
- **Chapter images**: the chapter page lazy-loads images via an HTMX request — `hx-get="/chapters/{ID}/images?is_prev=False&current_page=1"` (older pages) or `htmx.ajax('GET', "https://weebcentral.com/chapters/{ID}/images?...")` embedded in JS (modern pages). The server requires a `reading_style` parameter (missing → HTTP 400), so the parser appends `reading_style=long_strip`, which returns **all** image tags in a single response. Why: one fragment fetch replaces slow scroll-to-load, and the images come through as simple `<img src>` tags.
- **Asset filtering**: image URLs containing `icon`, `logo`, `brand`, or `static/` are excluded so UI assets don't end up in the CBZ.

## Chapter-list extraction flow

1. `parseWeebcentralChapters` regex-finds the `full-chapter-list` endpoint (`hx-get="…full-chapter-list…"`) in the series HTML and fetches it via the `RequestExecutor` (HTTP first, browser fallback). If no button is found it parses the current page directly.
2. `extractChapterLinks` regex-matches `/chapters/` anchors followed by a `<span>` containing `Chapter N`/`Episode N`; `extractChapterLinksSimple` is a broader fallback that scans the anchor body text. Results map `filename → absolute URL`.

## Chapter download flow

1. `parseWeebcentralImages` finds the images HTMX endpoint (absolute JS-string form first, then relative `hx-get`), unescapes `&amp;`, appends `&reading_style=long_strip` unless already present, and fetches it via the executor.
2. `extractImageURLs` collects `<img src="...">` values, keeps http(s) URLs, filters the icon/logo/brand/static assets, and de-duplicates.
3. Images are downloaded with CF-bypass headers, converted to JPEG, and packaged into the CBZ.

## Normalization

- **URL**: relative or protocol-relative paths become `https://weebcentral.com{path}`.
- **Filename** (`ch%03d[.frac].cbz`): regex `(?:Episode|Chapter)\s+(\d+)(?:\.(\d+))?`; both chapters and episodes use the `ch` prefix for consistency. Unparseable labels fall back to a lowercased, space→dash slug.

## Retry & backoff

Default policy (no `GetRetryPolicy` override):

| Operation | Total attempts | Backoff |
|-----------|----------------|---------|
| Chapter-list / image-list fetch | 3 | `2^attempt × 1s` |
| Chapter download | 3 | `2^attempt × 1s` |
| Image download | 3 | `2^attempt × 1s` (adaptive deadline: 2min base, +10s/fail, 30min cap) |

## Debug

Implements `DebugSite` with `SaveHTML: false`, `HTMLPath: "weebcentral_debug.html"`.