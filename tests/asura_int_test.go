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

func Test_Asura_SoloMaxLevelNewbie_Chapter_And_Images(t *testing.T) {
    ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
    defer cancel()

    site := &sites.AsuraSite{}

    // Stable, long-running manga URL
    const mangaURL = "https://asurascans.com/comics/solo-max-level-newbie-7f873ca6"

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
        if !strings.HasPrefix(img, "https://cdn.asurascans.com/") {
            t.Fatalf("Invalid image URL: %s", img)
        }
    }

    log.Printf("[TEST] SUCCESS — Asura scraper is working")
}
