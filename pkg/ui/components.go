package ui

import (
	"fmt"
	"time"

	"jira-quick-access/pkg/jira"

	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/widget"
)

// StatusDotWidget renders a glowing status dot with outer halo.
type StatusDotWidget struct {
	widget.WidgetBase
	color widget.Color
	glow  widget.Color
	pulse bool
}

func NewStatusDot(fg, glow widget.Color) *StatusDotWidget {
	d := &StatusDotWidget{
		color: fg,
		glow:  glow,
	}
	d.SetVisible(true)
	d.SetEnabled(true)
	return d
}

func (d *StatusDotWidget) Layout(ctx widget.Context, c geometry.Constraints) geometry.Size {
	return c.Constrain(geometry.Sz(16, 16))
}

func (d *StatusDotWidget) Draw(ctx widget.Context, canvas widget.Canvas) {
	b := d.Bounds()
	center := geometry.Pt(b.Min.X+b.Width()/2, b.Min.Y+b.Height()/2)

	// Draw outer radiant glow
	canvas.DrawCircle(center, 6.0, d.glow)
	// Draw crisp inner core dot
	canvas.DrawCircle(center, 3.5, d.color)
}

func (d *StatusDotWidget) Event(ctx widget.Context, e event.Event) bool {
	return false
}

func (d *StatusDotWidget) Children() []widget.Widget {
	return nil
}

// GlassButton is an interactive liquid-glass pill button with hover states.
type GlassButton struct {
	widget.WidgetBase
	text         string
	icon         string
	onClick      func()
	isHovered    bool
	isPressed    bool
	customBg     *widget.Color
	customFg     *widget.Color
	customBorder *widget.Color
	compact      bool
}

func NewGlassButton(text string, onClick func()) *GlassButton {
	b := &GlassButton{
		text:    text,
		onClick: onClick,
	}
	b.SetVisible(true)
	b.SetEnabled(true)
	return b
}

func (b *GlassButton) SetCompact(compact bool) *GlassButton {
	b.compact = compact
	return b
}

func (b *GlassButton) SetCustomColors(fg, bg, border widget.Color) *GlassButton {
	b.customFg = &fg
	b.customBg = &bg
	b.customBorder = &border
	return b
}

func (b *GlassButton) Layout(ctx widget.Context, c geometry.Constraints) geometry.Size {
	width := float32(len(b.text)*7 + 20)
	if width < 36 {
		width = 36
	}
	height := float32(26)
	if b.compact {
		height = 22
		width = float32(len(b.text)*6 + 14)
	}
	return c.Constrain(geometry.Sz(width, height))
}

func (b *GlassButton) Draw(ctx widget.Context, canvas widget.Canvas) {
	bounds := b.Bounds()
	radius := float32(6)

	bg := ColorPillBg
	border := ColorPillBorder
	fg := ColorTextPrimary

	if b.customBg != nil {
		bg = *b.customBg
	}
	if b.customBorder != nil {
		border = *b.customBorder
	}
	if b.customFg != nil {
		fg = *b.customFg
	}

	if b.isHovered {
		bg = ColorPillBgHover
		if b.customBg != nil {
			bg = widget.RGBA(b.customBg.R*1.15, b.customBg.G*1.15, b.customBg.B*1.15, b.customBg.A)
		}
		border = ColorBorderHover
	}
	if b.isPressed {
		bg = ColorGlassCardAct
	}

	// Draw rounded button surface
	canvas.DrawRoundRect(bounds, bg, radius)
	canvas.StrokeRoundRect(bounds, border, radius, 1.0)

	// Draw button text centered
	fontSize := float32(11)
	if b.compact {
		fontSize = 10
	}
	textBounds := geometry.NewRect(bounds.Min.X+4, bounds.Min.Y+4, bounds.Width()-8, bounds.Height()-8)
	canvas.DrawText(b.text, textBounds, fontSize, fg, false, widget.TextAlignCenter)
}

func (b *GlassButton) Event(ctx widget.Context, e event.Event) bool {
	switch ev := e.(type) {
	case *event.MouseEvent:
		contains := b.Bounds().Contains(ev.Position)
		if ev.MouseType == event.MouseMove {
			if contains != b.isHovered {
				b.isHovered = contains
				b.MarkNeedsLayout()
				return true
			}
		} else if ev.MouseType == event.MousePress && contains {
			b.isPressed = true
			if b.onClick != nil {
				b.onClick()
			}
			return true
		} else if ev.MouseType == event.MouseRelease {
			if b.isPressed {
				b.isPressed = false
				return true
			}
		}
	}
	return false
}

func (b *GlassButton) Children() []widget.Widget {
	return nil
}

// IssueCardWidget renders a single Jira issue card in the glassmorphism panel.
type IssueCardWidget struct {
	widget.WidgetBase
	issue         jira.Issue
	isHovered     bool
	onTransition  func(issueKey string)
	onCopyBranch  func(issue jira.Issue)
	onCopyKey     func(key string)
	onTogglePin   func(key string)
	onOpenBrowser func(url string)
	btnStatus     *GlassButton
	btnBranch     *GlassButton
	btnKey        *GlassButton
	btnPin        *GlassButton
	btnBrowser    *GlassButton
}

func NewIssueCard(
	issue jira.Issue,
	onTransition func(issueKey string),
	onCopyBranch func(issue jira.Issue),
	onCopyKey func(key string),
	onTogglePin func(key string),
	onOpenBrowser func(url string),
) *IssueCardWidget {
	card := &IssueCardWidget{
		issue:         issue,
		onTransition:  onTransition,
		onCopyBranch:  onCopyBranch,
		onCopyKey:     onCopyKey,
		onTogglePin:   onTogglePin,
		onOpenBrowser: onOpenBrowser,
	}

	fg, bg, border := GetStatusColors(issue.Status.CategoryKey, issue.Status.Name)
	card.btnStatus = NewGlassButton(fmt.Sprintf("%s ▾", issue.Status.Name), func() {
		if onTransition != nil {
			onTransition(issue.Key)
		}
	}).SetCompact(true)
	card.btnStatus.SetCustomColors(fg, bg, border)

	card.btnBranch = NewGlassButton("📋 Branch", func() {
		if onCopyBranch != nil {
			onCopyBranch(issue)
		}
	}).SetCompact(true)

	card.btnKey = NewGlassButton("📋 Key", func() {
		if onCopyKey != nil {
			onCopyKey(issue.Key)
		}
	}).SetCompact(true)

	pinText := "📌"
	if issue.Pinned {
		pinText = "📍"
	}
	card.btnPin = NewGlassButton(pinText, func() {
		if onTogglePin != nil {
			onTogglePin(issue.Key)
		}
	}).SetCompact(true)

	card.btnBrowser = NewGlassButton("↗", func() {
		if onOpenBrowser != nil {
			onOpenBrowser(issue.URL)
		}
	}).SetCompact(true)

	card.AddChild(card.btnStatus)
	card.AddChild(card.btnBranch)
	card.AddChild(card.btnKey)
	card.AddChild(card.btnPin)
	card.AddChild(card.btnBrowser)

	card.SetVisible(true)
	card.SetEnabled(true)
	return card
}

func (c *IssueCardWidget) Layout(ctx widget.Context, constraints geometry.Constraints) geometry.Size {
	width := constraints.MaxWidth
	if width <= 0 {
		width = 336
	}
	height := float32(78)

	b := c.Bounds()
	topY := b.Min.Y

	// Position action buttons in the bottom row
	btnY := topY + 48
	currX := b.Min.X + 12

	statusSz := c.btnStatus.Layout(ctx, geometry.Loose(geometry.Sz(110, 22)))
	c.btnStatus.SetBounds(geometry.NewRect(currX, btnY, statusSz.Width, statusSz.Height))
	currX += statusSz.Width + 6

	branchSz := c.btnBranch.Layout(ctx, geometry.Loose(geometry.Sz(80, 22)))
	c.btnBranch.SetBounds(geometry.NewRect(currX, btnY, branchSz.Width, branchSz.Height))
	currX += branchSz.Width + 6

	keySz := c.btnKey.Layout(ctx, geometry.Loose(geometry.Sz(60, 22)))
	c.btnKey.SetBounds(geometry.NewRect(currX, btnY, keySz.Width, keySz.Height))

	// Pin & Browser buttons on far right
	browserSz := c.btnBrowser.Layout(ctx, geometry.Loose(geometry.Sz(24, 22)))
	c.btnBrowser.SetBounds(geometry.NewRect(b.Min.X+width-32, topY+10, browserSz.Width, 22))

	pinSz := c.btnPin.Layout(ctx, geometry.Loose(geometry.Sz(24, 22)))
	c.btnPin.SetBounds(geometry.NewRect(b.Min.X+width-60, topY+10, pinSz.Width, 22))

	return constraints.Constrain(geometry.Sz(width, height))
}

func (c *IssueCardWidget) Draw(ctx widget.Context, canvas widget.Canvas) {
	b := c.Bounds()
	radius := float32(10)

	// Card background & glass stroke
	cardBg := ColorGlassCard
	border := ColorBorderSubtle
	if c.isHovered {
		cardBg = ColorGlassCardHov
		border = ColorBorderHover
	}

	canvas.DrawRoundRect(b, cardBg, radius)
	canvas.StrokeRoundRect(b, border, radius, 1.0)

	// Status dot indicator
	fg, _, glow := GetStatusColors(c.issue.Status.CategoryKey, c.issue.Status.Name)
	dotCenter := geometry.Pt(b.Min.X+16, b.Min.Y+18)
	canvas.DrawCircle(dotCenter, 5.0, glow)
	canvas.DrawCircle(dotCenter, 3.0, fg)

	// Issue Key (Monospace style)
	keyBounds := geometry.NewRect(b.Min.X+28, b.Min.Y+10, 80, 16)
	canvas.DrawText(c.issue.Key, keyBounds, 12, ColorTextPrimary, true, widget.TextAlignLeft)

	// Issue Summary
	summaryBounds := geometry.NewRect(b.Min.X+16, b.Min.Y+28, b.Width()-32, 16)
	canvas.DrawText(c.issue.Summary, summaryBounds, 11, ColorTextSecondary, false, widget.TextAlignLeft)

	// Draw child buttons
	for _, child := range c.Children() {
		child.Draw(ctx, canvas)
	}
}

func (c *IssueCardWidget) Event(ctx widget.Context, e event.Event) bool {
	for _, child := range c.Children() {
		if child.Event(ctx, e) {
			return true
		}
	}

	if me, ok := e.(*event.MouseEvent); ok {
		contains := c.Bounds().Contains(me.Position)
		if me.MouseType == event.MouseMove {
			if contains != c.isHovered {
				c.isHovered = contains
				return true
			}
		}
	}
	return false
}

// ToastNotification represents a temporary floating notification pill.
type ToastNotification struct {
	Message   string
	CreatedAt time.Time
	Duration  time.Duration
}

func NewToast(msg string) ToastNotification {
	return ToastNotification{
		Message:   msg,
		CreatedAt: time.Now(),
		Duration:  3 * time.Second,
	}
}

func (t *ToastNotification) IsActive() bool {
	return t.Message != "" && time.Since(t.CreatedAt) < t.Duration
}
