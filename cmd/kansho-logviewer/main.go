package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/nxadm/tail"
)

const (
	initialLinesToShow = 1000
	maxLinesToKeep     = 2000
	uiUpdateInterval   = 500 * time.Millisecond
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <log-file-path>\n", os.Args[0])
		os.Exit(1)
	}

	logFilePath := os.Args[1]

	if _, err := os.Stat(logFilePath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: log file does not exist: %s\n", logFilePath)
		os.Exit(1)
	}

	kanshoApp := app.New()
	showLogWindow(kanshoApp, logFilePath)
	kanshoApp.Run()
}

func showLogWindow(kanshoApp fyne.App, logFilePath string) {
	logWindow := kanshoApp.NewWindow("Kansho Log Viewer")
	logWindow.Resize(fyne.NewSize(1000, 700))

	logLabel := widget.NewLabel("Loading log file...")
	logLabel.Wrapping = fyne.TextWrapWord

	searchEntry := widget.NewEntry()
	searchEntry.SetPlaceHolder("Search in loaded lines...")

	var displayedLines []string
	var mu sync.Mutex
	var totalLineCount int
	var tailInstance *tail.Tail
	var pendingUpdate bool

	addLineNumbers := func(lines []string) string {
		var numbered []string
		startNum := len(lines)
		for i, line := range lines {
			lineNum := startNum - i
			numbered = append(numbered, fmt.Sprintf("%6d | %s", lineNum, line))
		}
		return strings.Join(numbered, "\n")
	}

	updateDisplay := func() {
		mu.Lock()
		content := addLineNumbers(displayedLines)
		mu.Unlock()
		logLabel.SetText(content)
	}

	performSearch := func() {
		query := searchEntry.Text
		if query == "" {
			updateDisplay()
			return
		}

		go func() {
			mu.Lock()
			var filtered []string
			var lineNumbers []int
			queryLower := strings.ToLower(query)

			startNum := len(displayedLines)
			for i, line := range displayedLines {
				if strings.Contains(strings.ToLower(line), queryLower) {
					filtered = append(filtered, line)
					lineNumbers = append(lineNumbers, startNum-i)
				}
			}
			mu.Unlock()

			result := ""
			if len(filtered) == 0 {
				result = fmt.Sprintf("No results found for: %s\n(Searching only in loaded lines)", query)
			} else {
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

	openEditorButton := widget.NewButton("Open in Editor", func() {
		openFileInEditor(logFilePath, logWindow)
	})

	infoLabel := widget.NewLabel("")

	searchBox := container.NewBorder(nil, nil, nil,
		container.NewHBox(searchButton, clearButton, openEditorButton),
		searchEntry)

	scroll := container.NewScroll(logLabel)

	content := container.NewBorder(
		container.NewVBox(searchBox, infoLabel),
		nil, nil, nil,
		scroll,
	)
	logWindow.SetContent(content)

	logWindow.SetOnClosed(func() {
		mu.Lock()
		defer mu.Unlock()
		if tailInstance != nil {
			tailInstance.Stop()
		}
	})

	logWindow.Show()

	// Start ticker for batched UI updates
	ticker := time.NewTicker(uiUpdateInterval)
	go func() {
		defer ticker.Stop()
		for range ticker.C {
			mu.Lock()
			needsUpdate := pendingUpdate
			pendingUpdate = false
			lineCount := len(displayedLines)
			mu.Unlock()

			if needsUpdate && searchEntry.Text == "" {
				fyne.Do(func() {
					updateDisplay()
					infoLabel.SetText(fmt.Sprintf("Following log file... (%d lines loaded, newest first)", lineCount))
				})
			}
		}
	}()

	// Load initial file content
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

		var linesToDisplay []string
		if len(lines) > initialLinesToShow {
			linesToDisplay = lines[len(lines)-initialLinesToShow:]
		} else {
			linesToDisplay = lines
		}

		mu.Lock()
		for i := len(linesToDisplay) - 1; i >= 0; i-- {
			displayedLines = append(displayedLines, linesToDisplay[i])
		}
		lineCount := len(displayedLines)
		mu.Unlock()

		fyne.Do(func() {
			infoLabel.SetText(fmt.Sprintf("Following log file... Loaded last %d of %d total lines (newest first).", lineCount, totalLineCount))
			updateDisplay()
		})

		// Start tailing
		go func() {
			t, err := tail.TailFile(logFilePath, tail.Config{
				Follow: true,
				ReOpen: true,
				Poll:   true,
				Location: &tail.SeekInfo{
					Offset: 0,
					Whence: 2,
				},
				Logger: tail.DiscardingLogger,
			})

			if err != nil {
				log.Printf("Failed to start following log file: %v", err)
				return
			}

			mu.Lock()
			tailInstance = t
			mu.Unlock()

			for line := range t.Lines {
				if line.Err != nil {
					log.Printf("Error tailing file: %v", line.Err)
					continue
				}

				mu.Lock()
				displayedLines = append([]string{line.Text}, displayedLines...)
				if len(displayedLines) > maxLinesToKeep {
					displayedLines = displayedLines[:maxLinesToKeep]
				}
				pendingUpdate = true
				mu.Unlock()
			}
		}()
	}()
}

func openFileInEditor(filePath string, parent fyne.Window) {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("notepad", filePath)
	case "darwin":
		cmd = exec.Command("open", "-t", filePath)
	case "linux":
		// Try common editors in order of preference
		editors := []string{"xdg-open", "gedit", "kate", "nano", "vim"}
		var found bool
		for _, editor := range editors {
			if _, err := exec.LookPath(editor); err == nil {
				if editor == "nano" || editor == "vim" {
					// Terminal editors need to be launched in a terminal
					cmd = exec.Command("x-terminal-emulator", "-e", editor, filePath)
				} else {
					cmd = exec.Command(editor, filePath)
				}
				found = true
				break
			}
		}
		if !found {
			dialog.ShowError(fmt.Errorf("no text editor found"), parent)
			return
		}
	default:
		dialog.ShowError(fmt.Errorf("unsupported operating system"), parent)
		return
	}

	err := cmd.Start()
	if err != nil {
		dialog.ShowError(fmt.Errorf("failed to open editor: %v", err), parent)
	}
}