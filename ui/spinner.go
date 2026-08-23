package ui

import (
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

// bashSpinner is a monospace label cycling through the classic bash-style
// |/-\ frames - the same animation the main window status bar shows while the
// chapter-list refresh pool has work in flight (see MainStatusBar).
//
// It reuses the status bar's frame sequence (refreshSpinnerFrames) and tick
// rate (poolSpinnerInterval) so both spinners look and feel identical.
type bashSpinner struct {
	widget.BaseWidget
	label *widget.Label

	// Animation state; mutations happen on the UI goroutine (ticker ticks are
	// marshalled back via fyne.Do), exactly like the status bar's spinner.
	ticker *time.Ticker
	done   chan struct{}
	frame  int
}

// newBashSpinner creates the spinner in its stopped state.
func newBashSpinner() *bashSpinner {
	s := &bashSpinner{}
	s.ExtendBaseWidget(s)
	s.label = widget.NewLabel("")
	s.label.TextStyle = fyne.TextStyle{Monospace: true}
	return s
}

// Start begins cycling the |/-\ frames. A no-op if already running. Must be
// called on the UI goroutine.
func (s *bashSpinner) Start() {
	if s.ticker != nil {
		return
	}

	// Show the first frame immediately instead of waiting for the first tick.
	s.frame = 0
	s.label.SetText(refreshSpinnerFrames[0])

	ticker := time.NewTicker(poolSpinnerInterval)
	done := make(chan struct{})
	s.ticker = ticker
	s.done = done

	go func() {
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				// Widget updates must happen on the UI thread; frame state is
				// only ever mutated inside fyne.Do, keeping it race-free.
				fyne.Do(func() {
					if s.ticker == nil { // stopped between tick and callback
						return
					}
					s.frame = (s.frame + 1) % len(refreshSpinnerFrames)
					s.label.SetText(refreshSpinnerFrames[s.frame])
				})
			}
		}
	}()
}

// Stop halts the animation and clears the glyph. Must be called on the UI
// goroutine.
func (s *bashSpinner) Stop() {
	if s.ticker == nil {
		return
	}
	close(s.done)
	s.ticker.Stop()
	s.ticker = nil
	s.done = nil
	s.label.SetText("")
}

func (s *bashSpinner) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(s.label)
}
