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

	performSearch := func() {
		query := searchEntry.Text
		if query == "" {
			configLabel.SetText(strings.Join(allLines, "\n"))
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
				configLabel.SetText(result)
			})
		}()
	}

	clearSearch := func() {
		searchEntry.SetText("")
		configLabel.SetText(strings.Join(allLines, "\n"))
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

	scroll := container.NewScroll(configLabel)

	content := container.NewBorder(searchBox, nil, nil, nil, scroll)
	configWindow.SetContent(content)
	configWindow.Show()

	// Load embedded file asynchronously
	go func() {
		// Read the embedded file from the sites package
		fileData, err := sites.GetEmbeddedSitesJSON()
		if err != nil {
			fyne.Do(func() {
				configLabel.SetText(fmt.Sprintf("Failed to load embedded configuration: %v", err))
			})
			return
		}

		// Parse the file content line by line
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
		finalContent := strings.Join(lines, "\n")

		fyne.Do(func() {
			configLabel.SetText(finalContent)
		})
	}()
}
