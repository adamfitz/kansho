package downloader

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"time"

	"kansho/cf"

	"github.com/PuerkitoBio/goquery"
)

// FetchChapterURLs fetches chapter URLs using site's extraction method
func FetchChapterURLs(ctx context.Context, mangaURL string, site SitePlugin) (map[string]string, error) {
	chapterMap, err := extractChapters(ctx, mangaURL, site)
	if err == nil {
		return chapterMap, nil
	}

	var cfErr *cf.CfChallengeError
	if errors.As(err, &cfErr) {
		log.Printf("[Downloader] ⚠️ CF challenge detected - returning error to queue")
		return nil, cfErr
	}

	maxRetries := 3
	lastErr := err

	for attempt := 1; attempt < maxRetries; attempt++ {
		backoff := time.Duration(math.Pow(2, float64(attempt))) * time.Second
		log.Printf("[Downloader] Retry %d/%d for chapter list after %v", attempt+1, maxRetries, backoff)
		time.Sleep(backoff)

		chapterMap, err := extractChapters(ctx, mangaURL, site)
		if err == nil {
			log.Printf("[Downloader] ✓ Success fetching chapters after %d retries", attempt+1)
			return chapterMap, nil
		}

		if errors.As(err, &cfErr) {
			log.Printf("[Downloader] ⚠️ CF challenge detected - returning error to queue")
			return nil, cfErr
		}

		lastErr = err
		log.Printf("[Downloader] Failed to fetch chapters (attempt %d/%d): %v", attempt+1, maxRetries, err)
	}

	return nil, fmt.Errorf("failed after %d retries: %w", maxRetries, lastErr)
}

// FetchChapterImages fetches image URLs using site's extraction method
func FetchChapterImages(ctx context.Context, chapterURL string, site SitePlugin) ([]string, error) {
	imageURLs, err := extractImages(ctx, chapterURL, site)
	if err == nil {
		return imageURLs, nil
	}

	var cfErr *cf.CfChallengeError
	if errors.As(err, &cfErr) {
		log.Printf("[Downloader] ⚠️ CF challenge detected - returning error to queue")
		return nil, cfErr
	}

	maxRetries := 3
	lastErr := err

	for attempt := 1; attempt < maxRetries; attempt++ {
		backoff := time.Duration(math.Pow(2, float64(attempt))) * time.Second
		log.Printf("[Downloader] Retry %d/%d for chapter images after %v", attempt+1, maxRetries, backoff)
		time.Sleep(backoff)

		imageURLs, err := extractImages(ctx, chapterURL, site)
		if err == nil {
			log.Printf("[Downloader] ✓ Success fetching images after %d retries", attempt+1)
			return imageURLs, nil
		}

		if errors.As(err, &cfErr) {
			log.Printf("[Downloader] ⚠️ CF challenge detected - returning error to queue")
			return nil, cfErr
		}

		lastErr = err
		log.Printf("[Downloader] Failed to fetch images (attempt %d/%d): %v", attempt+1, maxRetries, err)
	}

	return nil, fmt.Errorf("failed after %d retries: %w", maxRetries, lastErr)
}

// extractChapters uses the site's extraction method to get chapters
func extractChapters(ctx context.Context, mangaURL string, site SitePlugin) (map[string]string, error) {
	method := site.GetChapterExtractionMethod()

	switch method.Type {
	case "javascript":
		return extractChaptersWithJS(ctx, mangaURL, site, method)
	case "html_selector":
		return extractChaptersWithSelector(ctx, mangaURL, site, method)
	case "custom":
		return extractChaptersCustom(ctx, mangaURL, site, method)
	case "api":
		return extractChaptersWithAPI(ctx, mangaURL, site, method)
	default:
		return nil, fmt.Errorf("unknown extraction type: %s", method.Type)
	}
}

// extractImages uses the site's extraction method to get images
func extractImages(ctx context.Context, chapterURL string, site SitePlugin) ([]string, error) {
	method := site.GetImageExtractionMethod()

	switch method.Type {
	case "javascript":
		return extractImagesWithJS(ctx, chapterURL, site, method)
	case "html_selector":
		return extractImagesWithSelector(ctx, chapterURL, site, method)
	case "custom":
		return extractImagesCustom(ctx, chapterURL, site, method)
	case "api":
		return extractImagesWithAPI(ctx, chapterURL, site, method)
	default:
		return nil, fmt.Errorf("unknown extraction type: %s", method.Type)
	}
}

// extractChaptersWithJS uses JavaScript evaluation
func extractChaptersWithJS(ctx context.Context, mangaURL string, site SitePlugin, method *ChapterExtractionMethod) (map[string]string, error) {
	jsCtx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	var rawData []map[string]string

	session, err := NewBrowserSession(jsCtx, site.GetDomain(), site.NeedsCFBypass())
	if err != nil {
		return nil, err
	}
	defer session.Close()

	if err := session.NavigateAndEvaluate(mangaURL, method.WaitSelector, method.JavaScript, &rawData); err != nil {
		return nil, fmt.Errorf("navigation and JavaScript evaluation failed: %w", err)
	}

	result := make(map[string]string)
	for _, data := range rawData {
		filename := site.NormalizeChapterFilename(data)
		url := site.NormalizeChapterURL(data["url"], mangaURL)
		result[filename] = url
	}

	return result, nil
}

// extractChaptersWithSelector uses HTML parsing
func extractChaptersWithSelector(ctx context.Context, mangaURL string, site SitePlugin, method *ChapterExtractionMethod) (map[string]string, error) {
	fetchCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	html, err := FetchHTML(fetchCtx, mangaURL, site.GetDomain(), site.NeedsCFBypass(), method.WaitSelector)
	if err != nil {
		return nil, err
	}

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader([]byte(html)))
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	result := make(map[string]string)
	doc.Find(method.Selector).Each(func(i int, s *goquery.Selection) {
		href, exists := s.Attr("href")
		if !exists {
			return
		}

		text := s.Text()
		data := map[string]string{
			"url":  href,
			"text": text,
		}

		filename := site.NormalizeChapterFilename(data)
		url := site.NormalizeChapterURL(href, mangaURL)
		result[filename] = url
	})

	return result, nil
}

// extractChaptersCustom uses site's custom parser
func extractChaptersCustom(ctx context.Context, mangaURL string, site SitePlugin, method *ChapterExtractionMethod) (map[string]string, error) {
	if method.CustomParser == nil {
		return nil, fmt.Errorf("custom parser not provided")
	}

	var dbg *Debugger
	if d, ok := site.(DebugSite); ok {
		dbg = d.Debugger()
	}

	exec, err := NewRequestExecutor(mangaURL, site.NeedsCFBypass(), dbg)
	if err != nil {
		return nil, fmt.Errorf("failed to create request executor: %w", err)
	}

	fetchCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	html, err := exec.FetchHTML(fetchCtx, mangaURL, method.WaitSelector)
	if err != nil {
		return nil, fmt.Errorf("failed to get HTML via executor: %w", err)
	}

	return method.CustomParser(html)
}

// extractImagesWithJS uses JavaScript evaluation
func extractImagesWithJS(ctx context.Context, chapterURL string, site SitePlugin, method *ImageExtractionMethod) ([]string, error) {
	jsCtx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	var imageURLs []string

	session, err := NewBrowserSession(jsCtx, site.GetDomain(), site.NeedsCFBypass())
	if err != nil {
		return nil, err
	}
	defer session.Close()

	if err := session.NavigateAndEvaluate(chapterURL, method.WaitSelector, method.JavaScript, &imageURLs); err != nil {
		return nil, fmt.Errorf("navigation and JavaScript evaluation failed: %w", err)
	}

	return imageURLs, nil
}

// extractImagesWithSelector uses HTML parsing
func extractImagesWithSelector(ctx context.Context, chapterURL string, site SitePlugin, method *ImageExtractionMethod) ([]string, error) {
	fetchCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	html, err := FetchHTML(fetchCtx, chapterURL, site.GetDomain(), site.NeedsCFBypass(), method.WaitSelector)
	if err != nil {
		return nil, err
	}

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader([]byte(html)))
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	var imageURLs []string
	doc.Find(method.Selector).Each(func(i int, s *goquery.Selection) {
		src := s.AttrOr(method.Attribute, "")
		if src != "" {
			imageURLs = append(imageURLs, src)
		}
	})

	return imageURLs, nil
}

// extractImagesCustom uses site's custom parser.
// If WaitSelector is set, it forces browser rendering via FetchHTMLBatched (chromedp),
// which batches navigate + WaitReady + OuterHTML into a single chromedp.Run call.
// This avoids the sequential context cancellation issue in FetchHTML where Navigate
// exhausts the parent context before GetHTML can run.
// If WaitSelector is empty, it uses RequestExecutor (HTTP first, browser fallback)
// for efficiency on sites that serve images in SSR HTML.
func extractImagesCustom(ctx context.Context, chapterURL string, site SitePlugin, method *ImageExtractionMethod) ([]string, error) {
	if method.CustomParser == nil {
		return nil, fmt.Errorf("custom parser not provided")
	}

	var html string
	var err error

	if method.WaitSelector != "" {
		// WaitSelector set: force browser rendering so React/Next.js content is present.
		// FetchHTMLBatched runs navigate+wait+getHTML in one chromedp.Run call, avoiding
		// context cancellation between sequential Navigate and GetHTML calls.
		// The site's Debugger is passed through so HTML can be saved to disk when
		// SaveHTML is enabled in the site's Debugger() method.
		var dbg *Debugger
		if d, ok := site.(DebugSite); ok {
			dbg = d.Debugger()
		}
		html, err = FetchHTMLBatched(ctx, chapterURL, site.GetDomain(), site.NeedsCFBypass(), dbg)
		if err != nil {
			return nil, fmt.Errorf("failed to get rendered HTML via browser: %w", err)
		}
	} else {
		// No WaitSelector: use RequestExecutor (HTTP first, browser fallback)
		var dbg *Debugger
		if d, ok := site.(DebugSite); ok {
			dbg = d.Debugger()
		}

		exec, err := NewRequestExecutor(chapterURL, site.NeedsCFBypass(), dbg)
		if err != nil {
			return nil, fmt.Errorf("failed to create request executor: %w", err)
		}

		fetchCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		html, err = exec.FetchHTML(fetchCtx, chapterURL, "")
		if err != nil {
			return nil, fmt.Errorf("failed to get HTML via executor: %w", err)
		}
	}

	return method.CustomParser(html)
}

// extractChaptersWithAPI uses API-based extraction
func extractChaptersWithAPI(ctx context.Context, mangaURL string, site SitePlugin, method *ChapterExtractionMethod) (map[string]string, error) {
	if method.APIFunc == nil {
		return nil, fmt.Errorf("API function not provided")
	}

	client, err := NewAPIClient(site.GetDomain(), site.NeedsCFBypass())
	if err != nil {
		return nil, fmt.Errorf("failed to create API client: %w", err)
	}

	rawData, err := method.APIFunc(mangaURL, client)
	if err != nil {
		return nil, err
	}

	result := make(map[string]string)
	for _, data := range rawData {
		filename := site.NormalizeChapterFilename(data)
		url := site.NormalizeChapterURL(data["url"], mangaURL)

		if existingURL, exists := result[filename]; exists {
			log.Printf("[Downloader:API] WARNING: Duplicate chapter %s found (existing: %s, new: %s) - keeping first",
				filename, existingURL, url)
			continue
		}

		result[filename] = url
	}

	return result, nil
}

// extractImagesWithAPI uses API-based extraction
func extractImagesWithAPI(ctx context.Context, chapterURL string, site SitePlugin, method *ImageExtractionMethod) ([]string, error) {
	if method.APIFunc == nil {
		return nil, fmt.Errorf("API function not provided")
	}

	client, err := NewAPIClient(site.GetDomain(), site.NeedsCFBypass())
	if err != nil {
		return nil, fmt.Errorf("failed to create API client: %w", err)
	}

	chapterData := map[string]string{
		"url": chapterURL,
	}

	imageURLs, err := method.APIFunc(chapterURL, chapterData, client)
	if err != nil {
		return nil, err
	}

	return imageURLs, nil
}
