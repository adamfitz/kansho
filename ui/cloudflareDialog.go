package ui

import (
	"fmt"
	"log"

	"kansho/cf"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// ShowcfDialog displays a dialog when cf challenge is detected
// It includes instructions and an "Import cf Data" button
func ShowcfDialog(window fyne.Window, challengeURL string, onSuccess func()) {
	// Create instruction text
	instructions := widget.NewLabel(
		"A cf challenge was detected and opened in your browser.\n\n" +
			"Please complete the following steps:\n\n" +
			"1. Complete the challenge in your browser\n" +
			"2. Make sure you can see the actual manga page\n" +
			"3. Click the Kansho browser extension icon\n" +
			"4. Click 'Copy cf Data' in the extension\n" +
			"5. Return here and click 'Import Data' below\n\n" +
			"The browser extension must be installed first!\n" +
			"See: extensions/README.md for installation instructions.",
	)
	instructions.Wrapping = fyne.TextWrapWord

	// Create URL label (so user knows which page was opened)
	urlLabel := widget.NewLabel(fmt.Sprintf("Challenge URL:\n%s", challengeURL))
	urlLabel.Wrapping = fyne.TextWrapWord

	// Status label (shows import status)
	statusLabel := widget.NewLabel("")
	statusLabel.Hide()

	// Create buttons
	var importButton *widget.Button
	var closeButton *widget.Button
	var customDialog dialog.Dialog

	// Import button handler
	importButton = widget.NewButton("Import cf Data", func() {
		importButton.Disable()
		statusLabel.SetText("Reading clipboard...")
		statusLabel.Show()

		// Import from clipboard
		domain, err := cf.ImportFromClipboard()
		if err != nil {
			log.Printf("Failed to import cf data: %v", err)
			statusLabel.SetText(fmt.Sprintf("❌ Error: %v", err))
			importButton.Enable()
			return
		}

		// Success!
		log.Printf("Successfully imported cf data for: %s", domain)
		statusLabel.SetText(fmt.Sprintf("✅ Success! Imported data for: %s", domain))

		// Change import button to "Done"
		importButton.SetText("Done")
		importButton.OnTapped = func() {
			customDialog.Hide()
			// Call the success callback if provided
			if onSuccess != nil {
				onSuccess()
			}
		}
		importButton.Enable()
	})
	importButton.Importance = widget.HighImportance

	// Close button
	closeButton = widget.NewButton("Cancel", func() {
		customDialog.Hide()
	})

	// Layout
	content := container.NewVBox(
		widget.NewLabel("🔒 cf Challenge Detected"),
		widget.NewSeparator(),
		instructions,
		widget.NewSeparator(),
		urlLabel,
		widget.NewSeparator(),
		statusLabel,
		container.NewGridWithColumns(2,
			closeButton,
			importButton,
		),
	)

	// Create custom dialog
	customDialog = dialog.NewCustom(
		"cf Challenge",
		"", // No dismiss button text (we have our own buttons)
		content,
		window,
	)

	// Make dialog larger
	customDialog.Resize(fyne.NewSize(600, 400))
	customDialog.Show()
}
