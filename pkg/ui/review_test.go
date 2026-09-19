package ui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"jira-quick-access/pkg/jira"
	"jira-quick-access/pkg/status"
	"jira-quick-access/pkg/window"
)

func TestEmptyFilterResultIsCached(t *testing.T) {
	v := &AppView{
		config:      jira.Config{Instances: []jira.InstanceConfig{{ID: "one"}}},
		issues:      []jira.Issue{{InstanceID: "one", Key: "TEST-1", Summary: "A summary"}},
		searchQuery: "no match", cacheDirty: true,
	}
	if got := v.getFilteredIssues(); len(got) != 0 {
		t.Fatalf("expected no matches, got %v", got)
	}
	if allocs := testing.AllocsPerRun(100, func() { v.getFilteredIssues() }); allocs != 0 {
		t.Fatalf("cached empty search allocated %v times", allocs)
	}
	v.mu.Lock()
	v.searchQuery = "summary"
	v.invalidateFilterCacheLocked()
	v.mu.Unlock()
	if got := v.getFilteredIssues(); len(got) != 1 {
		t.Fatalf("invalidated filter returned %d issues", len(got))
	}
}

func TestStatusRefreshCancelledOnClose(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	v := &AppView{ctx: ctx, cancel: cancel,
		config:       jira.Config{StatusCheckEnabled: true},
		statusClient: status.NewClientWithURL(server.URL),
	}
	v.RefreshStatus()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("status request never started")
	}
	closed := make(chan struct{})
	go func() { v.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not cancel the status request promptly")
	}
}

func TestSettingsEditsDoNotMutateClient(t *testing.T) {
	v, client := createTestAppView()
	defer v.Close()
	before := client.GetConfig()
	v.mu.Lock()
	v.nameVal = "unsaved name"
	v.tokenVal = "unsaved-token"
	v.saveCurrentInstanceFieldsLocked()
	v.mu.Unlock()
	after := client.GetConfig()
	if after.Instances[0] != before.Instances[0] {
		t.Fatal("unsaved UI settings changed the client's configuration")
	}
}

func TestSnapshotCopiesOnlyRenderedIssues(t *testing.T) {
	for _, st := range []window.WindowState{window.StateRest, window.StateFan, window.StateExpanded} {
		v := &AppView{state: st, cacheDirty: true,
			config: jira.Config{Instances: []jira.InstanceConfig{{ID: "one"}}, PinnedKeys: []string{"TEST-1"}},
			issues: []jira.Issue{{InstanceID: "one", Key: "TEST-1"}},
		}
		snap := v.snapshot()
		if (len(snap.issues) > 0) != (st != window.StateFan) {
			t.Fatalf("unexpected full issue copy for state %v", st)
		}
		if (len(snap.filteredIssues) > 0) != (st != window.StateRest) {
			t.Fatalf("unexpected filtered issue copy for state %v", st)
		}
		snap.config.Instances[0].Name = "snapshot edit"
		snap.config.PinnedKeys[0] = "snapshot pin"
		if v.config.Instances[0].Name != "" || v.config.PinnedKeys[0] != "TEST-1" {
			t.Fatal("snapshot exposes mutable configuration")
		}
		if len(snap.issues) > 0 {
			snap.issues[0].Key = "changed"
		}
		if len(snap.filteredIssues) > 0 {
			snap.filteredIssues[0].Key = "changed"
		}
		if v.issues[0].Key != "TEST-1" {
			t.Fatal("snapshot exposes mutable issues")
		}
		v.showStatus = true
		if snap := v.snapshot(); len(snap.issues) != 0 || len(snap.filteredIssues) != 0 {
			t.Fatal("status snapshot copied unused ticket lists")
		}
	}
}

func BenchmarkFilterIssuesEmptyWarm(b *testing.B) {
	v := &AppView{issues: generateBenchmarkUIIssues(500), activeInstIdx: -1,
		searchQuery: "no matching ticket", cacheDirty: true}
	v.getFilteredIssues()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v.getFilteredIssues()
	}
}
