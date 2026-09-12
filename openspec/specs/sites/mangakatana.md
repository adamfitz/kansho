# mangakatana Site Plugin

- **Site name**: `mangakatana` — **Domain**: `mangakatana.com` — **File**: `sites/mangakatana.go`
- **Cloudflare bypass needed**: no
- **Extraction**: chapters via **browser JavaScript** (to support CF-bypass detection), images via a **custom** plain-HTML regex parser.

## What is scraped, how, and why

- **Chapter list**: links live in `.chapters a` elements on the manga page. Why use JavaScript instead of `html_selector`: the browser path gives proper CF-bypass/cookie handling before extraction, while still reading a simple selector-based DOM query.
- **Chapter images**: the reader page embeds its URLs in a JavaScript variable `var thzq = ["https://...", ...]` in the static HTML. Why a regex over the static HTML: the image list is fully present in the initial document, so no rendering is needed (`thzq` is in the HTML).
- Titles use the format `Chapter XX: Title Part Y`, where the "Part" inside the title (after the colon) must not be treated as a sub-chapter — see filename normalization.

## Chapter-list extraction flow

1. `extractChaptersWithJS` opens a browser session with `WaitSelector: .chapters a` and evaluates the JS, mapping each anchor to `{text, url}`.
2. Filenames/URLs are then produced by `NormalizeChapterFilename`/`NormalizeChapterURL`.

## Chapter download flow

1. `parseMangakatanaImages` regex-captures the `thzq` array body (`var\s+thzq\s*=\s*\[([^\]]+)\]`), then extracts each single-quoted URL with a second regex.
2. The URLs are downloaded with the standard plain-HTTP image downloader, converted to JPEG, and packaged into the CBZ.

## Normalization

- **URL**: not modified (anchors are already absolute).
- **Filename** (`ch%03s[.sub][.part].cbz`): regex `Chapter\s+(\d+)(?:\.(\d+))?` gives the main number and optional decimal. A `Part X` is appended as an extra segment only when it appears **before** the colon or there is no colon (i.e. it is a genuine sub-chapter indicator) and only when `X` differs from the decimal sub-number. Unparseable text falls back to a lowercased, space→dash slug.

## Retry & backoff

Default policy (no `GetRetryPolicy` override):

| Operation | Total attempts | Backoff |
|-----------|----------------|---------|
| Chapter-list / image-list fetch | 3 | `2^attempt × 1s` |
| Chapter download | 3 | `2^attempt × 1s` |
| Image download | 3 | `2^attempt × 1s` (adaptive deadline: 2min base, +10s/fail, 30min cap) |