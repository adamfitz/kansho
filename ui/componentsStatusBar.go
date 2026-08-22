package ui

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// MainStatusBar is a compact live readout pinned to the bottom of the main
// Kansho window, mirroring the status bar of the download queue page: a short
// bold badge on the left and a status message that truncates instead of
// wrapping. It shows the selected manga's download site and how many of its
// chapters are downloaded; once the user refreshes that manga's chapter list
// (fetching remote chapters) it also shows how many chapters are not
// downloaded.
type MainStatusBar struct {
	Bar     fyne.CanvasObject
	badge   *widget.Label
	message *widget.Label
}

// NewMainStatusBar creates the main window status bar in its idle state.
func NewMainStatusBar() *MainStatusBar {
	s := &MainStatusBar{}

	// Badge on the left (bold, like the queue page's status badge) and the
	// message in the centre, truncating so it never wraps.
	s.badge = widget.NewLabel("")
	s.badge.TextStyle = fyne.TextStyle{Bold: true}
	s.message = widget.NewLabel("No manga selected")
	s.message.Truncation = fyne.TextTruncateEllipsis

	statusRow := container.NewBorder(nil, nil, s.badge, nil, s.message)

	// A slim white strip keeps the bar readable against the purple gradient
	// and matches the look of the download queue page. It is built by hand
	// instead of via NewCard because the standard card forces a 100px minimum
	// height, which is far too tall for a one-line status bar.
	bg := canvas.NewRectangle(CardBackgroundColor)
	s.Bar = container.NewStack(bg, container.NewPadded(statusRow))

	return s
}

// ShowManga displays the download state for the given site. The number of
// downloaded chapters is always shown; the number of not-downloaded chapters
// is only included once refreshed is true, i.e. after the user pressed Refresh
// on the chapter list and remote chapters were fetched.
func (s *MainStatusBar) ShowManga(site string, downloaded, notDownloaded int, refreshed bool) {
	s.badge.SetText(site)
	if refreshed {
		s.message.SetText(fmt.Sprintf("%d chapters downloaded · %d to download", downloaded, notDownloaded))
	} else {
		s.message.SetText(fmt.Sprintf("%d chapters downloaded", downloaded))
	}
}

// SetIdle resets the bar to its initial state when no manga is selected.
func (s *MainStatusBar) SetIdle() {
	s.badge.SetText("")
	s.message.SetText("No manga selected")
}
