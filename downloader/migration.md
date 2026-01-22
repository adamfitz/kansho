# Migration Guide: Moving Sites to Enhanced Downloader Package

This guide explains how to migrate existing site implementations to use the new enhanced downloader package with automatic CF bypass, retry logic, and HTTP/Browser fallback.

## Benefits of Migration

After migration, your site will automatically get:

- ✅ Cloudflare bypass handling (cookie-based & Turnstile)
- ✅ Automatic retry with exponential backoff  
- ✅ Response decompression (gzip/brotli)
- ✅ CF challenge detection and browser opening
- ✅ HTTP→Browser fallback when needed
- ✅ Rate limiting
- ✅ Consistent error handling and logging

## Migration Steps

### Step 1: Remove Manual CF Handling

**Before** (in your site's code):
```go
// DON'T DO THIS ANYMORE
c := colly.NewCollector()
if err := cf.ApplyToCollector(c, mangaURL); err != nil {
    log.Printf("Failed to apply CF bypass: %v", err)
}

c.OnResponse(func(r *colly.Response) {
    cf.DecompressResponse(r, "<mysite>")
})
```

**After** (downloader handles this automatically):
```go
// Site plugin just implements the interface - no CF handling needed!
func (s *MySite) GetChapterURLs(ctx context.Context, mangaURL string) (map[string]string, error) {
    // The downloader package will handle CF bypass, decompression, etc.
    // Just focus on parsing!
}
```

### Step 2: Remove Manual Retry Logic

**Before**:
```go
func (s *MySite) getChapterURLs(url string) ([]string, error) {
    maxRetries := 5
    for attempt := 0; attempt < maxRetries; attempt++ {
        // ... retry logic ...
    }
    return chapters, nil
}
```

**After**:
```go
// Downloader handles retries automatically!
func (s *MySite) GetChapterURLs(ctx context.Context, mangaURL string) (map[string]string, error) {
    // Just make one attempt - downloader will retry if needed
    return s.parseChapters(mangaURL)
}
```

### Step 3: Use Context for Cancellation

**Before**:
```go
func MySiteDownloadChapters(ctx context.Context, manga *config.Bookmarks, ...) error {
    // Manual context checking
    select {
    case <-ctx.Done():
        return ctx.Err()
    default:
    }
}
```

**After**:
```go
// Context is passed to your methods - just use it!
func (s *MySite) GetChapterURLs(ctx context.Context, mangaURL string) (map[string]string, error) {
    // No need to manually check ctx.Done() - downloader handles it
    // But you can still check if you have long-running operations
}
```

## Complete Migration Example

### Before: kunmanga.go (Old Style)

```go
func kunmangaChapterUrls(mangaURL string) ([]string, error) {
    c := colly.NewCollector(
        colly.UserAgent("Mozilla/5.0 ..."),
        colly.AllowURLRevisit(),
    )

    // Manual CF bypass
    parsedURL, _ := url.Parse(mangaURL)
    domain := parsedURL.Hostname()
    bypassData, err := cf.LoadFromFile(domain)
    hasStoredData := (err == nil)
    
    if hasStoredData {
        if err := cf.ApplyToCollector(c, mangaURL); err != nil {
            log.Printf("Failed to apply bypass: %v", err)
        }
    }

    // Manual decompression
    c.OnResponse(func(r *colly.Response) {
        cf.DecompressResponse(r, "<kunmanga>")
    })

    // Manual CF detection
    var cfDetected bool
    var cfInfo *cf.CfInfo
    c.OnResponse(func(r *colly.Response) {
        isCF, info, _ := cf.DetectFromColly(r)
        if isCF {
            cfDetected = true
            cfInfo = info
        }
    })

    // ... scraping logic ...
}
```

### After: kunmanga.go (New Style with Downloader Package)

```go
package sites

import (
    "context"
    "kansho/config"
    "kansho/downloader"
)

// KunmangaSite implements the SitePlugin interface
type KunmangaSite struct{}

var _ downloader.SitePlugin = (*KunmangaSite)(nil)

func (s *KunmangaSite) GetSiteName() string {
    return "kunmanga"
}

func (s *KunmangaSite) NeedsCFBypass() bool {
    return true // Kunmanga uses Cloudflare
}

func (s *KunmangaSite) GetChapterURLs(ctx context.Context, mangaURL string) (map[string]string, error) {
    // Create executor - it handles CF bypass automatically!
    executor, err := downloader.NewRequestExecutor(mangaURL, s.NeedsCFBypass())
    if err != nil {
        return nil, err
    }

    // Fetch HTML - executor handles retries, CF, decompression automatically
    html, err := executor.FetchHTML(ctx, mangaURL, "ul.main.version-chap")
    if err != nil {
        return nil, err
    }

    // Just parse the HTML!
    return s.parseChapters(html)
}

func (s *KunmangaSite) GetChapterImages(ctx context.Context, chapterURL string) ([]string, error) {
    executor, err := downloader.NewRequestExecutor(chapterURL, s.NeedsCFBypass())
    if err != nil {
        return nil, err
    }

    html, err := executor.FetchHTML(ctx, chapterURL, "div.reading-content")
    if err != nil {
        return nil, err
    }

    return s.parseImages(html)
}

// Entry point for download queue
func KunmangaDownloadChapters(ctx context.Context, manga *config.Bookmarks, progressCallback func(string, float64, int, int, int)) error {
    site := &KunmangaSite{}
    
    cfg := &downloader.DownloadConfig{
        Manga:            manga,
        Site:             site,
        ProgressCallback: progressCallback,
    }

    return downloader.NewManager(cfg).Download(ctx)
}
```

## Using Colly with the Downloader

If your site needs Colly for complex scraping:

```go
func (s *MySite) GetChapterURLs(ctx context.Context, mangaURL string) (map[string]string, error) {
    // Create executor
    executor, err := downloader.NewRequestExecutor(mangaURL, s.NeedsCFBypass())
    if err != nil {
        return nil, err
    }

    // Get a pre-configured Colly collector with CF bypass!
    c := executor.GetHTTPClient().CreateCollyCollector()

    // Use it normally - CF bypass is already applied!
    var chapters []string
    c.OnHTML("a.chapter", func(e *colly.HTMLElement) {
        chapters = append(chapters, e.Attr("href"))
    })

    if err := c.Visit(mangaURL); err != nil {
        return nil, err
    }

    return s.normalizeChapters(chapters), nil
}
```

## Using Browser (chromedp) with the Downloader

For sites that need JavaScript execution:

```go
func (s *MySite) GetChapterImages(ctx context.Context, chapterURL string) ([]string, error) {
    // FetchHTML automatically tries HTTP first, then falls back to browser
    executor, err := downloader.NewRequestExecutor(chapterURL, false)
    if err != nil {
        return nil, err
    }

    // This will use HTTP if possible, browser if HTTP fails
    html, err := executor.FetchHTML(ctx, chapterURL, "#chapter-images img")
    if err != nil {
        return nil, err
    }

    return s.parseImages(html)
}
```

Or use browser directly:

```go
func (s *MySite) GetChapterImages(ctx context.Context, chapterURL string) ([]string, error) {
    // Create browser session with CF bypass
    session, err := downloader.NewBrowserSession(ctx, "mysite.com", s.NeedsCFBypass())
    if err != nil {
        return nil, err
    }
    defer session.Close()

    // Navigate with automatic CF cookie injection
    if err := session.Navigate(chapterURL, "img.page"); err != nil {
        return nil, err
    }

    // Extract data with JavaScript
    var imageURLs []string
    js := `[...document.querySelectorAll('img.page')].map(img => img.src)`
    if err := session.Evaluate(js, &imageURLs); err != nil {
        return nil, err
    }

    return imageURLs, nil
}
```

## Common Patterns

### Pattern 1: Simple HTTP Fetch

```go
func (s *MySite) GetChapterURLs(ctx context.Context, mangaURL string) (map[string]string, error) {
    executor, err := downloader.NewRequestExecutor(mangaURL, false)
    if err != nil {
        return nil, err
    }

    html, err := executor.FetchHTML(ctx, mangaURL, "")
    if err != nil {
        return nil, err
    }

    return parseChaptersFromHTML(html)
}
```

### Pattern 2: CF-Protected Site

```go
func (s *MySite) NeedsCFBypass() bool {
    return true // Site uses Cloudflare
}

// Everything else is the same - downloader handles CF automatically!
```

### Pattern 3: Pagination

```go
func (s *MySite) GetChapterURLs(ctx context.Context, mangaURL string) (map[string]string, error) {
    executor, err := downloader.NewRequestExecutor(mangaURL, s.NeedsCFBypass())
    if err != nil {
        return nil, err
    }

    allChapters := make(map[string]string)
    page := 1

    for {
        pageURL := fmt.Sprintf("%s?page=%d", mangaURL, page)
        
        // Each page fetch gets automatic retry/CF handling
        html, err := executor.FetchHTML(ctx, pageURL, "")
        if err != nil {
            return nil, err
        }

        chapters := parsePageChapters(html)
        if len(chapters) == 0 {
            break
        }

        for k, v := range chapters {
            allChapters[k] = v
        }

        page++
    }

    return allChapters, nil
}
```

## Testing Your Migrated Site

```go
func TestMySiteMigration(t *testing.T) {
    site := &MySite{}
    ctx := context.Background()

    // Test chapter fetching
    chapters, err := site.GetChapterURLs(ctx, "https://mysite.com/manga/test")
    if err != nil {
        t.Fatalf("Failed: %v", err)
    }

    if len(chapters) == 0 {
        t.Error("No chapters found")
    }

    // Test image fetching
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

The downloader automatically opens the browser for manual solve. After solving:
1. The browser extension captures the cookies
2. Import them in the app
3. Retry the download

### Images failing to download

The downloader uses `parser.DownloadConvertToJPGRenameCf()` which includes:
- Automatic CF bypass for image CDNs
- Retry logic with exponential backoff
- Automatic format conversion

### Site-specific headers needed

```go
// You can still customize the HTTP client if needed
executor, _ := downloader.NewRequestExecutor(mangaURL, true)
httpClient := executor.GetHTTPClient()

// Or create a custom Colly collector
c := httpClient.CreateCollyCollector()
c.OnRequest(func(r *colly.Request) {
    r.Headers.Set("X-Custom-Header", "value")
})
```

## Checklist

- [ ] Remove manual `cf.ApplyToCollector()` calls
- [ ] Remove manual `cf.DecompressResponse()` calls
- [ ] Remove manual `cf.Detectcf()` / `cf.DetectFromColly()` calls
- [ ] Remove manual retry loops
- [ ] Implement `SitePlugin` interface
- [ ] Set `NeedsCFBypass()` correctly
- [ ] Use `context.Context` in method signatures
- [ ] Update entry point to use `downloader.NewManager()`
- [ ] Test with a manga that has CF protection
- [ ] Test cancellation (Ctrl+C during download)

## Need Help?

Check these files for complete examples:
- `sites/stonescape.go` - Clean implementation using downloader
- `downloader/example.md` - More usage examples
- `downloader/README.md` - Package documentation