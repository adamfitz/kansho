# valirscans Site Plugin

- **Site name**: `valirscans` — **Domain**: `valirscans.org` — **File**: `sites/valirscans.go`
- **Cloudflare bypass needed**: no, today. `valirscans.org` is proxied by Cloudflare (`server: cloudflare`, `cf-ray` on every response) but a browser-like request is answered with a normal `200`, `cf-cache-status: DYNAMIC`, and the full page — no `cf-mitigated: challenge`, no interstitial. Set `NeedsCFBypass()` to `true` if a `Just a moment...` page ever appears.
- **Extraction**: `custom` parser for both chapters and images — no browser required, everything is server-rendered.

## The two bot checks (only one is Cloudflare)

The site has **two independent** protections, and they fail in different ways. Both were verified by requesting the series page with different `User-Agent` values:

| Protection | Signature | Blocked UAs (observed) | Allowed UAs (observed) | Fixed by `NeedsCFBypass() = true`? |
|------------|-----------|------------------------|-------------------------|-------------------------------------|
| Cloudflare WAF | **HTTP 403** + `cf-mitigated: challenge` + `Just a moment...` interstitial | absent/empty UA, `python-requests/2.31` | Chrome UA, `kansho/1.0`, `curl`, `wget`, `okhttp`, `PostmanRuntime`, `Java/17`, `Googlebot` | **Yes** — `cf_clearance` is what it wants |
| Site's own scanner blocklist (`x-blocked-by: scanner-blocklist`) | **HTTP 200** + a ~4.4 KB `Access Denied` page | `Go-http-client/1.1`, `Go-http-client/2.0`, `Scrapy/2.11` | Chrome UA, `kansho/1.0`, `curl`, `wget`, `Java/17` | **No** — it is a User-Agent blocklist, not a cookie challenge |

Why it matters: the second one returns **200**, so the downloader's Cloudflare challenge detection never sees it, and it would otherwise surface as the misleading `no RSC flight payload found`. Both parsers therefore call `valirscansBlockedErr` first, which detects the `Access Denied` page and reports it as a bot block with the reason it cannot be fixed by a CF bypass.

kansho does not trip either check today: the HTML path sends a Chrome UA (`downloader/client.go:155`) and the image path sends another one (`parser/imageConverter.go:230`).

**Image host**: `media.valirscans.org` has no bot protection at all — an absent UA, `Go-http-client/1.1`, `python-requests` and a Chrome UA all return the same 336 KB `image/webp` with no `Referer` needed, so even the bare `http.Get` image path is safe here.

## What is scraped, how, and why

- **Site shape**: a Next.js App Router app using React Server Components. Both the series page (`/series/comic/{slug}`) and the chapter page (`/series/comic/{slug}/chapter/{number}`) are fully server-rendered, so a plain HTTP fetch returns the complete data. Nothing needs JavaScript execution, so the plugin uses the HTTP-first request executor rather than launching Chrome.
- **RSC flight payload**: the data is streamed into the HTML as `<script>self.__next_f.push([1,"…"])</script>` chunks. Each chunk body is a JS string literal with its own escaping (`\"`, `\n`, `\uXXXX`, …), so the plugin concatenates the chunks and unescapes them once before reading the JSON inside. This is the same mechanism philiascans uses.
- **Chapter list**: the series page carries a `chapters` array in that payload — `[{number, title, isLocked, hasAccess, coverImage, publishedAt, …}]`. Why: it is the authoritative list (it matches the server-rendered `[data-chapter-row]` rows one-for-one), it is not paginated, and it exposes the access flags needed to skip chapters that cannot be downloaded.
- **Locked chapters**: `isLocked: true` or `hasAccess: false` marks early-access/premium chapters. The site lists them but serves their pages with `isRedacted: true` and empty `imageUrl`, so they are skipped when the list is built instead of failing later during download. A refresh picks them up once they are released.
- **Chapter images**: the chapter page carries the reader's `pages` array in the payload — `[{pageNumber, kind, isRedacted, imageUrl, width, height, …}]` — with every page of the chapter already in reading order. Pages are `kind: "CONTENT"`, `isEncrypted: false`, with empty `tiles`/`strips`/`fragments`.
- **Images**: plain unencrypted WebP files on a separate host, `media.valirscans.org/series/{slug}/{chapter}/p-{uuid}.webp`, which has no bot protection of its own. WebP is converted to JPEG by the shared image pipeline.

## Chapter-list extraction flow

1. `parseValirscansChapters` unescapes the RSC flight payload of the series page.
2. It reads the series base URL from the page's `og:url` meta tag so the site path segment (`/series/comic/{slug}`) is never hard-coded.
3. It pulls the `chapters` array out of the payload with a bracket-counting scan (a regex cannot match a nested array), unmarshals it, and skips locked/no-access chapters.
4. Each remaining chapter becomes `{num, text}` → `{filename → og:url + "/chapter/" + number}`. No chapters left → an error reporting how many locked chapters were skipped.

## Chapter download flow

1. `parseValirscansImages` unescapes the chapter page's RSC flight payload and extracts the `pages` array the same way.
2. Pages are sorted by `pageNumber` (the payload is already ordered; the sort keeps the CBZ order correct if that ever changes), then entries with an empty or non-`http` `imageUrl` are dropped and the rest are de-duplicated.
3. If every page comes back redacted, a specific "chapter is locked/early access" error is returned instead of a generic empty-list error.
4. Images are downloaded with the standard plain-HTTP downloader, converted to JPEG, and packaged into the CBZ.

## Normalization

- **URL**: relative or protocol-relative paths become `https://valirscans.org{path}`. In practice chapter URLs are built as `https://valirscans.org/series/comic/{slug}/chapter/{number}` from `og:url`. The bare `/series/{slug}` form 301-redirects to the canonical URL and is followed by the HTTP client.
- **Filename** (`ch%03d[.frac].cbz`): pulls `(\d+)(?:\.(\d+))?` from the chapter `number` (e.g. `91` → `ch091.cbz`, `1.5` → `ch001.5.cbz`), falling back to the chapter title. Unparseable labels fall back to a slugified `label.cbz` with a warning.

## Retry & backoff

Default policy (no `GetRetryPolicy` override):

| Operation | Total attempts | Backoff |
|-----------|----------------|---------|
| Chapter-list / image-list fetch | 3 | `2^attempt × 1s` |
| Chapter download | 3 | `2^attempt × 1s` |
| Image download | 3 | `2^attempt × 1s` (adaptive deadline: 2min base, +10s/fail, 30min cap) |

## Debug

Implements `DebugSite` with `SaveHTML: false`, `HTMLPath: "valirscans_debug.html"`.

## Tests

`tests/valirscans_int_test.go` (`-tags integration`) runs the real downloader against `https://valirscans.org/series/comic/my-bias-gets-on-the-last-train`: it fetches the chapter list, checks the `chNNN.cbz` filenames and chapter URL shape, then fetches the images of a random chapter and validates that every URL is an `https://media.valirscans.org/…` address with no duplicates.