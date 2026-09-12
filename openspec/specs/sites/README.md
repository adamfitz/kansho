# Site Plugin Index

Per-site documentation for every manga source plugin implemented in `sites/`. Each document walks the plugin from the initial chapter-list scrape through to the per-chapter download, states its extraction method (custom HTML parser, browser JavaScript, or direct API), its retry/backoff policy, and explains what is scraped, how, and why.

## Shared download pipeline

Every plugin runs through the same downloader manager (`downloader/manager.go`), which supplies the retry/backoff machinery. Per-site values in the docs below are the total attempt counts (initial attempt + retries). The defaults used by any site that does not implement a custom retry policy (`downloader/interfaces.go`) are:

| Operation | Total attempts (default) | Backoff |
|-----------|--------------------------|---------|
| Chapter-list / image-list fetch (`MaxFetchRetries`) | 3 | `2^attempt × 1s` (default, static) or decaying |
| Chapter download (`MaxChapterRetries`) | 3 | `2^attempt × 1s` |
| Individual image download (`MaxImageRetries`) | 3 | `2^attempt × 1s` |

Shared mechanics:
- **Fetches** run through `FetchChapterURLs` / `FetchChapterImages`. A Cloudflare challenge error aborts retrying immediately and is returned to the download queue.
- **Adaptive image deadline**: every image attempt gets a context deadline that starts at 2min, grows by 10s with each failed image of the manga, and is capped at 30min; any success resets it to 2min.
- **Rate limiting**: images within a chapter are spaced 1500ms apart (context-aware sleep).
- **Extraction timeouts**: "javascript" browser extraction defaults to 45s unless the site sets a custom `Timeout`.
- **CBZ packaging**: images are downloaded/converted into `/tmp/{site}/{chapter}`, then zipped to the manga location via `parser.CreateCbzFromDir`.
- **Decaying backoff**: when a site enables `DecayBackoff`, the effective backoff base doubles after every fully-failed attempt set and trickles back down one step per success instead of snapping back to base (`downloaders/backoff.go`).

## Sites

| Site | Plugin file(s) | Extraction (chapters → images) | Doc |
|------|---------------|---------------------------------|-----|
| arenascan | `arenascans.go` | custom HTML → custom HTML | [arenascan.md](arenascan.md) |
| asurascans | `asura.go` | custom (Astro JSON) → custom (Astro JSON) | [asurascans.md](asurascans.md) |
| comix | `comix.go` | javascript + Go pagination → javascript + scroll | [comix.md](comix.md) |
| cubari | `cubari.go` | custom (`__NEXT_DATA__` / gist JSON) → custom (JSON array) | [cubari.md](cubari.md) |
| flamecomics | `flamecomics.go` | custom (`__NEXT_DATA__` JSON) → custom (`__NEXT_DATA__`/regex) | [flamecomics.md](flamecomics.md) |
| kingofshojo | `kingofshojo.go` | custom HTML → custom (`ts_reader.run` JSON) | [kingofshojo.md](kingofshojo.md) |
| kunmanga | `kunmanga.go` | API JSON → javascript + browser download | [kunmanga.md](kunmanga.md) |
| mangadex | `managdex.go` | API → API (@Home) | [mangadex.md](mangadex.md) |
| mangakatana | `mangakatana.go` | javascript (CF detection) → custom (`var thzq`) | [mangakatana.md](mangakatana.md) |
| manhuaus | `manhuaus.go` | custom HTML (HTTP-first) → custom HTML (HTTP-first) | [manhuaus.md](manhuaus.md) |
| mgeko | `mgeko.go` | javascript → javascript | [mgeko.md](mgeko.md) |
| philiascans | `philliascans.go`, `philliascans_decrypt.go` | custom (RSC payload) → javascript (canvas/React fiber) + decrypt | [philiascans.md](philiascans.md) |
| ravenscans | `ravenscans.go` | javascript → custom (regex) | [ravenscans.md](ravenscans.md) |
| stonescape | `stonescape.go` | API → API | [stonescape.md](stonescape.md) |
| weebcentral | `weebcentral.go` | custom HTMX endpoint → custom HTMX endpoint | [weebcentral.md](weebcentral.md) |