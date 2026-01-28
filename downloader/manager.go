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

		log.Printf("[Downloader:%s] Starting chapter %d/%d: %s", manga.Title, currentDownload, newChaptersToDownload, cbzName)

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
			log.Printf("[Downloader:%s] Retry %d/%d after %v", cbzName, attempt+1, maxRetries, backoff)
			time.Sleep(backoff)
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

	// Get image URLs
	imageURLs, err := FetchChapterImages(ctx, chapterURL, site)
	if err != nil {
		return fmt.Errorf("failed to get chapter images: %w", err)
	}

	if len(imageURLs) == 0 {
		return fmt.Errorf("no images found")
	}

	log.Printf("[Downloader:%s] Found %d images", cbzName, len(imageURLs))

	// Create temp directory
	chapterDir := filepath.Join("/tmp", site.GetSiteName(), strings.TrimSuffix(cbzName, ".cbz"))
	if err := os.MkdirAll(chapterDir, 0755); err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(chapterDir) // Clean up temp dir

	// Download images with rate limiting
	successCount := 0
	rateLimiter := parser.NewRateLimiter(1500 * time.Millisecond)
	defer rateLimiter.Stop()

	for imgIdx, imgURL := range imageURLs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		rateLimiter.Wait()

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

		// Download image with retry
		filename := fmt.Sprintf("%03d", imgIdx+1)
		err := m.downloadImageWithRetry(imgURL, chapterDir, filename)
		if err != nil {
			log.Printf("[Downloader:%s] Failed to download image %d: %v", cbzName, imgIdx+1, err)
		} else {
			successCount++
		}
	}

	log.Printf("[Downloader:%s] Downloaded %d/%d images", cbzName, successCount, len(imageURLs))

	if successCount == 0 {
		return fmt.Errorf("no images downloaded successfully")
	}

	// Create CBZ
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

// downloadImageWithRetry downloads a single image with retry logic
func (m *Manager) downloadImageWithRetry(imageURL, targetDir, filename string) error {
	maxRetries := 3
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(math.Pow(2, float64(attempt))) * time.Second
			time.Sleep(backoff)
		}

		// Use parser's download function with CF support if needed
		if m.config.Site.NeedsCFBypass() {
			err := parser.DownloadConvertToJPGRenameCf(filename, imageURL, targetDir, m.domain)
			if err == nil {
				return nil
			}
			lastErr = err
		} else {
			err := parser.DownloadConvertToJPGRename(filename, imageURL, targetDir)
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
