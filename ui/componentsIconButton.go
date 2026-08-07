package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// iconButton is a plain icon (no button chrome and no hover highlight) that can
// be tapped. It is used for the per-chapter download arrow and the red
// circle-with-slash action so the chapter row controls stay visually quiet,
// especially while a download is running. Unlike a widget.Button it does not
// implement desktop.Hoverable, so hovering over it never changes how it is
// rendered.
type iconButton struct {
	widget.BaseWidget

	Resource fyne.Resource
	OnTapped func()
	enabled  bool
}

// newIconButton creates a tappable, non-hoverable icon button.
func newIconButton(res fyne.Resource) *iconButton {
	b := &iconButton{Resource: res, enabled: true}
	b.ExtendBaseWidget(b)
	return b
}

// Tapped triggers the button action when the icon is clicked.
func (b *iconButton) Tapped(*fyne.PointEvent) {
	if b.enabled && b.OnTapped != nil {
		b.OnTapped()
	}
}

// Disable greys out the icon and stops it responding to taps.
func (b *iconButton) Disable() {
	b.enabled = false
	b.Refresh()
}

// Enable restores the icon and allows it to be tapped again.
func (b *iconButton) Enable() {
	b.enabled = true
	b.Refresh()
}

// Disabled returns true if the icon is disabled.
func (b *iconButton) Disabled() bool {
	return !b.enabled
}

// MinSize returns the size this widget should not shrink below.
func (b *iconButton) MinSize() fyne.Size {
	b.ExtendBaseWidget(b)
	return b.BaseWidget.MinSize()
}

// CreateRenderer links this widget to its renderer.
func (b *iconButton) CreateRenderer() fyne.WidgetRenderer {
	raster := canvas.NewImageFromResource(b.Resource)
	raster.FillMode = canvas.ImageFillContain
	return &iconButtonRenderer{icon: b, raster: raster}
}

type iconButtonRenderer struct {
	raster *canvas.Image
	icon   *iconButton
}

func (r *iconButtonRenderer) Destroy() {}

func (r *iconButtonRenderer) Layout(size fyne.Size) {
	iconSize := r.icon.Theme().Size(theme.SizeNameInlineIcon)
	r.raster.Move(fyne.NewPos((size.Width-iconSize)/2, (size.Height-iconSize)/2))
	r.raster.Resize(fyne.NewSquareSize(iconSize))
}

func (r *iconButtonRenderer) MinSize() fyne.Size {
	// A square click target around the icon itself.
	return fyne.NewSquareSize(32)
}

func (r *iconButtonRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.raster}
}

func (r *iconButtonRenderer) Refresh() {
	res := r.icon.Resource
	if !r.icon.enabled && res != nil {
		res = theme.NewDisabledResource(res)
	}
	if r.raster.Resource != res {
		r.raster.Resource = res
		r.raster.Refresh()
	}
}
