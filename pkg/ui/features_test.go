package ui

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gogpu/ui/geometry"

	"jira-quick-access/pkg/jira"
	"jira-quick-access/pkg/update"
	"jira-quick-access/pkg/window"
)

func TestBranchName(t *testing.T) {
	cases := []struct {
		prefix, summary, want string
	}{
		{"feature/", "Fix login timeout!", "feature/OPS-1-fix-login-timeout"},
		{"", "  --Ünïcode & spaces--  ", "feature/OPS-1-n-code-spaces"},
		{"bugfix/", "!!!", "bugfix/OPS-1"},
		{"f/", "a very long summary that keeps going well past the forty character limit", "f/OPS-1-a-very-long-summary-that-keeps-going"},
	}
	for _, c := range cases {
		if got := branchName(c.prefix, jira.Issue{Key: "OPS-1", Summary: c.summary}); got != c.want {
			t.Errorf("branchName(%q, %q) = %q, want %q", c.prefix, c.summary, got, c.want)
		}
	}
}

func newFeatureView(t *testing.T) *AppView {
	t.Helper()
	cfg := twoInstanceConfig("https://jira.test")
	view := NewAppView(cfg, jira.NewClient(cfg), nil)
	t.Cleanup(view.Close)
	return view
}

func iss(inst, key, status string) jira.Issue {
	return jira.Issue{InstanceID: inst, Key: key, Summary: key + " summary", Status: jira.Status{Name: status}}
}

func TestRefreshResultKeepsTicketsOfFailedInstanceAndNotifiesOnlyRealChanges(t *testing.T) {
	view := newFeatureView(t)
	view.mu.Lock()
	defer view.mu.Unlock()

	// First refresh sets the baseline and announces nothing.
	changes, failures := view.applyFetchResultLocked(jira.FetchResult{
		Issues: []jira.Issue{iss("a", "A-1", "To Do"), iss("b", "B-1", "To Do")},
		Loaded: map[string]bool{"a": true, "b": true},
	})
	if len(changes) != 0 || len(failures) != 0 {
		t.Fatalf("first refresh must be silent, got %v %v", changes, failures)
	}

	// B fails: its ticket stays, A reports a status change and a new ticket.
	changes, failures = view.applyFetchResultLocked(jira.FetchResult{
		Issues: []jira.Issue{iss("a", "A-1", "In Progress"), iss("a", "A-2", "To Do")},
		Loaded: map[string]bool{"a": true},
		Failed: map[string]error{"b": errors.New("HTTP 401")},
	})
	if len(failures) != 1 || view.instErrors["b"] == "" {
		t.Fatalf("expected B's failure to be reported once, got %v / %v", failures, view.instErrors)
	}
	if indexOfIssue(view.issues, iss("b", "B-1", "")) < 0 {
		t.Fatal("tickets of a failed instance must be kept")
	}
	if len(changes) != 2 || changes[0].oldStatus != "To Do" || !changes[1].isNew {
		t.Fatalf("expected status change of A-1 and new A-2, got %+v", changes)
	}

	// B still failing: no second error toast.
	_, failures = view.applyFetchResultLocked(jira.FetchResult{
		Issues: []jira.Issue{iss("a", "A-1", "In Progress"), iss("a", "A-2", "To Do")},
		Loaded: map[string]bool{"a": true},
		Failed: map[string]error{"b": errors.New("HTTP 401")},
	})
	if len(failures) != 0 {
		t.Fatalf("a known failure must not be reported again, got %v", failures)
	}

	// B recovers with an extra ticket: compared against its earlier baseline.
	changes, _ = view.applyFetchResultLocked(jira.FetchResult{
		Issues: []jira.Issue{iss("a", "A-1", "In Progress"), iss("a", "A-2", "To Do"), iss("b", "B-1", "To Do"), iss("b", "B-2", "To Do")},
		Loaded: map[string]bool{"a": true, "b": true},
	})
	if len(view.instErrors) != 0 {
		t.Fatalf("recovered instance must clear its error, got %v", view.instErrors)
	}
	if len(changes) != 1 || changes[0].issue.Key != "B-2" {
		t.Fatalf("expected only B-2 to be new after recovery, got %+v", changes)
	}
}

func TestRefreshResultDoesNotAnnounceNewlyAddedInstance(t *testing.T) {
	view := newFeatureView(t)
	view.mu.Lock()
	defer view.mu.Unlock()
	view.applyFetchResultLocked(jira.FetchResult{
		Issues: []jira.Issue{iss("a", "A-1", "To Do")},
		Loaded: map[string]bool{"a": true},
	})
	changes, _ := view.applyFetchResultLocked(jira.FetchResult{
		Issues: []jira.Issue{iss("a", "A-1", "To Do"), iss("b", "B-1", "To Do")},
		Loaded: map[string]bool{"a": true, "b": true},
	})
	if len(changes) != 0 {
		t.Fatalf("tickets of an instance seen for the first time are not news, got %+v", changes)
	}
}

func TestStatusChangeOfRecentlyViewedTicketIsNotAnnounced(t *testing.T) {
	view := newFeatureView(t)
	view.mu.Lock()
	defer view.mu.Unlock()
	view.applyFetchResultLocked(jira.FetchResult{
		Issues: []jira.Issue{iss("a", "A-1", "To Do"), iss("a", "A-2", "To Do")},
		Loaded: map[string]bool{"a": true},
	})
	view.viewedAt[issueID(iss("a", "A-1", ""))] = time.Now()
	changes, _ := view.applyFetchResultLocked(jira.FetchResult{
		Issues: []jira.Issue{iss("a", "A-1", "Done"), iss("a", "A-2", "Done")},
		Loaded: map[string]bool{"a": true},
	})
	if len(changes) != 1 || changes[0].issue.Key != "A-2" {
		t.Fatalf("only A-2 (not opened in the panel) should be announced, got %+v", changes)
	}
}

func TestSortModeKeepsSelection(t *testing.T) {
	view := newFeatureView(t)
	view.mu.Lock()
	view.issues = []jira.Issue{
		{InstanceID: "a", Key: "A-1", Priority: jira.Priority{Name: "Low"}},
		{InstanceID: "a", Key: "A-2", Priority: jira.Priority{Name: "High"}},
	}
	view.activeIdx = 0
	view.mu.Unlock()

	view.SetSortMode(jira.SortPriority)
	if view.issues[0].Key != "A-2" || view.issues[view.activeIdx].Key != "A-1" {
		t.Fatalf("expected A-2 first and A-1 still selected, got %+v (active %d)", view.issues, view.activeIdx)
	}
}

func TestOrderIssuesGroupsInstancesInJiraMode(t *testing.T) {
	instances := []jira.InstanceConfig{{ID: "a"}, {ID: "b"}}
	issues := []jira.Issue{{InstanceID: "b", Key: "B-1"}, {InstanceID: "a", Key: "A-1"}, {InstanceID: "b", Key: "B-2"}}
	orderIssues(issues, instances, jira.SortJira)
	if issues[0].Key != "A-1" || issues[1].Key != "B-1" || issues[2].Key != "B-2" {
		t.Fatalf("unexpected order %+v", issues)
	}
}

func TestGlobalHotKeyTogglesFanWithSearch(t *testing.T) {
	view := newFeatureView(t)
	view.HandleGlobalHotKey()
	if view.state != window.StateFan || !view.searchActive {
		t.Fatalf("hotkey should open the fan with search focused, got state %v search %v", view.state, view.searchActive)
	}
	view.HandleGlobalHotKey()
	if view.state != window.StateRest {
		t.Fatalf("second press should collapse, got %v", view.state)
	}
}

func TestManualUpdateCheck(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"tag_name":"v9.0.0","html_url":"https://example.test/r"}`)
	}))
	defer ts.Close()

	view := newFeatureView(t)
	view.SetVersion("1.1.0")
	view.mu.Lock()
	view.updateChecker = update.Checker{Endpoint: ts.URL}
	view.mu.Unlock()

	view.checkForUpdate(true)
	s := view.snapshot()
	if s.update.Version != "9.0.0" || s.update.URL == "" {
		t.Fatalf("expected update 9.0.0, got %+v (%s)", s.update, s.statusMsg)
	}

	view.SetUpdateCheckEnabled(false)
	if view.snapshot().update.Version != "" {
		t.Fatal("disabling the check should hide the update badge")
	}
}

func TestSystemSettingsPageRendersAndSwitches(t *testing.T) {
	view := newFeatureView(t)
	view.OpenSettings()
	r := geometry.NewRect(8, 8, 654, 564)
	l := newSettingsLayout(r, 2, 0, 1)
	view.handleSettingsClick(r, geometry.Pt(l.systemTab.Min.X+5, l.systemTab.Min.Y+5))
	if view.settingsSection != settingsSystem {
		t.Fatalf("expected the system page, got %v", view.settingsSection)
	}

	canvas := &testMockCanvas{}
	view.drawSettingsOverlay(&testMockContext{}, canvas, r, view.snapshot())
	labels := map[string]bool{}
	for _, call := range canvas.getDrawTexts() {
		labels[call.text] = true
	}
	for _, label := range []string{"Startup & shortcut", "Notifications", "Ticket order", "Updates", "Hotkey ⌥⌘J: On"} {
		if !labels[label] {
			t.Errorf("missing label %q", label)
		}
	}

	view.handleSettingsClick(r, geometry.Pt(l.sortButtons[2].Min.X+5, l.sortButtons[2].Min.Y+5))
	if view.config.SortMode != jira.SortPriority {
		t.Fatalf("priority button should set the sort mode, got %q", view.config.SortMode)
	}
	view.handleSettingsClick(r, geometry.Pt(l.globalHotKey.Min.X+5, l.globalHotKey.Min.Y+5))
	if view.config.GlobalHotKey {
		t.Fatal("hotkey button should turn the hotkey off")
	}
}
