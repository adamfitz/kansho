package ui

import (
	"image/color"
	"math"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Number of radial ticks that make up the spinner ring.
const spinnerSegments = 24

// Number of ticks in the rotating bright arc.
const spinnerArcTicks = 7

// ajaxSpinner is a custom AJAX-style loading spinner: a ring of short radial
// ticks with a bright arc that rotates around the ring, like the classic web
// "loading" spinner.
type ajaxSpinner struct {
	widget.BaseWidget
	segments []*canvas.Line
	started  bool
	anim     *fyne.Animation
	col      color.NRGBA
}

// newAJAXSpinner creates a new AJAX-style loading spinner.
func newAJAXSpinner() *ajaxSpinner {
	s := &ajaxSpinner{}
	s.ExtendBaseWidget(s)
	return s
}

// Start begins the spinner animation.
func (s *ajaxSpinner) Start() {
	if s.started {
		return
	}
	s.started = true
	s.Refresh()
}

// Stop halts the spinner animation.
func (s *ajaxSpinner) Stop() {
	if !s.started {
		return
	}
	s.started = false
	s.Refresh()
}

// CreateRenderer builds the radial ticks that form the spinner ring.
func (s *ajaxSpinner) CreateRenderer() fyne.WidgetRenderer {
	v := fyne.CurrentApp().Settings().ThemeVariant()
	s.col = color.NRGBAModel.Convert(s.Theme().Color(theme.ColorNameForeground, v)).(color.NRGBA)

	segments := make([]*canvas.Line, spinnerSegments)
	for i := range segments {
		line := canvas.NewLine(color.NRGBA{R: s.col.R, G: s.col.G, B: s.col.B, A: 24})
		line.StrokeWidth = 4
		segments[i] = line
	}
	s.segments = segments

	r := &ajaxSpinnerRenderer{spinner: s}
	s.anim = &fyne.Animation{
		Duration:    time.Second,
		RepeatCount: fyne.AnimationRepeatForever,
		Tick:        r.animate,
	}

	if s.started {
		r.animStart()
	}

	return r
}

var _ fyne.WidgetRenderer = (*ajaxSpinnerRenderer)(nil)

type ajaxSpinnerRenderer struct {
	spinner   *ajaxSpinner
	animating bool
}

// Layout positions every radial tick so they form a ring around the centre.
func (r *ajaxSpinnerRenderer) Layout(size fyne.Size) {
	radius := float32(fyne.Min(size.Width, size.Height))
	centre := fyne.NewPos(size.Width/2, size.Height/2)
	inner := radius * 0.3
	outer := radius * 0.46

	for i, seg := range r.spinner.segments {
		angle := float64(i) * 2 * math.Pi / spinnerSegments
		cos, sin := float32(math.Cos(angle)), float32(math.Sin(angle))
		seg.Position1 = centre.Add(fyne.NewPos(cos*inner, sin*inner))
		seg.Position2 = centre.Add(fyne.NewPos(cos*outer, sin*outer))
	}
}

// MinSize keeps the spinner a small, clearly visible square.
func (r *ajaxSpinnerRenderer) MinSize() fyne.Size {
	return fyne.NewSquareSize(30)
}

func (r *ajaxSpinnerRenderer) Objects() []fyne.CanvasObject {
	out := make([]fyne.CanvasObject, len(r.spinner.segments))
	for i, seg := range r.spinner.segments {
		out[i] = seg
	}
	return out
}

func (r *ajaxSpinnerRenderer) Refresh() {
	if r.spinner.started {
		r.animStart()
	} else {
		r.animStop()
	}
}

func (r *ajaxSpinnerRenderer) Destroy() {
	r.spinner.started = false
	r.animStop()
}

// animate rotates the bright arc around the ring. The head tick is brightest and
// the ticks behind it fade out to the faint ring colour.
func (r *ajaxSpinnerRenderer) animate(done float32) {
	head := int(done * spinnerSegments)
	for i, seg := range r.spinner.segments {
		dist := head - i
		if dist < 0 {
			dist += spinnerSegments
		}

		var alpha uint8
		switch {
		case dist == 0:
			alpha = 255
		case dist < spinnerArcTicks:
			alpha = 230 - uint8(dist)*32
		default:
			alpha = 24
		}

		col := r.spinner.col
		col.A = alpha
		seg.StrokeColor = col
		seg.Refresh()
	}
}

func (r *ajaxSpinnerRenderer) animStart() {
	if r.animating {
		return
	}
	r.animating = true
	r.spinner.anim.Start()
}

func (r *ajaxSpinnerRenderer) animStop() {
	if !r.animating {
		return
	}
	r.animating = false
	r.spinner.anim.Stop()
}
