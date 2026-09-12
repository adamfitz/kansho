# flamecomics Site Plugin

- **Site name**: `flamecomics` — **Domain**: `flamecomics.xyz` — **File**: `sites/flamecomics.go`
- **Cloudflare bypass needed**: yes
- **Extraction**: `custom` parser, over the Next.js SSR/`__NEXT_DATA__` HTML produced by plain HTTP (no browser).

## What is scraped, how, and why

- **Chapter list**: the series page is Next.js and its `__NEXT_DATA__` JSON holds `props.pageProps.series.series_id` plus a `chapters` array (`chapter_id`, `chapter`, `token`). Why scrape the JSON: one static fetch contains the series id + every chapter token needed to build reader URLs, with no rendering required.
- **Chapter images**: the chapter page's `__NEXT_DATA__` provides the image list in `props.pageProps.chapter.images` (or a legacy `props.pageProps.images`, where entries may be strings or `{url|src}` objects). Why so many fallbacks: the site's payload has changed shape over time, and non-chapter CDN assets leak into the arrays.
- **Asset filtering**: FlameComics mixes the series thumbnail (`thumbnail.png`), cover/logo/icon/banner art, and "read on flame" promo images (`/assets/read/...`) into the image array. `isFlameComicsPageImage` rejects any URL under `/assets/` or whose basename contains `thumbnail`, `thumb_`, `cover`, `logo`, `icon`, `banner`, or `read_on_flame`. Why: the CBZ must only contain actual chapter pages.

## Chapter-list extraction flow

1. `parseFlameComicsChapters` regex-extracts the `__NEXT_DATA__` JSON and unmarshals it into `NextJsData`.
2. For each chapter with a non-empty `chapter` and `token`, it builds `https://flamecomics.xyz/series/{series_id}/{token}` and names it `ch%03d.cbz` (number from `extractFlameChapterNumber` over `chapter`, e.g. `"11.00"` → 11).

## Chapter download flow

1. `parseFlameComicsImages` tries, in order: `__NEXT_DATA__` `chapter.images` → `__NEXT_DATA__` `pageProps.images` → regex over `cdn.flamecomics.xyz/...\.(jpg|jpeg|png|webp|gif)` URLs (unwrapping Next.js `/_next/image?url=` proxies) → generic `data-src`/`src` regex. Every candidate passes `isFlameComicsPageImage`; results are de-duplicated.
2. Images are downloaded through `parser.DownloadFlameComicsImageTimeout`, the shared keep-alive client with stall detection (`parser.StalledError` reported in the status bar), converted to JPEG, and packaged into the CBZ.

## Normalization

- **URL**: absolute URLs pass through; relative ones get the `https://flamecomics.xyz` prefix.
- **Filename**: always `ch%03d.cbz` (integer) — the first number of `num`/`text`, falling back to the URL. `extractFlameChapterNumber` returns `-1` for non-numeric input, which yields `ch-1.cbz`.

## Retry & backoff

Custom policy via `GetRetryPolicy` — the CDN throttles bursts of fresh connections and chapters frequently time out, so images get more attempts with extra time between tries; chapter/fetch values fall back to defaults:

| Operation | Total attempts | Backoff |
|-----------|----------------|---------|
| Chapter-list / image-list fetch | 3 (default) | `2^attempt × 1s` |
| Chapter download | 3 (default) | `2^attempt × 1s` |
| Image download | **9** | `2^attempt × 3s` (adaptive deadline: 2min base, +10s/fail, 30min cap) |

## Debug

Implements `DebugSite` with `SaveHTML: false`, `HTMLPath: "flamecomics_debug.html"`.