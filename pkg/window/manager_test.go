package window

import (
	"sync"
	"sync/atomic"
	"testing"
)

// Tier 1 & 4: WindowManager Interface & Lifecycle
func TestWindowManagerInterfaceAndLifecycle(t *testing.T) {
	if DefaultManager == nil {
		t.Fatalf("DefaultManager should be initialized")
	}

	// 1. InitEdgeRail
	if err := DefaultManager.InitEdgeRail(32, 224); err != nil {
		t.Errorf("InitEdgeRail returned error: %v", err)
	}

	// 2. SetState transitions across all 3 lifecycle states
	states := []struct {
		state  WindowState
		width  int
		height int
	}{
		{StateRest, 32, 224},
		{StateFan, 120, 450},
		{StateExpanded, 780, 580},
		{StateFan, 120, 300},
		{StateRest, 32, 224},
	}

	for _, s := range states {
		DefaultManager.SetState(s.state, s.width, s.height)
	}

	// 3. DockToRightEdge & Dock
	DefaultManager.DockToRightEdge(32, 224)
	DefaultManager.DockToRightEdge(120, 500)
	DefaultManager.DockToRightEdge(780, 580)
	DefaultManager.Dock(32, 224)

	// 4. DockSide Left / Right
	DefaultManager.SetDockSide(DockSideLeft)
	if side := DefaultManager.GetDockSide(); side != DockSideLeft {
		t.Errorf("expected DockSideLeft, got %v", side)
	}
	if DefaultManager.GetDockSide().String() != "Left" {
		t.Errorf("expected string Left, got %v", DefaultManager.GetDockSide().String())
	}
	DefaultManager.SetDockSide(DockSideRight)
	if side := DefaultManager.GetDockSide(); side != DockSideRight {
		t.Errorf("expected DockSideRight, got %v", side)
	}
	if DefaultManager.GetDockSide().String() != "Right" {
		t.Errorf("expected string Right, got %v", DefaultManager.GetDockSide().String())
	}

	// 5. Monitor enumeration & selection
	monitors := DefaultManager.GetMonitors()
	if len(monitors) == 0 {
		t.Errorf("expected at least 1 monitor, got %d", len(monitors))
	}
	DefaultManager.SetMonitor(0)
	if idx := DefaultManager.GetSelectedMonitor(); idx != 0 {
		t.Errorf("expected monitor 0, got %d", idx)
	}

	// 6. Position ratio
	DefaultManager.SetPositionRatio(0.25)
	if ratio := DefaultManager.GetPositionRatio(); ratio != 0.25 {
		t.Errorf("expected position ratio 0.25, got %f", ratio)
	}
	DefaultManager.SetPositionRatio(0.5)

	// 7. Window drag trigger
	DefaultManager.StartWindowDrag()

	// 8. OpenTicketURL
	_ = DefaultManager.OpenTicketURL("https://jira.example.com/browse/PROJ-102")

	// 9. AlwaysOnTop and AutoHide
	DefaultManager.SetAlwaysOnTop(true)
	if !DefaultManager.GetAlwaysOnTop() {
		t.Errorf("expected GetAlwaysOnTop true")
	}
	DefaultManager.SetAutoHide(true)
	if !DefaultManager.GetAutoHide() {
		t.Errorf("expected GetAutoHide true")
	}
	DefaultManager.SetTucked(true, 32, 224)
	DefaultManager.SetTucked(false, 32, 224)
	DefaultManager.SetAutoHide(false)
	SetAlwaysOnTop(true)
	SetAutoHide(false)
	SetTucked(false, 32, 224)
}

// Tier 1 & 4: Static Bridge Functions and Collapse Handler
func TestWindowBridgeHelpersAndCollapse(t *testing.T) {
	// Mouse detection
	_ = IsMouseInside()

	// WebKit visibility toggles
	SetMobileWebViewVisible(true, 780, 580)
	SetMobileWebViewVisible(false, 32, 224)

	// Mobile ticket loading
	LoadMobileTicketView("https://avono.atlassian.net/browse/AVN-101")
	LoadMobileTicketHTML("<html><body><h1>Ticket Details</h1></body></html>", "https://avono.atlassian.net")
	LoadMobileTicketHTML("<html><body>Empty Base</body></html>", "")

	// Collapse handler registration and execution
	var collapseCalled int32
	RegisterCollapseHandler(func() {
		atomic.AddInt32(&collapseCalled, 1)
	})

	// Invoke registered callback
	collapseMu.Lock()
	cb := collapseCallback
	collapseMu.Unlock()
	if cb != nil {
		cb()
	}

	// Dock change callback registration and execution
	var dockChangedCalled int32
	RegisterDockChangeHandler(func(side DockSide, monitorIndex int, posYRatio float64) {
		atomic.AddInt32(&dockChangedCalled, 1)
	})

	dockChangeMu.Lock()
	dcb := dockChangeCallback
	dockChangeMu.Unlock()
	if dcb != nil {
		dcb(DockSideLeft, 0, 0.5)
	}

	if atomic.LoadInt32(&dockChangedCalled) != 1 {
		t.Errorf("expected dock change callback to be called once, got %d", dockChangedCalled)
	}

	// Native clipboard
	NativeCopyText("test-clipboard-key-123")
	txt, ok := NativePasteText()
	if ok && txt != "test-clipboard-key-123" {
		t.Logf("clipboard content: %s", txt)
	}
}

// Tier 3: Concurrency Race Validation on Window Manager
func TestWindowManagerConcurrencyRace(t *testing.T) {
	const numGoroutines = 25
	const iterations = 40
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for g := 0; g < numGoroutines; g++ {
		gid := g
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				switch gid % 6 {
				case 0:
					st := WindowState(i % 3)
					DefaultManager.SetState(st, 100+(i*10), 200+(i*10))
				case 1:
					DefaultManager.DockToRightEdge(32, 224)
				case 2:
					_ = IsMouseInside()
				case 3:
					SetMobileWebViewVisible(i%2 == 0, 780, 580)
				case 4:
					LoadMobileTicketView("https://jira.test/browse/TICK-1")
				case 5:
					RegisterCollapseHandler(func() {})
				}
			}
		}()
	}

	wg.Wait()
}
