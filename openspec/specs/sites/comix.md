# comix Site Plugin

- **Site name**: `comix` — **Domain**: `comix.to` — **File**: `sites/comix.go`
- **Cloudflare bypass needed**: yes
- **Extraction**: `javascript` for both chapters and images, running in a headless chromedp browser (it is a Vue SPA rendered client-side into `#app-root`).

## What is scraped, how, and why

- **Chapter list**: the title page renders chapter rows into an `ul.mchap-list` (`a.mchap-row__primary` per row). The list is paginated via an `.npager` pager. Why use the browser: the page is a Vue SPA, so the DOM only exists after client-side rendering. Pagination is driven from Go (see below) rather than with async/promise JS IIFEs.
- **Chapter images**: the reader renders one `<img class="rpage-page__img">` per page inside `.rpage-page` and lazy-loads them as the reader is scrolled. Why scroll: lazy-loaded images only get real `src` URLs once scrolled into view. Scrolling is done step by step by the browser driver (no JS promises).
- **Image CDN**: image URLs sit on a static CDN that returns HTTP 403 without a `Referer: https://comix.to/`, so image downloads go through the Referer-based downloader (`parser.DownloadConvertToJPGRenameWithRefererTimeout`).

## Chapter-list extraction flow

1. `extractChaptersWithJS` opens a browser session and runs `NavigateScrollPaginateAndEvaluate` with `WaitSelector: ul.mchap-list a.mchap-row__primary` and a **5-minute** extraction timeout.
2. The JS maps each row to `{url: a.href, text: span.mchap-row__ch text}`.
3. `NextPageJS` clicks the next-page control: it locates `.npager__num.is-active`, takes its next sibling, and clicks it only if it is the "Next page" nav button or another `.npager__num`, returning whether a next page existed. The Go loop repeats the evaluate/click cycle until the last page, accumulating unique chapters.

## Chapter download flow

1. `extractImagesWithJS` opens a browser session with `WaitSelector: .rpage-page`, `ScrollToLoad: true`, and a **10-minute** timeout.
2. The driver scrolls the page incrementally first; then the JS collects `.rpage-page img.rpage-page__img` `src` values, strips the `?r=` cache-buster query, and de-duplicates.
3. Each image is downloaded with the Referer `https://comix.to/`, converted to JPEG, and packaged into the CBZ.

## Normalization

- **URL**: relative or protocol-relative paths become `https://comix.to{path}`.
- **Filename** (`ch%03d[.frac].cbz`): the URL pattern `-chapter-(\d+)(?:\.(\d+))?$` is checked first (chapter URLs end in `/{id}-chapter-{num}`, where the leading digits are the chapter *id*, not the number), then a generic `(\d+)(?:\.(\d+))?` extraction from the label. Unparseable labels fall back to a slugified `label.cbz`.

## Retry & backoff

Custom policy via `GetRetryPolicy` — Comix's CDN reliably fails the first connection per image (context-deadline-exceeded throttling), so images get more attempts with more breathing room; all other values fall back to defaults:

| Operation | Total attempts | Backoff |
|-----------|----------------|---------|
| Chapter-list / image-list fetch | 3 (default) | `2^attempt × 1s` |
| Chapter download | 3 (default) | `2^attempt × 1s` |
| Image download | **9** | `2^attempt × 3s` (adaptive deadline: 2min base, +10s/fail, 30min cap) |

## Debug

Implements `DebugSite` with `SaveHTML: false`, `HTMLPath: "comix_debug.html"`.