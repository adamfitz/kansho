package ui

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"kansho/parser"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
)

// ShowLogWindow launches the standalone log viewer process
func ShowLogWindow(parent fyne.Window) {
	configDir, err := parser.ExpandPath("~/.config/kansho")
	if err != nil {
		log.Printf("cannot verify local configuration directory: %v", err)
		dialog.ShowError(fmt.Errorf("Failed to find config directory: %v", err), parent)
		return
	}
	logFilePath := fmt.Sprintf("%s/kansho.log", configDir)

	// Verify log file exists
	if _, err := os.Stat(logFilePath); os.IsNotExist(err) {
		dialog.ShowError(fmt.Errorf("Log file does not exist: %s", logFilePath), parent)
		return
	}

	// Find the logviewer binary
	viewerPath, err := findLogViewerBinary()
	if err != nil {
		dialog.ShowError(fmt.Errorf("Failed to find log viewer: %v", err), parent)
		return
	}

	// Launch the log viewer as a separate process
	cmd := exec.Command(viewerPath, logFilePath)
	if err := cmd.Start(); err != nil {
		dialog.ShowError(fmt.Errorf("Failed to launch log viewer: %v", err), parent)
		return
	}

	// Detach from the process - we don't wait for it
	go func() {
		if err := cmd.Wait(); err != nil {
			log.Printf("Log viewer exited with error: %v", err)
		}
	}()
}

// findLogViewerBinary locates the kansho-logviewer binary
func findLogViewerBinary() (string, error) {
	// Try same directory as main binary first
	exePath, err := os.Executable()
	if err == nil {
		// Resolve symlinks
		exePath, _ = filepath.EvalSymlinks(exePath)
		viewerPath := filepath.Join(filepath.Dir(exePath), "kansho-logviewer")
		if _, err := os.Stat(viewerPath); err == nil {
			return viewerPath, nil
		}
	}

	// Fall back to PATH
	return exec.LookPath("kansho-logviewer")
}
