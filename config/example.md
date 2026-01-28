# Downloader Package Usage Examples

## Basic Usage

### Example 1: Simple Site (No Cloudflare)

```go
package sites

import (
    "context"
    "fmt"
    "log"
    
    "kansho/config"
    "kansho/downloader"
    
    "github.com/chromedp/chromedp"
)

// SimpleSite demonstrates the minimal implementation
type SimpleSite struct{}

var _ downloader.SitePlugin = (*SimpleSite)(nil)

func (s *SimpleSite) GetSiteName() string {
    return "simplesite"
}

func (s *SimpleSite) NeedsCFBypass() bool {
    return false
}

func (s *SimpleSite) GetChapterURLs(ctx context.Context, mangaURL string) (map[string]string, error) {
    // Use chromedp to fetch the manga page
    browserCtx, cancel := chromedp.NewContext(ctx)
    defer cancel()
    
    var chapterLinks []map[string]string
    
    err := chromedp.Run(browserCtx,
        chromedp.Navigate(mangaURL),
        chromedp.WaitVisible("a.chapter-link"),
        chromedp.Evaluate(`
            [...document.querySelectorAll('a.chapter-link')]
            .map(a => ({
                number: a.dataset.chapter,
                url: a.href
            }))
        `, &chapterLinks),
    )
    if err != nil {
        return nil, err
    }
    
    // Convert to filename map
    result := make(map[string]string)
    for _, ch := range chapterLinks {
        filename := fmt.Sprintf("ch%03s.cbz", ch["number"])
        result[filename] = ch["url"]
    }
    
    return result, nil
}

func (s *SimpleSite) GetChapterImages(ctx context.Context, chapterURL string) ([]string, error) {
    browserCtx, cancel := chromedp.NewContext(ctx)
    defer cancel()
    
    var imageURLs []string
    
    err := chromedp.Run(browserCtx,
        chromedp.Navigate(chapterURL),
        chromedp.WaitVisible("img.page-image"),
        chromedp.Evaluate(`
            [...document.querySelectorAll('img.page-image')]
            .map(img => img.src)
        `, &imageURLs),
    )
    
    return imageURLs, err
}

// Entry point for download queue
func SimpleSiteDownloadChapters(ctx context.Context, manga *config.Bookmarks, progressCallback func(string, float64, int, int, int)) error {
    site := &SimpleSite{}
    
    cfg := &downloader.DownloadConfig{
        Manga:            manga,
        Site:             site,
        ProgressCallback: progressCallback,
    }
    
    manager := downloader.NewManager(cfg)
    return manager.Download(ctx)
}
```

### Example 2: Site with Cloudflare Bypass

```go
package sites

import (
    "context"
    "fmt"
    
    "kansho/cf"
    "kansho/config"
    "kansho/downloader"
    
    "github.com/gocolly/colly"
)

type CFProtectedSite struct{}

var _ downloader.SitePlugin = (*CFProtectedSite)(nil)

func (s *CFProtectedSite) GetSiteName() string {
    return "cfsite"
}

func (s *CFProtectedSite) NeedsCFBypass() bool {
    return true  // This site uses Cloudflare
}

func (s *CFProtectedSite) GetChapterURLs(ctx context.Context, mangaURL string) (map[string]string, error) {
    // Use Colly with CF bypass
    c := colly.NewCollector(
        colly.UserAgent("Mozilla/5.0 ..."),
        colly.AllowURLRevisit(),
    )
    
    // Apply CF bypass from stored cookies
    if err := cf.ApplyToCollector(c, mangaURL); err != nil {
        return nil, fmt.Errorf("failed to apply CF bypass: %w", err)
    }
    
    chapterMap := make(map[string]string)
    
    c.OnHTML("div.chapter-list a", func(e *colly.HTMLElement) {
        chapterNum := e.Attr("data-num")
        href := e.Attr("href")
        filename := fmt.Sprintf("ch%03s.cbz", chapterNum)
        chapterMap[filename] = href
    })
    
    if err := c.Visit(mangaURL); err != nil {
        return nil, err
    }
    
    return chapterMap, nil
}

func (s *CFProtectedSite) GetChapterImages(ctx context.Context, chapterURL string) ([]string, error) {
    c := colly.NewCollector()
    
    if err := cf.ApplyToCollector(c, chapterURL); err != nil {
        return nil, err
    }
    
    var images []string
    
    c.OnHTML("img.manga-page", func(e *colly.HTMLElement) {
        images = append(images, e.Attr("src"))
    })
    
    if err := c.Visit(chapterURL); err != nil {
        return nil, err
    }
    
    return images, nil
}

func CFProtectedSiteDownloadChapters(ctx context.Context, manga *config.Bookmarks, progressCallback func(string, float64, int, int, int)) error {
    site := &CFProtectedSite{}
    
    cfg := &downloader.DownloadConfig{
        Manga:            manga,
        Site:             site,
        ProgressCallback: progressCallback,
    }
    
    return downloader.NewManager(cfg).Download(ctx)
}
```

### Example 3: Site with Complex Chapter Numbering

```go
package sites

import (
    "context"
    "fmt"
    "regexp"
    "strings"
    
    "kansho/downloader"
)

type ComplexNumberingSite struct{}

var _ downloader.SitePlugin = (*ComplexNumberingSite)(nil)

func (s *ComplexNumberingSite) GetChapterURLs(ctx context.Context, mangaURL string) (map[string]string, error) {
    // ... fetch raw chapter data ...
    
    rawChapters := []struct {
        Text string
        URL  string
    }{
        {"Chapter 1", "https://..."},
        {"Chapter 1.5", "https://..."},
        {"Chapter 2 Part 1", "https://..."},
        {"Chapter 2 Part 2", "https://..."},
        {"Chapter 3 - Bonus", "https://..."},
    }
    
    result := make(map[string]string)
    
    for _, ch := range rawChapters {
        filename := s.normalizeChapterName(ch.Text)
        result[filename] = ch.URL
    }
    
    return result, nil
}

// normalizeChapterName converts various formats to standard filenames
func (s *ComplexNumberingSite) normalizeChapterName(text string) string {
    // Regex to match: "Chapter 1", "Chapter 1.5", "Chapter 2 Part 1", etc.
    re := regexp.MustCompile(`Chapter\s+(\d+)(?:\.(\d+))?(?:\s+Part\s+(\d+))?(?:\s*-\s*(.+))?`)
    
    matches := re.FindStringSubmatch(text)
    if len(matches) == 0 {
        // Fallback for unknown format
        return fmt.Sprintf("ch%s.cbz", strings.ReplaceAll(text, " ", "-"))
    }
    
    mainNum := matches[1]      // "1", "2", etc.
    decimal := matches[2]      // "5" from "1.5"
    part := matches[3]         // "1" from "Part 1"
    suffix := matches[4]       // "Bonus" from "- Bonus"
    
    // Build filename: ch001.cbz, ch001.5.cbz, ch002.1.cbz, ch003-bonus.cbz
    filename := fmt.Sprintf("ch%03s", mainNum)
    
    if decimal != "" {
        filename += "." + decimal
    }
    
    if part != "" {
        filename += "." + part
    }
    
    if suffix != "" {
        filename += "-" + strings.ToLower(strings.ReplaceAll(suffix, " ", "-"))
    }
    
    return filename + ".cbz"
}

func (s *ComplexNumberingSite) GetChapterImages(ctx context.Context, chapterURL string) ([]string, error) {
    // Standard image fetching
    // ...
    return nil, nil
}

func (s *ComplexNumberingSite) GetSiteName() string { return "complexsite" }
func (s *ComplexNumberingSite) NeedsCFBypass() bool { return false }
```

## Testing Your Site Plugin

### Unit Test Example

```go
package sites

import (
    "context"
    "testing"
)

func TestStonescapeChapterURLs(t *testing.T) {
    site := &StonescapeSite{}
    
    // Test manga URL (use a real one for integration testing)
    mangaURL := "https://stonescape.xyz/manga/test-manga"
    
    ctx := context.Background()
    chapters, err := site.GetChapterURLs(ctx, mangaURL)
    
    if err != nil {
        t.Fatalf("Failed to get chapters: %v", err)
    }
    
    if len(chapters) == 0 {
        t.Error("Expected chapters, got none")
    }
    
    // Check filename format
    for filename := range chapters {
        if !strings.HasPrefix(filename, "ch") || !strings.HasSuffix(filename, ".cbz") {
            t.Errorf("Invalid filename format: %s", filename)
        }
    }
}

func TestStonescapeChapterImages(t *testing.T) {
    site := &StonescapeSite{}
    
    // Test chapter URL
    chapterURL := "https://stonescape.xyz/manga/test-manga/chapter-1"
    
    ctx := context.Background()
    images, err := site.GetChapterImages(ctx, chapterURL)
    
    if err != nil {
        t.Fatalf("Failed to get images: %v", err)
    }
    
    if len(images) == 0 {
        t.Error("Expected images, got none")
    }
    
    // Check URLs are valid
    for _, url := range images {
        if !strings.HasPrefix(url, "http") {
            t.Errorf("Invalid image URL: %s", url)
        }
    }
}
```

## Common Patterns

### Pattern 1: Retry Logic for Network Errors

```go
func (s *MySite) GetChapterURLs(ctx context.Context, mangaURL string) (map[string]string, error) {
    maxRetries := 3
    var lastErr error
    
    for attempt := 0; attempt < maxRetries; attempt++ {
        chapters, err := s.fetchChapterURLsAttempt(ctx, mangaURL)
        if err == nil {
            return chapters, nil
        }
        
        lastErr = err
        log.Printf("[MySite] Attempt %d failed: %v", attempt+1, err)
        
        // Wait before retry
        time.Sleep(time.Duration(attempt+1) * 2 * time.Second)
    }
    
    return nil, fmt.Errorf("failed after %d retries: %w", maxRetries, lastErr)
}
```

### Pattern 2: Handling Pagination

```go
func (s *MySite) GetChapterURLs(ctx context.Context, mangaURL string) (map[string]string, error) {
    allChapters := make(map[string]string)
    page := 1
    
    for {
        pageURL := fmt.Sprintf("%s?page=%d", mangaURL, page)
        
        chapters, err := s.fetchChapterPage(ctx, pageURL)
        if err != nil {
            return nil, err
        }
        
        if len(chapters) == 0 {
            break // No more chapters
        }
        
        // Merge into result
        for filename, url := range chapters {
            allChapters[filename] = url
        }
        
        page++
    }
    
    return allChapters, nil
}
```

### Pattern 3: Using Browser Sessions Efficiently

```go
type MySite struct {
    session *downloader.BrowserSession
}

func (s *MySite) GetChapterImages(ctx context.Context, chapterURL string) ([]string, error) {
    // Reuse browser session if available
    if s.session == nil {
        var err error
        s.session, err = downloader.NewBrowserSession(ctx, "mysite.com", false)
        if err != nil {
            return nil, err
        }
    }
    
    if err := s.session.Navigate(chapterURL, "img.page"); err != nil {
        return nil, err
    }
    
    var images []string
    js := `[...document.querySelectorAll('img.page')].map(img => img.src)`
    
    if err := s.session.Evaluate(js, &images); err != nil {
        return nil, err
    }
    
    return images, nil
}
```

## Debugging Tips

### Enable Verbose Logging

```go
func (s *MySite) GetChapterURLs(ctx context.Context, mangaURL string) (map[string]string, error) {
    log.Printf("[MySite] Fetching chapters from: %s", mangaURL)
    
    chapters, err := s.fetchChapters(ctx, mangaURL)
    
    log.Printf("[MySite] Found %d chapters", len(chapters))
    for filename, url := range chapters {
        log.Printf("[MySite]   %s -> %s", filename, url)
    }
    
    return chapters, err
}
```

### Check HTML Structure

```go
func (s *MySite) GetChapterURLs(ctx context.Context, mangaURL string) (map[string]string, error) {
    // Fetch HTML
    html, err := downloader.FetchWithBrowser(ctx, mangaURL, "mysite.com", false, "")
    if err != nil {
        return nil, err
    }
    
    // Print first 500 chars for inspection
    preview := html
    if len(preview) > 500 {
        preview = preview[:500]
    }
    log.Printf("[MySite] HTML preview: %s", preview)
    
    // Continue with parsing...
}
```