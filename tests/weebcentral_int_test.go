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

func Test_WeebCentral_OnePiece_Chapters_And_Images(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	site := &sites.WeebcentralSite{}

	// One Piece — stable, long-running series with many chapters
	const mangaURL = "https://weebcentral.com/series/01J76XY7E9FNDZ1DBBM6PBJPFK/One-Piece"

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

	log.Printf("[TEST] Found %d chapters", len(chapterMap))

	// Sanity check: One Piece has 1000+ chapters
	if len(chapterMap) < 100 {
		t.Fatalf("Expected at least 100 chapters for One Piece, got %d — full-chapter-list endpoint may not be working", len(chapterMap))
	}

	// Verify chapter filenames look correct (e.g. ch001.cbz, ch1100.cbz)
	for filename := range chapterMap {
		if !strings.HasSuffix(filename, ".cbz") {
			t.Fatalf("Unexpected chapter filename format: %s", filename)
		}
		if !strings.HasPrefix(filename, "ch") && !strings.HasPrefix(filename, "ep") {
			t.Fatalf("Unexpected chapter filename prefix: %s", filename)
		}
	}

	// Verify chapter URLs look correct
	for filename, chapterURL := range chapterMap {
		if !strings.HasPrefix(chapterURL, "https://weebcentral.com/chapters/") {
			t.Fatalf("Unexpected chapter URL for %s: %s", filename, chapterURL)
		}
	}

	// ------------------------------------------------------------
	// 2. Fetch images for a random chapter
	// ------------------------------------------------------------
	chapterKeys := MapKeys(chapterMap)
	randomChapterFilename := PickRandom(chapterKeys)
	randomChapterURL := chapterMap[randomChapterFilename]

	log.Printf("[TEST] Selected random chapter: %s -> %s", randomChapterFilename, randomChapterURL)

	images, err := downloader.FetchChapterImages(ctx, randomChapterURL, site)
	if err != nil {
		t.Fatalf("Failed to extract chapter images for %s: %v", randomChapterFilename, err)
	}

	if len(images) == 0 {
		t.Fatalf("No images found for %s — image parser may be broken", randomChapterFilename)
	}

	log.Printf("[TEST] Found %d images in %s", len(images), randomChapterFilename)

	// Verify all image URLs are absolute HTTPS URLs
	for _, img := range images {
		if !strings.HasPrefix(img, "https://") {
			t.Fatalf("Non-HTTPS image URL: %s", img)
		}
	}

	log.Printf("[TEST] SUCCESS — WeebCentral scraper is working")
}
