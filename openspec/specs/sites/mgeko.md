# mgeko Site Plugin

- **Site name**: `mgeko` — **Domain**: `mgeko.cc` — **File**: `sites/mgeko.go`
- **Cloudflare bypass needed**: yes
- **Extraction**: `javascript` for both chapters and images, running in the headless browser (site renders client-side and is CF-protected).

## What is scraped, how, and why

- **Chapter list**: chapter links live in `ul.chapter-list li a` on the manga page. Why browser JS: mgeko is Cloudflare-protected and needs the browser/CF-cookie path, and a simple DOM query is all that is required.
- **Chapter images**: the reader renders images inside `#chapter-reader img`. Why browser JS: the reader content only exists after client-side rendering.
- The `GetDomain` hint (`mgeko.cc`) is used purely as a fallback — the actual CF-bypass domain is always derived from each request URL (`DomainFromURL`) so `www`/non-`www` mismatches with the browser-extension captured data never matter.

## Chapter-list extraction flow

1. `extractChaptersWithJS` opens a browser session with `WaitSelector: ul.chapter-list li a` and evaluates the JS, mapping each anchor to `{url, text}`.
2. Filenames/URLs are produced via `NormalizeChapterFilename`/`NormalizeChapterURL`.

## Chapter download flow

1. `extractImagesWithJS` opens a browser session with `WaitSelector: #chapter-reader img`; the JS maps every `img.src` and filters empty strings.
2. Images are downloaded with CF-bypass headers (mgeko requires Cloudflare clearance), converted to JPEG, and packaged into the CBZ.

## Normalization

- **URL**: not modified (anchors are already absolute, e.g. `https://www.mgeko.cc/read-manga/...`).
- **Filename** (`ch%03s[.part.cbz]`): regex `chapter[-_.]?(\d+)((?:[-_.]\d+)*)` over the URL handles `/chapter-72`, `/chapter-72-5`, `/chapter-72.5`, `/chapter-72-5-1`; separators normalize to dots (`.2.1`). Unparseable URLs fall back to a sanitized slug (`url` with `/`→`-`, lowercased).

## Retry & backoff

Default policy (no `GetRetryPolicy` override):

| Operation | Total attempts | Backoff |
|-----------|----------------|---------|
| Chapter-list / image-list fetch | 3 | `2^attempt × 1s` |
| Chapter download | 3 | `2^attempt × 1s` |
| Image download | 3 | `2^attempt × 1s` (adaptive deadline: 2min base, +10s/fail, 30min cap) |