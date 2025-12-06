package ui

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"kansho/parser"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

const (
	initialLinesToShow = 1000 // Show last 1000 lines initially
	linesPerScroll     = 500  // Load 500 more lines when scrolling up
)

func ShowLogWindow(kanshoApp fyne.App) {
	configDir, err := parser.ExpandPath("~/.config/kansho")
	if err != nil {
		log.Fatalf("cannot verify local configuration directory: %v", err)
	}
	logFilePath := fmt.Sprintf("%s/kansho.log", configDir)

	logWindow := kanshoApp.NewWindow("Kansho Log Content")
	logWindow.Resize(fyne.NewSize(800, 600))

	logLabel := widget.NewLabel("Loading log file...")
	logLabel.Wrapping = fyne.TextWrapWord

	searchEntry := widget.NewEntry()
	searchEntry.SetPlaceHolder("Search in loaded lines...")

	var allLines []string
	var displayedLines []string
	var currentStartIndex int
	var totalLineCount int
	var isLoading bool

	addLineNumbers := func(lines []string, startIndex int) string {
		var numbered []string
		for i, line := range lines {
			lineNum := startIndex + i + 1
			numbered = append(numbered, fmt.Sprintf("%6d | %s", lineNum, line))
		}
		return strings.Join(numbered, "\n")
	}

	updateDisplay := func() {
		content := addLineNumbers(displayedLines, currentStartIndex)
		logLabel.SetText(content)
	}

	performSearch := func() {
		query := searchEntry.Text
		if query == "" {
			updateDisplay()
			return
		}

		go func() {
			var filtered []string
			var lineNumbers []int
			queryLower := strings.ToLower(query)

			// Search and collect line numbers
			for i, line := range displayedLines {
				if strings.Contains(strings.ToLower(line), queryLower) {
					filtered = append(filtered, line)
					lineNumbers = append(lineNumbers, currentStartIndex+i+1)
				}
			}

			result := ""
			if len(filtered) == 0 {
				result = fmt.Sprintf("No results found for: %s\n(Searching only in loaded lines)", query)
			} else {
				// Show results with line numbers and context
				var resultLines []string
				for i, line := range filtered {
					resultLines = append(resultLines, fmt.Sprintf("Line %d | %s", lineNumbers[i], line))
				}
				result = strings.Join(resultLines, "\n") + fmt.Sprintf("\n\n[Found %d matches in loaded lines]", len(filtered))
			}

			fyne.Do(func() {
				logLabel.SetText(result)
			})
		}()
	}

	// Trigger search on Enter key
	searchEntry.OnSubmitted = func(string) {
		performSearch()
	}

	searchButton := widget.NewButton("Search", func() {
		performSearch()
	})

	clearButton := widget.NewButton("Clear Search", func() {
		searchEntry.SetText("")
		updateDisplay()
	})

	openDirButton := widget.NewButton("Open Log Directory", func() {
		openDirectory(configDir, logWindow)
	})

	loadMoreButton := widget.NewButton("Load More Lines", func() {
		if isLoading {
			return
		}

		isLoading = true
		go func() {
			defer func() { isLoading = false }()

			newStartIndex := currentStartIndex - linesPerScroll
			if newStartIndex < 0 {
				newStartIndex = 0
			}

			if newStartIndex == currentStartIndex {
				fyne.Do(func() {
					dialog.ShowInformation("Info", "All available lines are already loaded", logWindow)
				})
				return
			}

			additionalLines := allLines[newStartIndex:currentStartIndex]
			displayedLines = append(additionalLines, displayedLines...)
			currentStartIndex = newStartIndex

			fyne.Do(func() {
				updateDisplay()
			})
		}()
	})

	infoLabel := widget.NewLabel("")

	searchBox := container.NewBorder(nil, nil, nil,
		container.NewHBox(searchButton, clearButton, loadMoreButton, openDirButton),
		searchEntry)

	scroll := container.NewScroll(logLabel)

	content := container.NewBorder(
		container.NewVBox(searchBox, infoLabel),
		nil, nil, nil,
		scroll,
	)
	logWindow.SetContent(content)
	logWindow.Show()

	// Load file asynchronously
	go func() {
		file, err := os.Open(logFilePath)
		if err != nil {
			fyne.Do(func() {
				logLabel.SetText(fmt.Sprintf("Failed to open log file: %v", err))
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
				logLabel.SetText(fmt.Sprintf("Error reading log file: %v", err))
			})
			return
		}

		totalLineCount = len(lines)
		allLines = lines

		if len(lines) > initialLinesToShow {
			currentStartIndex = len(lines) - initialLinesToShow
			displayedLines = lines[currentStartIndex:]
		} else {
			currentStartIndex = 0
			displayedLines = lines
		}

		fyne.Do(func() {
			infoLabel.SetText(fmt.Sprintf("Showing last %d of %d total lines. Use 'Load More Lines' to see older entries. Search works on loaded lines only.",
				len(displayedLines), totalLineCount))
			updateDisplay()
		})
	}()
}

func openDirectory(path string, parent fyne.Window) {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", path)
	case "darwin":
		cmd = exec.Command("open", path)
	case "linux":
		cmd = exec.Command("xdg-open", path)
	default:
		dialog.ShowError(fmt.Errorf("unsupported operating system"), parent)
		return
	}

	err := cmd.Start()
	if err != nil {
		dialog.ShowError(fmt.Errorf("failed to open directory: %v", err), parent)
	}
}
