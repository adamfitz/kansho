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

func ShowLogWindow(kanshoApp fyne.App) {
	configDir, err := parser.ExpandPath("~/.config/kansho")
	if err != nil {
		log.Printf("cannot verify local configuration directory: %v", err)
		return
	}
	logFilePath := filepath.Join(configDir, "kansho.log")

	rlvPath, err := findRLVBinary()
	if err != nil {
		errWin := kanshoApp.NewWindow("rlv not found")
		errWin.Resize(fyne.NewSize(400, 200))
		dialog.ShowError(
			fmt.Errorf("rlv log viewer not found.\n\nEnsure rlv is installed and on your PATH, or reinstall kansho.\nhttps://github.com/adamfitz/rlv"),
			errWin,
		)
		errWin.Show()
		return
	}

	cmd := exec.Command(rlvPath, logFilePath)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout

	if err := cmd.Start(); err != nil {
		log.Printf("failed to launch rlv: %v", err)
		errWin := kanshoApp.NewWindow("Error")
		errWin.Resize(fyne.NewSize(400, 200))
		dialog.ShowError(fmt.Errorf("failed to launch rlv: %v", err), errWin)
		errWin.Show()
	}
}

func findRLVBinary() (string, error) {
	exe, err := os.Executable()
	if err == nil {
		rlvPath := filepath.Join(filepath.Dir(exe), "rlv")
		if _, err := os.Stat(rlvPath); err == nil {
			return rlvPath, nil
		}
	}
	return exec.LookPath("rlv")
}
