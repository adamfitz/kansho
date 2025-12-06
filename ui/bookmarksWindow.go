package ui

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"kansho/parser"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

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

	performSearch := func() {
		query := searchEntry.Text
		if query == "" {
			bookmarksLabel.SetText(strings.Join(allLines, "\n"))
			return
		}

		go func() {
			var filtered []string
			queryLower := strings.ToLower(query)

			for _, line := range allLines {
				if strings.Contains(strings.ToLower(line), queryLower) {
					filtered = append(filtered, line)
				}
			}

			result := ""
			if len(filtered) == 0 {
				result = fmt.Sprintf("No results found for: %s", query)
			} else {
				result = strings.Join(filtered, "\n") + fmt.Sprintf("\n\n[Found %d matches]", len(filtered))
			}

			fyne.Do(func() {
				bookmarksLabel.SetText(result)
			})
		}()
	}

	clearSearch := func() {
		searchEntry.SetText("")
		bookmarksLabel.SetText(strings.Join(allLines, "\n"))
	}

	// Trigger search on Enter key
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

	// Load file asynchronously
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
		finalContent := strings.Join(lines, "\n")

		fyne.Do(func() {
			bookmarksLabel.SetText(finalContent)
		})
	}()
}
