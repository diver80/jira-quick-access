package status

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientFetchReport(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v2/summary.json":
			w.Write([]byte(`{
				"page": {"id": "0f54fx204jpt", "name": "Atlassian"},
				"status": {"indicator": "minor", "description": "Minor Service Outage"},
				"incidents": [
					{
						"id": "inc-1",
						"name": "Jira Search Degradation",
						"status": "investigating",
						"impact": "minor",
						"shortlink": "https://status.atlassian.com/incidents/inc-1",
						"updated_at": "2026-09-18T14:00:00Z",
						"incident_updates": [
							{"body": "Investigating search latency issues across US-East."}
						]
					}
				],
				"components": [
					{"id": "c1", "name": "Jira Software", "status": "degraded_performance"},
					{"id": "c2", "name": "Confluence", "status": "operational"}
				]
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	client := NewClientWithURL(ts.URL)
	report, err := client.FetchReport()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if report.OverallIndicator != IndicatorMinor {
		t.Errorf("expected IndicatorMinor, got %v", report.OverallIndicator)
	}
	if len(report.ActiveIncidents) != 1 {
		t.Fatalf("expected 1 incident, got %d", len(report.ActiveIncidents))
	}
	if report.ActiveIncidents[0].Name != "Jira Search Degradation" {
		t.Errorf("unexpected incident name: %s", report.ActiveIncidents[0].Name)
	}
	if report.ActiveIncidents[0].Body != "Investigating search latency issues across US-East." {
		t.Errorf("unexpected incident body: %s", report.ActiveIncidents[0].Body)
	}
}

func TestClientCachingAndFallback(t *testing.T) {
	calls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{
				"page": {"id": "0f54fx204jpt", "name": "Atlassian"},
				"status": {"indicator": "none", "description": "All Systems Operational"},
				"incidents": []
			}`))
			return
		}
		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	defer ts.Close()

	client := NewClientWithURL(ts.URL)
	rep1, err := client.FetchReport()
	if err != nil {
		t.Fatalf("call 1 failed: %v", err)
	}
	if rep1.OverallIndicator != IndicatorNone {
		t.Errorf("expected IndicatorNone, got %v", rep1.OverallIndicator)
	}

	// Call 2 fails HTTP, but cached report is returned with Error set
	rep2, err := client.FetchReport()
	if err == nil {
		t.Fatalf("expected error on second call")
	}
	if rep2.OverallIndicator != IndicatorNone {
		t.Errorf("expected cached IndicatorNone, got %v", rep2.OverallIndicator)
	}
	if rep2.Error == "" {
		t.Errorf("expected error message recorded in report")
	}

	cached := client.GetCachedReport()
	if cached.OverallIndicator != IndicatorNone {
		t.Errorf("cached indicator mismatch: %v", cached.OverallIndicator)
	}
}

func TestClientSubServiceReports(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"page": {"id": "sub-1", "name": "Jira Software"},
			"status": {"indicator": "none", "description": "All Systems Operational"},
			"components": [
				{"id": "comp-1", "name": "Create and edit", "status": "operational"},
				{"id": "comp-2", "name": "Search", "status": "operational"}
			]
		}`))
	}))
	defer ts.Close()

	client := NewClientWithCustomEndpoints(ts.URL, map[string]string{
		"Jira Software": ts.URL,
	})
	report, err := client.FetchReport()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(report.Services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(report.Services))
	}
	if report.Services[0].Name != "Jira Software" {
		t.Errorf("expected 'Jira Software', got %q", report.Services[0].Name)
	}
	if len(report.Services[0].Components) != 2 {
		t.Errorf("expected 2 components, got %d", len(report.Services[0].Components))
	}
}

func TestClientLastCheckedTimestamp(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"page": {"id": "0f54fx204jpt", "name": "Atlassian"},
			"status": {"indicator": "none", "description": "All Systems Operational"}
		}`))
	}))
	defer ts.Close()

	client := NewClientWithURL(ts.URL)
	before := time.Now().Add(-1 * time.Second)
	rep, err := client.FetchReport()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rep.LastChecked.Before(before) {
		t.Errorf("LastChecked %v should be after %v", rep.LastChecked, before)
	}
}
