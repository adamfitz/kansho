# asurascans Site Plugin

- **Site name**: `asurascans` — **Domain**: `asurascans.com` — **File**: `sites/asura.go`
- **Cloudflare bypass needed**: yes
- **Extraction**: `custom` parser for both chapters and images, running over the server-rendered Astro SSR HTML — no headless browser required.

## What is scraped, how, and why

- **Chapter list**: the series page (`/comics/{slug}`) is an Astro SSR page. The chapter list is serialized as JSON inside the `props` attribute of the `<ChapterListReact>` astro-island component. Why scrape the JSON: one static HTTP response contains every chapter and the exact numeric fields needed to build reader URLs.
  - `publicUrl` (`/comics/{full-slug}`) carries the full slug *including the hash suffix* that reader URLs require — the bare `seriesSlug` field must NOT be used.
  - `number` fields give each chapter number.
- **Chapter images**: the reader page (`/comics/{slug}/chapter/{n}`) is also Astro SSR; the `<ChapterReader>` astro-island props hold a `pages` array whose entries reference `cdn.asurascans.com/asura-images/chapters/...` image URLs. Why: the full image list is present in one static page, so no browser is needed, and no WaitSelector is set.

## Chapter-list extraction flow

1. `parseAsuraChapters` greps the HTML for the `ChapterListReact` island and captures its `props` blob via regex (`asuraChapterListPropsRe`).
2. Extracts the full `publicUrl` slug and all `"number":[0,N]` entries with targeted regexes (the props use Astro's `[type,value]` encoding).
3. Builds each chapter URL as `https://asurascans.com/comics/{full-slug}/chapter/{number}` and maps `ch%03d.cbz → URL`, skipping unknown/duplicate numbers.

## Chapter download flow

1. `parseAsuraImages` captures the `ChapterReader` island props, HTML-unescapes (`&quot;`/`&#34;`), and regex-matches `https://cdn.asurascans.com/asura-images/chapters/{a}/{b}/{file}.ext` URLs.
2. If the props yield nothing, it falls back to scanning the whole unescaped page HTML with the same CDN regex; results are de-duplicated preserving order.
3. Images are downloaded via the standard per-image download path with the site's CF-bypass headers, converted to JPEG, and packaged into the CBZ.

## Normalization

- **URL**: already absolute or starts with `/` → prefixed with `https://asurascans.com`.
- **Filename** (`ch%03s[.frac].cbz`): zero-pads the number; a decimal like `92.5` keeps its `.5` suffix (via `asuraChapterFilenameFromInt`); an empty number yields `unknown.cbz`.

## Retry & backoff

Default policy (no `GetRetryPolicy` override):

| Operation | Total attempts | Backoff |
|-----------|----------------|---------|
| Chapter-list / image-list fetch | 3 | `2^attempt × 1s` |
| Chapter download | 3 | `2^attempt × 1s` |
| Image download | 3 | `2^attempt × 1s` (adaptive deadline: 2min base, +10s/fail, 30min cap) |

## Debug

Implements `DebugSite` with `SaveHTML: false`, `HTMLPath: "/tmp/asura_chapter_debug.html"`.