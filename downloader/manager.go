package downloader

import (
	"context"
	"fmt"
	"log"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"kansho/parser"
)

// Manager orchestrates the entire download process
type Manager struct {
	config *DownloadConfig
	domain string
}

// NewManager creates a new download manager
func NewManager(config *DownloadConfig) *Manager {
	// Extract domain from manga URL
	parsedURL, _ := url.Parse(config.Manga.Url)
	domain := parsedURL.Hostname()

	return &Manager{
		config: config,
		domain: domain,
	}
}

// Download executes the full download workflow
func (m *Manager) Download(ctx context.Context) error {
	manga := m.config.Manga
	site := m.config.Site
	callback := m.config.ProgressCallback

	log.Printf("[Downloader] Starting download for %s from %s", manga.Title, site.GetSiteName())

	// Step 1: Get all chapter URLs from the site
	if callback != nil {
		callback("Fetching chapter list...", 0, 0, 0, 0)
	}

	chapterMap, err := FetchChapterURLs(ctx, manga.Url, site)
	if err != nil {
		return fmt.Errorf("failed to get chapter URLs: %w", err)
	}

	log.Printf("[Downloader] Found %d total chapters", len(chapterMap))

	// Step 2: Get already downloaded chapters
	downloadedChapters, err := parser.LocalChapterList(manga.Location)
	if err != nil {
		return fmt.Errorf("failed to list local chapters: %w", err)
	}

	log.Printf("[Downloader] Found %d already downloaded chapters", len(downloadedChapters))

	totalChaptersFound := len(chapterMap)

	// Step 3: Remove already downloaded chapters
	for _, chapter := range downloadedChapters {
		delete(chapterMap, chapter)
	}

	newChaptersToDownload := len(chapterMap)
	if newChaptersToDownload == 0 {
		log.Printf("[Downloader] No new chapters to download")
		if callback != nil {
			callback("No new chapters to download", 1.0, 0, 0, totalChaptersFound)
		}
		return nil
	}

	log.Printf("[Downloader] %d new chapters to download", newChaptersToDownload)
	if callback != nil {
		callback(fmt.Sprintf("Found %d new chapters to download", newChaptersToDownload), 0, 0, 0, totalChaptersFound)
	}

	// Step 4: Sort chapters
	sortedChapters, err := parser.SortKeys(chapterMap)
	if err != nil {
		return fmt.Errorf("failed to sort chapters: %w", err)
	}

	// Step 5: Download each chapter
	for idx, cbzName := range sortedChapters {
		select {
		case <-ctx.Done():
			log.Printf("[Downloader:%s] Cancelled - stopping download", manga.Title)
			if callback != nil {
				callback("Cancelling...", 0, 0, idx, totalChaptersFound)
			}
			return ctx.Err()
		default:
		}

		chapterURL := chapterMap[cbzName]
		actualChapterNum := extractChapterNumber(cbzName)
		currentDownload := idx + 1
		progress := float64(currentDownload) / float64(newChaptersToDownload)

		if callback != nil {
			callback(
				fmt.Sprintf("Downloading chapter %d of %d", actualChapterNum, totalChaptersFound),
				progress,
				actualChapterNum,
				currentDownload,
				totalChaptersFound,
			)
		}

		log.Printf("[Downloader:%s] Starting chapter download: %d/%d", manga.Title, actualChapterNum, totalChaptersFound)

		// Download this chapter with retry
		err := m.downloadChapterWithRetry(ctx, chapterURL, cbzName, actualChapterNum, currentDownload, totalChaptersFound, newChaptersToDownload, progress)
		if err != nil {
			log.Printf("[Downloader:%s] Failed to download chapter %s: %v", manga.Title, cbzName, err)
			continue
		}

		log.Printf("[Downloader:%s] ✓ Completed chapter %s", manga.Title, cbzName)
	}

	log.Printf("[Downloader] Download complete for %s", manga.Title)
	if callback != nil {
		callback(
			fmt.Sprintf("Download complete! Downloaded %d chapters", newChaptersToDownload),
			1.0,
			0,
			newChaptersToDownload,
			totalChaptersFound,
		)
	}

	return nil
}

// downloadChapterWithRetry downloads a single chapter with retry logic
func (m *Manager) downloadChapterWithRetry(ctx context.Context, chapterURL, cbzName string, actualChapterNum, currentDownload, totalChaptersFound, newChaptersToDownload int, progress float64) error {
	maxRetries := 3
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(math.Pow(2, float64(attempt))) * time.Second

			if cb := m.config.ProgressCallback; cb != nil {
				cb(fmt.Sprintf("Retrying chapter %d in %v (attempt %d/%d)...", actualChapterNum, backoff, attempt+1, maxRetries), progress, actualChapterNum, currentDownload, totalChaptersFound)
			}

			log.Printf("[Downloader:%s] Retry %d/%d after %v", cbzName, attempt+1, maxRetries, backoff)
			if !parser.SleepCtx(ctx, backoff) {
				log.Printf("[Downloader:%s] Retry cancelled during backoff", cbzName)
				return ctx.Err()
			}
		}

		err := m.downloadChapter(ctx, chapterURL, cbzName, actualChapterNum, currentDownload, totalChaptersFound, newChaptersToDownload, progress)
		if err == nil {
			if attempt > 0 {
				log.Printf("[Downloader:%s] ✓ Success after %d retries", cbzName, attempt+1)
			}
			return nil
		}

		lastErr = err
		log.Printf("[Downloader:%s] Failed (attempt %d/%d): %v", cbzName, attempt+1, maxRetries, err)
	}

	return fmt.Errorf("failed after %d retries: %w", maxRetries, lastErr)
}

// downloadChapter handles downloading a single chapter
func (m *Manager) downloadChapter(ctx context.Context, chapterURL, cbzName string, actualChapterNum, currentDownload, totalChaptersFound, newChaptersToDownload int, progress float64) error {
	manga := m.config.Manga
	site := m.config.Site
	callback := m.config.ProgressCallback

	// Create temp directory
	chapterDir := filepath.Join("/tmp", site.GetSiteName(), strings.TrimSuffix(cbzName, ".cbz"))
	if err := os.MkdirAll(chapterDir, 0755); err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(chapterDir)

	var imageURLs []string
	successCount := 0

	// For sites that need CF bypass, use the browser's network stack to
	// download images directly — this bypasses Cloudflare's TLS fingerprint
	// checks that block Go/curl HTTP clients.
	if site.NeedsCFBypass() {
		imgMethod := site.GetImageExtractionMethod()
		if imgMethod.Type == "javascript" {
			log.Printf("[Downloader:%s] Trying browser-based download for CF-bypass site", cbzName)

			browserCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
			defer cancel()

			session, err := NewBrowserSession(browserCtx, DomainFromURL(chapterURL, site.GetDomain()), true)
			if err != nil {
				log.Printf("[Downloader:%s] Failed to create browser session, falling back to HTTP: %v", cbzName, err)
			} else {
				chapterImages, dlErr := session.DownloadChapterImages(
					chapterURL,
					imgMethod.WaitSelector,
					imgMethod.JavaScript,
					"",
				)
				session.Close()

				if dlErr != nil {
					log.Printf("[Downloader:%s] Browser download failed, falling back to HTTP: %v", cbzName, dlErr)
				} else if len(chapterImages.Data) == 0 {
					log.Printf("[Downloader:%s] Browser returned 0 images, falling back to HTTP", cbzName)
				} else {
					log.Printf("[Downloader:%s] Browser downloaded %d images", cbzName, len(chapterImages.Data))

					for id, imgURL := range chapterImages.URLs {
						data, ok := chapterImages.Data[imgURL]
						if !ok {
							log.Printf("[Downloader:%s] Image URL not in browser results: %s", cbzName, imgURL)
							continue
						}
						filename := fmt.Sprintf("%03d", id+1)
						ext := guessExtension(data)
						if err := os.WriteFile(filepath.Join(chapterDir, filename+"."+ext), data, 0644); err != nil {
							log.Printf("[Downloader:%s] Failed to save image %s: %v", cbzName, filename, err)
							continue
						}
						successCount++
					}

					imageURLs = chapterImages.URLs
					log.Printf("[Downloader:%s] Downloaded %d/%d images from browser", cbzName, successCount, len(chapterImages.Data))
				}
			}
		}
	}

	// Fallback to standard flow for non-CF sites or non-JS extraction methods
	if successCount == 0 {
		var err error
		imageURLs, err = FetchChapterImages(ctx, chapterURL, site)
		if err != nil {
			return fmt.Errorf("failed to get chapter images: %w", err)
		}

		if len(imageURLs) == 0 {
			return fmt.Errorf("no images found")
		}

		log.Printf("[Downloader:%s] Found %d images", cbzName, len(imageURLs))

		rateLimiter := parser.NewRateLimiter(1500 * time.Millisecond)
		defer rateLimiter.Stop()

		for imgIdx, imgURL := range imageURLs {
			log.Printf("[Downloader:%s] Downloading image %d/%d", cbzName, imgIdx+1, len(imageURLs))
			select {
			case <-ctx.Done():
				log.Printf("[Downloader:%s] Cancelled during image download", cbzName)
				return ctx.Err()
			default:
			}

			if !rateLimiter.WaitCtx(ctx) {
				log.Printf("[Downloader:%s] Cancelled during rate limit wait", cbzName)
				return ctx.Err()
			}

			if callback != nil {
				imgProgress := progress + (float64(imgIdx) / float64(len(imageURLs)) / float64(newChaptersToDownload))
				callback(
					fmt.Sprintf("Chapter %d/%d: Downloading image %d/%d", actualChapterNum, totalChaptersFound, imgIdx+1, len(imageURLs)),
					imgProgress,
					actualChapterNum,
					currentDownload,
					totalChaptersFound,
				)
			}

			filename := fmt.Sprintf("%03d", imgIdx+1)
			err := m.downloadImageWithRetry(ctx, imgURL, chapterDir, filename)
			if err != nil {
				log.Printf("[Downloader:%s] Failed to download image %d: %v", cbzName, imgIdx+1, err)
			} else {
				successCount++
			}
		}
	}

	log.Printf("[Downloader:%s] Downloaded %d/%d images", cbzName, successCount, len(imageURLs))

	if successCount == 0 {
		return fmt.Errorf("no images downloaded successfully")
	}

	// Create CBZ
	select {
	case <-ctx.Done():
		log.Printf("[Downloader:%s] Cancelled before CBZ creation", cbzName)
		return ctx.Err()
	default:
	}

	if callback != nil {
		callback(
			fmt.Sprintf("Chapter %d/%d: Creating CBZ file...", actualChapterNum, totalChaptersFound),
			progress,
			actualChapterNum,
			currentDownload,
			totalChaptersFound,
		)
	}

	cbzPath := filepath.Join(manga.Location, cbzName)
	if err := parser.CreateCbzFromDir(chapterDir, cbzPath); err != nil {
		return fmt.Errorf("failed to create CBZ: %w", err)
	}

	log.Printf("[Downloader] ✓ Created CBZ: %s (%d images)", cbzName, successCount)
	return nil
}

// guessExtension returns the file extension based on magic bytes
func guessExtension(data []byte) string {
	if len(data) < 4 {
		return "bin"
	}
	// WebP: RIFF....WEBP
	if data[0] == 0x52 && data[1] == 0x49 && data[2] == 0x46 && data[3] == 0x46 {
		return "webp"
	}
	// JPEG
	if data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return "jpg"
	}
	// PNG
	if data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 {
		return "png"
	}
	// GIF
	if data[0] == 0x47 && data[1] == 0x49 && data[2] == 0x46 {
		return "gif"
	}
	return "bin"
}

// downloadImageWithRetry downloads a single image with retry logic
func (m *Manager) downloadImageWithRetry(ctx context.Context, imageURL, targetDir, filename string) error {
	maxRetries := 3
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(math.Pow(2, float64(attempt))) * time.Second
			if !parser.SleepCtx(ctx, backoff) {
				log.Printf("[Downloader] Image retry cancelled for: %s", filename)
				return ctx.Err()
			}
		}

		// Use parser's download function with CF support if needed
		if m.config.Site.NeedsCFBypass() {
			err := parser.DownloadConvertToJPGRenameCf(ctx, filename, imageURL, targetDir, m.domain)
			if err == nil {
				return nil
			}
			lastErr = err
		} else {
			err := parser.DownloadConvertToJPGRename(ctx, filename, imageURL, targetDir)
			if err == nil {
				return nil
			}
			lastErr = err
		}
	}

	return lastErr
}

// extractChapterNumber extracts the numeric chapter number from filenames like "ch001.cbz"
func extractChapterNumber(filename string) int {
	name := strings.TrimSuffix(filename, ".cbz")
	name = strings.TrimPrefix(name, "ch")
	parts := strings.Split(name, ".")
	if len(parts) == 0 {
		return 0
	}

	var chapterNum int
	fmt.Sscanf(parts[0], "%d", &chapterNum)
	return chapterNum
}
