package ui

import (
	"fmt"
	"strings"
	"time"

	"jira-quick-access/pkg/jira"

	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/widget"
)

// PanelWidget implements the expanded 360px liquid-glass Jira drawer.
type PanelWidget struct {
	widget.WidgetBase
	issues        []jira.Issue
	history       []jira.TransitionHistory
	pinned        bool
	searchQuery   string
	searchFocused bool
	activeFilter  string // "all", "inprogress", "pinned", "assigned"
	scrollY       float32
	lastSynced    time.Time
	isSyncing     bool

	// Callbacks
	onCollapse     func()
	onOpenSettings func()
	onRefresh      func()
	onTransition   func(issueKey string)
	onCopyBranch   func(issue jira.Issue)
	onCopyKey      func(key string)
	onTogglePin    func(key string)
	onOpenBrowser  func(url string)

	// Header buttons
	btnSettings  *GlassButton
	btnRefresh   *GlassButton
	btnPinToggle *GlassButton
	btnClose     *GlassButton

	// Filter tab buttons
	btnTabAll  *GlassButton
	btnTabProg *GlassButton
	btnTabPin  *GlassButton

	// Issue cards
	cards []*IssueCardWidget
}

func NewPanelWidget(
	issues []jira.Issue,
	history []jira.TransitionHistory,
	pinned bool,
	onCollapse func(),
	onOpenSettings func(),
	onRefresh func(),
	onTransition func(issueKey string),
	onCopyBranch func(issue jira.Issue),
	onCopyKey func(key string),
	onTogglePin func(key string),
	onOpenBrowser func(url string),
) *PanelWidget {
	p := &PanelWidget{
		issues:         issues,
		history:        history,
		pinned:         pinned,
		activeFilter:   "all",
		lastSynced:     time.Now(),
		onCollapse:     onCollapse,
		onOpenSettings: onOpenSettings,
		onRefresh:      onRefresh,
		onTransition:   onTransition,
		onCopyBranch:   onCopyBranch,
		onCopyKey:      onCopyKey,
		onTogglePin:    onTogglePin,
		onOpenBrowser:  onOpenBrowser,
	}

	p.btnSettings = NewGlassButton("⚙", onOpenSettings).SetCompact(true)
	p.btnRefresh = NewGlassButton("🔄", onRefresh).SetCompact(true)

	pinIcon := "📌"
	if pinned {
		pinIcon = "📍"
	}
	p.btnPinToggle = NewGlassButton(pinIcon, func() {
		p.pinned = !p.pinned
		if p.pinned {
			p.btnPinToggle.text = "📍"
		} else {
			p.btnPinToggle.text = "📌"
		}
		p.MarkNeedsLayout()
	}).SetCompact(true)

	p.btnClose = NewGlassButton("✕", onCollapse).SetCompact(true)

	p.btnTabAll = NewGlassButton("All", func() {
		p.activeFilter = "all"
		p.rebuildCards()
	}).SetCompact(true)

	p.btnTabProg = NewGlassButton("In Progress", func() {
		p.activeFilter = "inprogress"
		p.rebuildCards()
	}).SetCompact(true)

	p.btnTabPin = NewGlassButton("Pinned", func() {
		p.activeFilter = "pinned"
		p.rebuildCards()
	}).SetCompact(true)

	p.AddChild(p.btnSettings)
	p.AddChild(p.btnRefresh)
	p.AddChild(p.btnPinToggle)
	p.AddChild(p.btnClose)
	p.AddChild(p.btnTabAll)
	p.AddChild(p.btnTabProg)
	p.AddChild(p.btnTabPin)

	p.rebuildCards()

	p.SetVisible(true)
	p.SetEnabled(true)
	return p
}

func (p *PanelWidget) SetIssues(issues []jira.Issue, history []jira.TransitionHistory) {
	p.issues = issues
	p.history = history
	p.lastSynced = time.Now()
	p.isSyncing = false
	p.rebuildCards()
}

func (p *PanelWidget) SetSyncing(syncing bool) {
	p.isSyncing = syncing
	p.MarkNeedsLayout()
}

func (p *PanelWidget) rebuildCards() {
	// Remove old cards from children
	for _, card := range p.cards {
		p.RemoveChild(card)
	}
	p.cards = nil

	filtered := p.getFilteredIssues()
	for _, iss := range filtered {
		card := NewIssueCard(
			iss,
			p.onTransition,
			p.onCopyBranch,
			p.onCopyKey,
			p.onTogglePin,
			p.onOpenBrowser,
		)
		p.cards = append(p.cards, card)
		p.AddChild(card)
	}
	p.MarkNeedsLayout()
}

func (p *PanelWidget) getFilteredIssues() []jira.Issue {
	var result []jira.Issue
	query := strings.ToLower(strings.TrimSpace(p.searchQuery))

	for _, iss := range p.issues {
		// Filter by query
		if query != "" {
			matchKey := strings.Contains(strings.ToLower(iss.Key), query)
			matchSum := strings.Contains(strings.ToLower(iss.Summary), query)
			if !matchKey && !matchSum {
				continue
			}
		}

		// Filter by tab
		switch p.activeFilter {
		case "inprogress":
			if iss.Status.Name != "In Progress" {
				continue
			}
		case "pinned":
			if !iss.Pinned {
				continue
			}
		}

		result = append(result, iss)
	}
	return result
}

func (p *PanelWidget) Layout(ctx widget.Context, constraints geometry.Constraints) geometry.Size {
	width := float32(360)
	height := constraints.MaxHeight
	if height <= 0 {
		height = 680
	}

	b := p.Bounds()

	// Header action buttons
	p.btnSettings.Layout(ctx, geometry.Loose(geometry.Sz(26, 22)))
	p.btnSettings.SetBounds(geometry.NewRect(b.Min.X+12, b.Min.Y+12, 26, 22))

	p.btnRefresh.Layout(ctx, geometry.Loose(geometry.Sz(26, 22)))
	p.btnRefresh.SetBounds(geometry.NewRect(b.Min.X+width-94, b.Min.Y+12, 26, 22))

	p.btnPinToggle.Layout(ctx, geometry.Loose(geometry.Sz(26, 22)))
	p.btnPinToggle.SetBounds(geometry.NewRect(b.Min.X+width-64, b.Min.Y+12, 26, 22))

	p.btnClose.Layout(ctx, geometry.Loose(geometry.Sz(26, 22)))
	p.btnClose.SetBounds(geometry.NewRect(b.Min.X+width-34, b.Min.Y+12, 26, 22))

	// Filter tabs
	tabY := b.Min.Y + 76
	p.btnTabAll.Layout(ctx, geometry.Loose(geometry.Sz(48, 22)))
	p.btnTabAll.SetBounds(geometry.NewRect(b.Min.X+12, tabY, 48, 22))

	p.btnTabProg.Layout(ctx, geometry.Loose(geometry.Sz(84, 22)))
	p.btnTabProg.SetBounds(geometry.NewRect(b.Min.X+64, tabY, 84, 22))

	p.btnTabPin.Layout(ctx, geometry.Loose(geometry.Sz(64, 22)))
	p.btnTabPin.SetBounds(geometry.NewRect(b.Min.X+152, tabY, 64, 22))

	// Layout cards vertically
	cardY := b.Min.Y + 108 - p.scrollY
	cardWidth := width - 24

	for _, card := range p.cards {
		cardSz := card.Layout(ctx, geometry.TightWidth(cardWidth))
		card.SetBounds(geometry.NewRect(b.Min.X+12, cardY, cardWidth, cardSz.Height))
		cardY += cardSz.Height + 8
	}

	return constraints.Constrain(geometry.Sz(width, height))
}

func (p *PanelWidget) Draw(ctx widget.Context, canvas widget.Canvas) {
	b := p.Bounds()
	radius := float32(12)

	// Panel frosted obsidian background
	canvas.DrawRoundRect(b, ColorGlassBg, radius)
	canvas.StrokeRoundRect(b, ColorBorderSubtle, radius, 1.0)

	// Top spec highlight
	canvas.DrawLine(
		geometry.Pt(b.Min.X+6, b.Min.Y+1),
		geometry.Pt(b.Min.X+b.Width()-6, b.Min.Y+1),
		ColorGlowTop,
		1.0,
	)

	// App Header Title & Live Pulse Dot
	titleRect := geometry.NewRect(b.Min.X+44, b.Min.Y+14, 180, 18)
	canvas.DrawText("Jira Quick Access", titleRect, 13, ColorTextPrimary, true, widget.TextAlignLeft)

	// Sync status dot
	syncCenter := geometry.Pt(b.Min.X+168, b.Min.Y+23)
	if p.isSyncing {
		canvas.DrawCircle(syncCenter, 4.0, ColorStatusInReviewGlow)
		canvas.DrawCircle(syncCenter, 2.5, ColorStatusInReview)
	} else {
		canvas.DrawCircle(syncCenter, 4.0, ColorStatusInProgressGlow)
		canvas.DrawCircle(syncCenter, 2.5, ColorStatusInProgress)
	}

	// Search & Filter Box
	searchRect := geometry.NewRect(b.Min.X+12, b.Min.Y+42, b.Width()-24, 28)
	searchBg := ColorInputBg
	searchBorder := ColorBorderSubtle
	if p.searchFocused {
		searchBg = ColorInputFocused
		searchBorder = ColorBorderAccent
	}
	canvas.DrawRoundRect(searchRect, searchBg, 6)
	canvas.StrokeRoundRect(searchRect, searchBorder, 6, 1.0)

	// Search Icon & Text
	placeholder := "🔍 Filter or Search Issue... (⌘K)"
	if p.searchQuery != "" {
		placeholder = fmt.Sprintf("🔍 %s", p.searchQuery)
	}
	searchTxtRect := geometry.NewRect(searchRect.Min.X+8, searchRect.Min.Y+6, searchRect.Width()-16, 16)
	txtColor := ColorTextMuted
	if p.searchQuery != "" {
		txtColor = ColorTextPrimary
	}
	canvas.DrawText(placeholder, searchTxtRect, 11, txtColor, false, widget.TextAlignLeft)

	// Draw Header & Tab Buttons
	p.btnSettings.Draw(ctx, canvas)
	p.btnRefresh.Draw(ctx, canvas)
	p.btnPinToggle.Draw(ctx, canvas)
	p.btnClose.Draw(ctx, canvas)
	p.btnTabAll.Draw(ctx, canvas)
	p.btnTabProg.Draw(ctx, canvas)
	p.btnTabPin.Draw(ctx, canvas)

	// Draw Issue Cards
	for _, card := range p.cards {
		card.Draw(ctx, canvas)
	}

	// Recent activity log at the bottom if space allows
	if len(p.history) > 0 {
		histY := b.Min.Y + b.Height() - 40
		histBg := geometry.NewRect(b.Min.X+12, histY, b.Width()-24, 28)
		canvas.DrawRoundRect(histBg, ColorGlassCard, 6)
		canvas.StrokeRoundRect(histBg, ColorBorderSubtle, 6, 1.0)

		last := p.history[len(p.history)-1]
		msg := fmt.Sprintf("⚡ %s moved to %s (%s)", last.IssueKey, last.ToState, formatTimeAgo(last.Timestamp))
		msgRect := geometry.NewRect(histBg.Min.X+8, histBg.Min.Y+6, histBg.Width()-16, 16)
		canvas.DrawText(msg, msgRect, 10, ColorTextSecondary, false, widget.TextAlignLeft)
	}
}

func formatTimeAgo(t time.Time) string {
	d := time.Since(t)
	if d < time.Minute {
		return "just now"
	} else if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh ago", int(d.Hours()))
}

func (p *PanelWidget) Event(ctx widget.Context, e event.Event) bool {
	if p.btnSettings.Event(ctx, e) {
		return true
	}
	if p.btnRefresh.Event(ctx, e) {
		return true
	}
	if p.btnPinToggle.Event(ctx, e) {
		return true
	}
	if p.btnClose.Event(ctx, e) {
		return true
	}
	if p.btnTabAll.Event(ctx, e) {
		return true
	}
	if p.btnTabProg.Event(ctx, e) {
		return true
	}
	if p.btnTabPin.Event(ctx, e) {
		return true
	}

	for _, card := range p.cards {
		if card.Event(ctx, e) {
			return true
		}
	}

	switch ev := e.(type) {
	case *event.MouseEvent:
		if ev.MouseType == event.MousePress {
			searchRect := geometry.NewRect(p.Bounds().Min.X+12, p.Bounds().Min.Y+42, p.Bounds().Width()-24, 28)
			if searchRect.Contains(ev.Position) {
				p.searchFocused = true
				p.MarkNeedsLayout()
				return true
			}
			p.searchFocused = false
			p.MarkNeedsLayout()
		}
	case *event.KeyEvent:
		if ev.KeyType == event.KeyPress {
			if ev.Key == event.KeyEscape {
				if p.searchQuery != "" {
					p.searchQuery = ""
					p.searchFocused = false
					p.rebuildCards()
					return true
				}
				if p.onCollapse != nil {
					p.onCollapse()
					return true
				}
			}
			if p.searchFocused {
				if ev.Key == event.KeyBackspace {
					if len(p.searchQuery) > 0 {
						p.searchQuery = p.searchQuery[:len(p.searchQuery)-1]
						p.rebuildCards()
						return true
					}
				} else if ev.Rune >= 32 && ev.Rune <= 126 {
					p.searchQuery += string(ev.Rune)
					p.rebuildCards()
					return true
				}
			}
		}
	case *event.WheelEvent:
		p.scrollY -= ev.Delta.Y * 0.5
		if p.scrollY < 0 {
			p.scrollY = 0
		}
		p.MarkNeedsLayout()
		return true
	}

	return false
}
