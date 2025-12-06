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

	addLineNumbers := func(lines []string) string {
		var numbered []string
		for i, line := range lines {
			numbered = append(numbered, fmt.Sprintf("%4d | %s", i+1, line))
		}
		return strings.Join(numbered, "\n")
	}

	performSearch := func() {
		query := searchEntry.Text
		if query == "" {
			bookmarksLabel.SetText(addLineNumbers(allLines))
			return
		}

		go func() {
			var filtered []string
			var lineNumbers []int
			queryLower := strings.ToLower(query)

			for i, line := range allLines {
				if strings.Contains(strings.ToLower(line), queryLower) {
					filtered = append(filtered, line)
					lineNumbers = append(lineNumbers, i+1)
				}
			}

			result := ""
			if len(filtered) == 0 {
				result = fmt.Sprintf("No results found for: %s", query)
			} else {
				var resultLines []string
				for i, line := range filtered {
					resultLines = append(resultLines, fmt.Sprintf("Line %d | %s", lineNumbers[i], line))
				}
				result = strings.Join(resultLines, "\n") + fmt.Sprintf("\n\n[Found %d matches]", len(filtered))
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
		finalContent := addLineNumbers(lines)

		fyne.Do(func() {
			bookmarksLabel.SetText(finalContent)
		})
	}()
}
