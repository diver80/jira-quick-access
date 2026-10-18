package ui

import (
	"testing"

	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"

	"jira-quick-access/pkg/window"
)

func TestShortcutOverviewFits(t *testing.T) {
	r := geometry.NewRect(118, 8, 654, 564)
	cards := shortcutCardRects(r)
	footer := r.Min.Y + r.Height() - 48
	for i, card := range cards {
		if !settingsRectContains(r, card) || card.Max.Y > footer {
			t.Fatalf("card %d out of bounds: %+v", i, card)
		}
		for j := 0; j < i; j++ {
			if settingsRectsOverlap(card, cards[j]) {
				t.Fatalf("cards %d and %d overlap", i, j)
			}
		}
		for _, row := range shortcutGroups[i].rows {
			w := float32(0)
			for _, key := range row.keys {
				w += keyCapWidth(key) + 4
			}
			if w-4 > shortcutKeysW-6 {
				t.Errorf("keys %v are wider than the key column (%.0f)", row.keys, w)
			}
			label := row.label
			if row.keys[0] == "⌥⌘J" {
				label += " (off)"
			}
			if measureTextWidth(label, 11) > card.Width()-24-shortcutKeysW {
				t.Errorf("label %q is cut off", row.label)
			}
		}
	}
}

func TestShortcutsTabShowsOverview(t *testing.T) {
	v, _ := createTestAppView()
	defer v.Close()
	v.OpenSettings()
	v.handleClick(settingsTestCenter(settingsTestLayout(v).shortcutsTab))
	if v.settingsSection != settingsShortcuts {
		t.Fatalf("expected the shortcuts page, got %d", v.settingsSection)
	}
	canvas := &testMockCanvas{}
	v.Draw(&testMockContext{}, canvas)
	texts := map[string]bool{}
	for _, call := range canvas.getDrawTexts() {
		texts[call.text] = true
	}
	for _, want := range []string{"Shortcuts", "⌘⇧C", "⌥⌘J", "⌘B", "Copy Git branch name"} {
		if !texts[want] {
			t.Errorf("shortcuts page is missing %q", want)
		}
	}
	// Tab has no fields to move between on this page.
	v.handleKey(event.NewKeyEvent(event.KeyPress, event.KeyTab, 0, event.ModNone))
	if v.activeField != 0 {
		t.Fatalf("tab focused field %d on the shortcuts page", v.activeField)
	}
}

func TestCmdWClosesLikeEscape(t *testing.T) {
	view := boardView(t)
	view.SetState(window.StateFan)
	view.handleKey(event.NewKeyEvent(event.KeyPress, event.KeyW, 'w', event.ModSuper))
	if view.state != window.StateRest {
		t.Fatalf("Cmd+W should collapse the fan, state %v", view.state)
	}
}
