package ui

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"kansho/sites"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// SiteConfig represents a single site configuration block
type SiteConfig struct {
	Name           string          `json:"name"`
	DisplayName    string          `json:"display_name"`
	RequiredFields map[string]bool `json:"required_fields"`
}

// SitesConfig represents the full sites.json structure
type SitesConfigFile struct {
	Sites []SiteConfig `json:"sites"`
}

func ShowConfigWindow(kanshoApp fyne.App) {
	configWindow := kanshoApp.NewWindow("Kansho Site Configuration")
	configWindow.Resize(fyne.NewSize(800, 600))

	configLabel := widget.NewLabel("Loading configuration file...")
	configLabel.Wrapping = fyne.TextWrapWord

	searchEntry := widget.NewEntry()
	searchEntry.SetPlaceHolder("Search configuration...")

	var allLines []string
	var sitesConfig SitesConfigFile

	addLineNumbers := func(lines []string) string {
		var numbered []string
		for i, line := range lines {
			numbered = append(numbered, fmt.Sprintf("%4d | %s", i+1, line))
		}
		return strings.Join(numbered, "\n")
	}

	// Check if query exactly matches a site name or display_name
	findExactSiteMatch := func(query string) *SiteConfig {
		queryLower := strings.ToLower(strings.TrimSpace(query))
		for _, site := range sitesConfig.Sites {
			if strings.ToLower(site.Name) == queryLower || strings.ToLower(site.DisplayName) == queryLower {
				return &site
			}
		}
		return nil
	}

	// Find the line range for a site block in the JSON
	findSiteBlockLines := func(siteName string) (startLine, endLine int) {
		inSiteBlock := false
		braceCount := 0

		for i, line := range allLines {
			trimmed := strings.TrimSpace(line)

			// Check if this line contains the site name
			if strings.Contains(line, fmt.Sprintf(`"name": "%s"`, siteName)) {
				// Walk backwards to find the opening brace of this block
				for j := i; j >= 0; j-- {
					if strings.Contains(allLines[j], "{") && !strings.Contains(allLines[j], `"required_fields"`) {
						startLine = j
						inSiteBlock = true
						break
					}
				}
			}

			// If we're in the site block, count braces to find the end
			if inSiteBlock {
				braceCount += strings.Count(trimmed, "{")
				braceCount -= strings.Count(trimmed, "}")

				if braceCount == 0 && strings.Contains(trimmed, "}") {
					endLine = i
					return startLine, endLine
				}
			}
		}

		return -1, -1
	}

	performSearch := func() {
		query := searchEntry.Text
		if query == "" {
			configLabel.SetText(addLineNumbers(allLines))
			return
		}

		go func() {
			// Check for exact site name/display_name match
			if exactSite := findExactSiteMatch(query); exactSite != nil {
				startLine, endLine := findSiteBlockLines(exactSite.Name)

				if startLine != -1 && endLine != -1 {
					// Extract the full block with line numbers
					blockLines := allLines[startLine : endLine+1]
					var numberedBlock []string
					for i, line := range blockLines {
						numberedBlock = append(numberedBlock, fmt.Sprintf("Line %d | %s", startLine+i+1, line))
					}

					result := fmt.Sprintf("Exact match found for site: %s\n\n", exactSite.Name)
					result += strings.Join(numberedBlock, "\n")
					result += "\n\n[Showing complete site configuration block]"

					fyne.Do(func() {
						configLabel.SetText(result)
					})
					return
				}
			}

			// Otherwise, do regular line-by-line search
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

		// Parse the JSON structure for exact matching
		if err := json.Unmarshal(fileData, &sitesConfig); err != nil {
			fyne.Do(func() {
				configLabel.SetText(fmt.Sprintf("Failed to parse configuration: %v", err))
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
