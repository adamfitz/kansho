# manhuaus Site Plugin

- **Site name**: `manhuaus` — **Domain**: `manhuaus.com` — **File**: `sites/manhuaus.go`
- **Cloudflare bypass needed**: yes
- **Extraction**: `custom` parsers for both chapters and images, deliberately over the **HTTP-first executor path** (browser only as a last resort).

## What is scraped, how, and why

- **Chapter list**: WordPress Madara — the full chapter list is **always server-rendered** as `li.wp-manga-chapter a` links in the manga page HTML. Why avoid the browser: headless Chrome regularly stalls on manhuaus's Cloudflare challenge for minutes and then dies at the refresh pool's 90s deadline, while plain HTTP with the captured CF bypass data returns the complete list in ~2s.
- **Chapter images**: the reading page also server-renders every image as `<img class="wp-manga-chapter-img" data-src="https://img.manhuaus.com/...">` in the initial HTML, read from `div.reading-content img`. Why: same HTTP-first rationale — no rendering needed.
- **CF data**: CF bypass data captured by the browser extension is applied to the HTTP requests; a `WaitSelector` is still declared (`li.wp-manga-chapter a`) but is only used if the executor ever falls back to the browser.

## Chapter-list extraction flow

1. `parseManhuausChapters` loads the HTML into goquery and iterates `li.wp-manga-chapter a`.
2. It extracts the chapter number with `chapterNumRe = /chapter-([\d.]+)/?$` (matching the same shape the old JS extraction produced), taking `href`, number, and link text, and emits `filename → absolute URL`. No matches → error.
3. Empty `WaitSelector` semantics: the executor is used with a **60-second** timeout for the custom parser fetch.

## Chapter download flow

1. `parseManhuausImages` runs over the chapter page's static HTML with **no WaitSelector** (routes through the executor's HTTP-first path, not the browser), collecting `data-src` (falling back to `src`) for every `div.reading-content img`.
2. Images are downloaded with CF-bypass headers, converted to JPEG, and packaged into the CBZ.

## Normalization

- **URL**: not modified (URLs are already absolute).
- **Filename** (`ch%03s` or `ch%03s.%s` for decimals): pads the integer part to 3 digits and preserves a decimal part (`1.5` → `ch001.5.cbz`).

## Retry & backoff

Custom policy via `GetRetryPolicy` — the site frequently stops answering for short stretches, so every loop gets a high budget with a doubled backoff and **decaying backoff** is enabled so waits build up across failing chapters and step back down (one `DecayStep` per success) once the site responds again, rather than snapping back to base:

| Operation | Total attempts | Backoff |
|-----------|----------------|---------|
| Chapter-list / image-list fetch | **8** | `2^attempt × 2s`, decaying (doubles on failure, −2s/step on success, **60s cap**) |
| Chapter download | **5** | `2^attempt × 2s` |
| Image download | **8** | `2^attempt × 2s` (adaptive deadline: 2min base, +10s/fail, 30min cap) |