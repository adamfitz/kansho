//go:build integration

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

func Test_Comix_Chapters_And_Images(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	site := &sites.ComixSite{}

	// Stable comix.to series
	const mangaURL = "https://comix.to/title/20ld2-i-turn-skills-into-broken-ones-with-a-single-word"

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
		if !strings.HasPrefix(img, "https://") {
			t.Fatalf("Invalid image URL: %s", img)
		}
		if strings.Contains(img, "?r=") {
			t.Fatalf("Image URL still has retry suffix: %s", img)
		}
	}

	log.Printf("[TEST] SUCCESS — comix scraper is working")
}
