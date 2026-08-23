package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// tapArea wraps content so it can act as a click target. Taps are forwarded
// to onTapped only while clickable is true, and a pointer cursor signals the
// affordance; while not clickable the area is inert and looks passive.
type tapArea struct {
	widget.BaseWidget
	content   fyne.CanvasObject
	onTapped  func()
	clickable bool
}

// newTapArea builds the wrapper around content; onTapped fires on clicks
// while the area is clickable.
func newTapArea(content fyne.CanvasObject, onTapped func()) *tapArea {
	t := &tapArea{content: content, onTapped: onTapped}
	t.ExtendBaseWidget(t)
	return t
}

// SetClickable turns tap handling (and the pointer cursor) on or off.
func (t *tapArea) SetClickable(on bool) {
	if t.clickable == on {
		return
	}
	t.clickable = on
	t.Refresh()
}

// Tapped implements fyne.Tappable.
func (t *tapArea) Tapped(_ *fyne.PointEvent) {
	if t.clickable && t.onTapped != nil {
		t.onTapped()
	}
}

// Cursor implements fyne.Cursorable.
func (t *tapArea) Cursor() desktop.Cursor {
	if t.clickable {
		return desktop.PointerCursor
	}
	return desktop.DefaultCursor
}

func (t *tapArea) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(t.content)
}
