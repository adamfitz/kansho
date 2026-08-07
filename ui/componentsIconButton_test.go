package ui

import (
	"testing"

	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
)

// TestIconButtonHasNoHoverHighlight verifies that the row action icons do not
// react to hover, so they never highlight while a download is running. This is
// in contrast to widget.Button which implements desktop.Hoverable.
func TestIconButtonHasNoHoverHighlight(t *testing.T) {
	app := test.NewApp()
	w := test.NewWindow(nil)
	t.Cleanup(func() {
		w.Close()
		app.Quit()
	})

	btn := newIconButton(theme.DownloadIcon())
	if _, ok := interface{}(btn).(desktop.Hoverable); ok {
		t.Fatal("iconButton must not implement desktop.Hoverable (no hover highlight)")
	}
}

// TestIconButtonTapDisabled verifies that an iconButton only fires its action
// when it is enabled.
func TestIconButtonTapDisabled(t *testing.T) {
	tapped := 0
	btn := newIconButton(theme.DownloadIcon())
	btn.OnTapped = func() { tapped++ }

	btn.Tapped(nil)
	if tapped != 1 {
		t.Fatalf("expected the enabled icon to fire once, got %d", tapped)
	}

	btn.Disable()
	btn.Tapped(nil)
	if tapped != 1 {
		t.Fatalf("expected the disabled icon to ignore taps, got %d taps", tapped)
	}
	if !btn.Disabled() {
		t.Error("expected the icon to be disabled")
	}

	btn.Enable()
	btn.Tapped(nil)
	if tapped != 2 {
		t.Fatalf("expected the re-enabled icon to fire again, got %d taps", tapped)
	}
	if btn.Disabled() {
		t.Error("expected the icon to be enabled again")
	}
}

// TestIconButtonRendersIcon verifies that the iconButton renders its resource
// through a canvas image, exactly like the plain icon row controls.
func TestIconButtonRendersIcon(t *testing.T) {
	btn := newIconButton(greenTickResource)
	renderer := test.WidgetRenderer(btn)
	objs := renderer.Objects()
	if len(objs) != 1 {
		t.Fatalf("expected 1 rendered object, got %d", len(objs))
	}
	img, ok := objs[0].(*canvas.Image)
	if !ok {
		t.Fatalf("expected a canvas.Image, got %T", objs[0])
	}
	if img.Resource.Name() != greenTickResource.Name() {
		t.Errorf("expected the rendered icon to use %s, got %s", greenTickResource.Name(), img.Resource.Name())
	}
}
