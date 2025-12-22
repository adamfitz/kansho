package ui

import (
	"kansho/config"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

func ShowAboutDialog(kanshoApp fyne.App) {
	title := widget.NewLabel("Kansho")
	title.TextStyle = fyne.TextStyle{Bold: true}

	version := widget.NewLabel(
		"Version: " + config.Version +
			"\nCommit: " + config.GitCommit +
			"\nBuilt: " + config.BuildTime,
	)

	version.Alignment = fyne.TextAlignCenter

	description := widget.NewLabel(
		"Adhoc download of manga chapters for offline reading.",
	)
	description.Wrapping = fyne.TextWrapWord

	features := widget.NewLabel(
		"Features:\n" +
			"• Multiple sites\n" +
			"• Download queue\n" +
			"• Chapters saved as cbz files\n" +
			"• Chapters stored under 'manga name' directory\n" +
			"• Standard chapter names (chxxx.cbz)\n" +
			"• Cross-platform support",
	)
	features.Wrapping = fyne.TextWrapWord

	// Centered bold title
	centeredTitle := container.NewCenter(title)

	// centered version
	centeredVersion := container.NewCenter(version)

	// Declare window first so the close button can reference it
	var aboutWin fyne.Window
	closeBtn := widget.NewButton("Close", func() {
		aboutWin.Close()
	})

	// Main content (scrollable)
	mainContent := container.NewVBox(
		centeredTitle,
		centeredVersion,
		widget.NewSeparator(),
		description,
		widget.NewSeparator(),
		features,
	)

	scroll := container.NewScroll(mainContent)

	// Bottom area: separator + centered Close button
	bottom := container.NewVBox(
		widget.NewSeparator(),
		container.NewCenter(closeBtn),
	)

	// Border layout: scroll in center, close button at bottom
	content := container.NewBorder(nil, bottom, nil, nil, scroll)

	// Create and show window
	aboutWin = kanshoApp.NewWindow("About Kansho")
	aboutWin.SetContent(content)
	aboutWin.Resize(fyne.NewSize(400, 400))
	aboutWin.SetFixedSize(true)
	aboutWin.Show()
}
