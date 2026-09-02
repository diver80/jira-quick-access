package ui

import (
	"sync"
	"testing"
	"time"

	"jira-quick-access/pkg/jira"
	"jira-quick-access/pkg/window"

	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
)

func TestAppViewStateTransitionsAndLifecycle(t *testing.T) {
	cfg := jira.DefaultConfig()
	client := jira.NewClient(cfg)
	view := NewAppView(cfg, client, nil)
	defer view.Close()

	// Initial State: Rest
	if view.state != window.StateRest {
		t.Errorf("expected initial state Rest, got %v", view.state)
	}
	w, h := view.computeSize(view.state)
	if w != 32 || h != 224 {
		t.Errorf("expected Rest dimensions (32, 224), got (%d, %d)", w, h)
	}

	// Transition to Fan
	view.SetState(window.StateFan)
	if view.state != window.StateFan {
		t.Errorf("expected state Fan, got %v", view.state)
	}
	w, h = view.computeSize(view.state)
	if w != 120 {
		t.Errorf("expected Fan width 120, got %d", w)
	}

	// Transition to Expanded
	view.SetState(window.StateExpanded)
	if view.state != window.StateExpanded {
		t.Errorf("expected state Expanded, got %v", view.state)
	}
	w, h = view.computeSize(view.state)
	if w != 780 || h != 580 {
		t.Errorf("expected Expanded dimensions (780, 580), got (%d, %d)", w, h)
	}

	// Open Settings
	view.OpenSettings()
	if !view.showSettings || view.state != window.StateExpanded {
		t.Errorf("expected showSettings=true and StateExpanded, got showSettings=%v, state=%v", view.showSettings, view.state)
	}

	// Toggle Settings
	view.ToggleSettings()
	if view.showSettings {
		t.Errorf("expected showSettings=false after toggle, got true")
	}
}

func TestAppViewMultiInstanceSettingsInteractions(t *testing.T) {
	cfg := jira.Config{
		Instances: []jira.InstanceConfig{
			{
				ID:       "inst-1",
				Name:     "avono",
				BaseURL:  "https://avono.atlassian.net",
				Email:    "frank.hess@avono.de",
				APIToken: "token-avono",
				JQLQuery: "assignee = currentUser()",
			},
			{
				ID:       "inst-2",
				Name:     "avono DC",
				BaseURL:  "https://jira.avono.de",
				Email:    "frank.hess@avono.de",
				APIToken: "token-dc",
				JQLQuery: "project = AVN",
			},
		},
	}
	client := jira.NewClient(cfg)
	view := NewAppView(cfg, client, nil)
	defer view.Close()

	view.OpenSettings()

	// 1. Check loaded initial fields for inst-1
	view.mu.Lock()
	if view.nameVal != "avono" || view.urlVal != "https://avono.atlassian.net" {
		t.Errorf("expected avono fields loaded, got name=%q, url=%q", view.nameVal, view.urlVal)
	}
	view.mu.Unlock()

	// 2. Switch to tab index 1 (avono DC)
	view.mu.Lock()
	view.saveCurrentInstanceFieldsLocked()
	view.loadInstanceFieldsLocked(1)
	view.mu.Unlock()

	view.mu.Lock()
	if view.nameVal != "avono DC" || view.urlVal != "https://jira.avono.de" {
		t.Errorf("expected avono DC fields loaded, got name=%q, url=%q", view.nameVal, view.urlVal)
	}
	view.mu.Unlock()

	// 3. Edit field value for avono DC
	view.mu.Lock()
	view.activeField = 1 // Name field
	view.mu.Unlock()

	// Type ' Server' into name
	for _, ch := range " Server" {
		ev := event.NewKeyEvent(event.KeyPress, event.KeySpace, ch, event.ModNone)
		view.handleKey(ev)
	}

	view.mu.Lock()
	if view.nameVal != "avono DC Server" {
		t.Errorf("expected nameVal 'avono DC Server', got %q", view.nameVal)
	}
	view.saveCurrentInstanceFieldsLocked()
	view.mu.Unlock()

	// 4. Add a 3rd instance
	view.mu.Lock()
	view.config.Instances = append(view.config.Instances, jira.InstanceConfig{
		ID:       "inst-3",
		Name:     "Sandbox",
		BaseURL:  "https://sandbox.atlassian.net",
		Email:    "frank@sandbox.de",
		APIToken: "token-sandbox",
	})
	view.loadInstanceFieldsLocked(2)
	view.mu.Unlock()

	view.mu.Lock()
	if len(view.config.Instances) != 3 || view.nameVal != "Sandbox" {
		t.Errorf("expected 3 instances and Sandbox loaded, got len=%d, name=%q", len(view.config.Instances), view.nameVal)
	}
	view.mu.Unlock()

	// 5. Delete an instance (delete inst-2 at index 1)
	view.mu.Lock()
	view.config.Instances = append(view.config.Instances[:1], view.config.Instances[2:]...)
	view.loadInstanceFieldsLocked(1) // Now Sandbox is at index 1
	view.mu.Unlock()

	view.mu.Lock()
	if len(view.config.Instances) != 2 || view.nameVal != "Sandbox" {
		t.Errorf("expected 2 instances remaining with Sandbox at idx 1, got len=%d, name=%q", len(view.config.Instances), view.nameVal)
	}
	view.mu.Unlock()

	// 6. Save settings
	view.saveSettings()
	view.mu.Lock()
	if view.showSettings {
		t.Errorf("expected showSettings to be false after saveSettings")
	}
	if view.state != window.StateFan {
		t.Errorf("expected state to return to StateFan after saveSettings, got %v", view.state)
	}
	view.mu.Unlock()
}

func TestAppViewSearchFilteringAndMemoizationExhaustive(t *testing.T) {
	cfg := jira.Config{
		Instances: []jira.InstanceConfig{
			{ID: "inst-1", Name: "Org A", BaseURL: "https://orga.atlassian.net"},
			{ID: "inst-2", Name: "Org B", BaseURL: "https://orgb.atlassian.net"},
		},
	}
	client := jira.NewClient(cfg)
	view := NewAppView(cfg, client, nil)
	defer view.Close()

	view.mu.Lock()
	view.issues = []jira.Issue{
		{Key: "A-101", Summary: "Deploy Kubernetes Pods", InstanceID: "inst-1", Status: jira.Status{Name: "To Do"}},
		{Key: "A-102", Summary: "Fix Memory Leak in WebKit", InstanceID: "inst-1", Status: jira.Status{Name: "In Progress"}},
		{Key: "B-201", Summary: "Update Jira REST API v3", InstanceID: "inst-2", Status: jira.Status{Name: "In Review"}},
		{Key: "B-202", Summary: "Verify Race Detector with Go 1.27", InstanceID: "inst-2", Status: jira.Status{Name: "Done"}},
	}
	view.activeInstIdx = 0
	view.invalidateFilterCacheLocked()
	view.mu.Unlock()

	// Filter for Org A
	filtered := view.getFilteredIssues()
	if len(filtered) != 2 {
		t.Fatalf("expected 2 issues for Org A, got %d", len(filtered))
	}

	// Search for 'leak' in Summary
	view.mu.Lock()
	view.searchQuery = "leak"
	view.invalidateFilterCacheLocked()
	view.mu.Unlock()

	filtered = view.getFilteredIssues()
	if len(filtered) != 1 || filtered[0].Key != "A-102" {
		t.Fatalf("expected issue A-102 for 'leak', got %+v", filtered)
	}

	// Search for 'TO DO' in Status (case-insensitive)
	view.mu.Lock()
	view.searchQuery = "TO DO"
	view.invalidateFilterCacheLocked()
	view.mu.Unlock()

	filtered = view.getFilteredIssues()
	if len(filtered) != 1 || filtered[0].Key != "A-101" {
		t.Fatalf("expected issue A-101 for 'TO DO', got %+v", filtered)
	}

	// Search for '101' in Key
	view.mu.Lock()
	view.searchQuery = "101"
	view.invalidateFilterCacheLocked()
	view.mu.Unlock()

	filtered = view.getFilteredIssues()
	if len(filtered) != 1 || filtered[0].Key != "A-101" {
		t.Fatalf("expected issue A-101 for '101', got %+v", filtered)
	}

	// Switch to Org B with query 'race'
	view.mu.Lock()
	view.activeInstIdx = 1
	view.searchQuery = "race"
	view.invalidateFilterCacheLocked()
	view.mu.Unlock()

	filtered = view.getFilteredIssues()
	if len(filtered) != 1 || filtered[0].Key != "B-202" {
		t.Fatalf("expected issue B-202 for 'race' on Org B, got %+v", filtered)
	}
}

func TestAppViewModifierKeyGuardInFanState(t *testing.T) {
	cfg := jira.DefaultConfig()
	client := jira.NewClient(cfg)
	view := NewAppView(cfg, client, nil)
	defer view.Close()

	view.SetState(window.StateFan)

	// Send modified key combinations: Cmd+C, Cmd+V, Cmd+W, Cmd+A, Cmd+Z
	shortcuts := []struct {
		key  event.Key
		rune rune
		mod  event.Modifiers
	}{
		{event.KeyC, 'c', event.Modifiers(1)},
		{event.KeyV, 'v', event.Modifiers(1)},
		{event.KeyW, 'w', event.Modifiers(1)},
		{event.KeyA, 'a', event.Modifiers(1)},
		{event.KeyZ, 'z', event.Modifiers(1)},
		{event.KeyC, 'c', event.Modifiers(2)},
		{event.KeyV, 'v', event.Modifiers(2)},
		{event.KeyA, 'a', event.Modifiers(4)},
	}

	for _, sc := range shortcuts {
		ev := event.NewKeyEvent(event.KeyPress, sc.key, sc.rune, sc.mod)
		view.handleKey(ev)
	}

	view.mu.Lock()
	query := view.searchQuery
	view.mu.Unlock()

	if query != "" {
		t.Errorf("expected search query to remain empty after modifier keys, got %q", query)
	}

	// Type printable letters: 'P', 'R', 'O', 'J'
	for _, r := range "PROJ" {
		ev := event.NewKeyEvent(event.KeyPress, event.KeyP, r, event.ModNone)
		view.handleKey(ev)
	}

	view.mu.Lock()
	query = view.searchQuery
	view.mu.Unlock()

	if query != "PROJ" {
		t.Errorf("expected search query 'PROJ', got %q", query)
	}

	// Backspace deletes one letter -> 'PRO'
	bsEv := event.NewKeyEvent(event.KeyPress, event.KeyBackspace, 0, event.ModNone)
	view.handleKey(bsEv)

	view.mu.Lock()
	query = view.searchQuery
	view.mu.Unlock()

	if query != "PRO" {
		t.Errorf("expected search query 'PRO' after backspace, got %q", query)
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
}

func TestAppViewConcurrentStress(t *testing.T) {
	cfg := jira.DefaultConfig()
	client := jira.NewClient(cfg)
	view := NewAppView(cfg, client, nil)
	defer view.Close()

	view.mu.Lock()
	view.issues = []jira.Issue{
		{Key: "STRESS-1", Summary: "Stress Issue 1", InstanceID: "inst-1", Status: jira.Status{Name: "To Do"}},
		{Key: "STRESS-2", Summary: "Stress Issue 2", InstanceID: "inst-1", Status: jira.Status{Name: "In Progress"}},
	}
	view.mu.Unlock()

	const numGoroutines = 30
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
					_ = view.snapshot()
				case 1:
					_ = view.getFilteredIssues()
				case 2:
					view.SetState(window.WindowState(i % 3))
				case 3:
					view.handleHover(geometry.Pt(float32(10+i), float32(20+i)))
				case 4:
					view.handleClick(geometry.Pt(float32(15), float32(30)))
				case 5:
					ev := event.NewKeyEvent(event.KeyPress, event.KeyA, 'a', event.ModNone)
					view.handleKey(ev)
				}
				time.Sleep(1 * time.Millisecond)
			}
		}()
	}

	wg.Wait()
}
