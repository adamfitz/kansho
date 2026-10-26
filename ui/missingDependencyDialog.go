package ui

import (
	"kansho/downloader"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// showMissingDependencyDialog tells the user that Kansho needs an external
// program that is not installed, and explains how to install it. It is shown
// from the UI goroutine.
func showMissingDependencyDialog(window fyne.Window, depErr *downloader.MissingDependencyError) {
	if window == nil || depErr == nil {
		return
	}

	heading := widget.NewLabelWithStyle(
		"Kansho is missing a browser dependency",
		fyne.TextAlignLeading,
		fyne.TextStyle{Bold: true},
	)

	message := widget.NewLabel(depErr.Error())
	message.Wrapping = fyne.TextWrapWord

	content := container.NewVScroll(container.NewVBox(
		heading,
		widget.NewSeparator(),
		message,
	))
	content.SetMinSize(fyne.NewSize(640, 300))

	d := dialog.NewCustom("Missing Dependency", "Close", content, window)
	d.Resize(fyne.NewSize(720, 420))
	d.Show()
}
