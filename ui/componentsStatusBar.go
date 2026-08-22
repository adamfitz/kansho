package ui

import (
	"fmt"
	"time"

	"kansho/refreshpool"

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
// downloaded. The right edge carries the single representation of the
// chapter-list refresh worker pool (see refreshpool): an ASCII spinner while
// scrapes are in flight plus the running/queued counts.
type MainStatusBar struct {
	Bar        fyne.CanvasObject
	badge      *widget.Label
	message    *widget.Label
	spinner    *widget.Label // bash-style |/-\ animation while the pool is busy
	poolStatus *widget.Label

	// Spinner state; all mutations happen on the UI goroutine (ticker ticks
	// are marshalled back via fyne.Do).
	spinTicker *time.Ticker
	spinDone   chan struct{}
	spinFrame  int
}

// NewMainStatusBar creates the main window status bar in its idle state.
func NewMainStatusBar() *MainStatusBar {
	s := &MainStatusBar{}

	// Badge on the left (bold, like the queue page's status badge) and the
	// message in the centre, truncating so it never wraps. The refresh pool
	// readout is pinned to the right edge via the Border layout.
	s.badge = widget.NewLabel("")
	s.badge.TextStyle = fyne.TextStyle{Bold: true}
	s.message = widget.NewLabel("No manga selected")
	s.message.Truncation = fyne.TextTruncateEllipsis

	// No truncation here: the Border layout sizes the right edge to the
	// label's required width, so enabling ellipsis only ever produces
	// spurious "..." while the text grows (idle -> busy).
	s.poolStatus = widget.NewLabel("")
	s.poolStatus.Alignment = fyne.TextAlignTrailing
	s.poolStatus.SetText(refreshPoolIdleText)

	// The spinner sits just left of the counts. Monospace keeps the frame
	// glyphs at a constant width so the layout does not jitter while spinning.
	s.spinner = widget.NewLabel("")
	s.spinner.TextStyle = fyne.TextStyle{Monospace: true}

	statusRow := container.NewBorder(nil, nil, s.badge,
		container.NewHBox(s.spinner, s.poolStatus), s.message)

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

// refreshPoolIdleText is shown on the right edge while the chapter-list
// refresh pool has nothing queued or running.
const refreshPoolIdleText = "⟳ Refreshes: idle"

// refreshSpinnerFrames is the classic bash-style spinner sequence, cycled
// while the refresh pool has work in flight.
var refreshSpinnerFrames = []string{"|", "/", "-", "\\"}

// poolSpinnerInterval is how often the spinner advances its frame.
const poolSpinnerInterval = 120 * time.Millisecond

// SetRefreshPoolStatus shows the single representation of the chapter-list
// refresh worker pool: an animated spinner while scrapes are in flight plus
// how many are running across sites and how many tasks are still queued.
// Must be called on the UI goroutine.
func (s *MainStatusBar) SetRefreshPoolStatus(status refreshpool.Status) {
	if status.IsIdle() {
		s.stopPoolSpinner()
		s.poolStatus.SetText(refreshPoolIdleText)
		return
	}
	s.startPoolSpinner()
	s.poolStatus.SetText(fmt.Sprintf("Refreshes: %d running · %d queued", status.Running, status.Queued))
}

// startPoolSpinner begins cycling the |/-\ frames next to the counts. It is a
// no-op if the spinner is already running. Must be called on the UI goroutine.
func (s *MainStatusBar) startPoolSpinner() {
	if s.spinTicker != nil {
		return
	}

	// Show the first frame immediately instead of waiting for the first tick.
	s.spinFrame = 0
	s.spinner.SetText(refreshSpinnerFrames[0])

	ticker := time.NewTicker(poolSpinnerInterval)
	done := make(chan struct{})
	s.spinTicker = ticker
	s.spinDone = done

	go func() {
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				// Widget updates must happen on the UI thread; frame state is
				// only ever mutated inside fyne.Do, keeping it race-free.
				fyne.Do(func() {
					if s.spinTicker == nil { // stopped between tick and callback
						return
					}
					s.spinFrame = (s.spinFrame + 1) % len(refreshSpinnerFrames)
					s.spinner.SetText(refreshSpinnerFrames[s.spinFrame])
				})
			}
		}
	}()
}

// stopPoolSpinner halts the animation and clears the glyph. Must be called on
// the UI goroutine.
func (s *MainStatusBar) stopPoolSpinner() {
	if s.spinTicker == nil {
		return
	}
	close(s.spinDone)
	s.spinTicker.Stop()
	s.spinTicker = nil
	s.spinDone = nil
	s.spinner.SetText("")
}
