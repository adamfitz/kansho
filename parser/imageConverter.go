package parser

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/gif"
	"image/png"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"kansho/cf"

	"github.com/disintegration/imaging"
	"github.com/gocolly/colly"
	"golang.org/x/image/webp"
)

// detectImageFormat reads the magic bytes and returns the current image format string
func detectImageFormat(data []byte) (string, error) {
	if len(data) < 12 {
		return "", errors.New("data too short to determine format")
	}

	if data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return "jpeg", nil
	}
	if data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 {
		return "png", nil
	}
	if string(data[0:6]) == "GIF87a" || string(data[0:6]) == "GIF89a" {
		return "gif", nil
	}
	if string(data[0:4]) == "RIFF" && len(data) >= 12 && string(data[8:12]) == "WEBP" {
		return "webp", nil
	}

	return "", errors.New("unknown image format")
}

// ConvertImageToJPEG converts image bytes to JPEG and saves to outputPath
// If already JPEG, saves directly without re-encoding
func ConvertImageToJPEG(imgBytes []byte, outputPath string) error {
	if len(imgBytes) == 0 {
		return errors.New("empty image data")
	}

	format, err := detectImageFormat(imgBytes)
	if err != nil {
		return err
	}

	// If already JPEG, just save raw bytes directly (no conversion needed)
	if format == "jpeg" {
		return saveRawBytes(imgBytes, outputPath)
	}

	// Decode the image based on format
	var img image.Image
	reader := bytes.NewReader(imgBytes)

	switch format {
	case "png":
		img, err = png.Decode(reader)
	case "gif":
		img, err = gif.Decode(reader)
	case "webp":
		img, err = webp.Decode(reader)
	default:
		return errors.New("unsupported image format: " + format)
	}

	if err != nil {
		return errors.New("failed to decode " + format + " image: " + err.Error())
	}

	// Save as JPEG with quality 90
	return imaging.Save(img, outputPath, imaging.JPEGQuality(90))
}

// downloadAndConvertToJPGWithRetry downloads with retry logic
func downloadAndConvertToJPGWithRetry(imageURL, targetDir string, maxRetries int) error {
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			log.Printf("Retry attempt %d/%d for: %s", attempt, maxRetries, imageURL)
		}

		err := downloadAndConvertToJPG(imageURL, targetDir)
		if err == nil {
			return nil
		}

		lastErr = err
		log.Printf("Download/conversion failed (attempt %d/%d): %v", attempt+1, maxRetries+1, err)
	}

	return lastErr
}

// downloadAndConvertToJPG downloads an image from imageURL, converts to JPG if needed, and saves it inside targetDir
func downloadAndConvertToJPG(imageURL, targetDir string) error {
	resp, err := http.Get(imageURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return errors.New("bad response status: " + resp.Status)
	}

	imgBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if len(imgBytes) == 0 {
		return errors.New("empty response body")
	}

	base := filepath.Base(imageURL)
	ext := strings.ToLower(filepath.Ext(base))
	name := strings.TrimSuffix(base, ext)

	// pad the image filename to 3 digits
	paddedFileName := padFileName(name + ".jpg")

	// join the padded dir / filename back together
	outputFile := filepath.Join(targetDir, paddedFileName)

	// Convert and save
	return ConvertImageToJPEG(imgBytes, outputFile)
}

// DownloadAndConvertToJPG is the public wrapper with retry logic
func DownloadAndConvertToJPG(imageURL, targetDir string) error {
	return downloadAndConvertToJPGWithRetry(imageURL, targetDir, 3)
}

// DownloadConvertToJPGRename downloads an image, converts to JPEG, and saves it.
// Uses the provided context for cancellation support.
func DownloadConvertToJPGRename(ctx context.Context, filename, imageURL, targetDir string) error {
	var lastErr error
	maxRetries := 3

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			log.Printf("Retry attempt %d/%d for: %s", attempt, maxRetries, imageURL)
		}

		err := downloadConvertToJPGRenameCtx(ctx, filename, imageURL, targetDir)
		if err == nil {
			return nil
		}

		lastErr = err
		log.Printf("Download/conversion failed (attempt %d/%d): %v", attempt+1, maxRetries+1, err)
	}

	return lastErr
}

// DownloadConvertToJPGRenameWithReferer downloads an image with a Referer and
// browser-like User-Agent header, converts it to JPEG, and saves it into
// targetDir. Some CDNs (e.g. comix.to) refuse image requests that do not carry
// a Referer from the reading page. Uses a shared keep-alive client so a batch
// of images reuses connections instead of opening fresh ones per image.
func DownloadConvertToJPGRenameWithReferer(ctx context.Context, filename, imageURL, targetDir, referer string) error {
	var lastErr error
	maxRetries := 3

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			log.Printf("Retry attempt %d/%d for: %s", attempt, maxRetries, imageURL)
		}

		err := downloadConvertToJPGRenameWithRefererCtx(ctx, filename, imageURL, targetDir, referer)
		if err == nil {
			return nil
		}

		lastErr = err
		log.Printf("Download/conversion failed (attempt %d/%d): %v", attempt+1, maxRetries+1, err)
	}

	return lastErr
}

// downloadConvertToJPGRenameWithRefererCtx is the context-aware internal
// function without retry logic.
func downloadConvertToJPGRenameWithRefererCtx(ctx context.Context, filename, imageURL, targetDir, referer string) error {
	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "GET", imageURL, nil)
	if err != nil {
		return err
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/143.0.0.0 Safari/537.36")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}

	resp, err := sharedRefererClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return errors.New("bad response status: " + resp.Status)
	}

	imgBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if len(imgBytes) == 0 {
		return errors.New("empty response body")
	}

	// pad the image filename to 3 digits
	paddedFileName := padFileName(filename + ".jpg")

	// join the padded dir / filename back together
	outputFile := filepath.Join(targetDir, paddedFileName)

	// Convert and save
	return ConvertImageToJPEG(imgBytes, outputFile)
}

// sharedRefererClient returns a single keep-alive HTTP client for the
// Referer-based image downloads. Reusing the transport lets a batch of images
// share connections, avoiding Cloudflare throttling of fresh handshakes.
func sharedRefererClient() *http.Client {
	refererClientOnce.Do(func() {
		refererClient = &http.Client{
			Transport: &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				ForceAttemptHTTP2:     true,
				MaxIdleConns:          10,
				MaxIdleConnsPerHost:   4,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   15 * time.Second,
				ResponseHeaderTimeout: 30 * time.Second,
			},
		}
	})
	return refererClient
}

var (
	refererClientOnce sync.Once
	refererClient     *http.Client
)

// downloadConvertToJPGRenameCtx is the context-aware internal function without retry
func downloadConvertToJPGRenameCtx(ctx context.Context, filename, imageURL, targetDir string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", imageURL, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return errors.New("bad response status: " + resp.Status)
	}

	imgBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if len(imgBytes) == 0 {
		return errors.New("empty response body")
	}

	// pad the image filename to 3 digits
	paddedFileName := padFileName(filename + ".jpg")

	// join the padded dir / filename back together
	outputFile := filepath.Join(targetDir, paddedFileName)

	// Convert and save
	return ConvertImageToJPEG(imgBytes, outputFile)
}

// saveRawBytes saves bytes directly to file without conversion
func saveRawBytes(data []byte, outputPath string) error {
	return os.WriteFile(outputPath, data, 0644)
}

// DownloadConvertToJPGRenameCf downloads an image using Cloudflare bypass,
// converts it to JPEG if needed, and saves it with the specified filename.
// Uses the provided context for cancellation support.
// This function uses Colly with CF bypass data for sites that require it.
//
// Parameters:
//   - ctx: Context for cancellation support (checked before and after Colly Visit)
//   - filename: Base filename without extension (e.g., "1", "2", "3")
//   - imageURL: Full URL of the image to download
//   - targetDir: Directory where the image should be saved
//   - domain: Domain for which to load CF bypass data (e.g., "manhuaus.com")
//
// Returns:
//   - error: Any error encountered during download/conversion, nil on success
//
// The function will:
// 1. Create a Colly collector with CF bypass applied
// 2. Download the image using the collector
// 3. Convert to JPEG if needed (reuses ConvertImageToJPEG)
// 4. Save with padded filename (reuses padFileName)
func DownloadConvertToJPGRenameCf(ctx context.Context, filename, imageURL, targetDir, domain string) error {
	var lastErr error
	maxRetries := 3

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			log.Printf("Retry attempt %d/%d for: %s", attempt, maxRetries, imageURL)
		}

		err := downloadConvertToJPGRenameCfCtx(ctx, filename, imageURL, targetDir, domain)
		if err == nil {
			return nil
		}

		lastErr = err
		log.Printf("Download/conversion failed (attempt %d/%d): %v", attempt+1, maxRetries+1, err)
	}

	return lastErr
}

// downloadConvertToJPGRenameCfCtx is the context-aware internal function without retry logic
func downloadConvertToJPGRenameCfCtx(ctx context.Context, filename, imageURL, targetDir, domain string) error {
	// Create a new Colly collector for this download with extended timeout for large images
	c := colly.NewCollector(
		colly.UserAgent("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/143.0.0.0 Safari/537.36"),
		colly.MaxBodySize(0), // CRITICAL: Remove body size limit (default is 10MB which truncates large images)
	)

	// Set longer timeout for large image downloads (60 seconds to handle slow connections)
	c.SetRequestTimeout(60 * time.Second)

	// Load CF bypass data for the provided domain
	bypassData, err := cf.LoadFromFile(domain)
	if err != nil {
		log.Printf("No bypass data found for domain: %s", domain)
		// Continue anyway - maybe the site doesn't need bypass for images
	} else {
		// CRITICAL FIX: Ensure cookie domain has dot prefix for subdomain support
		if bypassData.CfClearanceStruct != nil {
			originalDomain := bypassData.CfClearanceStruct.Domain
			if originalDomain != "" && !strings.HasPrefix(originalDomain, ".") {
				bypassData.CfClearanceStruct.Domain = "." + originalDomain
				log.Printf("Modified cookie domain from '%s' to '%s' for subdomain support", originalDomain, bypassData.CfClearanceStruct.Domain)
			}

			// Manually set the cookie with the modified domain
			httpCookie := &http.Cookie{
				Name:     bypassData.CfClearanceStruct.Name,
				Value:    bypassData.CfClearanceStruct.Value,
				Path:     bypassData.CfClearanceStruct.Path,
				Domain:   bypassData.CfClearanceStruct.Domain, // This now has the dot prefix
				Secure:   bypassData.CfClearanceStruct.Secure,
				HttpOnly: bypassData.CfClearanceStruct.HttpOnly,
			}

			if bypassData.CfClearanceStruct.Expires != nil {
				httpCookie.Expires = *bypassData.CfClearanceStruct.Expires
			}

			c.SetCookies(imageURL, []*http.Cookie{httpCookie})

			// Set User-Agent
			c.UserAgent = bypassData.Entropy.UserAgent

			log.Printf("✓ Applied CF bypass with cookie domain: %s for URL: %s", bypassData.CfClearanceStruct.Domain, imageURL)
		}
	}

	// Variables to capture response
	var imgBytes []byte
	var downloadErr error

	// Handle successful response
	c.OnResponse(func(r *colly.Response) {
		if r.StatusCode != 200 {
			statusText := http.StatusText(r.StatusCode)
			downloadErr = errors.New("bad response status: " + string(rune(r.StatusCode)) + " " + statusText)
			log.Printf("Image download error: status=%d, url=%s", r.StatusCode, imageURL)
			return
		}

		// Validate we got the full response by checking Content-Length if available
		contentLength := r.Headers.Get("Content-Length")
		if contentLength != "" {
			expectedLen := int64(0)
			fmt.Sscanf(contentLength, "%d", &expectedLen)
			actualLen := int64(len(r.Body))

			if expectedLen > 0 && actualLen < expectedLen {
				downloadErr = errors.New(fmt.Sprintf("incomplete download: got %d bytes, expected %d bytes", actualLen, expectedLen))
				log.Printf("Image download incomplete: got %d/%d bytes, url=%s", actualLen, expectedLen, imageURL)
				return
			}

			log.Printf("Downloaded %d bytes (Content-Length: %d) for: %s", actualLen, expectedLen, imageURL)
		} else {
			log.Printf("Downloaded %d bytes (no Content-Length header) for: %s", len(r.Body), imageURL)
		}

		imgBytes = r.Body
	})

	// Handle errors
	c.OnError(func(r *colly.Response, err error) {
		if r != nil {
			statusText := http.StatusText(r.StatusCode)
			downloadErr = errors.New("request failed: " + string(rune(r.StatusCode)) + " " + statusText + " - " + err.Error())
			log.Printf("Image download error: status=%d, error=%v, url=%s", r.StatusCode, err, imageURL)
		} else {
			downloadErr = errors.New("request failed: " + err.Error())
			log.Printf("Image download error: %v, url=%s", err, imageURL)
		}
	})

	// Check for cancellation before starting the Colly request
	select {
	case <-ctx.Done():
		log.Printf("Image download cancelled before Visit: %s", imageURL)
		return ctx.Err()
	default:
	}

	// Visit the image URL (Colly doesn't natively support context propagation,
	// but has a 60s request timeout set above as a safety net)
	visitErr := c.Visit(imageURL)
	if visitErr != nil {
		log.Printf("Failed to visit image URL: %v, url=%s", visitErr, imageURL)
		return errors.New("failed to visit URL: " + visitErr.Error())
	}

	// Check for download errors
	if downloadErr != nil {
		return downloadErr
	}

	// Check for cancellation before converting/saving
	select {
	case <-ctx.Done():
		log.Printf("Image cancelled after download: %s", imageURL)
		return ctx.Err()
	default:
	}

	// Check for empty response
	if len(imgBytes) == 0 {
		log.Printf("Empty response body for image: %s", imageURL)
		return errors.New("empty response body")
	}

	// Pad the image filename to 3 digits (reuse existing helper)
	paddedFileName := padFileName(filename + ".jpg")

	// Join the padded dir / filename back together
	outputFile := filepath.Join(targetDir, paddedFileName)

	// Convert and save (reuse existing function)
	convertErr := ConvertImageToJPEG(imgBytes, outputFile)
	if convertErr != nil {
		log.Printf("Failed to convert/save image: %v, url=%s, output=%s", convertErr, imageURL, outputFile)
		return convertErr
	}

	return nil
}

// DownloadConvertToJPGRenameCfWithCollector allows passing a pre-configured Colly collector.
// This is useful when you want to share a single collector (with CF bypass already applied)
// across multiple image downloads for better performance.
//
// Parameters:
//   - c: Pre-configured Colly collector (should already have CF bypass applied)
//   - filename: Base filename without extension (e.g., "1", "2", "3")
//   - imageURL: Full URL of the image to download
//   - targetDir: Directory where the image should be saved
//
// Returns:
//   - error: Any error encountered during download/conversion, nil on success
func DownloadConvertToJPGRenameCfWithCollector(c *colly.Collector, filename, imageURL, targetDir string) error {
	// Variables to capture response
	var imgBytes []byte
	var downloadErr error

	// Create a new collector that clones the settings
	// This prevents callback conflicts when reusing the same collector
	imgCollector := c.Clone()

	// Handle successful response
	imgCollector.OnResponse(func(r *colly.Response) {
		if r.StatusCode != 200 {
			statusText := http.StatusText(r.StatusCode)
			downloadErr = errors.New("bad response status: " + string(rune(r.StatusCode)) + " " + statusText)
			log.Printf("Image download error: status=%d, url=%s", r.StatusCode, imageURL)
			return
		}
		imgBytes = r.Body
	})

	// Handle errors
	imgCollector.OnError(func(r *colly.Response, err error) {
		if r != nil {
			statusText := http.StatusText(r.StatusCode)
			downloadErr = errors.New("request failed: " + string(rune(r.StatusCode)) + " " + statusText + " - " + err.Error())
			log.Printf("Image download error: status=%d, error=%v, url=%s", r.StatusCode, err, imageURL)
		} else {
			downloadErr = errors.New("request failed: " + err.Error())
			log.Printf("Image download error: %v, url=%s", err, imageURL)
		}
	})

	// Visit the image URL
	visitErr := imgCollector.Visit(imageURL)
	if visitErr != nil {
		log.Printf("Failed to visit image URL: %v, url=%s", visitErr, imageURL)
		return errors.New("failed to visit URL: " + visitErr.Error())
	}

	// Check for download errors
	if downloadErr != nil {
		return downloadErr
	}

	// Check for empty response
	if len(imgBytes) == 0 {
		log.Printf("Empty response body for image: %s", imageURL)
		return errors.New("empty response body")
	}

	// Pad the image filename to 3 digits (reuse existing helper)
	paddedFileName := padFileName(filename + ".jpg")

	// Join the padded dir / filename back together
	outputFile := filepath.Join(targetDir, paddedFileName)

	// Convert and save (reuse existing function)
	convertErr := ConvertImageToJPEG(imgBytes, outputFile)
	if convertErr != nil {
		log.Printf("Failed to convert/save image: %v, url=%s, output=%s", convertErr, imageURL, outputFile)
		return convertErr
	}

	return nil
}

// -------------------------
// FlameComics shared-client image download
// -------------------------

// StalledError reports that a server accepted a connection but sent no body
// bytes for the no-data timeout. Cloudflare-protected CDNs do this when they
// throttle a burst of fresh connections.
type StalledError struct {
	URL string
}

func (e *StalledError) Error() string {
	return fmt.Sprintf("stalled (no data received in %d seconds): %s", noDataTimeout/time.Second, e.URL)
}

// noDataTimeout is how long a download may receive zero bytes before it is
// aborted as stalled and retried.
const noDataTimeout = 20 * time.Second

var (
	flameComicsClientOnce sync.Once
	flameComicsClient     *http.Client
)

// sharedFlameComicsClient returns a single keep-alive HTTP client. Reusing the
// transport means every image of a batch reuses the TCP/TLS connection instead
// of opening a fresh one (a burst of fresh handshakes to the Cloudflare CDN
// gets throttled, causing silent stalls).
func sharedFlameComicsClient() *http.Client {
	flameComicsClientOnce.Do(func() {
		flameComicsClient = &http.Client{
			Transport: &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				ForceAttemptHTTP2:     true,
				MaxIdleConns:          10,
				MaxIdleConnsPerHost:   4,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   15 * time.Second,
				ResponseHeaderTimeout: 30 * time.Second,
			},
		}
	})
	return flameComicsClient
}

var (
	flameComicsBypassMu    sync.Mutex
	flameComicsBypassCache = map[string]*cf.BypassData{}
)

// flameComicsBypassData loads the CF bypass cookies once per session per domain.
func flameComicsBypassData(domain string) (*cf.BypassData, error) {
	flameComicsBypassMu.Lock()
	defer flameComicsBypassMu.Unlock()

	if data, ok := flameComicsBypassCache[domain]; ok {
		return data, nil
	}

	data, err := cf.LoadFromFile(domain)
	if err != nil {
		return nil, err
	}

	flameComicsBypassCache[domain] = data
	return data, nil
}

// readBodyWithStallDetect reads the response body, aborting with a StalledError
// if no data arrives for noDataTimeout. Returns the full body on success.
func readBodyWithStallDetect(ctx context.Context, cancel context.CancelFunc, body io.Reader, imageURL string) ([]byte, error) {
	var buf bytes.Buffer
	buf.Grow(1024 * 1024)
	chunk := make([]byte, 32*1024)

	readWithWatchdog := func() (int, error) {
		type result struct {
			n   int
			err error
		}
		resCh := make(chan result, 1)
		go func() {
			n, err := body.Read(chunk)
			resCh <- result{n, err}
		}()
		select {
		case r := <-resCh:
			return r.n, r.err
		case <-time.After(noDataTimeout):
			log.Printf("⚠️ STALLED: no data received from %s for %d seconds, aborting", imageURL, int(noDataTimeout/time.Second))
			cancel()
			return 0, &StalledError{URL: imageURL}
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}

	for {
		n, err := readWithWatchdog()
		if n > 0 {
			buf.Write(chunk[:n])
		}
		if err == io.EOF {
			return buf.Bytes(), nil
		}
		if err != nil {
			return nil, err
		}
	}
}

// DownloadFlameComicsImage downloads a FlameComics image using the shared
// keep-alive client and CF bypass cookies, converts it to JPEG, and saves it
// into targetDir. It is FlameComics-only: every other site keeps the legacy
// per-image download paths.
func DownloadFlameComicsImage(ctx context.Context, filename, imageURL, targetDir, domain string) error {
	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "GET", imageURL, nil)
	if err != nil {
		return err
	}

	bypassData, err := flameComicsBypassData(domain)
	if err != nil {
		log.Printf("[FlameComics] No CF bypass data found for domain: %s", domain)
	} else {
		req.Header.Set("User-Agent", bypassData.Entropy.UserAgent)

		if bypassData.CfClearanceStruct != nil {
			cs := bypassData.CfClearanceStruct

			// Dot-prefix the cookie domain so it is also sent for the CDN
			// subdomain (e.g. cdn.flamecomics.xyz).
			cookieDomain := cs.Domain
			if cookieDomain != "" && !strings.HasPrefix(cookieDomain, ".") {
				cookieDomain = "." + cookieDomain
			}

			httpCookie := &http.Cookie{
				Name:     cs.Name,
				Value:    cs.Value,
				Path:     cs.Path,
				Domain:   cookieDomain,
				Secure:   cs.Secure,
				HttpOnly: cs.HttpOnly,
			}
			if cs.Expires != nil {
				httpCookie.Expires = *cs.Expires
			}
			req.AddCookie(httpCookie)
			log.Printf("✓ Applied CF bypass with cookie domain: %s for URL: %s", cookieDomain, imageURL)
		}

		for _, ck := range bypassData.AllCookies {
			if ck.Name != "" && ck.Name != "cf_clearance" {
				req.AddCookie(&http.Cookie{Name: ck.Name, Value: ck.Value, Path: ck.Path})
			}
		}
	}

	resp, err := sharedFlameComicsClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New("bad response status: " + resp.Status)
	}

	imgBytes, err := readBodyWithStallDetect(reqCtx, cancel, resp.Body, imageURL)
	if err != nil {
		return err
	}

	if len(imgBytes) == 0 {
		return errors.New("empty response body")
	}

	outputFile := filepath.Join(targetDir, padFileName(filename+".jpg"))
	return ConvertImageToJPEG(imgBytes, outputFile)
}
