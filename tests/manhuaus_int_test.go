//go:build integration

package integration

import (
	"archive/zip"
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kansho/config"
	"kansho/downloader"
	"kansho/sites"
)

func Test_Manhuaus_Chapters_And_Images(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	site := &sites.ManhuausSite{}

	// Stable manhuaus.com series from the local bookmarks.
	const mangaURL = "https://manhuaus.com/manga/chronicles-of-the-demon-faction"

	// ------------------------------------------------------------
	// 1. Fetch chapter list using real downloader logic (custom HTTP parser)
	// ------------------------------------------------------------
	chapterMap, err := downloader.FetchChapterURLs(ctx, mangaURL, site)
	if err != nil {
		t.Fatalf("Failed to extract chapter list: %v", err)
	}

	if len(chapterMap) == 0 {
		t.Fatalf("Chapter list is empty — parser may be broken")
	}

	log.Printf("[TEST] Found %d chapters", len(chapterMap))

	// ------------------------------------------------------------
	// 2. Fetch chapter images using real downloader logic (custom HTTP parser)
	// ------------------------------------------------------------
	chapterKeys := MapKeys(chapterMap)
	randomChapterFilename := PickRandom(chapterKeys)
	randomChapterURL := chapterMap[randomChapterFilename]

	log.Printf("[TEST] Selected random chapter: %s -> %s", randomChapterFilename, randomChapterURL)

	images, err := downloader.FetchChapterImages(ctx, randomChapterURL, site)
	if err != nil {
		t.Fatalf("Failed to extract chapter images: %v", err)
	}

	if len(images) == 0 {
		t.Fatalf("No images found — image parser may be broken")
	}

	log.Printf("[TEST] Found %d images", len(images))

	// ------------------------------------------------------------
	// 3. Basic validation
	// ------------------------------------------------------------
	for _, img := range images {
		if !strings.HasPrefix(img, "https://") {
			t.Fatalf("Invalid image URL: %s", img)
		}
		if strings.Contains(img, "?r=") {
			t.Fatalf("Image URL still has retry suffix: %s", img)
		}
	}

	log.Printf("[TEST] SUCCESS — manhuaus chapter list + image extraction is working")
}

func Test_Manhuaus_Browser_Chapter_Download(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	site := &sites.ManhuausSite{}

	const mangaURL = "https://manhuaus.com/manga/chronicles-of-the-demon-faction"

	chapterMap, err := downloader.FetchChapterURLs(ctx, mangaURL, site)
	if err != nil {
		t.Fatalf("Failed to extract chapter list: %v", err)
	}

	if len(chapterMap) == 0 {
		t.Fatalf("Chapter list is empty — parser may be broken")
	}

	chapterKeys := MapKeys(chapterMap)
	randomChapterFilename := PickRandom(chapterKeys)
	randomChapterURL := chapterMap[randomChapterFilename]

	log.Printf("[TEST] Selected random chapter: %s -> %s", randomChapterFilename, randomChapterURL)

	// ------------------------------------------------------------
	// Download ONE chapter through the full manager flow, which routes
	// manhuaus image bytes through the browser's network stack (the fix).
	// ------------------------------------------------------------
	location := t.TempDir()

	manga := &config.Bookmarks{
		Title:     "Chronicles of the Demon Faction",
		Url:       mangaURL,
		Chapters:  "1",
		Location:  location,
		Site:      "manhuaus",
		Shortname: "manhuaus-test",
	}

	cfg := &downloader.DownloadConfig{
		Manga: manga,
		Site:  site,
	}

	manager := downloader.NewManager(cfg)

	start := time.Now()
	err = manager.DownloadSingleChapter(ctx, randomChapterURL, randomChapterFilename)
	if err != nil {
		t.Fatalf("Browser-based chapter download failed: %v", err)
	}
	log.Printf("[TEST] Chapter downloaded in %v", time.Since(start))

	// ------------------------------------------------------------
	// 4. Verify the CBZ exists and contains real pages
	// ------------------------------------------------------------
	cbzPath := filepath.Join(location, randomChapterFilename)
	f, err := os.Open(cbzPath)
	if err != nil {
		t.Fatalf("CBZ not created at %s: %v", cbzPath, err)
	}
	defer f.Close()

	zr, err := zip.NewReader(f, 0)
	if err != nil {
		// zip.NewReader needs a stat; make it happy.
		st, _ := f.Stat()
		zr, err = zip.NewReader(f, st.Size())
		if err != nil {
			t.Fatalf("Failed to open CBZ as zip: %v", err)
		}
	}

	imageCount := 0
	for _, zf := range zr.File {
		if strings.HasPrefix(zf.Name, "__MACOSX") {
			continue
		}
		rc, err := zf.Open()
		if err == nil {
			io.Copy(io.Discard, rc)
			rc.Close()
		}
		imageCount++
	}

	if imageCount == 0 {
		t.Fatalf("CBZ contains no files")
	}

	t.Logf("CBZ contains %d pages: %s (%d bytes)", imageCount, cbzPath, cbzFileSize(cbzPath))
	log.Printf("[TEST] SUCCESS — manhuaus browser chapter download produced a valid CBZ with %d pages", imageCount)
}

func cbzFileSize(path string) int64 {
	st, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return st.Size()
}