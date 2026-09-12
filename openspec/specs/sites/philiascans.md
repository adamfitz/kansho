# philiascans Site Plugin

- **Site name**: `philiascans` — **Domain**: `philiascans.org` — **Files**: `sites/philliascans.go`, `sites/philliascans_decrypt.go`
- **Cloudflare bypass needed**: no
- **Extraction**: chapters via a **custom** parser over the Next.js RSC streaming payloads; images via **browser JavaScript/React-fiber extraction + in-Go AES decryption** (the site serves *encrypted, tile-scrambled* images).

## What is scraped, how, and why

- **Chapter list**: the series page is a Next.js app with React Server Components (RSC) streaming payloads. Chapter data lives in `langChapters` arrays inside `self.__next_f.push([1,"..."])` script tags, each entry carrying `number`, `slug`, `lang`, `coinPrice`, `isEarlyAccess`. Why scrape the RSC payload: it carries **pricing metadata** — free chapters have `coinPrice=0`, premium chapters `coinPrice>0` and are skipped. Why the bracket-counting scanner: the array contains nested structures, so a naive regex can't capture it; escape sequences (`\"`, `\\`) are handled and decoded before JSON unmarshal.
- **Chapter images**: the reader renders `<canvas>` elements (React) whose fiber data holds encrypted image URLs, the chapter **decryption key**, and the **tile grid size**. Why a browser: the URLs/scrambling parameters exist only in the rendered React fiber tree. Why decrypt in Go: once the metadata is extracted, `DownloadCanvasImages` fetches the encrypted bytes over plain HTTP and calls the site's `TransformImage` (an `ImageDecryptorSite`) to recover the real image.
- **Why scrape at all**: without the fiber-derived chapter key and grid, the encrypted blobs are useless — this is DRM-style protection where the page JS would otherwise decrypt client-side.

## Chapter-list extraction flow

1. `parsePhiliaScansChapters` first tries `parseRSCChapters`:
   - `extractMangaSlug` pulls the slug from the RSC flight route data (`["","series","{slug}"]`) or `/series/{slug}` patterns.
   - Each `__next_f.push` payload is scanned for `\"langChapters\":[`, balanced with a bracket counter, decoded, and unmarshaled into `[]struct{number,slug,lang,coinPrice,isEarlyAccess}`.
   - Paid chapters (`coinPrice > 0`) are skipped; results are de-duplicated by number; URLs look like `https://philiascans.org/series/{slug}/{chapterSlug}?lang={lang}`.
2. If RSC extraction yields nothing, the fallback `parseHTMLChapters` regex-matches `<a href="/read/...">…<div class="chapter-num">Ch.N</div>` anchors and builds `/read/` URLs.

## Chapter download flow

1. **Special-cased in the manager** (`manager.go`): a browser session (120s timeout) runs `DownloadCanvasImages` on the chapter URL (WaitSelector `#pages-container, .page-wrap`), which walks the React fiber tree, collects every encrypted image URL + chapter key + grid size, and closes the session.
2. Each encrypted blob is fetched over HTTP and passed to `TransformImage`, which:
   - Passes through already-clean data (RIFF WebP / `FF D8 FF` JPEG magic).
   - Reads the header (`0xFF 0x04`-style): scheme, width, height.
   - Scheme 4 → AES-CTR with key `HMAC-SHA256(chapterKey, "aesctr4:"+pageIndex)`, zero IV, no descrambling.
   - Scheme 2 → AES-CTR with `HMAC-SHA256(chapterKey, "aesctr:"+pageIndex)` **plus** Fisher-Yates tile descrambling seeded by `HMAC-SHA256(HMAC-SHA256(chapterKey,"tiles:"+pageIndex),"perm:"+counter)` (`gridSize×gridSize` grid).
   - Re-encodes to JPEG (quality 95) for consistent CBZ output.
3. Decrypted images are written as `001.jpg…` and packaged into the CBZ. If the browser path fails, HTTP/simple-HTML parsing is not used by the manager, so the chapter is skipped via the normal error/retry path.
4. A static-HTML parser (`parsePhiliaScansImages`) also exists: it isolates `#ch-images`, reads lazy `data-src` URLs under `/wp-content/uploads/WP-manga/`, and filters the trailing `9999.webp` subscribe/promo sentinel.

## Normalization

- **URL**: relative or protocol-relative paths become `https://philiascans.org{path}`.
- **Filename** (`ch%03d[.frac].cbz`): parses `"Chapter N(.N)"` from the label; unparseable labels fall back to a slugified `label.cbz`.

## Retry & backoff

Default policy (no `GetRetryPolicy` override):

| Operation | Total attempts | Backoff |
|-----------|----------------|---------|
| Chapter-list / image-list fetch | 3 | `2^attempt × 1s` |
| Chapter download | 3 | `2^attempt × 1s` |
| Image download | 3 | `2^attempt × 1s` (adaptive deadline: 2min base, +10s/fail, 30min cap) |

## Debug

Implements `DebugSite` with `SaveHTML: false`, `HTMLPath: "philiascans_debug.html"`.