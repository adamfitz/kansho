package ui

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"

	"kansho/sites"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

func ShowConfigWindow(kanshoApp fyne.App) {
	configWindow := kanshoApp.NewWindow("Kansho Site Configuration")
	configWindow.Resize(fyne.NewSize(800, 600))

	configLabel := widget.NewLabel("Loading configuration file...")
	configLabel.Wrapping = fyne.TextWrapWord

	searchEntry := widget.NewEntry()
	searchEntry.SetPlaceHolder("Search configuration...")

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
			configLabel.SetText(addLineNumbers(allLines))
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
				configLabel.SetText(result)
			})
		}()
	}

	clearSearch := func() {
		searchEntry.SetText("")
		configLabel.SetText(addLineNumbers(allLines))
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

	scroll := container.NewScroll(configLabel)

	content := container.NewBorder(searchBox, nil, nil, nil, scroll)
	configWindow.SetContent(content)
	configWindow.Show()

	go func() {
		fileData, err := sites.GetEmbeddedSitesJSON()
		if err != nil {
			fyne.Do(func() {
				configLabel.SetText(fmt.Sprintf("Failed to load embedded configuration: %v", err))
			})
			return
		}

		scanner := bufio.NewScanner(bytes.NewReader(fileData))
		var lines []string

		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}

		if err := scanner.Err(); err != nil {
			fyne.Do(func() {
				configLabel.SetText(fmt.Sprintf("Error reading configuration: %v", err))
			})
			return
		}

		allLines = lines
		finalContent := addLineNumbers(lines)

		fyne.Do(func() {
			configLabel.SetText(finalContent)
		})
	}()
}
