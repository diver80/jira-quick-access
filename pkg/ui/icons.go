package ui

import (
	"math"

	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/widget"
)

// The embedded Inter font has no "✕", "⚙" or "▾"; these icons are drawn instead.

// drawCloseIcon draws a cross of the given size centred in r.
func drawCloseIcon(canvas widget.Canvas, r geometry.Rect, size float32, color widget.Color) {
	c := r.Center()
	h := size / 2
	canvas.DrawLine(geometry.Pt(c.X-h, c.Y-h), geometry.Pt(c.X+h, c.Y+h), color, 1.6)
	canvas.DrawLine(geometry.Pt(c.X-h, c.Y+h), geometry.Pt(c.X+h, c.Y-h), color, 1.6)
}

// drawGearIcon draws a settings gear (ring with eight teeth) centred in r.
func drawGearIcon(canvas widget.Canvas, r geometry.Rect, radius float32, color widget.Color) {
	c := r.Center()
	canvas.StrokeCircle(c, radius*0.62, color, radius*0.36)
	for i := 0; i < 8; i++ {
		a := float64(i) * math.Pi / 4
		dx, dy := float32(math.Cos(a)), float32(math.Sin(a))
		canvas.DrawLine(geometry.Pt(c.X+dx*radius*0.7, c.Y+dy*radius*0.7), geometry.Pt(c.X+dx*radius, c.Y+dy*radius), color, radius*0.42)
	}
}

// drawChevronDown draws a small downward chevron centred in r.
func drawChevronDown(canvas widget.Canvas, r geometry.Rect, size float32, color widget.Color) {
	c := r.Center()
	h := size / 2
	canvas.DrawLine(geometry.Pt(c.X-h, c.Y-h/2), geometry.Pt(c.X, c.Y+h/2), color, 1.4)
	canvas.DrawLine(geometry.Pt(c.X, c.Y+h/2), geometry.Pt(c.X+h, c.Y-h/2), color, 1.4)
}
