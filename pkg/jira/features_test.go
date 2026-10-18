package jira

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFetchAllInstancesKeepsConfigOrderAndReportsFailures(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond) // answers last, but is configured first
		fmt.Fprintf(w, `{"issues":[%s],"isLast":true}`, issueJSON("SLOW-1"))
	}))
	defer slow.Close()
	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"issues":[%s],"isLast":true}`, issueJSON("FAST-1"))
	}))
	defer fast.Close()
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer broken.Close()

	client := NewClient(Config{Instances: []InstanceConfig{
		{ID: "slow", BaseURL: slow.URL, APIToken: "t"},
		{ID: "broken", BaseURL: broken.URL, APIToken: "t"},
		{ID: "fast", BaseURL: fast.URL, APIToken: "t"},
		{ID: "unconfigured"},
	}})
	res := client.FetchAllInstances(context.Background())

	if len(res.Issues) != 2 || res.Issues[0].Key != "SLOW-1" || res.Issues[1].Key != "FAST-1" {
		t.Fatalf("tickets must follow the instance order, got %+v", res.Issues)
	}
	if !res.Loaded["slow"] || !res.Loaded["fast"] || res.Loaded["broken"] || res.Loaded["unconfigured"] {
		t.Errorf("unexpected loaded set %v", res.Loaded)
	}
	if res.Failed["broken"] == nil || len(res.Failed) != 1 {
		t.Errorf("expected only the broken instance to fail, got %v", res.Failed)
	}
	if res.FirstError() == nil {
		t.Error("FirstError should report the failed instance")
	}
}

func TestSortIssues(t *testing.T) {
	at := func(h int) time.Time { return time.Date(2026, 10, 1, h, 0, 0, 0, time.UTC) }
	issues := []Issue{
		{Key: "A", Updated: at(1), Priority: Priority{Name: "Low"}, Status: Status{Name: "Done", CategoryKey: "done"}},
		{Key: "B", Updated: at(3), Priority: Priority{Name: "Highest"}, Status: Status{Name: "To Do", CategoryKey: "new"}},
		{Key: "C", Updated: at(2), Priority: Priority{ID: "2", Name: "Dringend"}, Status: Status{Name: "In Review", CategoryKey: "indeterminate"}},
		{Key: "D", Updated: at(2)},
	}
	keys := func(list []Issue) string {
		s := ""
		for _, iss := range list {
			s += iss.Key
		}
		return s
	}
	for _, c := range []struct{ mode, want string }{
		{SortJira, "ABCD"},
		{SortUpdated, "BCDA"},
		{SortPriority, "BCAD"}, // custom priority by ID 2 ranks between Highest and Low
		{SortStatus, "CDBA"},   // in progress first; D (no status) sorts by name before "To Do"
	} {
		list := append([]Issue(nil), issues...)
		SortIssues(list, c.mode)
		if got := keys(list); got != c.want {
			t.Errorf("mode %q: got %s, want %s", c.mode, got, c.want)
		}
	}
}

func TestLoadConfigDefaultsNewFeaturesOn(t *testing.T) {
	path := reviewConfigPath(t)
	writeReviewConfig(t, path, []byte(`{"instances":[{"id":"i","base_url":"https://x.atlassian.net","api_token":"t"}]}`))
	cfg := LoadConfig()
	if !cfg.GlobalHotKey || !cfg.Notifications || !cfg.UpdateCheck || cfg.SortMode != SortJira {
		t.Fatalf("configs from older versions should enable the new features: %+v", cfg)
	}

	writeReviewConfig(t, path, []byte(`{"instances":[{"id":"i","base_url":"https://x.atlassian.net","api_token":"t"}],
		"global_hotkey":false,"notifications":false,"update_check":false,"sort_mode":"priority"}`))
	cfg = LoadConfig()
	if cfg.GlobalHotKey || cfg.Notifications || cfg.UpdateCheck || cfg.SortMode != SortPriority {
		t.Fatalf("explicit choices must be kept: %+v", cfg)
	}
}
