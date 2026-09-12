# kunmanga Site Plugin

- **Site name**: `kunmanga` — **Domain**: `www.kunmanga.online` — **File**: `sites/kunmanga.go`
- **Cloudflare bypass needed**: yes, and implements `ManualCFPromptSite` (`NeedsManualCFPrompt() = true`)
- **Extraction**: chapters via the site's **internal JSON API** (`api` type); images via **browser JavaScript** plus a browser-network download path.

## What is scraped, how, and why

- **Chapter list**: chapters are loaded dynamically through the internal API endpoint `https://www.kunmanga.online/api/comics/{slug}/chapters?page=N`. Why use the API instead of the page: the site renders chapters client-side, but the JSON API returns them in clean, paginated form.
- **Chapter images**: the reader page renders images into `div.reading-content img`. Why browser JavaScript: image extraction requires a rendered DOM, and CF cookies are needed for the image CDN.
- **Manual CF prompt**: `NeedsManualCFPrompt` forces the manga URL to be opened in the user's real browser *before* chapter extraction even when the main manga page does not trigger a CF challenge, so the browser extension captures the `cf_clearance` cookies that the image CDN requires. The prompt is skipped if valid CF data already exists on disk for the domain.

## Chapter-list extraction flow

1. `fetchChaptersViaAPI` pulls the slug from the manga URL (`/manga/{slug}`), then loops `page = 1, 2, …` over the chapters API, sleeping **200ms** between pages to be polite.
2. Each page's `chapters[]` yields `chapter_num` + `chapter_slug`; chapter URLs are built as `https://www.kunmanga.online/manga/{slug}/{chapter_slug}`.
3. Loop stops at `last_page`; all chapters are returned as `{num, url}` maps.

## Chapter download flow

1. **Special-cased in the manager** (`manager.go`): kunmanga images are downloaded through the headless browser's network stack (`NewBrowserSession` + `DownloadChapterImages`, 90s session timeout) instead of Go's HTTP client — this bypasses Cloudflare's TLS-fingerprint checks that block Go/curl clients. If the browser path fails or returns 0 images, it falls back to the normal HTTP flow.
2. HTTP fallback: `extractImagesWithJS` runs in a browser with `WaitSelector: div.reading-content img`; the JS maps every `img.src` and filters empties. Images are then downloaded with CF-bypass headers, converted to JPEG, and packaged into the CBZ.

## Normalization

- **URL**: not modified (API returns absolute URLs).
- **Filename** (`ch%03s[.part].cbz`): regex `chapter[-_.]?(\d+)((?:[-_.]\d+)*)` over the chapter URL handles `/chapter-72`, `/chapter-72-5`, `/chapter-72.5`, `/chapter-72-5-1`; separators normalize to dots. Unparseable URLs warn and fall back to `ch000.cbz`.

## Retry & backoff

Default policy (no `GetRetryPolicy` override):

| Operation | Total attempts | Backoff |
|-----------|----------------|---------|
| Chapter-list / image-list fetch | 3 | `2^attempt × 1s` |
| Chapter download | 3 | `2^attempt × 1s` |
| Image download | 3 | `2^attempt × 1s` (adaptive deadline: 2min base, +10s/fail, 30min cap) |

Plus the API's own 200ms inter-page sleep and the 1500ms inter-image rate limit.