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

	// 3. DockToRightEdge
	DefaultManager.DockToRightEdge(32, 224)
	DefaultManager.DockToRightEdge(120, 500)
	DefaultManager.DockToRightEdge(780, 580)

	// 4. OpenTicketURL
	_ = DefaultManager.OpenTicketURL("https://jira.example.com/browse/PROJ-102")
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

	if atomic.LoadInt32(&collapseCalled) != 1 {
		t.Errorf("expected collapse callback to be called once, got %d", collapseCalled)
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
