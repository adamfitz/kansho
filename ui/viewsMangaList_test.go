package ui

import (
	"image/color"
	"testing"

	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
)

// TestHoverLabelHideTooltipPreservesDialogOverlay is a regression test for the
// cf Challenge dialog disappearing when the browser window opens.
//
// The hover tooltip and modal dialogs share the window's overlay stack. Fyne's
// OverlayStack.Remove removes the removed overlay AND everything above it, so
// hiding a tooltip that sits below an open dialog used to silently destroy the
// dialog. hideTooltip must preserve any overlays above the tooltip.
func TestHoverLabelHideTooltipPreservesDialogOverlay(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()

	w := test.NewWindow(nil)
	defer w.Close()

	h := newHoverLabel("Test", "tooltip", w)

	// Simulate the tooltip being shown and added to the overlay stack.
	h.tooltipContainer = container.NewWithoutLayout(canvas.NewRectangle(color.Black))
	h.window.Canvas().Overlays().Add(h.tooltipContainer)
	h.overlayShown = true

	// Simulate a modal dialog being opened on top of the tooltip.
	dialogOverlay := canvas.NewRectangle(color.RGBA{255, 0, 0, 255})
	w.Canvas().Overlays().Add(dialogOverlay)

	// Hiding the tooltip must NOT remove the dialog overlay above it.
	h.hideTooltip()

	found := false
	for _, o := range w.Canvas().Overlays().List() {
		if o == dialogOverlay {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("dialog overlay was removed from the overlay stack when the tooltip was hidden")
	}

	// The tooltip itself SHALL be removed from the overlay stack.
	for _, o := range w.Canvas().Overlays().List() {
		if o == h.tooltipContainer {
			t.Fatal("tooltip container should have been removed from the overlay stack")
		}
	}
}
