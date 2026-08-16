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

func Test_KingOfShojo_Chapters_And_Images(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	site := &sites.KingOfShojoSite{}

	// Stable kingofshojo.com series
	const mangaURL = "https://kingofshojo.com/manga/you-re-the-only-one-i-can-see/"

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
	for name := range chapterMap {
		if !strings.HasPrefix(name, "ch") || !strings.HasSuffix(name, ".cbz") {
			t.Fatalf("Unexpected chapter filename: %s", name)
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
	for _, img := range images {
		if !strings.HasPrefix(img, "https://") {
			t.Fatalf("Invalid image URL: %s", img)
		}
	}

	log.Printf("[TEST] SUCCESS — kingofshojo scraper is working")
}
