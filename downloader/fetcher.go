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
	// Try to extract chapters
	chapterMap, err := extractChapters(ctx, mangaURL, site)
	if err == nil {
		return chapterMap, nil
	}

	// Check for CF challenge (including wrapped errors) - return immediately to let queue handle it
	var cfErr *cf.CfChallengeError
	if errors.As(err, &cfErr) {
		log.Printf("[Downloader] ⚠️ CF challenge detected - returning error to queue")
		return nil, cfErr
	}

	// For non-CF errors, retry with backoff
	maxRetries := 3
	var lastErr error = err

	for attempt := 1; attempt < maxRetries; attempt++ {
		backoff := time.Duration(math.Pow(2, float64(attempt))) * time.Second
		log.Printf("[Downloader] Retry %d/%d for chapter list after %v", attempt+1, maxRetries, backoff)
		time.Sleep(backoff)

		chapterMap, err := extractChapters(ctx, mangaURL, site)
		if err == nil {
			log.Printf("[Downloader] ✓ Success fetching chapters after %d retries", attempt+1)
			return chapterMap, nil
		}

		// Check for CF challenge on retry (including wrapped errors) - return immediately
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
	// Try to extract images
	imageURLs, err := extractImages(ctx, chapterURL, site)
	if err == nil {
		return imageURLs, nil
	}

	// Check for CF challenge (including wrapped errors) - return immediately to let queue handle it
	var cfErr *cf.CfChallengeError
	if errors.As(err, &cfErr) {
		log.Printf("[Downloader] ⚠️ CF challenge detected - returning error to queue")
		return nil, cfErr
	}

	// For non-CF errors, retry with backoff
	maxRetries := 3
	var lastErr error = err

	for attempt := 1; attempt < maxRetries; attempt++ {
		backoff := time.Duration(math.Pow(2, float64(attempt))) * time.Second
		log.Printf("[Downloader] Retry %d/%d for chapter images after %v", attempt+1, maxRetries, backoff)
		time.Sleep(backoff)

		imageURLs, err := extractImages(ctx, chapterURL, site)
		if err == nil {
			log.Printf("[Downloader] ✓ Success fetching images after %d retries", attempt+1)
			return imageURLs, nil
		}

		// Check for CF challenge on retry (including wrapped errors) - return immediately
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
// CRITICAL: Uses NavigateAndEvaluate to batch all operations in a single chromedp.Run()
// This matches the working CLI version and prevents context cancellation issues
func extractChaptersWithJS(ctx context.Context, mangaURL string, site SitePlugin, method *ChapterExtractionMethod) (map[string]string, error) {
	var rawData []map[string]string

	session, err := NewBrowserSession(ctx, site.GetDomain(), site.NeedsCFBypass())
	if err != nil {
		return nil, err
	}
	defer session.Close()

	// Use NavigateAndEvaluate to batch all operations in a single chromedp.Run()
	// This matches the working CLI version and prevents context cancellation issues
	if err := session.NavigateAndEvaluate(mangaURL, method.WaitSelector, method.JavaScript, &rawData); err != nil {
		return nil, fmt.Errorf("navigation and JavaScript evaluation failed: %w", err)
	}

	// Convert raw data to chapter map using site's normalization
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
	html, err := FetchHTML(ctx, mangaURL, site.GetDomain(), site.NeedsCFBypass(), method.WaitSelector)
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

	html, err := FetchHTML(ctx, mangaURL, site.GetDomain(), site.NeedsCFBypass(), method.WaitSelector)
	if err != nil {
		return nil, err
	}

	return method.CustomParser(html)
}

// extractImagesWithJS uses JavaScript evaluation
// CRITICAL: Uses NavigateAndEvaluate to batch all operations in a single chromedp.Run()
func extractImagesWithJS(ctx context.Context, chapterURL string, site SitePlugin, method *ImageExtractionMethod) ([]string, error) {
	var imageURLs []string

	session, err := NewBrowserSession(ctx, site.GetDomain(), site.NeedsCFBypass())
	if err != nil {
		return nil, err
	}
	defer session.Close()

	// Use NavigateAndEvaluate to batch all operations in a single chromedp.Run()
	if err := session.NavigateAndEvaluate(chapterURL, method.WaitSelector, method.JavaScript, &imageURLs); err != nil {
		return nil, fmt.Errorf("navigation and JavaScript evaluation failed: %w", err)
	}

	return imageURLs, nil
}

// extractImagesWithSelector uses HTML parsing
func extractImagesWithSelector(ctx context.Context, chapterURL string, site SitePlugin, method *ImageExtractionMethod) ([]string, error) {
	html, err := FetchHTML(ctx, chapterURL, site.GetDomain(), site.NeedsCFBypass(), method.WaitSelector)
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

// extractImagesCustom uses site's custom parser
func extractImagesCustom(ctx context.Context, chapterURL string, site SitePlugin, method *ImageExtractionMethod) ([]string, error) {
	if method.CustomParser == nil {
		return nil, fmt.Errorf("custom parser not provided")
	}

	html, err := FetchHTML(ctx, chapterURL, site.GetDomain(), site.NeedsCFBypass(), method.WaitSelector)
	if err != nil {
		return nil, err
	}

	return method.CustomParser(html)
}

// extractChaptersWithAPI uses API-based extraction
func extractChaptersWithAPI(ctx context.Context, mangaURL string, site SitePlugin, method *ChapterExtractionMethod) (map[string]string, error) {
	if method.APIFunc == nil {
		return nil, fmt.Errorf("API function not provided")
	}

	// Create API client
	client, err := NewAPIClient(site.GetDomain(), site.NeedsCFBypass())
	if err != nil {
		return nil, fmt.Errorf("failed to create API client: %w", err)
	}

	// Call the site's API function
	rawData, err := method.APIFunc(mangaURL, client)
	if err != nil {
		return nil, err
	}

	// Convert raw data to chapter map using site's normalization
	result := make(map[string]string)
	for _, data := range rawData {
		filename := site.NormalizeChapterFilename(data)
		url := site.NormalizeChapterURL(data["url"], mangaURL)

		// Check for duplicates - keep the first one
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

	// Create API client
	client, err := NewAPIClient(site.GetDomain(), site.NeedsCFBypass())
	if err != nil {
		return nil, fmt.Errorf("failed to create API client: %w", err)
	}

	// For API-based extraction, the chapterURL might actually be chapter metadata encoded as a string
	// We need to parse it back to get the chapter data
	// The site plugin should handle this in their APIFunc
	chapterData := map[string]string{
		"url": chapterURL,
	}

	// Call the site's API function
	imageURLs, err := method.APIFunc(chapterURL, chapterData, client)
	if err != nil {
		return nil, err
	}

	return imageURLs, nil
}
