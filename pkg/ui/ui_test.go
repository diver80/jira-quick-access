package ui

import (
	"testing"
	"time"

	"jira-quick-access/pkg/jira"

	"github.com/gogpu/ui/event"
)

func TestStatusColors(t *testing.T) {
	fg, bg, glow := GetStatusColors("indeterminate", "In Progress")
	if fg.A == 0 || bg.A == 0 || glow.A == 0 {
		t.Errorf("status colors should not be transparent")
	}

	fgRev, _, _ := GetStatusColors("indeterminate", "In Review")
	if fgRev != ColorStatusInReview {
		t.Errorf("expected In Review color, got %v", fgRev)
	}
}

func TestSettingsWidgetKeyHandling(t *testing.T) {
	cfg := jira.DefaultConfig()
	settings := NewSettingsWidget(cfg, nil, nil)

	settings.activeField = 1 // url
	settings.urlValue = "https://test"

	// Simulate typing '.com'
	settings.handleKeyInput(event.NewKeyEvent(event.KeyPress, event.KeyPeriod, '.', event.ModNone))
	settings.handleKeyInput(event.NewKeyEvent(event.KeyPress, event.KeyC, 'c', event.ModNone))
	settings.handleKeyInput(event.NewKeyEvent(event.KeyPress, event.KeyO, 'o', event.ModNone))
	settings.handleKeyInput(event.NewKeyEvent(event.KeyPress, event.KeyM, 'm', event.ModNone))

	if settings.urlValue != "https://test.com" {
		t.Errorf("expected https://test.com, got %s", settings.urlValue)
	}

	// Simulate Backspace
	settings.handleKeyInput(event.NewKeyEvent(event.KeyPress, event.KeyBackspace, 0, event.ModNone))
	if settings.urlValue != "https://test.co" {
		t.Errorf("expected https://test.co after backspace, got %s", settings.urlValue)
	}
}

func TestToastNotification(t *testing.T) {
	toast := NewToast("Copied!")
	if !toast.IsActive() {
		t.Errorf("new toast should be active")
	}

	// Check timeout logic
	toast.CreatedAt = time.Now().Add(-5 * time.Second)
	if toast.IsActive() {
		t.Errorf("expired toast should not be active")
	}
}
