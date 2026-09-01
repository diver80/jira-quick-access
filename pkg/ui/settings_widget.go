package ui

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"jira-quick-access/pkg/jira"

	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/widget"
)

// SettingsWidget handles credential configuration and endpoint verification.
type SettingsWidget struct {
	widget.WidgetBase
	mu            sync.RWMutex
	config        jira.Config
	onSave        func(cfg jira.Config)
	onClose       func()
	urlValue      string
	emailValue    string
	tokenValue    string
	jqlValue      string
	intervalValue string
	demoMode      bool
	activeField   int // 1: URL, 2: Email, 3: Token, 4: JQL, 5: Interval
	statusMessage string
	statusColor   widget.Color
	btnClose      *GlassButton
	btnTest       *GlassButton
	btnSave       *GlassButton
	btnDemoToggle *GlassButton
}

func NewSettingsWidget(cfg jira.Config, onSave func(cfg jira.Config), onClose func()) *SettingsWidget {
	s := &SettingsWidget{
		config:        cfg,
		onSave:        onSave,
		onClose:       onClose,
		urlValue:      cfg.BaseURL,
		emailValue:    cfg.Email,
		tokenValue:    cfg.APIToken,
		jqlValue:      cfg.JQLQuery,
		intervalValue: fmt.Sprintf("%d", cfg.PollInterval),
		demoMode:      cfg.DemoMode,
	}

	if s.intervalValue == "" || s.intervalValue == "0" {
		s.intervalValue = "60"
	}

	s.btnClose = NewGlassButton("✕", onClose).SetCompact(true)
	s.btnTest = NewGlassButton("⚡ Test Connection", func() {
		s.testConnection()
	})

	s.btnSave = NewGlassButton("💾 Save Configuration", func() {
		s.saveAndApply()
	})
	s.btnSave.SetCustomColors(widget.RGBA8(255, 255, 255, 255), widget.RGBA8(16, 185, 129, 180), widget.RGBA8(16, 185, 129, 240))

	s.updateDemoToggleText()

	s.AddChild(s.btnClose)
	s.AddChild(s.btnTest)
	s.AddChild(s.btnSave)
	s.AddChild(s.btnDemoToggle)

	s.SetVisible(true)
	s.SetEnabled(true)
	return s
}

func (s *SettingsWidget) updateDemoToggleText() {
	s.mu.RLock()
	demo := s.demoMode
	s.mu.RUnlock()

	text := "Mode: Live Jira"
	if demo {
		text = "Mode: Demo Mock Data"
	}
	s.btnDemoToggle = NewGlassButton(text, func() {
		s.mu.Lock()
		s.demoMode = !s.demoMode
		s.mu.Unlock()
		s.updateDemoToggleText()
		s.MarkNeedsLayout()
	}).SetCompact(true)
}

func (s *SettingsWidget) testConnection() {
	s.mu.Lock()
	s.statusMessage = "Testing connection..."
	s.statusColor = ColorStatusInProgress
	s.mu.Unlock()

	testCfg := s.currentConfig()
	testClient := jira.NewClient(testCfg)

	go func() {
		user, err := testClient.VerifyConnection(context.Background())
		s.mu.Lock()
		if err != nil {
			s.statusMessage = fmt.Sprintf("Error: %v", err)
			s.statusColor = widget.RGBA8(239, 68, 68, 255)
		} else {
			s.statusMessage = fmt.Sprintf("✓ Connected as %s", user)
			s.statusColor = ColorStatusDone
		}
		s.mu.Unlock()
		s.MarkNeedsLayout()
	}()
}

func (s *SettingsWidget) currentConfig() jira.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var pollInterval int
	fmt.Sscanf(s.intervalValue, "%d", &pollInterval)
	if pollInterval <= 0 {
		pollInterval = 60
	}

	cfg := s.config
	cfg.BaseURL = strings.TrimSpace(s.urlValue)
	cfg.Email = strings.TrimSpace(s.emailValue)
	cfg.APIToken = strings.TrimSpace(s.tokenValue)
	cfg.JQLQuery = strings.TrimSpace(s.jqlValue)
	cfg.PollInterval = pollInterval
	cfg.DemoMode = s.demoMode
	cfg.PinnedKeys = s.config.PinnedKeys

	if len(cfg.Instances) > 0 {
		instances := make([]jira.InstanceConfig, len(cfg.Instances))
		copy(instances, cfg.Instances)
		instances[0].BaseURL = cfg.BaseURL
		instances[0].Email = cfg.Email
		instances[0].APIToken = cfg.APIToken
		instances[0].JQLQuery = cfg.JQLQuery
		cfg.Instances = instances
	}

	return cfg
}

func (s *SettingsWidget) saveAndApply() {
	cfg := s.currentConfig()
	_ = jira.SaveConfig(cfg)
	if s.onSave != nil {
		s.onSave(cfg)
	}
}

func (s *SettingsWidget) Layout(ctx widget.Context, constraints geometry.Constraints) geometry.Size {
	b := s.Bounds()
	width := b.Width()
	height := b.Height()

	if s.btnClose != nil {
		s.btnClose.SetBounds(geometry.NewRect(width-44, 12, 32, 28))
		s.btnClose.Layout(ctx, geometry.Tight(geometry.Sz(32, 28)))
	}

	btnY := height - 44
	if s.btnDemoToggle != nil {
		s.btnDemoToggle.SetBounds(geometry.NewRect(16, btnY, 150, 32))
		s.btnDemoToggle.Layout(ctx, geometry.Tight(geometry.Sz(150, 32)))
	}

	if s.btnTest != nil {
		s.btnTest.SetBounds(geometry.NewRect(174, btnY, 140, 32))
		s.btnTest.Layout(ctx, geometry.Tight(geometry.Sz(140, 32)))
	}

	if s.btnSave != nil {
		s.btnSave.SetBounds(geometry.NewRect(width-176, btnY, 160, 32))
		s.btnSave.Layout(ctx, geometry.Tight(geometry.Sz(160, 32)))
	}

	return constraints.Constrain(b.Size())
}

func (s *SettingsWidget) Draw(ctx widget.Context, canvas widget.Canvas) {
	b := s.Bounds()
	radius := float32(14)

	// Backdrop
	canvas.DrawRoundRect(b, ColorCardBackdrop, radius)
	canvas.StrokeRoundRect(b, ColorGlassBorder, radius, 1.0)

	// Header
	headerRect := geometry.NewRect(b.Min.X+20, b.Min.Y+16, b.Width()-80, 24)
	canvas.DrawText("⚙ Jira Configuration & Credentials", headerRect, 14, widget.RGBA8(255, 255, 255, 255), true, widget.TextAlignLeft)

	s.mu.RLock()
	stMsg := s.statusMessage
	stCol := s.statusColor
	urlVal := s.urlValue
	emailVal := s.emailValue
	tokVal := s.tokenValue
	jqlVal := s.jqlValue
	intVal := s.intervalValue
	actField := s.activeField
	s.mu.RUnlock()

	// Fields
	fields := []struct {
		label string
		value string
		idx   int
	}{
		{"Jira Base URL", urlVal, 1},
		{"User Email", emailVal, 2},
		{"API Token", maskToken(tokVal), 3},
		{"Custom JQL Query", jqlVal, 4},
		{"Poll Interval (seconds)", intVal, 5},
	}

	startY := b.Min.Y + 48
	for i, f := range fields {
		fieldY := startY + float32(i*46)

		// Label
		lblRect := geometry.NewRect(b.Min.X+20, fieldY, b.Width()-40, 14)
		canvas.DrawText(f.label, lblRect, 10, widget.RGBA8(148, 163, 184, 255), false, widget.TextAlignLeft)

		// Input Box
		inputRect := geometry.NewRect(b.Min.X+20, fieldY+16, b.Width()-40, 24)
		bgCol := widget.RGBA8(14, 18, 26, 255)
		borderCol := widget.RGBA8(255, 255, 255, 20)
		if actField == f.idx {
			bgCol = widget.RGBA8(22, 28, 42, 255)
			borderCol = ColorStatusToDo
		}
		canvas.DrawRoundRect(inputRect, bgCol, 4)
		canvas.StrokeRoundRect(inputRect, borderCol, 4, 1.0)

		// Text
		valDisplay := f.value
		if valDisplay == "" {
			valDisplay = "(click to type or paste with ⌘V)"
		}
		valRect := geometry.NewRect(inputRect.Min.X+8, inputRect.Min.Y+5, inputRect.Width()-16, 14)
		canvas.DrawText(valDisplay, valRect, 11, widget.RGBA8(240, 245, 255, 255), false, widget.TextAlignLeft)
	}

	// Status Message
	if stMsg != "" {
		stRect := geometry.NewRect(b.Min.X+20, b.Min.Y+b.Height()-68, b.Width()-40, 16)
		canvas.DrawText(stMsg, stRect, 11, stCol, false, widget.TextAlignLeft)
	}

	// Buttons
	s.btnClose.Draw(ctx, canvas)
	s.btnDemoToggle.Draw(ctx, canvas)
	s.btnTest.Draw(ctx, canvas)
	s.btnSave.Draw(ctx, canvas)
}

func (s *SettingsWidget) Event(ctx widget.Context, e event.Event) bool {
	if s.btnClose.Event(ctx, e) {
		return true
	}
	if s.btnTest.Event(ctx, e) {
		return true
	}
	if s.btnSave.Event(ctx, e) {
		return true
	}
	if s.btnDemoToggle.Event(ctx, e) {
		return true
	}

	switch ev := e.(type) {
	case *event.MouseEvent:
		if ev.MouseType == event.MousePress {
			b := s.Bounds()
			startY := b.Min.Y + 48
			s.mu.Lock()
			matched := false
			for i := 0; i < 5; i++ {
				fieldY := startY + float32(i*46)
				inputRect := geometry.NewRect(b.Min.X+20, fieldY+16, b.Width()-40, 24)
				if inputRect.Contains(ev.Position) {
					s.activeField = i + 1
					matched = true
					break
				}
			}
			if !matched {
				s.activeField = 0
			}
			s.mu.Unlock()
			s.MarkNeedsLayout()
			return matched
		}
	case *event.KeyEvent:
		s.mu.RLock()
		actField := s.activeField
		s.mu.RUnlock()
		if ev.KeyType == event.KeyPress && actField > 0 {
			s.handleKeyInput(ev)
			s.MarkNeedsLayout()
			return true
		}
	}

	return false
}

func (s *SettingsWidget) handleKeyInput(ev *event.KeyEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	target := &s.urlValue
	switch s.activeField {
	case 1:
		target = &s.urlValue
	case 2:
		target = &s.emailValue
	case 3:
		target = &s.tokenValue
	case 4:
		target = &s.jqlValue
	case 5:
		target = &s.intervalValue
	}

	if ev.Key == event.KeyBackspace {
		if len(*target) > 0 {
			*target = (*target)[:len(*target)-1]
		}
	} else if ev.Key == event.KeyEnter || ev.Key == event.KeyTab {
		s.activeField = (s.activeField % 5) + 1
	} else if ev.Key == event.KeyEscape {
		s.activeField = 0
	} else if ev.Rune >= 32 && ev.Rune <= 126 {
		*target += string(ev.Rune)
	}
}
