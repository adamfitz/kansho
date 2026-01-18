package ui

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"kansho/parser"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

type Bookmark struct {
	Title     string `json:"title"`
	URL       string `json:"url"`
	Chapters  string `json:"chapters"`
	Location  string `json:"location"`
	Site      string `json:"site"`
	Shortname string `json:"shortname"`
}

type BookmarksData struct {
	Manga []Bookmark `json:"manga"`
}

func ShowBookmarksWindow(kanshoApp fyne.App) {
	configDir, err := parser.ExpandPath("~/.config/kansho")
	if err != nil {
		return
	}
	bookmarksFilePath := fmt.Sprintf("%s/bookmarks.json", configDir)

	bookmarksWindow := kanshoApp.NewWindow("Kansho Bookmarks")
	bookmarksWindow.Resize(fyne.NewSize(800, 600))

	bookmarksLabel := widget.NewLabel("Loading bookmarks file...")
	bookmarksLabel.Wrapping = fyne.TextWrapWord

	searchEntry := widget.NewEntry()
	searchEntry.SetPlaceHolder("Search bookmarks...")

	var allLines []string
	var bookmarksData BookmarksData

	addLineNumbers := func(lines []string) string {
		var numbered []string
		for i, line := range lines {
			numbered = append(numbered, fmt.Sprintf("%4d | %s", i+1, line))
		}
		return strings.Join(numbered, "\n")
	}

	// Find which block a line belongs to and return the block's line range
	findBlockRange := func(lineNum int) (start, end int) {
		inMangaArray := false
		blockStart := -1
		braceCount := 0

		for i, line := range allLines {
			trimmed := strings.TrimSpace(line)

			// Check if we're entering the manga array
			if strings.Contains(trimmed, `"manga":`) {
				inMangaArray = true
				continue
			}

			if !inMangaArray {
				continue
			}

			// Track opening braces for blocks
			if trimmed == "{" && blockStart == -1 {
				blockStart = i
				braceCount = 1
			} else if blockStart != -1 {
				braceCount += strings.Count(trimmed, "{")
				braceCount -= strings.Count(trimmed, "}")

				if braceCount == 0 {
					// Block is complete
					if i >= lineNum && lineNum >= blockStart {
						return blockStart, i
					}
					blockStart = -1
				}
			}
		}
		return -1, -1
	}

	performSearch := func() {
		query := searchEntry.Text
		if query == "" {
			bookmarksLabel.SetText(addLineNumbers(allLines))
			return
		}

		go func() {
			queryLower := strings.ToLower(query)
			matchedBlocks := make(map[int]bool) // Track unique blocks by start line
			var results []string

			// Search through each line
			for i, line := range allLines {
				if strings.Contains(strings.ToLower(line), queryLower) {
					start, end := findBlockRange(i)
					if start != -1 && !matchedBlocks[start] {
						matchedBlocks[start] = true

						// Extract the block
						var blockLines []string
						for j := start; j <= end; j++ {
							blockLines = append(blockLines, fmt.Sprintf("%4d | %s", j+1, allLines[j]))
						}
						results = append(results, strings.Join(blockLines, "\n"))
					}
				}
			}

			result := ""
			if len(results) == 0 {
				result = fmt.Sprintf("No results found for: %s", query)
			} else {
				result = strings.Join(results, "\n\n") + fmt.Sprintf("\n\n[Found %d matching blocks]", len(results))
			}

			fyne.Do(func() {
				bookmarksLabel.SetText(result)
			})
		}()
	}

	clearSearch := func() {
		searchEntry.SetText("")
		bookmarksLabel.SetText(addLineNumbers(allLines))
	}

	searchEntry.OnSubmitted = func(string) {
		performSearch()
	}

	searchButton := widget.NewButton("Search", func() {
		performSearch()
	})

	clearButton := widget.NewButton("Clear", func() {
		clearSearch()
	})

	searchBox := container.NewBorder(nil, nil, nil,
		container.NewHBox(searchButton, clearButton),
		searchEntry)

	scroll := container.NewScroll(bookmarksLabel)

	content := container.NewBorder(searchBox, nil, nil, nil, scroll)
	bookmarksWindow.SetContent(content)
	bookmarksWindow.Show()

	go func() {
		file, err := os.Open(bookmarksFilePath)
		if err != nil {
			fyne.Do(func() {
				bookmarksLabel.SetText(fmt.Sprintf("Failed to open bookmarks file: %v", err))
			})
			return
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 1024*1024)

		var lines []string

		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}

		if err := scanner.Err(); err != nil {
			fyne.Do(func() {
				bookmarksLabel.SetText(fmt.Sprintf("Error reading bookmarks file: %v", err))
			})
			return
		}

		allLines = lines

		// Also parse the JSON for potential future use
		file.Seek(0, 0)
		decoder := json.NewDecoder(file)
		if err := decoder.Decode(&bookmarksData); err != nil {
			// Non-fatal, just log it
			fmt.Printf("Warning: Could not parse JSON: %v\n", err)
		}

		finalContent := addLineNumbers(lines)

		fyne.Do(func() {
			bookmarksLabel.SetText(finalContent)
		})
	}()
}
