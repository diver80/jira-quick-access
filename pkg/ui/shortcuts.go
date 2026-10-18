package ui

import (
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/widget"
)

// shortcut is one row of the read-only overview on the Shortcuts settings page.
// Each entry in keys is drawn as its own key cap.
type shortcut struct {
	keys  []string
	label string
}

type shortcutGroup struct {
	title string
	rows  []shortcut
}

// shortcutGroups mirrors handleKey; keep both in sync when adding a shortcut.
var shortcutGroups = []shortcutGroup{
	{"Ticket list & ticket view", []shortcut{
		{[]string{"↑", "↓"}, "Previous / next ticket"},
		{[]string{"Type"}, "Search tickets (fan)"},
		{[]string{"⌘K", "⌘F"}, "Focus search"},
		{[]string{"⌘B"}, "Open or close the board"},
		{[]string{"⌘O"}, "Open ticket in browser"},
		{[]string{"⌘C"}, "Copy ticket key"},
		{[]string{"⌘⇧C"}, "Copy Git branch name"},
		{[]string{"⌘R"}, "Refresh tickets now"},
		{[]string{"Esc", "⌘W"}, "Back or collapse"},
	}},
	{"Anywhere", []shortcut{
		{[]string{"⌥⌘J"}, "Ticket list + search"},
	}},
	{"Settings fields", []shortcut{
		{[]string{"Tab", "⇧Tab"}, "Next / previous field"},
		{[]string{"Enter"}, "Next field"},
		{[]string{"⌘A", "⌘C", "⌘V"}, "Select all, copy, paste"},
	}},
}

const (
	shortcutRowH    = 26
	shortcutKeysW   = 112 // key cap column; labels start after it
	shortcutCapSize = 10
)

// shortcutCardRects places the first group in the left column and the rest
// stacked in the right column.
func shortcutCardRects(r geometry.Rect) []geometry.Rect {
	rect := func(x, y, w, h float32) geometry.Rect { return geometry.NewRect(r.Min.X+x, r.Min.Y+y, w, h) }
	colW := (r.Width() - 40 - 10) / 2
	height := func(g shortcutGroup) float32 { return 36 + float32(len(g.rows))*shortcutRowH + 6 }
	rects := make([]geometry.Rect, len(shortcutGroups))
	y := float32(92)
	for i, g := range shortcutGroups {
		if i == 0 {
			rects[i] = rect(20, 92, colW, height(g))
			continue
		}
		rects[i] = rect(30+colW, y, colW, height(g))
		y += height(g) + 10
	}
	return rects
}

func keyCapWidth(key string) float32 {
	return measureTextWidth(key, shortcutCapSize) + 12
}

func drawKeyCap(canvas widget.Canvas, r geometry.Rect, key string) {
	canvas.DrawRoundRect(r, widget.RGBA8(40, 52, 72, 255), 4)
	canvas.StrokeRoundRect(r, widget.RGBA8(255, 255, 255, 45), 4, 1)
	canvas.DrawText(key, r, shortcutCapSize, widget.RGBA8(228, 237, 250, 255), true, widget.TextAlignCenter)
}

func drawShortcutSettings(canvas widget.Canvas, l settingsLayout, s appViewStateSnapshot) {
	for i, card := range shortcutCardRects(l.bounds) {
		g := shortcutGroups[i]
		settingsCard(canvas, card)
		settingsText(canvas, geometry.NewRect(card.Min.X+12, card.Min.Y+10, card.Width()-24, 16), g.title, 12, true)
		for j, row := range g.rows {
			y := card.Min.Y + 36 + float32(j)*shortcutRowH
			x := card.Min.X + 12
			for _, key := range row.keys {
				w := keyCapWidth(key)
				drawKeyCap(canvas, geometry.NewRect(x, y+2, w, 20), key)
				x += w + 4
			}
			label := row.label
			if row.keys[0] == "⌥⌘J" && !s.config.GlobalHotKey {
				label += " (off)"
			}
			labelR := geometry.NewRect(card.Min.X+12+shortcutKeysW, y+4, card.Width()-24-shortcutKeysW, 16)
			settingsText(canvas, labelR, fitText(label, 11, labelR.Width()), 11, false)
		}
	}
	r := l.bounds
	settingsText(canvas, geometry.NewRect(r.Min.X+32, r.Min.Y+382, r.Width()-64, 14),
		"The global hotkey can be turned on or off on the System page.", 10, false)
}
