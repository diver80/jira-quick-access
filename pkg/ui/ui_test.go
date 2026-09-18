package ui

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"jira-quick-access/pkg/jira"
	"jira-quick-access/pkg/window"

	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/widget"
)

func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "jira-ui-test-*")
	if err == nil {
		jira.SetConfigFilePathForTesting(filepath.Join(tmpDir, "config.json"))
		defer os.RemoveAll(tmpDir)
	}
	code := m.Run()
	jira.ResetConfigFilePathForTesting()
	os.Exit(code)
}

type testDrawTextCall struct {
	text     string
	bounds   geometry.Rect
	fontSize float32
	color    widget.Color
	bold     bool
	align    widget.TextAlign
}

type testMockCanvas struct {
	mu        sync.Mutex
	drawTexts []testDrawTextCall
}

func (m *testMockCanvas) Clear(color widget.Color)                                               {}
func (m *testMockCanvas) DrawRect(r geometry.Rect, color widget.Color)                           {}
func (m *testMockCanvas) FillRectDirect(r geometry.Rect, color widget.Color)                     {}
func (m *testMockCanvas) StrokeRect(r geometry.Rect, color widget.Color, strokeWidth float32)    {}
func (m *testMockCanvas) DrawRoundRect(r geometry.Rect, color widget.Color, radius float32)     {}
func (m *testMockCanvas) StrokeRoundRect(r geometry.Rect, color widget.Color, radius float32, strokeWidth float32) {
}
func (m *testMockCanvas) DrawCircle(center geometry.Point, radius float32, color widget.Color) {}
func (m *testMockCanvas) StrokeCircle(center geometry.Point, radius float32, color widget.Color, strokeWidth float32) {
}
func (m *testMockCanvas) StrokeArc(center geometry.Point, radius float32, startAngle, sweepAngle float64, color widget.Color, strokeWidth float32) {
}
func (m *testMockCanvas) DrawLine(from, to geometry.Point, color widget.Color, strokeWidth float32) {}
func (m *testMockCanvas) DrawText(text string, bounds geometry.Rect, fontSize float32, color widget.Color, bold bool, align widget.TextAlign) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.drawTexts = append(m.drawTexts, testDrawTextCall{
		text:     text,
		bounds:   bounds,
		fontSize: fontSize,
		color:    color,
		bold:     bold,
		align:    align,
	})
}
func (m *testMockCanvas) getDrawTexts() []testDrawTextCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	res := make([]testDrawTextCall, len(m.drawTexts))
	copy(res, m.drawTexts)
	return res
}
func (m *testMockCanvas) MeasureText(text string, fontSize float32, bold bool) float32 {
	return float32(len(text)) * fontSize * 0.6
}
func (m *testMockCanvas) DrawImage(img image.Image, at geometry.Point)   {}
func (m *testMockCanvas) PushClip(r geometry.Rect)                       {}
func (m *testMockCanvas) PushClipRoundRect(r geometry.Rect, radius float32) {}
func (m *testMockCanvas) PopClip()                                       {}
func (m *testMockCanvas) PushTransform(offset geometry.Point)            {}
func (m *testMockCanvas) PopTransform()                                  {}
func (m *testMockCanvas) TransformOffset() geometry.Point                { return geometry.Pt(0, 0) }
func (m *testMockCanvas) ScreenOriginBase() geometry.Point               { return geometry.Pt(0, 0) }
func (m *testMockCanvas) ClipBounds() geometry.Rect                      { return geometry.NewRect(0, 0, 1000, 1000) }
func (m *testMockCanvas) ReplayScene(s widget.SceneCache)                {}

type testMockContext struct{}
type testContext = testMockContext

func (c *testMockContext) RequestFocus(w widget.Widget)          {}
func (c *testMockContext) ReleaseFocus(w widget.Widget)          {}
func (c *testMockContext) IsFocused(w widget.Widget) bool        { return false }
func (c *testMockContext) FocusedWidget() widget.Widget          { return nil }
func (c *testMockContext) Now() time.Time                        { return time.Now() }
func (c *testMockContext) DeltaTime() time.Duration              { return 16 * time.Millisecond }
func (c *testMockContext) Invalidate()                           {}
func (c *testMockContext) InvalidateRect(r geometry.Rect)        {}
func (c *testMockContext) Cursor() widget.CursorType             { return 0 }
func (c *testMockContext) SetCursor(cursor widget.CursorType)    {}
func (c *testMockContext) Scale() float32                        { return 1.0 }
func (c *testMockContext) ThemeProvider() widget.ThemeProvider   { return nil }
func (c *testMockContext) OverlayManager() widget.OverlayManager { return nil }
func (c *testMockContext) WindowSize() geometry.Size             { return geometry.Sz(800, 600) }
func (c *testMockContext) Scheduler() widget.SchedulerRef        { return nil }

func createTestAppView() (*AppView, *jira.Client) {
	cfg := jira.Config{
		DemoMode: true,
		Instances: []jira.InstanceConfig{
			{
				ID:       "inst-1",
				Name:     "avono cloud",
				BaseURL:  "https://avono.atlassian.net",
				Email:    "frank.hess@avono.de",
				APIToken: "token-avono",
				JQLQuery: "assignee = currentUser() AND resolution = Unresolved ORDER BY updated DESC",
			},
			{
				ID:       "inst-2",
				Name:     "avono DC",
				BaseURL:  "https://jira.avono.de",
				Email:    "frank.hess@avono.de",
				APIToken: "token-dc",
				JQLQuery: "assignee = currentUser() AND resolution = Unresolved ORDER BY updated DESC",
			},
		},
		ActiveInstID: "inst-1",
		BranchPrefix: "feature/",
	}
	client := jira.NewClient(cfg)
	view := NewAppView(cfg, client, func() {})
	return view, client
}

// Tier 1 & 4: State Machine Transitions (Rest <-> Fan <-> Expanded) & Bounds
func TestAppViewStateTransitionsAndBounds(t *testing.T) {
	view, _ := createTestAppView()
	defer view.Close()

	ctx := &testMockContext{}

	// 1. Initial State: Rest
	if view.state != window.StateRest {
		t.Fatalf("expected initial state StateRest, got %v", view.state)
	}
	w, h := view.computeSize(view.state)
	if w != 36 || h != 224 {
		t.Errorf("expected Rest size (36, 224), got (%d, %d)", w, h)
	}
	sz := view.Layout(ctx, geometry.Loose(geometry.Sz(800, 600)))
	if sz.Width != 36 || sz.Height != 224 {
		t.Errorf("expected Layout size in Rest to be (36, 224), got (%v, %v)", sz.Width, sz.Height)
	}

	// 2. Transition to Fan
	view.SetState(window.StateFan)
	if view.state != window.StateFan {
		t.Fatalf("expected state StateFan, got %v", view.state)
	}
	w, h = view.computeSize(view.state)
	if w != 120 {
		t.Errorf("expected Fan width 120, got %d", w)
	}
	if h < 224 || h > 500 {
		t.Errorf("expected Fan height in range [224, 500], got %d", h)
	}

	// 3. Transition to Expanded
	view.SetState(window.StateExpanded)
	if view.state != window.StateExpanded {
		t.Fatalf("expected state StateExpanded, got %v", view.state)
	}
	w, h = view.computeSize(view.state)
	if w != 780 || h != 580 {
		t.Errorf("expected Expanded size (780, 580), got (%d, %d)", w, h)
	}

	// 4. Expand specific tab
	view.Expand(1)
	if view.state != window.StateExpanded {
		t.Errorf("expected StateExpanded after Expand(1), got %v", view.state)
	}
	view.mu.Lock()
	activeIdx := view.activeIdx
	showSettings := view.showSettings
	view.mu.Unlock()
	if activeIdx != 1 || showSettings {
		t.Errorf("expected activeIdx=1 and showSettings=false, got activeIdx=%d, showSettings=%v", activeIdx, showSettings)
	}

	// 5. Open and Toggle Settings
	view.OpenSettings()
	view.mu.Lock()
	showSettings = view.showSettings
	view.mu.Unlock()
	if !showSettings || view.state != window.StateExpanded {
		t.Errorf("expected showSettings=true in StateExpanded, got %v, %v", showSettings, view.state)
	}

	view.ToggleSettings()
	view.mu.Lock()
	showSettings = view.showSettings
	view.mu.Unlock()
	if showSettings {
		t.Errorf("expected showSettings=false after ToggleSettings")
	}

	// 6. Transition back to Rest
	view.SetState(window.StateRest)
	if view.state != window.StateRest {
		t.Errorf("expected StateRest, got %v", view.state)
	}
}

// Tier 1 & 4: Filter Memoization, Search Query Matching, Case Insensitivity, Dirty Invalidation
func TestAppViewFilterMemoizationAndInvalidation(t *testing.T) {
	view, _ := createTestAppView()
	defer view.Close()

	now := time.Now()
	view.mu.Lock()
	view.issues = []jira.Issue{
		{
			Key:          "AVN-101",
			Summary:      "Implement OAuth 2.0 PKCE Flow",
			InstanceID:   "inst-1",
			Status:       jira.Status{Name: "To Do", CategoryKey: "new"},
			Priority:     jira.Priority{Name: "High"},
			IssueType:    jira.IssueType{Name: "Story"},
			Updated:      now,
			Pinned:       true,
		},
		{
			Key:          "AVN-102",
			Summary:      "Fix WebKit Memory Leak on macOS Darwin",
			InstanceID:   "inst-1",
			Status:       jira.Status{Name: "In Progress", CategoryKey: "indeterminate"},
			Priority:     jira.Priority{Name: "Highest"},
			IssueType:    jira.IssueType{Name: "Bug"},
			Updated:      now.Add(-1 * time.Hour),
			Pinned:       false,
		},
		{
			Key:          "AVN-201",
			Summary:      "Migrate Database to PostgreSQL",
			InstanceID:   "inst-2",
			Status:       jira.Status{Name: "In Review", CategoryKey: "indeterminate"},
			Priority:     jira.Priority{Name: "Medium"},
			IssueType:    jira.IssueType{Name: "Task"},
			Updated:      now.Add(-2 * time.Hour),
			Pinned:       false,
		},
		{
			Key:          "AVN-202",
			Summary:      "Data Center Rest v2 API Endpoints",
			InstanceID:   "inst-2",
			Status:       jira.Status{Name: "Done", CategoryKey: "done"},
			Priority:     jira.Priority{Name: "Low"},
			IssueType:    jira.IssueType{Name: "Story"},
			Updated:      now.Add(-3 * time.Hour),
			Pinned:       true,
		},
	}
	view.activeInstIdx = 0 // "inst-1" (avono)
	view.invalidateFilterCacheLocked()
	view.mu.Unlock()

	// 1. Initial filtered issues for Instance 0 (avono) -> AVN-101 and AVN-102
	filtered := view.getFilteredIssues()
	if len(filtered) != 2 {
		t.Fatalf("expected 2 issues for instance 0, got %d", len(filtered))
	}
	if filtered[0].Key != "AVN-101" || filtered[1].Key != "AVN-102" {
		t.Errorf("unexpected filtered issues for inst 0: %+v", filtered)
	}

	// Pinned issue AVN-101 should appear first
	if !filtered[0].Pinned {
		t.Errorf("expected pinned issue first, got %+v", filtered[0])
	}

	// 2. Memoization test: calling getFilteredIssues again without changes returns cached slice
	filteredCached := view.getFilteredIssues()
	if len(filteredCached) != 2 || filteredCached[0].Key != filtered[0].Key {
		t.Errorf("memoized cache mismatch")
	}

	// 3. Search query: Summary match with case-insensitivity ("leak")
	view.mu.Lock()
	view.searchQuery = "LEAK"
	view.invalidateFilterCacheLocked()
	view.mu.Unlock()

	filtered = view.getFilteredIssues()
	if len(filtered) != 1 || filtered[0].Key != "AVN-102" {
		t.Fatalf("expected 1 issue AVN-102 for 'LEAK', got %+v", filtered)
	}

	// 4. Search query: Status name match ("to do")
	view.mu.Lock()
	view.searchQuery = "to do"
	view.invalidateFilterCacheLocked()
	view.mu.Unlock()

	filtered = view.getFilteredIssues()
	if len(filtered) != 1 || filtered[0].Key != "AVN-101" {
		t.Fatalf("expected 1 issue AVN-101 for 'to do', got %+v", filtered)
	}

	// 5. Search query: Key match ("101")
	view.mu.Lock()
	view.searchQuery = "101"
	view.invalidateFilterCacheLocked()
	view.mu.Unlock()

	filtered = view.getFilteredIssues()
	if len(filtered) != 1 || filtered[0].Key != "AVN-101" {
		t.Fatalf("expected 1 issue AVN-101 for '101', got %+v", filtered)
	}

	// 6. Switch to Instance 1 (avono DC)
	view.mu.Lock()
	view.activeInstIdx = 1
	view.searchQuery = ""
	view.invalidateFilterCacheLocked()
	view.mu.Unlock()

	filtered = view.getFilteredIssues()
	if len(filtered) != 2 {
		t.Fatalf("expected 2 issues for avono DC, got %d", len(filtered))
	}
	if filtered[0].Key != "AVN-201" || filtered[1].Key != "AVN-202" {
		t.Errorf("unexpected issues for avono DC: %+v", filtered)
	}

	// 7. Search on Instance 1 for "postgres"
	view.mu.Lock()
	view.searchQuery = "postgres"
	view.invalidateFilterCacheLocked()
	view.mu.Unlock()

	filtered = view.getFilteredIssues()
	if len(filtered) != 1 || filtered[0].Key != "AVN-201" {
		t.Fatalf("expected 1 issue AVN-201 for 'postgres', got %+v", filtered)
	}
}

// Tier 1 & 4: Keyboard Shortcuts (⌘C, ⌘V, ⌘A, ⌘W, Escape, Modifier Isolation in Fan Search)
func TestAppViewKeyboardShortcutsAndModifiers(t *testing.T) {
	view, _ := createTestAppView()
	defer view.Close()

	// StateFan: Test modifier key isolation (shortcut combinations MUST NOT pollute search query)
	view.SetState(window.StateFan)

	modifiedKeys := []struct {
		key  event.Key
		char rune
		mod  event.Modifiers
	}{
		{event.KeyC, 'c', event.Modifiers(1)}, // Cmd+C
		{event.KeyV, 'v', event.Modifiers(1)}, // Cmd+V
		{event.KeyA, 'a', event.Modifiers(1)}, // Cmd+A
		{event.KeyW, 'w', event.Modifiers(1)}, // Cmd+W
		{event.KeyZ, 'z', event.Modifiers(1)}, // Cmd+Z
		{event.KeyF, 'f', event.Modifiers(2)}, // Ctrl+F
		{event.KeyTab, 0, event.Modifiers(4)}, // Alt+Tab
	}

	for _, k := range modifiedKeys {
		ev := event.NewKeyEvent(event.KeyPress, k.key, k.char, k.mod)
		view.handleKey(ev)
	}

	view.mu.Lock()
	query := view.searchQuery
	view.mu.Unlock()
	if query != "" {
		t.Errorf("search query should remain empty after modifier keys, got %q", query)
	}

	// Plain printable typing in Fan state appends to search query
	for _, ch := range "DOCKER" {
		ev := event.NewKeyEvent(event.KeyPress, event.KeyD, ch, event.ModNone)
		view.handleKey(ev)
	}

	view.mu.Lock()
	query = view.searchQuery
	view.mu.Unlock()
	if query != "DOCKER" {
		t.Errorf("expected search query 'DOCKER', got %q", query)
	}

	// Backspace deletes last character
	bsEv := event.NewKeyEvent(event.KeyPress, event.KeyBackspace, 0, event.ModNone)
	view.handleKey(bsEv)

	view.mu.Lock()
	query = view.searchQuery
	view.mu.Unlock()
	if query != "DOCKE" {
		t.Errorf("expected search query 'DOCKE' after backspace, got %q", query)
	}

	// Escape clears search query
	escEv := event.NewKeyEvent(event.KeyPress, event.KeyEscape, 0, event.ModNone)
	view.handleKey(escEv)

	view.mu.Lock()
	query = view.searchQuery
	view.mu.Unlock()
	if query != "" {
		t.Errorf("expected empty search query after escape, got %q", query)
	}

	// Expanded State: Escape collapses state to StateFan
	view.SetState(window.StateExpanded)
	view.handleKey(escEv)
	if view.state != window.StateFan {
		t.Errorf("expected state to collapse to StateFan on Escape, got %v", view.state)
	}

	// Settings text editing: Tab cycles activeField (1=Name, 2=BaseURL, etc.)
	view.OpenSettings()
	view.mu.Lock()
	view.activeField = 1
	view.mu.Unlock()

	tabEv := event.NewKeyEvent(event.KeyPress, event.KeyTab, 0, event.ModNone)
	view.handleKey(tabEv)
	view.mu.Lock()
	if view.activeField != 2 {
		t.Errorf("expected activeField=2 after Tab, got %d", view.activeField)
	}
	view.mu.Unlock()

	// Tab switching in expanded view
	view.Expand(1)
	view.mu.Lock()
	activeIdx := view.activeIdx
	view.mu.Unlock()
	if activeIdx != 1 {
		t.Errorf("expected activeIdx=1 after Expand(1), got %d", activeIdx)
	}
}

// Tier 1 & 4: Multi-Instance Settings Editing & Instance Management
func TestSettingsWidgetMultiInstanceEditing(t *testing.T) {
	cfg := jira.Config{
		BaseURL:      "https://primary.atlassian.net",
		Email:        "user@primary.com",
		APIToken:     "tok-primary",
		ActiveInstID: "inst-2",
		PollInterval: 120,
		BranchPrefix: "bugfix/",
		Instances: []jira.InstanceConfig{
			{
				ID:       "inst-1",
				Name:     "avono",
				BaseURL:  "https://avono.atlassian.net",
				Email:    "frank.hess@avono.de",
				APIToken: "tok-1",
				JQLQuery: "assignee = currentUser()",
			},
			{
				ID:       "inst-2",
				Name:     "avono DC",
				BaseURL:  "https://jira.avono.de",
				Email:    "frank.hess@avono.de",
				APIToken: "tok-2",
				JQLQuery: "project = AVN",
			},
		},
	}

	widget := NewSettingsWidget(cfg, nil, nil)

	// 1. Initial field values match instance 0
	if widget.urlValue != "https://primary.atlassian.net" || widget.emailValue != "user@primary.com" {
		t.Errorf("unexpected initial values: url=%s, email=%s", widget.urlValue, widget.emailValue)
	}

	// 2. Typing into active field (URL)
	widget.activeField = 1 // URL
	widget.urlValue = "https://updated.atlassian.net"

	savedCfg := widget.currentConfig()
	if len(savedCfg.Instances) != 2 {
		t.Fatalf("expected 2 instances preserved, got %d", len(savedCfg.Instances))
	}
	if savedCfg.ActiveInstID != "inst-2" {
		t.Errorf("expected ActiveInstID 'inst-2' preserved, got %q", savedCfg.ActiveInstID)
	}
	if savedCfg.Instances[0].BaseURL != "https://updated.atlassian.net" {
		t.Errorf("expected instance 0 URL updated to match top level, got %s", savedCfg.Instances[0].BaseURL)
	}
	if savedCfg.PollInterval != 120 || savedCfg.BranchPrefix != "bugfix/" {
		t.Errorf("poll interval or branch prefix lost: %+v", savedCfg)
	}
}

// Tier 1 & 4: ToastWidget and Color Status Helpers
func TestToastWidgetAndColorThemes(t *testing.T) {
	// 1. Toast lifecycle
	toast := NewToast("Copied to clipboard!")
	if !toast.IsActive() {
		t.Errorf("new toast should be active")
	}

	// Expire toast
	toast.CreatedAt = time.Now().Add(-5 * time.Second)
	if toast.IsActive() {
		t.Errorf("expired toast should not be active")
	}

	// 2. Color status lookups
	fgNew, bgNew, glowNew := GetStatusColors("new", "To Do")
	if fgNew.A == 0 || bgNew.A == 0 || glowNew.A == 0 {
		t.Errorf("status colors for 'new' should have non-zero alpha")
	}

	fgIndet, bgIndet, glowIndet := GetStatusColors("indeterminate", "In Progress")
	if fgIndet.A == 0 || bgIndet.A == 0 || glowIndet != ColorStatusInProgressGlow {
		t.Errorf("expected In Progress glow color and non-zero alpha, got %v, %v", fgIndet, glowIndet)
	}

	fgRev, _, glowRev := GetStatusColors("indeterminate", "In Review")
	if fgRev != ColorStatusInReview || glowRev != ColorStatusInReviewGlow {
		t.Errorf("expected ColorStatusInReview, got %v, glow=%v", fgRev, glowRev)
	}

	fgDone, bgDone, glowDone := GetStatusColors("done", "Done")
	if fgDone.A == 0 || bgDone.A == 0 || glowDone != ColorStatusDoneGlow {
		t.Errorf("expected ColorStatusDoneGlow, got %v, glow=%v", fgDone, glowDone)
	}

	// 3. Ticket Themes
	for i := 0; i < 10; i++ {
		theme := GetTicketTheme(i)
		if theme.Name == "" || theme.AccentPill.A == 0 {
			t.Errorf("ticket theme %d is invalid: %+v", i, theme)
		}
	}
}

// Tier 3: Goroutine Lifecycle and Context Cancellation in AppView
func TestAppViewGoroutineLifecycleAndCleanShutdown(t *testing.T) {
	initialGoroutines := runtime.NumGoroutine()

	for i := 0; i < 15; i++ {
		view, _ := createTestAppView()
		view.SetState(window.StateFan)
		view.handleHover(geometry.Pt(16, 50))
		view.showToast("Goroutine lifecycle test")
		time.Sleep(2 * time.Millisecond)
		view.Close()
	}

	time.Sleep(150 * time.Millisecond)
	finalGoroutines := runtime.NumGoroutine()

	if finalGoroutines > initialGoroutines+5 {
		t.Errorf("AppView goroutine leak detected: initial=%d, final=%d", initialGoroutines, finalGoroutines)
	}
}

// Tier 3: Concurrency Race Tests with Parallel Operations
func TestAppViewConcurrencyParallelRace(t *testing.T) {
	view, _ := createTestAppView()
	defer view.Close()

	ctx := &testMockContext{}
	canvas := &testMockCanvas{}

	const numGoroutines = 30
	const iterations = 40
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for g := 0; g < numGoroutines; g++ {
		gid := g
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				switch gid % 8 {
				case 0:
					// Draw
					view.Draw(ctx, canvas)
				case 1:
					// Key events
					ev := event.NewKeyEvent(event.KeyPress, event.KeyA, rune('a'+(i%26)), event.ModNone)
					view.Event(ctx, ev)
					view.handleKey(ev)
				case 2:
					// Modifier shortcuts (Cmd+C, Cmd+V, Cmd+W)
					modEv := event.NewKeyEvent(event.KeyPress, event.KeyC, 'c', event.Modifiers(1))
					view.handleKey(modEv)
				case 3:
					// Mouse clicks & hover
					pos := geometry.Pt(float32(10+(i%50)), float32(20+(i%100)))
					view.handleClick(pos)
					view.handleHover(pos)
				case 4:
					// State transitions & layout
					view.SetState(window.WindowState(i % 3))
					view.Layout(ctx, geometry.Loose(geometry.Sz(800, 600)))
				case 5:
					// Snapshots & filter cache
					_ = view.snapshot()
					_ = view.getFilteredIssues()
				case 6:
					// Expand & Settings toggle
					if i%2 == 0 {
						view.Expand(i % 3)
					} else {
						view.ToggleSettings()
					}
				case 7:
					// Toast & Wheel scroll
					view.showToast(fmt.Sprintf("Toast %d", i))
					wheelEv := event.NewWheelEvent(geometry.Pt(0, float32(i*2)), geometry.Pt(10, 10), geometry.Pt(10, 10), event.ModNone)
					view.Event(ctx, wheelEv)
				}
			}
		}()
	}

	wg.Wait()
}

// Tier 1 & 4: StatusDot, GlassButton, and IssueCardWidget tests
func TestStatusDotAndGlassButtonAndIssueCard(t *testing.T) {
	ctx := &testMockContext{}
	canvas := &testMockCanvas{}

	// 1. StatusDotWidget
	dot := NewStatusDot(ColorStatusDone, ColorStatusDoneGlow)
	sz := dot.Layout(ctx, geometry.Loose(geometry.Sz(50, 50)))
	if sz.Width != 16 || sz.Height != 16 {
		t.Errorf("expected StatusDot size (16, 16), got %+v", sz)
	}
	dot.SetBounds(geometry.NewRect(0, 0, 16, 16))
	dot.Draw(ctx, canvas)
	if dot.Event(ctx, &event.MouseEvent{}) {
		t.Errorf("StatusDot should return false for events")
	}
	if len(dot.Children()) != 0 {
		t.Errorf("StatusDot should have no children")
	}

	// 2. GlassButton
	var clicked bool
	btn := NewGlassButton("Click Me", func() {
		clicked = true
	})
	btn.SetCompact(true)
	btn.SetCustomColors(ColorTextPrimary, ColorGlassCard, ColorBorderAccent)
	btnSz := btn.Layout(ctx, geometry.Loose(geometry.Sz(100, 30)))
	if btnSz.Height != 22 {
		t.Errorf("expected compact button height 22, got %v", btnSz.Height)
	}
	btn.SetBounds(geometry.NewRect(0, 0, btnSz.Width, btnSz.Height))
	btn.Draw(ctx, canvas)

	// Simulate hover, press, and click
	mouseMove := event.NewMouseEvent(event.MouseMove, event.ButtonNone, event.ButtonState(0), geometry.Pt(5, 5), geometry.Pt(5, 5), event.ModNone)
	btn.Event(ctx, mouseMove)
	if !btn.isHovered {
		t.Errorf("button should be hovered")
	}

	mousePress := event.NewMouseEvent(event.MousePress, event.ButtonLeft, event.ButtonStateLeft, geometry.Pt(5, 5), geometry.Pt(5, 5), event.ModNone)
	btn.Event(ctx, mousePress)
	if !btn.isPressed {
		t.Errorf("button should be pressed")
	}

	mouseRelease := event.NewMouseEvent(event.MouseRelease, event.ButtonLeft, event.ButtonState(0), geometry.Pt(5, 5), geometry.Pt(5, 5), event.ModNone)
	btn.Event(ctx, mouseRelease)
	if !clicked {
		t.Errorf("button click callback was not invoked")
	}
	if len(btn.Children()) != 0 {
		t.Errorf("GlassButton should have no children")
	}

	// 3. IssueCardWidget
	iss := jira.Issue{
		Key:       "CARD-1",
		Summary:   "Card summary test",
		Status:    jira.Status{Name: "To Do", CategoryKey: "new"},
		Priority:  jira.Priority{Name: "High"},
		IssueType: jira.IssueType{Name: "Bug"},
	}
	card := NewIssueCard(iss, func(k string) {}, func(i jira.Issue) {}, func(k string) {}, func(k string) {}, func(u string) {})
	cardSz := card.Layout(ctx, geometry.Loose(geometry.Sz(340, 100)))
	if cardSz.Width != 340 || cardSz.Height != 78 {
		t.Errorf("expected card size (340, 78), got %+v", cardSz)
	}
	card.SetBounds(geometry.NewRect(0, 0, 340, 78))
	card.Draw(ctx, canvas)
	if len(card.Children()) < 3 {
		t.Errorf("expected child buttons in IssueCardWidget, got %d", len(card.Children()))
	}
	card.Event(ctx, mouseMove)
	if !card.isHovered {
		t.Errorf("card should be hovered")
	}
}

// Tier 1 & 4: RailWidget and PanelWidget lifecycle tests
func TestRailAndPanelWidgets(t *testing.T) {
	ctx := &testMockContext{}
	canvas := &testMockCanvas{}

	issues := []jira.Issue{
		{Key: "RAIL-1", Summary: "Rail issue 1", Status: jira.Status{Name: "Open", CategoryKey: "new"}},
		{Key: "RAIL-2", Summary: "Rail issue 2", Status: jira.Status{Name: "Done", CategoryKey: "done"}},
	}

	// 1. RailWidget
	var expandCalled, configCalled bool
	rail := NewRailWidget(issues, func() { expandCalled = true }, func() { configCalled = true })
	railSz := rail.Layout(ctx, geometry.Loose(geometry.Sz(36, 500)))
	if railSz.Width != 36 {
		t.Errorf("expected rail width 36, got %v", railSz.Width)
	}
	rail.SetBounds(geometry.NewRect(0, 0, 36, 500))
	rail.Draw(ctx, canvas)

	rail.UpdateIssues([]jira.Issue{{Key: "RAIL-3"}})
	if len(rail.issues) != 1 || rail.issues[0].Key != "RAIL-3" {
		t.Errorf("rail UpdateIssues failed: %+v", rail.issues)
	}

	railMove := event.NewMouseEvent(event.MouseMove, event.ButtonNone, event.ButtonState(0), geometry.Pt(18, 50), geometry.Pt(18, 50), event.ModNone)
	rail.Event(ctx, railMove)
	if !rail.isHovered {
		t.Errorf("rail should be hovered")
	}

	// 2. PanelWidget
	var collapsed, settingsOpened, refreshed bool
	panel := NewPanelWidget(
		issues,
		nil,
		false,
		func() { collapsed = true },
		func() { settingsOpened = true },
		func() { refreshed = true },
		func(k string) {},
		func(i jira.Issue) {},
		func(k string) {},
		func(k string) {},
		func(u string) {},
	)
	panelSz := panel.Layout(ctx, geometry.Loose(geometry.Sz(360, 600)))
	if panelSz.Width != 360 {
		t.Errorf("expected panel width 360, got %v", panelSz.Width)
	}
	panel.SetBounds(geometry.NewRect(0, 0, 360, 600))
	panel.Draw(ctx, canvas)

	panel.SetSyncing(true)
	if !panel.isSyncing {
		t.Errorf("panel should be syncing")
	}
	panel.SetIssues(issues, nil)

	// Filter tab switching on panel
	tabProgEv := event.NewMouseEvent(event.MousePress, event.ButtonLeft, event.ButtonStateLeft, geometry.Pt(100, 85), geometry.Pt(100, 85), event.ModNone)
	panel.Event(ctx, tabProgEv)
	tabRelease := event.NewMouseEvent(event.MouseRelease, event.ButtonLeft, event.ButtonState(0), geometry.Pt(100, 85), geometry.Pt(100, 85), event.ModNone)
	panel.Event(ctx, tabRelease)

	_ = collapsed
	_ = settingsOpened
	_ = refreshed
	_ = expandCalled
	_ = configCalled
}

// Tier 1 & 4: Comprehensive AppView Click, Hover, and Settings Scenarios
func TestAppViewClickAndHoverDetails(t *testing.T) {
	view, _ := createTestAppView()
	defer view.Close()

	ctx := &testMockContext{}
	canvas := &testMockCanvas{}

	view.mu.Lock()
	view.issues = []jira.Issue{
		{Key: "CLK-1", Summary: "Click test 1", InstanceID: "inst-1", Status: jira.Status{Name: "To Do", CategoryKey: "new"}, URL: "https://avono.atlassian.net/browse/CLK-1"},
		{Key: "CLK-2", Summary: "Click test 2", InstanceID: "inst-2", Status: jira.Status{Name: "Done", CategoryKey: "done"}, URL: "https://avono.atlassian.net/browse/CLK-2"},
	}
	view.mu.Unlock()

	// 1. Hover in Rest State
	view.SetState(window.StateRest)
	view.SetBounds(geometry.NewRect(0, 0, 32, 224))
	view.Draw(ctx, canvas)

	// Hover settings dot at bottom of rest capsule sets hoveredSettings without accidentally opening
	view.handleHover(geometry.Pt(16, 205))
	view.mu.Lock()
	hovSettings := view.hoveredSettings
	view.mu.Unlock()
	if !hovSettings {
		t.Errorf("expected hovering settings dot in Rest to set hoveredSettings")
	}

	// Click settings dot at bottom of rest capsule to open Settings
	view.handleClick(geometry.Pt(16, 205))
	view.mu.Lock()
	showSettings := view.showSettings
	view.mu.Unlock()
	if !showSettings {
		t.Errorf("expected clicking settings dot in Rest to open Settings")
	}

	// 2. Click in Expanded State Settings Modal
	view.OpenSettings()
	view.SetBounds(geometry.NewRect(0, 0, 780, 580))
	view.Draw(ctx, canvas)

	// Click instance tab 2 ("avono DC") at x=160, y=60
	view.handleClick(geometry.Pt(160, 60))
	view.mu.Lock()
	selectedInst := view.selectedInstIdx
	view.mu.Unlock()
	if selectedInst != 1 {
		t.Errorf("expected selectedInstIdx=1 after clicking tab 2, got %d", selectedInst)
	}

	// Click on input field 1 (Name field at y=125)
	view.handleClick(geometry.Pt(100, 125))
	view.mu.Lock()
	if view.activeField != 1 {
		t.Errorf("expected activeField=1, got %d", view.activeField)
	}
	view.mu.Unlock()

	// Click Test Connection Button (x=300, y=535)
	view.handleClick(geometry.Pt(300, 535))

	// Click Save Button in settings modal (x=580, y=535)
	view.handleClick(geometry.Pt(580, 535))
	view.mu.Lock()
	if view.showSettings {
		t.Errorf("expected showSettings=false after clicking save button")
	}
	view.mu.Unlock()

	// 3. Fan State Clicks & Hover
	view.SetState(window.StateFan)
	view.SetBounds(geometry.NewRect(0, 0, 120, 500))
	view.Draw(ctx, canvas)

	// Click instance header in Fan state (y=18) to cycle instances
	view.handleClick(geometry.Pt(50, 18))
	view.mu.Lock()
	activeInst := view.activeInstIdx
	view.mu.Unlock()
	if activeInst != 1 {
		t.Errorf("expected activeInstIdx to cycle to 1, got %d", activeInst)
	}

	// Click tab in Fan state (y=80) to expand
	view.handleClick(geometry.Pt(50, 80))
	if view.state != window.StateExpanded {
		t.Errorf("expected clicking tab in Fan to expand to StateExpanded, got %v", view.state)
	}

	// 4. Expanded State Rail Clicks (Settings tab at x=700, y=550)
	view.SetBounds(geometry.NewRect(0, 0, 780, 580))
	view.Draw(ctx, canvas)
	view.handleClick(geometry.Pt(700, 550))
	view.mu.Lock()
	showSettings = view.showSettings
	view.mu.Unlock()
	if !showSettings {
		t.Errorf("expected clicking settings icon in rail to open settings modal")
	}
}

// Tier 1: Token Sanitization Helpers
func TestAppViewTokenSanitization(t *testing.T) {
	// 1. Whitespace trimming
	if s := sanitizeToken("  token123  "); s != "token123" {
		t.Errorf("expected trimmed token, got %q", s)
	}

	// 2. 193-char token starting with ATATT and ending with 'v' has trailing 'v' stripped
	longTok := "ATATT" + strings.Repeat("x", 187) + "v"
	if len(longTok) == 193 {
		sanitized := sanitizeToken(longTok)
		if len(sanitized) != 192 || strings.HasSuffix(sanitized, "v") {
			t.Errorf("expected trailing 'v' stripped from 193-char ATATT token, got len=%d", len(sanitized))
		}
	}

	// 3. Normal length token unchanged
	normalTok := "ATATT123456789"
	if s := sanitizeToken(normalTok); s != normalTok {
		t.Errorf("expected normal token unchanged, got %q", s)
	}
}

// Tier 1 & 3: Docking, Profile Colors, Always On Top, and Auto-Hide Controls
func TestAppViewDockingAndProfileColorControls(t *testing.T) {
	cfg := jira.DefaultConfig()
	cfg.DemoMode = true
	client := jira.NewClient(cfg)
	view := NewAppView(cfg, client, nil)
	defer view.Close()

	ctx := &mockContext{}
	canvas := &mockCanvas{}

	// 1. SetDockSide and SetMonitor directly
	view.SetDockSide(window.DockSideLeft)
	if view.dockSide != window.DockSideLeft {
		t.Errorf("expected DockSideLeft, got %v", view.dockSide)
	}
	view.SetDockSide(window.DockSideRight)
	if view.dockSide != window.DockSideRight {
		t.Errorf("expected DockSideRight, got %v", view.dockSide)
	}
	view.SetMonitor(0)
	if view.selectedMonitor != 0 {
		t.Errorf("expected monitor 0, got %d", view.selectedMonitor)
	}

	// 2. SetAlwaysOnTop and SetAutoHide
	view.SetAlwaysOnTop(true)
	if !view.alwaysOnTop {
		t.Errorf("expected alwaysOnTop true")
	}
	view.SetAlwaysOnTop(false)
	if view.alwaysOnTop {
		t.Errorf("expected alwaysOnTop false")
	}
	view.SetAutoHide(true)
	if !view.autoHide {
		t.Errorf("expected autoHide true")
	}
	view.setTucked(true)
	if !view.isTucked {
		t.Errorf("expected isTucked true")
	}
	view.setTucked(false)
	if view.isTucked {
		t.Errorf("expected isTucked false")
	}
	view.SetAutoHide(false)

	// 3. StartDrag
	view.StartDrag()

	// 4. Open Settings and test interactive clicks
	view.OpenSettings()
	view.SetBounds(geometry.NewRect(0, 0, 780, 580))
	view.Draw(ctx, canvas)

	getRMinX := func() float32 {
		if view.dockSide == window.DockSideLeft {
			return 118
		}
		return 8
	}

	// Click Left Edge button in Settings
	dockSecY := float32(78 + 12 + 5*45 + 2 + 40)
	row1Y := dockSecY + 24
	view.handleClick(geometry.Pt(getRMinX()+40, row1Y+10))
	if view.dockSide != window.DockSideLeft {
		t.Errorf("expected Left Edge click to set DockSideLeft, got %v", view.dockSide)
	}

	// Click Right Edge button in Settings
	view.handleClick(geometry.Pt(getRMinX()+140, row1Y+10))
	if view.dockSide != window.DockSideRight {
		t.Errorf("expected Right Edge click to set DockSideRight, got %v", view.dockSide)
	}

	// Click Always On Top toggle
	row2Y := dockSecY + 54
	prevAOT := view.alwaysOnTop
	view.handleClick(geometry.Pt(getRMinX()+40, row2Y+10))
	if view.alwaysOnTop == prevAOT {
		t.Errorf("expected Always On Top toggle click to flip state")
	}

	// Click Auto-Hide toggle
	prevAH := view.autoHide
	view.handleClick(geometry.Pt(getRMinX()+180, row2Y+10))
	if view.autoHide == prevAH {
		t.Errorf("expected Auto-Hide toggle click to flip state")
	}

	// Click 2nd Profile Color chip (Purple: #a855f7 at rMinX+30+28=58, y=colorRowY+24)
	colorRowY := float32(78 + 12 + 5*45 + 2)
	view.handleClick(geometry.Pt(getRMinX()+30+28, colorRowY+24))
	view.mu.Lock()
	col := view.colorVal
	view.mu.Unlock()
	if !strings.EqualFold(col, "#a855f7") {
		t.Errorf("expected colorVal #a855f7, got %q", col)
	}
}

// Tier 1 & 2: Global Shortcuts and Arrow Key Ticket Navigation
func TestAppViewGlobalShortcutsAndNavigation(t *testing.T) {
	cfg := jira.DefaultConfig()
	cfg.DemoMode = true
	client := jira.NewClient(cfg)
	view := NewAppView(cfg, client, nil)
	defer view.Close()

	view.SetState(window.StateFan)
	view.SetBounds(geometry.NewRect(0, 0, 120, 500))

	// 1. Cmd+R: Refresh
	refreshEv := event.NewKeyEvent(event.KeyPress, event.KeyR, 'r', event.ModSuper)
	if !view.handleKey(refreshEv) {
		t.Errorf("expected Cmd+R to be handled")
	}

	// 2. Cmd+K: Focus search
	searchEv := event.NewKeyEvent(event.KeyPress, event.KeyK, 'k', event.ModSuper)
	if !view.handleKey(searchEv) {
		t.Errorf("expected Cmd+K to be handled")
	}
	if !view.searchActive {
		t.Errorf("expected searchActive true after Cmd+K")
	}

	// 3. Arrow Down and Up navigation
	view.SetState(window.StateExpanded)
	downEv := event.NewKeyEvent(event.KeyPress, event.KeyDown, 0, event.ModNone)
	if !view.handleKey(downEv) {
		t.Errorf("expected Arrow Down to be handled")
	}
	upEv := event.NewKeyEvent(event.KeyPress, event.KeyUp, 0, event.ModNone)
	if !view.handleKey(upEv) {
		t.Errorf("expected Arrow Up to be handled")
	}

	// 4. Cmd+C: Copy issue key
	copyEv := event.NewKeyEvent(event.KeyPress, event.KeyC, 'c', event.ModSuper)
	if !view.handleKey(copyEv) {
		t.Errorf("expected Cmd+C to be handled")
	}

	// 5. Cmd+Shift+C: Copy branch name
	branchEv := event.NewKeyEvent(event.KeyPress, event.KeyC, 'c', event.ModSuper|event.ModShift)
	if !view.handleKey(branchEv) {
		t.Errorf("expected Cmd+Shift+C to be handled")
	}

	// 6. Cmd+O: Open ticket in browser
	openEv := event.NewKeyEvent(event.KeyPress, event.KeyO, 'o', event.ModSuper)
	if !view.handleKey(openEv) {
		t.Errorf("expected Cmd+O to be handled")
	}
}

// TestAppViewEdgeMarkerDockSides tests rendering of the edge marker and badge on left vs right dock sides.
func TestAppViewEdgeMarkerDockSides(t *testing.T) {
	cfg := jira.DefaultConfig()
	cfg.DemoMode = true
	client := jira.NewClient(cfg)
	view := NewAppView(cfg, client, nil)
	defer view.Close()

	ctx := widget.NewContext()
	canvas := &mockCanvas{}

	// 1. StateRest with DockSideRight
	view.SetDockSide(window.DockSideRight)
	view.SetState(window.StateRest)
	view.SetBounds(geometry.NewRect(0, 0, 32, 224))
	view.Draw(ctx, canvas)

	// 2. StateRest with DockSideLeft
	view.SetDockSide(window.DockSideLeft)
	view.Draw(ctx, canvas)

	// 3. StateFan with DockSideRight and DockSideLeft
	view.SetDockSide(window.DockSideRight)
	view.SetState(window.StateFan)
	view.SetBounds(geometry.NewRect(0, 0, 120, 500))
	view.hoveredTabIdx = 0
	view.Draw(ctx, canvas)

	view.SetDockSide(window.DockSideLeft)
	view.Draw(ctx, canvas)

	// 4. StateExpanded with DockSideRight and DockSideLeft
	view.SetDockSide(window.DockSideRight)
	view.SetState(window.StateExpanded)
	view.SetBounds(geometry.NewRect(0, 0, 780, 580))
	view.activeIdx = 0
	view.Draw(ctx, canvas)

	view.SetDockSide(window.DockSideLeft)
	view.Draw(ctx, canvas)
}

func TestAppViewDockSideDoesNotMutateRealUserConfig(t *testing.T) {
	home, _ := os.UserHomeDir()
	realConfigPath := filepath.Join(home, ".jira-quick-access", "config.json")
	var initialStat os.FileInfo
	if info, err := os.Stat(realConfigPath); err == nil {
		initialStat = info
	}

	cfg := jira.DefaultConfig()
	cfg.DemoMode = true
	view := NewAppView(cfg, nil, nil)
	view.SetDockSide(window.DockSideLeft)
	view.SetDockSide(window.DockSideRight)

	if initialStat != nil {
		afterStat, err := os.Stat(realConfigPath)
		if err != nil {
			t.Fatalf("real config file unexpectedly deleted: %v", err)
		}
		if afterStat.ModTime() != initialStat.ModTime() {
			t.Fatalf("CRITICAL BUG: real user config file was modified by AppView.SetDockSide!")
		}
	}
}

func TestTruncateSummary(t *testing.T) {
	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{"", 17, ""},
		{"Short text", 17, "Short text"},
		{"Exact seventeen c", 17, "Exact seventeen c"},
		{"Implement OAuth PKCE Flow", 17, "Implement OAuth …"},
		{"   Spaced   out   summary   ", 17, "Spaced out summa…"},
		{"Fix race condition in token poller", 10, "Fix race …"},
	}

	for _, tt := range tests {
		got := truncateSummary(tt.input, tt.maxLen)
		if got != tt.expected {
			t.Errorf("truncateSummary(%q, %d) = %q; want %q", tt.input, tt.maxLen, got, tt.expected)
		}
	}
}

func TestComputeSizeFanHeight(t *testing.T) {
	v := NewAppView(jira.DefaultConfig(), nil, nil)
	defer v.Close()
	// 0 issues -> min height 270
	w, h := v.computeSize(window.StateFan)
	if w != 120 || h != 270 {
		t.Errorf("computeSize(StateFan) with 0 issues = (%d, %d), want (120, 270)", w, h)
	}

	// 5 issues -> 5*70 + 120 = 470
	v.issues = make([]jira.Issue, 5)
	for i := range v.issues {
		v.issues[i] = jira.Issue{Key: fmt.Sprintf("KEY-%d", i), Summary: "Test issue"}
	}
	v.cacheDirty = true
	w, h = v.computeSize(window.StateFan)
	if w != 120 || h != 470 {
		t.Errorf("computeSize(StateFan) with 5 issues = (%d, %d), want (120, 470)", w, h)
	}

	// 15 issues -> clamped at 720
	v.issues = make([]jira.Issue, 15)
	v.cacheDirty = true
	_, h = v.computeSize(window.StateFan)
	if h != 720 {
		t.Errorf("computeSize(StateFan) with 15 issues = h %d, want 720", h)
	}
}

func TestDrawTabThreeLines(t *testing.T) {
	v := NewAppView(jira.DefaultConfig(), nil, nil)
	defer v.Close()
	v.issues = []jira.Issue{
		{
			Key:     "PROJ-101",
			Summary: "Fix race condition in poller",
			Status:  jira.Status{Name: "In Progress", CategoryKey: "indeterminate"},
		},
	}
	v.cacheDirty = true
	v.state = window.StateFan

	mockC := &testMockCanvas{}
	ctx := &testContext{}
	v.Draw(ctx, mockC)

	// Verify DrawText recorded calls for:
	// 1. "PROJ-101"
	// 2. "Fix race conditi…"
	// 3. "In Progress"
	var foundKey, foundSummary, foundStatus bool
	for _, dt := range mockC.getDrawTexts() {
		if dt.text == "PROJ-101" {
			foundKey = true
		}
		if dt.text == "Fix race conditi…" {
			foundSummary = true
		}
		if dt.text == "In Progress" {
			foundStatus = true
		}
	}

	if !foundKey {
		t.Error("did not find PROJ-101 in canvas text calls")
	}
	if !foundSummary {
		t.Error("did not find truncated summary in canvas text calls")
	}
	if !foundStatus {
		t.Error("did not find status in canvas text calls")
	}

	// Verify StateExpanded draws 3 lines as well
	v.state = window.StateExpanded
	v.SetBounds(geometry.NewRect(0, 0, 780, 580))
	mockCExp := &testMockCanvas{}
	v.Draw(ctx, mockCExp)

	var foundExpKey, foundExpSummary, foundExpStatus bool
	for _, dt := range mockCExp.getDrawTexts() {
		if dt.text == "PROJ-101" {
			foundExpKey = true
		}
		if dt.text == "Fix race conditi…" {
			foundExpSummary = true
		}
		if dt.text == "In Progress" {
			foundExpStatus = true
		}
	}

	if !foundExpKey {
		t.Error("did not find PROJ-101 in expanded canvas text calls")
	}
	if !foundExpSummary {
		t.Error("did not find truncated summary in expanded canvas text calls")
	}
	if !foundExpStatus {
		t.Error("did not find status in expanded canvas text calls")
	}
}

type mockManager struct {
	window.WindowManager
	tooltip string
}

func (m *mockManager) InitEdgeRail(width, height int) error                  { return nil }
func (m *mockManager) SetState(state window.WindowState, width, height int) {}
func (m *mockManager) Dock(width, height int)                               {}
func (m *mockManager) DockToRightEdge(width, height int)                    {}
func (m *mockManager) SetDockSide(side window.DockSide)                     {}
func (m *mockManager) GetDockSide() window.DockSide                         { return window.DockSideRight }
func (m *mockManager) SetMonitor(screenIndex int)                           {}
func (m *mockManager) GetSelectedMonitor() int                              { return 0 }
func (m *mockManager) GetMonitors() []window.MonitorInfo                    { return nil }
func (m *mockManager) SetPositionRatio(ratio float64)                       {}
func (m *mockManager) GetPositionRatio() float64                            { return 0.5 }
func (m *mockManager) StartWindowDrag()                                     {}
func (m *mockManager) OpenTicketURL(url string) error                       { return nil }
func (m *mockManager) SetAlwaysOnTop(alwaysOnTop bool)                      {}
func (m *mockManager) GetAlwaysOnTop() bool                                 { return false }
func (m *mockManager) SetAutoHide(autoHide bool)                            {}
func (m *mockManager) GetAutoHide() bool                                    { return false }
func (m *mockManager) SetTucked(tucked bool, width, height int)             {}
func (m *mockManager) SetToolTip(tooltip string) {
	m.tooltip = tooltip
}

func TestHoverTooltipLifecycle(t *testing.T) {
	mgr := &mockManager{}
	orig := window.DefaultManager
	defer func() { window.DefaultManager = orig }()
	window.DefaultManager = mgr

	v := NewAppView(jira.DefaultConfig(), nil, nil)
	defer v.Close()
	v.issues = []jira.Issue{
		{Key: "PROJ-1", Summary: "Build rocket", Status: jira.Status{Name: "To Do"}},
	}
	v.cacheDirty = true
	v.state = window.StateFan

	// Hover over first tab
	v.setHoveredTab(0)
	if mgr.tooltip != "[PROJ-1] Build rocket • To Do" {
		t.Errorf("expected tooltip on hover, got %q", mgr.tooltip)
	}

	// Exit hover
	v.setHoveredTab(-1)
	if mgr.tooltip != "" {
		t.Errorf("expected tooltip to be cleared, got %q", mgr.tooltip)
	}

	// Re-hover then transition to StateRest
	v.setHoveredTab(0)
	if mgr.tooltip != "[PROJ-1] Build rocket • To Do" {
		t.Errorf("expected tooltip on hover, got %q", mgr.tooltip)
	}
	v.SetState(window.StateRest)
	if mgr.tooltip != "" {
		t.Errorf("expected tooltip to be cleared on SetState(StateRest), got %q", mgr.tooltip)
	}

	// Re-hover in StateFan then transition to StateExpanded: tooltip should remain active
	v.SetState(window.StateFan)
	v.setHoveredTab(0)
	v.SetState(window.StateExpanded)
	if mgr.tooltip != "[PROJ-1] Build rocket • To Do" {
		t.Errorf("expected tooltip to remain in StateExpanded, got %q", mgr.tooltip)
	}
	v.SetState(window.StateRest)
	if mgr.tooltip != "" {
		t.Errorf("expected tooltip to be cleared on SetState(StateRest), got %q", mgr.tooltip)
	}
}

func TestHoverExpandedTab(t *testing.T) {
	mgr := &mockManager{}
	orig := window.DefaultManager
	defer func() { window.DefaultManager = orig }()
	window.DefaultManager = mgr

	v := NewAppView(jira.DefaultConfig(), nil, nil)
	defer v.Close()
	v.issues = []jira.Issue{
		{Key: "PROJ-1", Summary: "Ship rocket", Status: jira.Status{Name: "In Review"}},
	}
	v.cacheDirty = true
	v.SetState(window.StateExpanded)
	v.SetBounds(geometry.NewRect(0, 0, 780, 580))

	// In StateExpanded with DockSideRight (default), tabs are on the right side:
	// tabBarWidth = 110, tabStartX = 780 - 110 = 670, tabStartY = 44, tabHeight = 64
	// Point at x=700, y=60 is inside the first tab.
	v.handleHover(geometry.Pt(700, 60))

	v.mu.Lock()
	hovIdx := v.hoveredTabIdx
	v.mu.Unlock()

	if hovIdx != 0 {
		t.Errorf("expected hoveredTabIdx=0 in StateExpanded, got %d", hovIdx)
	}
	if mgr.tooltip != "[PROJ-1] Ship rocket • In Review" {
		t.Errorf("expected tooltip for hovered tab in StateExpanded, got %q", mgr.tooltip)
	}

	// Transition to StateRest should reset hoveredTabIdx to -1 and clear tooltip
	v.SetState(window.StateRest)
	v.mu.Lock()
	hovIdxAfterRest := v.hoveredTabIdx
	v.mu.Unlock()

	if hovIdxAfterRest != -1 {
		t.Errorf("expected hoveredTabIdx=-1 after SetState(StateRest), got %d", hovIdxAfterRest)
	}
	if mgr.tooltip != "" {
		t.Errorf("expected tooltip to be cleared after SetState(StateRest), got %q", mgr.tooltip)
	}
}

func TestStatusUTF8RuneSlicing(t *testing.T) {
	v := NewAppView(jira.DefaultConfig(), nil, nil)
	defer v.Close()

	// 18-rune UTF-8 status with multi-byte characters
	statusName := "In Bearbeitung üöä"
	v.issues = []jira.Issue{
		{
			Key:     "UTF-1",
			Summary: "Test UTF-8",
			Status:  jira.Status{Name: statusName},
		},
	}
	v.cacheDirty = true
	v.state = window.StateFan
	v.SetBounds(geometry.NewRect(0, 0, 120, 300))

	ctx := &testContext{}
	mockC := &testMockCanvas{}
	v.Draw(ctx, mockC)

	expectedShort := string([]rune(statusName)[:13])
	var foundFanStatus bool
	for _, dt := range mockC.getDrawTexts() {
		if dt.text == expectedShort {
			foundFanStatus = true
			break
		}
	}
	if !foundFanStatus {
		t.Errorf("expected truncated UTF-8 status %q in Fan state, not found", expectedShort)
	}

	// Also test Expanded state
	v.state = window.StateExpanded
	v.SetBounds(geometry.NewRect(0, 0, 780, 580))
	mockCExp := &testMockCanvas{}
	v.Draw(ctx, mockCExp)

	var foundExpStatus bool
	for _, dt := range mockCExp.getDrawTexts() {
		if dt.text == expectedShort {
			foundExpStatus = true
			break
		}
	}
	if !foundExpStatus {
		t.Errorf("expected truncated UTF-8 status %q in Expanded state, not found", expectedShort)
	}
}

func TestIsIssueForInstanceIsolation(t *testing.T) {
	// Scenario from user bug report:
	// Two instances pointing to the same Jira Cloud host (e.g. https://avono.atlassian.net)
	// but with different JQL queries and IDs ("inst-3" and "inst-4").
	inst3 := jira.InstanceConfig{
		ID:      "inst-3",
		Name:    "Project Alpha",
		BaseURL: "https://avono.atlassian.net",
	}
	inst4 := jira.InstanceConfig{
		ID:      "inst-4",
		Name:    "Project Beta",
		BaseURL: "https://avono.atlassian.net",
	}

	issueForInst3 := jira.Issue{
		InstanceID:   "inst-3",
		InstanceName: "Project Alpha",
		BaseURL:      "https://avono.atlassian.net",
		Key:          "ALPHA-1",
	}
	issueForInst4 := jira.Issue{
		InstanceID:   "inst-4",
		InstanceName: "Project Beta",
		BaseURL:      "https://avono.atlassian.net",
		Key:          "BETA-1",
	}

	// issueForInst3 must match inst3, NOT inst4
	if !isIssueForInstance(issueForInst3, inst3, 2) {
		t.Errorf("expected issueForInst3 to match inst3")
	}
	if isIssueForInstance(issueForInst3, inst4, 3) {
		t.Errorf("issueForInst3 MUST NOT match inst4 despite identical BaseURL")
	}

	// issueForInst4 must match inst4, NOT inst3
	if !isIssueForInstance(issueForInst4, inst4, 3) {
		t.Errorf("expected issueForInst4 to match inst4")
	}
	if isIssueForInstance(issueForInst4, inst3, 2) {
		t.Errorf("issueForInst4 MUST NOT match inst3 despite identical BaseURL")
	}

	// Mock issues (no InstanceID) should only match primary instance (instIdx == 0)
	mockIss := jira.Issue{Key: "MOCK-1"}
	if !isIssueForInstance(mockIss, inst3, 0) {
		t.Errorf("mock issue should match when instIdx == 0")
	}
	if isIssueForInstance(mockIss, inst4, 1) {
		t.Errorf("mock issue should not match when instIdx > 0")
	}
}

func TestStatusPanelToggle(t *testing.T) {
	view, _ := createTestAppView()
	defer view.Close()

	if view.IsStatusOpen() {
		t.Errorf("status panel should be closed initially")
	}

	// Toggle status panel open
	view.ToggleStatus()
	if !view.IsStatusOpen() {
		t.Errorf("status panel should be open after toggle")
	}
	b := view.Bounds()
	if b.Width() != 780 || b.Height() != 580 {
		t.Errorf("expected bounds 780x580 when status panel is open, got %vx%v", b.Width(), b.Height())
	}

	// Toggle status panel closed
	view.ToggleStatus()
	if view.IsStatusOpen() {
		t.Errorf("status panel should be closed after second toggle")
	}
}

func TestStatusIndicatorClicks(t *testing.T) {
	view, _ := createTestAppView()
	defer view.Close()

	// 1. In Rest state, clicking near top status beacon (Y=14) opens status panel
	view.SetState(window.StateRest)
	view.handleClick(geometry.Pt(18, 14))
	if !view.IsStatusOpen() {
		t.Errorf("expected status panel to open after clicking top beacon in StateRest")
	}

	// Close status panel
	view.CloseStatus()
	if view.IsStatusOpen() {
		t.Errorf("expected status panel to be closed")
	}

	// 2. In Fan state, status indicator is intentionally not present to prioritize instance work items
	view.SetState(window.StateFan)
	w := float32(200)
	view.handleClick(geometry.Pt(w-18, 19))
	if view.IsStatusOpen() {
		t.Errorf("expected status panel NOT to open in StateFan because status is omitted from work items")
	}

	// 3. In Expanded state, clicking top shelf status button opens status panel
	view.SetState(window.StateExpanded)
	tabBarWidth := float32(110)
	tabStartX := float32(780) - tabBarWidth
	view.handleClick(geometry.Pt(tabStartX+12, 18))
	if !view.IsStatusOpen() {
		t.Errorf("expected status panel to open after clicking shelf status button in StateExpanded")
	}
}

func TestSettingsSyncAndStatusConfiguration(t *testing.T) {
	view, _ := createTestAppView()
	defer view.Close()

	reloadInvoked := false
	view.SetOnConfigReload(func() {
		reloadInvoked = true
	})

	view.OpenSettings()
	if !view.showSettings {
		t.Fatalf("expected settings to be open")
	}

	// Simulate typing new poll interval into field 6
	view.mu.Lock()
	view.intervalVal = "120"
	view.statusIntervalVal = "240"
	view.config.StatusCheckEnabled = false
	view.mu.Unlock()

	// Save settings
	view.saveSettings()

	view.mu.Lock()
	savedPoll := view.config.PollInterval
	savedStatusPoll := view.config.StatusPollInterval
	savedStatusEnabled := view.config.StatusCheckEnabled
	view.mu.Unlock()

	if savedPoll != 120 {
		t.Errorf("expected PollInterval 120, got %d", savedPoll)
	}
	if savedStatusPoll != 240 {
		t.Errorf("expected StatusPollInterval 240, got %d", savedStatusPoll)
	}
	if savedStatusEnabled != false {
		t.Errorf("expected StatusCheckEnabled false, got %v", savedStatusEnabled)
	}
	if !reloadInvoked {
		t.Errorf("expected onConfigReload to be invoked on save")
	}
}

func TestNoZoomBugOnStateTransitions(t *testing.T) {
	view, _ := createTestAppView()
	defer view.Close()

	// 1. Open Status overlay (780x580)
	view.OpenStatus()
	if !view.IsStatusOpen() || view.state != window.StateExpanded {
		t.Fatalf("expected Status to be open in StateExpanded")
	}

	// 2. Transition directly to StateRest (e.g. from click away, escape, or timer)
	view.SetState(window.StateRest)
	if view.state != window.StateRest {
		t.Errorf("expected state to be StateRest, got %v", view.state)
	}
	if view.IsStatusOpen() {
		t.Errorf("expected Status overlay to be closed in StateRest")
	}
	w, h := view.computeSize(view.state)
	if w != 36 || h != 224 {
		t.Errorf("expected StateRest size (36, 224), got (%d, %d)", w, h)
	}
	b := view.Bounds()
	if b.Width() != 36 || b.Height() != 224 {
		t.Errorf("expected bounds in StateRest to be 36x224, got %fx%f", b.Width(), b.Height())
	}

	// 3. Open Settings overlay (780x580)
	view.OpenSettings()
	if !view.showSettings || view.state != window.StateExpanded {
		t.Fatalf("expected Settings to be open in StateExpanded")
	}

	// 4. Transition directly to StateRest
	view.SetState(window.StateRest)
	if view.showSettings {
		t.Errorf("expected Settings overlay to be closed in StateRest")
	}
	w, h = view.computeSize(view.state)
	if w != 36 || h != 224 {
		t.Errorf("expected StateRest size (36, 224), got (%d, %d)", w, h)
	}
	b = view.Bounds()
	if b.Width() != 36 || b.Height() != 224 {
		t.Errorf("expected bounds in StateRest to be 36x224, got %fx%f", b.Width(), b.Height())
	}
}

func TestStatusEmptyTicketListAndStateRestore(t *testing.T) {
	view, _ := createTestAppView()
	defer view.Close()

	// Initially in StateRest
	view.SetState(window.StateRest)

	// 1. Calling OpenStatus from StateRest opens StateExpanded
	view.OpenStatus()
	if !view.IsStatusOpen() || view.state != window.StateExpanded {
		t.Fatalf("expected Status open in StateExpanded, got state=%v, isOpen=%v", view.state, view.IsStatusOpen())
	}

	// 2. Snapshot must have empty filteredIssues when status is showing
	snap := view.snapshot()
	if len(snap.filteredIssues) != 0 {
		t.Errorf("expected snapshot.filteredIssues to be empty while viewing status, got %d issues", len(snap.filteredIssues))
	}

	// 3. Hovering over the side rail area must NOT hover any ticket tabs
	view.handleHover(geometry.Pt(780-50, 80))
	if view.hoveredTabIdx != -1 {
		t.Errorf("expected hoveredTabIdx=-1 while viewing status, got %d", view.hoveredTabIdx)
	}

	// 4. Clicking in the rail area where tabs normally are must NOT expand any ticket
	view.activeIdx = 0
	view.handleClick(geometry.Pt(780-50, 80))
	if !view.IsStatusOpen() {
		t.Errorf("clicking in rail during status must not close status or switch ticket")
	}

	// 5. Closing Status restores origin state (StateRest)
	view.CloseStatus()
	if view.IsStatusOpen() {
		t.Errorf("expected status to be closed")
	}
	if view.state != window.StateRest {
		t.Errorf("expected state to return to StateRest, got %v", view.state)
	}

	// 6. Calling OpenStatus from StateFan restores StateFan upon close
	view.SetState(window.StateFan)
	view.OpenStatus()
	if !view.IsStatusOpen() || view.state != window.StateExpanded {
		t.Fatalf("expected Status open in StateExpanded from Fan")
	}
	view.CloseStatus()
	if view.state != window.StateFan {
		t.Errorf("expected state to return to StateFan, got %v", view.state)
	}
}

func TestStatusPanelEightProducts(t *testing.T) {
	view, _ := createTestAppView()
	defer view.Close()

	if len(atlassianCoreApps) != 8 {
		t.Fatalf("expected 8 core apps, got %d", len(atlassianCoreApps))
	}

	expectedNames := []string{
		"Jira Software",
		"Jira Service Management",
		"Confluence",
		"Bitbucket",
		"Atlassian Migrations",
		"Atlassian Analytics",
		"Rovo",
		"Rovo Dev",
	}

	for i, expected := range expectedNames {
		if atlassianCoreApps[i].Name != expected {
			t.Errorf("expected app %d to be %q, got %q", i, expected, atlassianCoreApps[i].Name)
		}
		if atlassianCoreApps[i].URL == "" {
			t.Errorf("expected app %q to have non-empty statuspage URL", expected)
		}
	}

	view.OpenStatus()
	defer view.CloseStatus()

	// Click on one of the product cards in StateExpanded
	// For instance, the 5th product: "Atlassian Migrations" (row 2, col 0)
	cardGap := float32(8)
	_ = (float32(780-110-14) - 40 - cardGap) / 2
	cardH := float32(42)
	gridCardsY := float32(8 + 62 + 48 + 12 + 16)
	cardX := float32(8 + 20)
	cardY := gridCardsY + 2*(cardH+cardGap)

	handled := view.handleClick(geometry.Pt(cardX+10, cardY+10))
	if !handled {
		t.Errorf("expected clicking on product card to be handled")
	}
}

func TestAppViewSetVersionAndSettingsBadge(t *testing.T) {
	cfg := jira.Config{
		Instances: []jira.InstanceConfig{
			{Name: "TestInst", BaseURL: "https://test.atlassian.net"},
		},
	}
	client := jira.NewClient(cfg)
	view := NewAppView(cfg, client, func() {})
	defer view.Close()

	if view.snapshot().version != "" {
		t.Errorf("expected empty version initially, got %q", view.snapshot().version)
	}

	view.SetVersion("1.0.5")
	snap := view.snapshot()
	if snap.version != "1.0.5" {
		t.Errorf("expected version 1.0.5 in snapshot, got %q", snap.version)
	}
}

func TestFanStateFourWorkItemsClickable(t *testing.T) {
	cfg := jira.Config{
		StatusCheckEnabled: true,
		Instances: []jira.InstanceConfig{
			{Name: "TestInst", BaseURL: "https://test.atlassian.net"},
		},
	}
	client := jira.NewClient(cfg)
	view := NewAppView(cfg, client, func() {})
	defer view.Close()

	// 4 test issues
	issues := []jira.Issue{
		{Key: "PROJ-1", Summary: "Issue 1", Status: jira.Status{Name: "To Do"}},
		{Key: "PROJ-2", Summary: "Issue 2", Status: jira.Status{Name: "In Progress"}},
		{Key: "PROJ-3", Summary: "Issue 3", Status: jira.Status{Name: "In Review"}},
		{Key: "PROJ-4", Summary: "Issue 4", Status: jira.Status{Name: "Done"}},
	}
	view.mu.Lock()
	view.issues = issues
	view.invalidateFilterCacheLocked()
	view.mu.Unlock()

	view.SetState(window.StateFan)
	w, h := view.computeSize(window.StateFan)
	view.SetBounds(geometry.NewRect(0, 0, float32(w), float32(h)))

	// 4th issue is at index 3:
	// tabStartY = 60
	// tab 0: 60..122
	// tab 1: 128..190
	// tab 2: 196..258
	// tab 3: 264..326
	hovered := view.handleHover(geometry.Pt(50, 295))
	if !hovered {
		t.Errorf("expected handleHover to return true for 4th issue tab")
	}
	view.mu.Lock()
	hovIdx := view.hoveredTabIdx
	view.mu.Unlock()
	if hovIdx != 3 {
		t.Errorf("expected hoveredTabIdx to be 3 (the 4th issue), got %d", hovIdx)
	}

	clicked := view.handleClick(geometry.Pt(50, 295))
	if !clicked {
		t.Errorf("expected clicking on 4th issue tab to be handled")
	}
	view.mu.Lock()
	st := view.state
	active := view.activeIdx
	view.mu.Unlock()
	if st != window.StateExpanded {
		t.Errorf("expected state to expand to StateExpanded, got %v", st)
	}
	if active != 3 {
		t.Errorf("expected activeIdx to be 3 (PROJ-4), got %d", active)
	}
}
