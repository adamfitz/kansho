# Downloader Package

## Overview

The `downloader` package is a **centralized download orchestration system** for manga sites with **transparent handling of network complexities**. 

It implements a **plugin architecture** where site-specific code only handles parsing logic, while all download execution, network error handling, and bypass mechanisms are managed automatically by the downloader.

## Key Features ✨

### Automatic & Transparent
- ✅ **Smart Retry Logic**: Exponential backoff (2^attempt seconds) for all network operations
- ✅ **Response Decompression**: Automatic gzip/brotli decompression
- ✅ **HTTP→Browser Fallback**: Automatically falls back to chromedp when HTTP fails
- ✅ **Rate Limiting**: Built-in 1.5s rate limiting for polite image scraping
- ✅ **Context-Aware**: Full support for cancellation and timeouts

### Cloudflare Handling (When Needed)
- ✅ **Proactive Bypass**: For sites with `NeedsCFBypass() = true`, automatically applies stored CF cookies if available
- ✅ **Challenge Detection**: Detects CF challenges in responses (regardless of `NeedsCFBypass` setting)
- ✅ **Browser Integration**: Opens browser for manual solve when challenges are detected
- ✅ **Cookie Management**: Validates stored cookies, marks expired ones as failed

**Note:** CF bypass features only activate when:
1. **Proactive bypass**: Site reports `NeedsCFBypass() = true` AND stored cookies exist
2. **Challenge detection**: CF actually returns a challenge page (can happen to any site)

Sites without CF protection (`NeedsCFBypass() = false`) work normally with zero CF overhead.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     Download Queue                          │
│              (config.ExecuteSiteDownload)                   │
└───────────────────────┬─────────────────────────────────────┘
                        │
                        ▼
┌─────────────────────────────────────────────────────────────┐
│                  Site Download Function                      │
│              (e.g., StonescapeDownloadChapters)             │
│                                                              │
│  ┌────────────────────────────────────────────────────┐    │
│  │  1. Create SitePlugin instance                      │    │
│  │  2. Create DownloadConfig                           │    │
│  │  3. Create Manager and call Download()             │    │
│  └────────────────────────────────────────────────────┘    │
└───────────────────────┬─────────────────────────────────────┘
                        │
                        ▼
┌─────────────────────────────────────────────────────────────┐
│                    Downloader Manager                        │
│                  (downloader.Manager)                        │
│                                                              │
│  ┌────────────────────────────────────────────────────┐    │
│  │  Orchestrates entire download workflow:             │    │
│  │  • Creates HTTPClient with optional CF bypass       │    │
│  │  • Fetches chapter URLs (with retry)                │    │
│  │  • Filters already downloaded                        │    │
│  │  • Downloads each chapter (with retry)              │    │
│  │  • Fetches chapter images (with retry)              │    │
│  │  • Downloads images (with retry + rate limit)       │    │
│  │  • Handles progress callbacks                        │    │
│  │  • Manages temp directories                          │    │
│  │  • Creates CBZ files                                 │    │
│  └────────────────────────────────────────────────────┘    │
└───────────────────────┬─────────────────────────────────────┘
                        │
                        ▼
┌─────────────────────────────────────────────────────────────┐
│                   HTTPClient / Executor                      │
│           (Handles all network complexity)                   │
│                                                              │
│  ┌────────────────────────────────────────────────────┐    │
│  │  • Loads CF bypass data (if NeedsCFBypass=true)    │    │
│  │  • Applies CF cookies to requests (if available)    │    │
│  │  • Retries with exponential backoff (5 attempts)    │    │
│  │  • Decompresses responses (gzip/brotli)            │    │
│  │  • Detects CF challenges in responses               │    │
│  │  • Opens browser when challenges detected           │    │
│  │  • Falls back to chromedp if HTTP fails            │    │
│  └────────────────────────────────────────────────────┘    │
└───────────────────────┬─────────────────────────────────────┘
                        │
                        ▼
┌─────────────────────────────────────────────────────────────┐
│                     SitePlugin                               │
│              (Site-Specific Parsing Logic)                   │
│                                                              │
│  ┌────────────────────────────────────────────────────┐    │
│  │  GetChapterURLs(mangaURL) → map[filename]url       │    │
│  │  GetChapterImages(chapterURL) → []imageURL         │    │
│  │  GetSiteName() → "sitename"                        │    │
│  │  NeedsCFBypass() → bool                            │    │
│  └────────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────┘
```

## Key Design Principles

### 1. **Separation of Concerns**
- **Site Plugins**: Only handle HTML/JSON parsing and URL extraction
- **Downloader**: Handles all execution logic (downloading, retries, CF bypass, conversion, CBZ creation)

### 2. **Transparency**
- Sites just implement `GetChapterURLs()` and `GetChapterImages()`
- All network complexity is handled automatically
- CF bypass only activates when needed
- Retry logic works the same for all sites

### 3. **Modularity**
- Each site is a self-contained plugin
- New sites can be added without modifying core download logic
- Bug fixes to download logic benefit all sites automatically

### 4. **Consistency**
- All sites use the same download workflow
- Uniform error handling and progress reporting
- Standardized logging format

## Files

```
downloader/
├── interfaces.go      # SitePlugin interface and types
├── manager.go         # Main orchestration logic with retry
├── client.go          # HTTP client with CF bypass & retry
├── executor.go        # Request executor with HTTP/Browser fallback
├── chromedp.go        # Browser automation utilities
├── example.md         # Usage examples
└── README.md          # This file
```

## Network Retry Strategy

The downloader implements **exponential backoff** for all network operations:

| Operation | Max Retries | Base Timeout | Backoff Strategy |
|-----------|-------------|--------------|------------------|
| Chapter URLs fetch | 3 | 10s | 2^attempt seconds |
| Chapter images fetch | 3 | 10s | 2^attempt seconds |
| Individual chapter download | 3 | - | 2^attempt seconds |
| Image download | 3 | - | 2^attempt seconds |
| HTTP request (internal) | 5 | 10s | +5s per attempt |

**Example**: Image download fails:
- Attempt 1: Immediate
- Attempt 2: Wait 2s (2^1)
- Attempt 3: Wait 4s (2^2)
- Attempt 4: Fail after total ~6s of backoff

## Cloudflare Bypass Workflow

### For Sites with `NeedsCFBypass() = true`

```
1. Manager creates HTTPClient
   └─> HTTPClient.LoadFromFile(domain)
       ├─> Found? → Validate cookies
       │   ├─> Valid? → Apply to all requests ✓
       │   └─> Expired? → Mark as failed, continue without bypass
       └─> Not found? → Continue without bypass

2. Make HTTP request
   ├─> Applies CF cookies (if loaded above)
   ├─> Sets browser-like headers
   └─> Automatic decompression

3. Check response
   ├─> No CF challenge? → Success! ✓
   └─> CF challenge detected? 
       ├─> Delete invalid cookies
       ├─> Open browser for manual solve
       └─> Return CfChallengeError
```

### For Sites with `NeedsCFBypass() = false`

```
1. Manager creates HTTPClient
   └─> Skips CF cookie loading

2. Make HTTP request
   ├─> Uses generic browser headers
   └─> Automatic decompression

3. Check response
   ├─> No CF challenge? → Success! ✓
   └─> CF challenge detected? (unexpected)
       ├─> Open browser for manual solve
       └─> Return CfChallengeError
```

**Key Point**: Challenge detection works for ALL sites. CF bypass is only proactive for sites that declare they need it.

## Creating a New Site Plugin

### Step 1: Implement the SitePlugin Interface

```go
package sites

import (
    "context"
    "kansho/downloader"
)

type MySite struct{}

// Ensure interface compliance
var _ downloader.SitePlugin = (*MySite)(nil)

func (s *MySite) GetSiteName() string {
    return "mysite"
}

func (s *MySite) NeedsCFBypass() bool {
    // Return true if this site uses Cloudflare protection
    // Return false if it doesn't
    return false
}

func (s *MySite) GetChapterURLs(ctx context.Context, mangaURL string) (map[string]string, error) {
    // Parse manga page and return map[filename]url
    // The downloader handles:
    // - HTTP requests with retry
    // - CF bypass (if NeedsCFBypass=true)
    // - Response decompression
    // - Error handling
    
    // You can use:
    // 1. downloader.RequestExecutor for HTTP/Browser
    // 2. parser functions for parsing
    // 3. Or any other approach
    
    // Example: {"ch001.cbz": "https://mysite.com/manga/chapter-1"}
    return parseChaptersFromMangaPage(mangaURL)
}

func (s *MySite) GetChapterImages(ctx context.Context, chapterURL string) ([]string, error) {
    // Parse chapter page and return ordered image URLs
    // Same automatic handling as GetChapterURLs
    
    // Example: ["https://cdn.mysite.com/img1.jpg", "..."]
    return parseImagesFromChapterPage(chapterURL)
}
```

### Step 2: Create the Download Entry Point

```go
func MySiteDownloadChapters(ctx context.Context, manga *config.Bookmarks, progressCallback func(string, float64, int, int, int)) error {
    site := &MySite{}
    
    config := &downloader.DownloadConfig{
        Manga:            manga,
        Site:             site,
        ProgressCallback: progressCallback,
    }

    manager := downloader.NewManager(config)
    return manager.Download(ctx)
}
```

### Step 3: Register the Site

In `sites/siteRegistry.go`:

```go
func init() {
    // ... existing sites ...
    config.RegisterSite("mysite", MySiteDownloadChapters)
}
```

### Step 4: Add to sites.json

```json
{
  "sites": [
    {
      "name": "mysite",
      "display_name": "My Site",
      "required_fields": {
        "url": true,
        "shortname": false,
        "title": true,
        "location": true
      }
    }
  ]
}
```

## Example: Stonescape Plugin

See `sites/stonescape.go` for a complete working example:

```go
type StonescapeSite struct{}

func (s *StonescapeSite) GetSiteName() string {
    return "stonescape"
}

func (s *StonescapeSite) NeedsCFBypass() bool {
    return false // StoneScape doesn't use CF
}

func (s *StonescapeSite) GetChapterURLs(ctx context.Context, mangaURL string) (map[string]string, error) {
    // Uses chromedp directly - no CF needed
    browserCtx, cancel := chromedp.NewContext(ctx)
    defer cancel()
    
    var rawChapters []map[string]string
    err := chromedp.Run(browserCtx,
        chromedp.Navigate(mangaURL),
        chromedp.WaitVisible("li.wp-manga-chapter a"),
        chromedp.Evaluate(`...`, &rawChapters),
    )
    
    return normalizeChapters(rawChapters), err
}
```

**Key features**:
- Clean separation: Only ~150 lines vs 400+ in old implementations
- No download logic: Just parsing functions
- No manual retry handling: Downloader does it
- No manual CF handling: Not needed for this site

## Using the Downloader in Your Site

### Option 1: Using RequestExecutor (Recommended)

```go
func (s *MySite) GetChapterURLs(ctx context.Context, mangaURL string) (map[string]string, error) {
    // Create executor - automatically handles CF bypass based on NeedsCFBypass()
    executor, err := downloader.NewRequestExecutor(mangaURL, s.NeedsCFBypass())
    if err != nil {
        return nil, err
    }

    // Fetch HTML with automatic retry, decompression, CF handling
    html, err := executor.FetchHTML(ctx, mangaURL, "div.chapter-list")
    if err != nil {
        return nil, err
    }

    // Just parse!
    return parseChaptersFromHTML(html), nil
}
```

### Option 2: Using HTTPClient Directly

```go
func (s *MySite) GetChapterURLs(ctx context.Context, mangaURL string) (map[string]string, error) {
    // Create HTTP client with CF bypass
    client, err := downloader.NewHTTPClient("mysite.com", s.NeedsCFBypass())
    if err != nil {
        return nil, err
    }

    // Fetch with automatic retry and decompression
    html, err := client.FetchHTML(ctx, mangaURL)
    if err != nil {
        return nil, err
    }

    return parseChaptersFromHTML(html), nil
}
```

### Option 3: Using Colly with CF Bypass

```go
func (s *MySite) GetChapterURLs(ctx context.Context, mangaURL string) (map[string]string, error) {
    client, _ := downloader.NewHTTPClient("mysite.com", s.NeedsCFBypass())
    
    // Get pre-configured Colly collector with CF bypass applied!
    c := client.CreateCollyCollector()

    var chapters []string
    c.OnHTML("a.chapter", func(e *colly.HTMLElement) {
        chapters = append(chapters, e.Attr("href"))
    })

    if err := c.Visit(mangaURL); err != nil {
        return nil, err
    }

    return normalizeChapters(chapters), nil
}
```

### Option 4: Using Browser (chromedp)

```go
func (s *MySite) GetChapterImages(ctx context.Context, chapterURL string) ([]string, error) {
    // Create browser session with CF bypass
    session, err := downloader.NewBrowserSession(ctx, "mysite.com", s.NeedsCFBypass())
    if err != nil {
        return nil, err
    }
    defer session.Close()

    // Navigate with automatic CF cookie injection (if NeedsCFBypass=true)
    if err := session.Navigate(chapterURL, "img.page"); err != nil {
        return nil, err
    }

    // Extract with JavaScript
    var images []string
    js := `[...document.querySelectorAll('img.page')].map(img => img.src)`
    if err := session.Evaluate(js, &images); err != nil {
        return nil, err
    }

    return images, nil
}
```

## What the Downloader Handles Automatically

Site plugins don't need to worry about:

- ✅ **Retry logic**: All network operations retry automatically with exponential backoff
- ✅ **CF cookie loading**: Loaded automatically if `NeedsCFBypass() = true`
- ✅ **CF cookie validation**: Expired cookies are detected and marked as failed
- ✅ **CF challenge detection**: Works for all sites regardless of `NeedsCFBypass()` setting
- ✅ **Browser opening**: Automatically opens when CF challenges detected
- ✅ **Response decompression**: Gzip and Brotli handled automatically
- ✅ **HTTP headers**: Browser-like headers set automatically
- ✅ **Timeout handling**: Increasing timeouts on retry attempts
- ✅ **Context cancellation**: Respects context cancellation throughout
- ✅ **Image downloading**: CF bypass applied to image downloads if needed
- ✅ **Rate limiting**: 1.5s delay between image downloads
- ✅ **Progress reporting**: Automatic progress callbacks
- ✅ **Temp directory management**: Created and cleaned up automatically
- ✅ **CBZ creation**: Automatic from downloaded images

## Migration from Old Style

See `MIGRATION_GUIDE.md` for detailed migration instructions.

**Before** (manual everything):
```go
c := colly.NewCollector()
cf.ApplyToCollector(c, url)
c.OnResponse(func(r *colly.Response) {
    cf.DecompressResponse(r, "<site>")
})
// ... manual retry loops ...
// ... manual CF detection ...
```

**After** (automatic):
```go
func (s *Site) GetChapterURLs(ctx context.Context, url string) (map[string]string, error) {
    executor, _ := downloader.NewRequestExecutor(url, s.NeedsCFBypass())
    html, _ := executor.FetchHTML(ctx, url, "")
    return parseChapters(html), nil
}
```

## Benefits

### For Site Maintainers
- Focus only on parsing logic
- No need to understand retry strategies, CF bypass, or network error handling
- Changes don't affect other sites
- Works the same whether site uses CF or not

### For Download Logic
- Fix bugs once, benefits all sites
- Add features (better retries, new bypass methods) universally
- Easier to test and maintain
- Centralized logging and debugging

### For New Features
- Want parallel downloads? Modify `manager.go`
- Need different rate limiting? Change one place
- Want download resumption? Implement in downloader
- New CF bypass method? Update `client.go`, all sites benefit

## Testing Your Site Plugin

```go
func TestMySite(t *testing.T) {
    site := &MySite{}
    ctx := context.Background()

    // Test chapter fetching (with automatic retry/CF handling)
    chapters, err := site.GetChapterURLs(ctx, "https://mysite.com/manga/test")
    if err != nil {
        t.Fatalf("Failed: %v", err)
    }

    if len(chapters) == 0 {
        t.Error("No chapters found")
    }

    // Test image fetching (with automatic retry/CF handling)
    images, err := site.GetChapterImages(ctx, chapters["ch001.cbz"])
    if err != nil {
        t.Fatalf("Failed: %v", err)
    }

    if len(images) == 0 {
        t.Error("No images found")
    }
}
```

## Troubleshooting

### "CF challenge detected" errors

This means Cloudflare returned a challenge page:

1. The downloader automatically opens your browser
2. Use the browser extension to capture cookies after solving
3. Import the cookies in the app
4. Retry the download - stored cookies will be applied automatically

### Images failing to download even with retry

Check if the image CDN uses a different domain:
- Main site: `mysite.com` (CF cookies captured here)
- Image CDN: `cdn.mysite.com` (might need separate cookies)

Solution: Ensure browser extension captures cookies for both domains.

### Site works without CF bypass but downloads fail

The site might have recently added CF protection:
1. Set `NeedsCFBypass() = true`
2. Restart the download - it will prompt for cookies
3. Capture cookies with browser extension

### Want to see what's happening under the hood

Enable verbose logging - the downloader logs:
- CF cookie loading/validation
- Each retry attempt with backoff time
- Response decompression
- CF challenge detection
- Browser fallback triggers

Check your log file at `~/.config/kansho/kansho.log`

## Future Enhancements

- [ ] Parallel image downloads (within chapter)
- [ ] Configurable retry counts per operation
- [ ] Configurable rate limiting per site
- [ ] Download statistics and metrics
- [ ] Pre-download URL validation
- [ ] Post-download CBZ verification
- [ ] Automatic CF cookie refresh before expiry
- [ ] Support for other anti-bot systems (PerimeterX, etc.)