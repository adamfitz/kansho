package integration

import (
	"context"
	"log"
	"strings"
	"testing"
	"time"

	"kansho/downloader"
	"kansho/sites"
)

func Test_Mangakatana_ItsNotMyFault_Chapters_And_Images(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	site := &sites.MangakatanaSite{}

	// Stable, long-running Mangakatana series
	const mangaURL = "https://mangakatana.com/manga/its-not-my-fault-that-im-not-popular.17750"

	// ------------------------------------------------------------
	// 1. Fetch chapter list using real downloader logic
	// ------------------------------------------------------------
	chapterMap, err := downloader.FetchChapterURLs(ctx, mangaURL, site)
	if err != nil {
		t.Fatalf("Failed to extract chapter list: %v", err)
	}

	if len(chapterMap) == 0 {
		t.Fatalf("Chapter list is empty — parser may be broken")
	}

	log.Printf("[TEST] Found %d chapters", len(chapterMap))

	// Pick a random chapter
	chapterKeys := MapKeys(chapterMap)
	randomChapterFilename := PickRandom(chapterKeys)
	randomChapterURL := chapterMap[randomChapterFilename]

	log.Printf("[TEST] Selected random chapter: %s -> %s", randomChapterFilename, randomChapterURL)

	// ------------------------------------------------------------
	// 2. Fetch chapter images using real downloader logic
	// ------------------------------------------------------------
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
		if !strings.HasPrefix(img, "https://") ||
			!strings.Contains(img, "mangakatana.com") {
			t.Fatalf("Invalid image URL: %s", img)
		}
	}

	log.Printf("[TEST] SUCCESS — Mangakatana scraper is working")
}
