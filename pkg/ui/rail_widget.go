package ui

import (
	"time"

	"jira-quick-access/pkg/jira"

	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/widget"
)

// RailWidget implements the 36px collapsed edge rail view.
type RailWidget struct {
	widget.WidgetBase
	issues       []jira.Issue
	onExpand     func()
	onOpenConfig func()
	isHovered    bool
	hoverEntered time.Time
	btnConfig    *GlassButton
	btnExpand    *GlassButton
}

func NewRailWidget(
	issues []jira.Issue,
	onExpand func(),
	onOpenConfig func(),
) *RailWidget {
	r := &RailWidget{
		issues:       issues,
		onExpand:     onExpand,
		onOpenConfig: onOpenConfig,
	}

	r.btnConfig = NewGlassButton("⚙", onOpenConfig).SetCompact(true)
	r.btnExpand = NewGlassButton("🔍", onExpand).SetCompact(true)

	r.AddChild(r.btnConfig)
	r.AddChild(r.btnExpand)

	r.SetVisible(true)
	r.SetEnabled(true)
	return r
}

func (r *RailWidget) UpdateIssues(issues []jira.Issue) {
	r.issues = issues
	r.MarkNeedsLayout()
}

func (r *RailWidget) Layout(ctx widget.Context, constraints geometry.Constraints) geometry.Size {
	width := float32(36)
	height := constraints.MaxHeight
	if height <= 0 {
		height = 500
	}

	b := r.Bounds()
	r.btnConfig.Layout(ctx, geometry.Tight(geometry.Sz(28, 26)))
	r.btnConfig.SetBounds(geometry.NewRect(b.Min.X+4, b.Min.Y+8, 28, 26))

	r.btnExpand.Layout(ctx, geometry.Tight(geometry.Sz(28, 26)))
	r.btnExpand.SetBounds(geometry.NewRect(b.Min.X+4, b.Min.Y+height-34, 28, 26))

	return constraints.Constrain(geometry.Sz(width, height))
}

func (r *RailWidget) Draw(ctx widget.Context, canvas widget.Canvas) {
	b := r.Bounds()
	radius := float32(8)

	// Draw obsidian rail background
	canvas.DrawRoundRect(b, ColorGlassBgDeep, radius)
	canvas.StrokeRoundRect(b, ColorBorderSubtle, radius, 1.0)

	// Draw top specular glow line
	canvas.DrawLine(
		geometry.Pt(b.Min.X+4, b.Min.Y+1),
		geometry.Pt(b.Min.X+b.Width()-4, b.Min.Y+1),
		ColorGlowTop,
		1.0,
	)

	// Draw config button
	r.btnConfig.Draw(ctx, canvas)

	// Draw vertical status dots for active tickets
	startY := b.Min.Y + 48
	maxDots := 10
	count := 0

	for _, iss := range r.issues {
		if count >= maxDots {
			break
		}
		fg, _, glow := GetStatusColors(iss.Status.CategoryKey, iss.Status.Name)
		center := geometry.Pt(b.Min.X+b.Width()/2, startY+float32(count*22))

		// Radiant halo glow
		canvas.DrawCircle(center, 5.0, glow)
		// Crisp status center
		canvas.DrawCircle(center, 3.0, fg)
		count++
	}

	// Draw bottom expand trigger
	r.btnExpand.Draw(ctx, canvas)
}

func (r *RailWidget) Event(ctx widget.Context, e event.Event) bool {
	if r.btnConfig.Event(ctx, e) {
		return true
	}
	if r.btnExpand.Event(ctx, e) {
		return true
	}

	if me, ok := e.(*event.MouseEvent); ok {
		contains := r.Bounds().Contains(me.Position)
		if me.MouseType == event.MouseMove {
			if contains && !r.isHovered {
				r.isHovered = true
				r.hoverEntered = time.Now()
			} else if !contains && r.isHovered {
				r.isHovered = false
			}

			// Hover expansion debounce check (>150ms)
			if r.isHovered && time.Since(r.hoverEntered) > 150*time.Millisecond {
				if r.onExpand != nil {
					r.onExpand()
					return true
				}
			}
		} else if me.MouseType == event.MousePress && contains {
			if r.onExpand != nil {
				r.onExpand()
				return true
			}
		}
	}
	return false
}
