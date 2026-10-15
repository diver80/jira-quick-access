package ui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"

	"jira-quick-access/pkg/jira"
	"jira-quick-access/pkg/window"
)

func twoInstanceConfig(baseURL string) jira.Config {
	cfg := jira.DefaultConfig()
	cfg.Instances = []jira.InstanceConfig{
		{ID: "a", Name: "A", BaseURL: baseURL, Email: "a@test", APIToken: "t"},
		{ID: "b", Name: "B", BaseURL: baseURL, Email: "b@test", APIToken: "t"},
	}
	cfg.ActiveInstID = "b"
	return cfg
}

// Two sites can both have OPS-12; arrow navigation must stay on the selected instance's ticket.
func TestArrowNavigationDistinguishesInstancesWithSameKey(t *testing.T) {
	cfg := twoInstanceConfig("https://jira.test")
	cfg.DemoMode = true
	view := NewAppView(cfg, jira.NewClient(cfg), nil)
	defer view.Close()
	view.SetState(window.StateFan)
	view.SetBounds(geometry.NewRect(0, 0, 120, 500))

	view.mu.Lock()
	view.activeInstIdx = 1
	view.issues = []jira.Issue{
		{InstanceID: "a", Key: "OPS-12"},
		{InstanceID: "b", Key: "OPS-12"},
		{InstanceID: "b", Key: "OPS-13"},
	}
	view.activeIdx = 1
	view.invalidateFilterCacheLocked()
	view.mu.Unlock()

	view.handleKey(event.NewKeyEvent(event.KeyPress, event.KeyDown, 0, 0))
	if view.activeIdx != 2 {
		t.Fatalf("Down: expected B/OPS-13 (2), got %d", view.activeIdx)
	}
	view.handleKey(event.NewKeyEvent(event.KeyPress, event.KeyUp, 0, 0))
	if view.activeIdx != 1 {
		t.Fatalf("Up: expected B/OPS-12 (1), not A/OPS-12, got %d", view.activeIdx)
	}
}

// A refresh that reorders results must keep the selection on the same ticket.
func TestRefreshKeepsSelectedTicket(t *testing.T) {
	var reordered atomic.Bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		issue := func(key string) string {
			return fmt.Sprintf(`{"id":"%s","key":"%s","fields":{"summary":"s","status":{"name":"Open"}}}`, key, key)
		}
		if reordered.Load() {
			fmt.Fprintf(w, `{"startAt":0,"total":2,"issues":[%s,%s]}`, issue("X-2"), issue("X-1"))
			return
		}
		fmt.Fprintf(w, `{"startAt":0,"total":2,"issues":[%s,%s]}`, issue("X-1"), issue("X-2"))
	}))
	defer ts.Close()

	cfg := jira.DefaultConfig()
	cfg.Instances = []jira.InstanceConfig{{ID: "a", Name: "A", BaseURL: ts.URL, Email: "a@test", APIToken: "t"}}
	view := NewAppView(cfg, jira.NewClient(cfg), nil)
	defer view.Close()

	waitForIssues := func(firstKey string) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			view.mu.Lock()
			ok := len(view.issues) == 2 && view.issues[0].Key == firstKey
			view.mu.Unlock()
			if ok {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("refresh did not deliver issues starting with %s", firstKey)
	}

	view.RefreshIssues()
	waitForIssues("X-1")
	view.mu.Lock()
	view.activeIdx = 0 // X-1
	view.mu.Unlock()

	reordered.Store(true)
	view.RefreshIssues()
	waitForIssues("X-2")

	view.mu.Lock()
	defer view.mu.Unlock()
	if got := view.issues[view.activeIdx].Key; got != "X-1" {
		t.Fatalf("selection jumped to %s after reorder", got)
	}
}
