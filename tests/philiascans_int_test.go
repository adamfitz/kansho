package integration

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kansho/downloader"
	"kansho/sites"
)

func Test_Philiascans_Chapter_And_Images(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	site := &sites.PhiliaScansSite{}

	const mangaURL = "https://philiascans.org/series/living-together-with-the-queen-from-my-high-school-days"

	// ------------------------------------------------------------
	// 1. Fetch chapter list
	// ------------------------------------------------------------
	chapterMap, err := downloader.FetchChapterURLs(ctx, mangaURL, site)
	if err != nil {
		t.Fatalf("Failed to extract chapter list: %v", err)
	}

	if len(chapterMap) == 0 {
		t.Fatalf("Chapter list is empty — parser may be broken")
	}

	log.Printf("[TEST] Found %d free chapters", len(chapterMap))

	// Pick chapter 1 for deterministic testing
	chapterURL := ""
	for name, url := range chapterMap {
		if strings.Contains(name, "ch001") {
			chapterURL = url
			log.Printf("[TEST] Using chapter: %s -> %s", name, url)
			break
		}
	}
	if chapterURL == "" {
		// Fall back to first available
		for name, url := range chapterMap {
			chapterURL = url
			log.Printf("[TEST] Using first available chapter: %s -> %s", name, url)
			break
		}
	}

	// ------------------------------------------------------------
	// 2. Test FetchChapterImages (Type: "javascript" path)
	// ------------------------------------------------------------
	images, err := downloader.FetchChapterImages(ctx, chapterURL, site)
	if err != nil {
		t.Fatalf("Failed to extract chapter images: %v", err)
	}

	if len(images) == 0 {
		t.Fatalf("No images found — image parser may be broken")
	}

	log.Printf("[TEST] FetchChapterImages returned %d results", len(images))

	for _, img := range images {
		isDataURL := strings.HasPrefix(img, "data:image/")
		isHTTPURL := strings.HasPrefix(img, "https://") || strings.HasPrefix(img, "http://")
		if !isDataURL && !isHTTPURL {
			t.Fatalf("Invalid image result: %s", img[:80])
		}
	}

	// ------------------------------------------------------------
	// 3. Test DownloadCanvasImages (manager browser path)
	// ------------------------------------------------------------
	browserCtx, browserCancel := context.WithTimeout(ctx, 120*time.Second)
	defer browserCancel()

	session, err := downloader.NewBrowserSession(browserCtx, "philiascans.org", false)
	if err != nil {
		t.Fatalf("Failed to create browser session: %v", err)
	}
	defer session.Close()

	// Pass transform function for decryption
	siteImpl := &sites.PhiliaScansSite{}
	chapterImages, err := session.DownloadCanvasImages(chapterURL, "#pages-container, .page-wrap", siteImpl.TransformImage)
	if err != nil {
		t.Fatalf("DownloadCanvasImages failed: %v", err)
	}

	if len(chapterImages.Data) == 0 {
		t.Fatalf("DownloadCanvasImages returned 0 images")
	}

	log.Printf("[TEST] DownloadCanvasImages extracted %d images", len(chapterImages.Data))

	// Save first image to temp dir and verify it's valid
	tmpDir := t.TempDir()
	imgIdx := 0
	for _, data := range chapterImages.Data {
		path := filepath.Join(tmpDir, fmt.Sprintf("img_%03d.jpg", imgIdx))
		imgIdx++
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatalf("Failed to write image %s: %v", path, err)
		}
		log.Printf("[TEST] Saved %s (%d bytes)", path, len(data))

		// Verify magic bytes: JPEG starts with FF D8 FF, WebP starts with RIFF
		isJPEG := len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF
		isWebP := len(data) >= 4 && data[0] == 'R' && data[1] == 'I' && data[2] == 'F' && data[3] == 'F'
		if !isJPEG && !isWebP {
			t.Fatalf("Image %s is not valid JPEG/WebP (magic bytes: %x)", path, data[:4])
		}
		break // Just check one
	}

	log.Printf("[TEST] SUCCESS — PhiliaScans scraper is working")
}
