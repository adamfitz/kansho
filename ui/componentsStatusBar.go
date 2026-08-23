package ui

import (
	"fmt"
	"time"

	"kansho/refreshpool"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
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
// scrapes are in flight plus the running/queued counts. While the pool has
// work the readout is clickable and opens a dialog listing every domain
// currently being refreshed.
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

	// Refresh dialog state; mutations happen on the UI goroutine.
	window        fyne.Window // needed to show the refresh dialog
	curStatus     refreshpool.Status
	refreshArea   *tapArea // wraps spinner+poolStatus; clickable while busy
	refreshDialog *dialog.CustomDialog
	domainLabels  []*widget.Label // one row per site, in display order
	dotsLabels    []*widget.Label // dots indicator of the matching row
	rowsBox       *fyne.Container
	dialogSize    fyne.Size // last size applied to refreshDialog
	dotsTicker    *time.Ticker
	dotsDone      chan struct{}
	dotsFrame     int
}

// SetWindow provides the window used for the refresh-status dialog. Must be
// called before taps should open dialogs; without it the click target stays
// inert.
func (s *MainStatusBar) SetWindow(w fyne.Window) {
	s.window = w
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

	// While the pool is busy the whole readout becomes a click target that
	// opens the refresh dialog; idle it is inert (no cursor, no dialog).
	right := container.NewHBox(s.spinner, s.poolStatus)
	s.refreshArea = newTapArea(right, s.showRefreshDialog)

	statusRow := container.NewBorder(nil, nil, s.badge,
		s.refreshArea, s.message)

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
const refreshPoolIdleText = "⟳ Chapter Refresh: idle"

// refreshSpinnerFrames is the classic bash-style spinner sequence, cycled
// while the refresh pool has work in flight.
var refreshSpinnerFrames = []string{"|", "/", "-", "\\"}

// poolSpinnerInterval is how often the spinner advances its frame.
const poolSpinnerInterval = 120 * time.Millisecond

// SetRefreshPoolStatus shows the single representation of the chapter-list
// refresh pool: an animated spinner while scrapes are in flight plus
// how many are running across sites and how many tasks are still queued.
// While busy the readout is clickable; when idle any open refresh dialog is
// closed again. Must be called on the UI goroutine.
func (s *MainStatusBar) SetRefreshPoolStatus(status refreshpool.Status) {
	s.curStatus = status
	if status.IsIdle() {
		s.refreshArea.SetClickable(false)
		s.stopPoolSpinner()
		s.poolStatus.SetText(refreshPoolIdleText)
		s.closeRefreshDialog()
		return
	}
	s.refreshArea.SetClickable(true)
	s.startPoolSpinner()
	s.poolStatus.SetText(fmt.Sprintf("Chapter Refresh: %d running · %d queued", status.Running, status.Queued))

	// Keep an already open dialog current as sites start and finish.
	if s.refreshDialog != nil {
		s.rebuildDomainRows(status.Sites)
	}
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

// refreshDotsFrames is the animated "ongoing" indicator shown in the refresh
// dialog, cycling one to three dots.
var refreshDotsFrames = []string{".", "..", "..."}

// dotsInterval is how often the dialog's dot indicator advances its frame.
const dotsInterval = 400 * time.Millisecond

// showRefreshDialog opens a dialog listing every domain that currently has a
// chapter-list refresh in flight, each row showing the domain on the left and
// its animated dot indicator on the right. The dialog is sized to fit its
// content (capped to the window). It is a no-op while idle or if the dialog
// is already open. Must be called on the UI goroutine (the tapArea callback).
func (s *MainStatusBar) showRefreshDialog() {
	if s.window == nil || s.curStatus.IsIdle() || s.refreshDialog != nil {
		return
	}

	s.rowsBox = container.NewVBox()
	s.rebuildDomainRows(s.curStatus.Sites)

	content := container.NewVBox(
		widget.NewLabel("Refreshing chapter lists for:"),
		s.rowsBox,
	)

	d := dialog.NewCustom("Chapter Refresh", "Close", content, s.window)
	d.SetOnClosed(func() {
		s.stopDots()
		s.refreshDialog = nil
		s.domainLabels = nil
		s.dotsLabels = nil
	})
	s.refreshDialog = d

	s.startDots()
	d.Show()
	s.sizeRefreshDialog()
}

// rebuildDomainRows replaces the dialog's domain rows: one line per site with
// the domain name on the left and that domain's dot indicator on the right.
func (s *MainStatusBar) rebuildDomainRows(sites []string) {
	s.domainLabels = nil
	s.dotsLabels = nil
	s.rowsBox.Objects = nil
	for _, site := range sites {
		domain := widget.NewLabel(site)
		dots := widget.NewLabel(refreshDotsFrames[0])
		dots.TextStyle = fyne.TextStyle{Monospace: true}
		s.domainLabels = append(s.domainLabels, domain)
		s.dotsLabels = append(s.dotsLabels, dots)
		s.rowsBox.Add(container.NewBorder(nil, nil, nil, dots, domain))
	}
	s.rowsBox.Refresh()

	if s.refreshDialog != nil {
		s.sizeRefreshDialog()
	}
}

// sizeRefreshDialog grows the dialog so the longest domain and every row fit
// without truncation, capped to the window size so an unusually long list
// cannot overflow the screen.
func (s *MainStatusBar) sizeRefreshDialog() {
	if s.refreshDialog == nil || s.rowsBox == nil {
		return
	}

	canvasSize := s.window.Canvas().Size()
	canvasW, canvasH := canvasSize.Width, canvasSize.Height
	if canvasW <= 0 { // not laid out yet (e.g. in tests)
		canvasW, canvasH = 900, 700
	}

	rowsMin := s.rowsBox.MinSize()
	w := rowsMin.Width + 96   // dialog padding, title bar margins
	h := rowsMin.Height + 170 // title bar, dismiss button, header label, padding
	if maxW := canvasW * 0.95; w > maxW {
		w = maxW
	}
	if maxH := canvasH * 0.9; h > maxH {
		h = maxH
	}
	if w < 380 {
		w = 380
	}
	s.dialogSize = fyne.NewSize(w, h)
	s.refreshDialog.Resize(s.dialogSize)
}

// closeRefreshDialog hides the open refresh dialog, if any; the SetOnClosed
// callback stops the animation and clears the reference.
func (s *MainStatusBar) closeRefreshDialog() {
	if s.refreshDialog == nil {
		return
	}
	s.refreshDialog.Hide()
}

// startDots begins cycling the . / .. / ... frames on every domain row's
// indicator. Must be called on the UI goroutine.
func (s *MainStatusBar) startDots() {
	if s.dotsTicker != nil {
		return
	}

	// Show the first frame immediately instead of waiting for the first tick.
	s.dotsFrame = 0
	s.setDotsFrames(refreshDotsFrames[0])

	ticker := time.NewTicker(dotsInterval)
	done := make(chan struct{})
	s.dotsTicker = ticker
	s.dotsDone = done

	go func() {
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				// Widget updates must happen on the UI thread; frame state is
				// only ever mutated inside fyne.Do, keeping it race-free.
				fyne.Do(func() {
					if s.dotsTicker == nil { // stopped between tick and callback
						return
					}
					s.dotsFrame = (s.dotsFrame + 1) % len(refreshDotsFrames)
					s.setDotsFrames(refreshDotsFrames[s.dotsFrame])
				})
			}
		}
	}()
}

// setDotsFrames shows frame on every current domain row's dot indicator.
// Must be called on the UI goroutine.
func (s *MainStatusBar) setDotsFrames(frame string) {
	for _, dots := range s.dotsLabels {
		dots.SetText(frame)
	}
}

// stopDots halts the dialog's dot animation. Must be called on the UI
// goroutine.
func (s *MainStatusBar) stopDots() {
	if s.dotsTicker == nil {
		return
	}
	close(s.dotsDone)
	s.dotsTicker.Stop()
	s.dotsTicker = nil
	s.dotsDone = nil
}
