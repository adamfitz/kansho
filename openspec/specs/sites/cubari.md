# cubari Site Plugin

- **Site name**: `cubari` — **Domain**: `cubari.moe` — **File**: `sites/cubari.go`
- **Cloudflare bypass needed**: no
- **Extraction**: `custom` parser for both chapters and images, over plain HTTP (no browser).

## What is scraped, how, and why

- **Chapter list**: cubari serves a series in two shapes:
  1. *Normal series*: the page is a Next.js app with a `__NEXT_DATA__` JSON script block. The chapter metadata lives at `props.pageProps.series.chapters` (a map), each entry having `chapter` (number) and `id`. Why: one static fetch of the series page gives every chapter.
  2. *Gist series*: no `__NEXT_DATA__` — the page contains a `read/gist/{base64url-id}` route. The gist ID is base64 (URL-safe, padded) and decodes to a `raw/{owner}/{repo}/{path}` GitHub path; the parser fetches the raw GitHub JSON and reads `chapters.{key}.groups` (first group value = API path). Why: the gist JSON mirrors other proxy sources and carries the chapter grouping needed to build reader paths.
- **Chapter images**: each chapter URL resolves to a JSON image-list endpoint (ImgChest-style); the body is a raw JSON array of image URL strings. Why: the parser can consume the array directly without HTML parsing — it unmarshals the fetched body into `[]string`.
- The normal-series `__NEXT_DATA__` chapter JSON also carries per-chapter image URLs inside `props.pageProps.chapter.groups` (helpers `parseNormalCubariImages` / `parseGistChapterImages` implement this), but the active image parser uses the raw JSON-array response.

## Chapter-list extraction flow

1. `parseCubariChapters` first tries `extractNextDataJSON` (`<script id="__NEXT_DATA__"...>`). On success, `parseCubariSeriesJSON` builds `https://cubari.moe/read/{id}/` URLs with `ch%03d.cbz` names.
2. Missing `__NEXT_DATA__` → gist path: `extractGistRawURL` decodes the base64 id, builds `https://raw.githubusercontent.com/{path}`, fetches it through the `RequestExecutor` (HTTP first, browser fallback) with a **20-second** timeout, and `parseCubariGistJSON` builds `https://cubari.moe{apiPath}` URLs.
3. Empty results error ("no usable chapters / chapter not found").

## Chapter download flow

1. `FetchChapterImages` fetches the chapter URL through the executor and calls `parseCubariImages`, which unmarshals the body as a raw JSON array of image URL strings.
2. Images are downloaded with the standard plain-HTTP image downloader, converted to JPEG, and packaged into the CBZ.

## Normalization

- **URL**: not modified — the value produced by the chapter parser is already a full cubari URL (normal series) or built with the host prefix (gist).
- **Filename** (`ch%03d.cbz` / `ch%03.1f.cbz`): the `chapter` value is parsed as a float; integers pad to 3 digits (`ch005.cbz`), decimals keep one fractional digit (`ch005.5.cbz`); a missing value defaults to `0`.

## Retry & backoff

Default policy (no `GetRetryPolicy` override):

| Operation | Total attempts | Backoff |
|-----------|----------------|---------|
| Chapter-list / image-list fetch | 3 | `2^attempt × 1s` |
| Chapter download | 3 | `2^attempt × 1s` |
| Image download | 3 | `2^attempt × 1s` (adaptive deadline: 2min base, +10s/fail, 30min cap) |

## Debug

Implements `DebugSite` with `SaveHTML: **true**` — fetched HTML is written to `cubari_debug.html`. The parser also records the last decoded gist ID in a package-level variable.