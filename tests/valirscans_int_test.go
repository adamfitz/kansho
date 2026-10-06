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

func Test_Valirscans_Chapters_And_Images(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	site := &sites.ValirscansSite{}

	// Stable valirscans.org series
	const mangaURL = "https://valirscans.org/series/comic/my-bias-gets-on-the-last-train"

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

	// Chapter filenames must follow the chNNN.cbz convention
	for name, url := range chapterMap {
		if !strings.HasPrefix(name, "ch") || !strings.HasSuffix(name, ".cbz") {
			t.Fatalf("Unexpected chapter filename: %s", name)
		}
		if !strings.HasPrefix(url, "https://valirscans.org/series/comic/") {
			t.Fatalf("Unexpected chapter URL: %s", url)
		}
	}

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
	for i, img := range images {
		if !strings.HasPrefix(img, "https://media.valirscans.org/") {
			t.Fatalf("Invalid image URL at index %d: %s", i, img)
		}
		if i > 0 && images[i-1] == img {
			t.Fatalf("Duplicate image URL: %s", img)
		}
	}

	log.Printf("[TEST] SUCCESS — valirscans scraper is working")
}
