package status

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"
)

const reviewSummary = `{"status":{"indicator":"minor","description":"Degraded"},"components":[{"id":"one","name":"API","status":"degraded_performance"}],"incidents":[{"id":"inc","name":"Incident","impact":"minor"}]}`

func TestFetchReportContextCancellation(t *testing.T) {
	for _, stage := range []string{"root", "body", "service"} {
		t.Run(stage, func(t *testing.T) {
			started := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if stage == "service" && r.URL.Path == "/api/v2/summary.json" {
					_, _ = w.Write([]byte(reviewSummary))
					return
				}
				if stage == "body" {
					_, _ = w.Write([]byte(`{"status":`))
					w.(http.Flusher).Flush()
				}
				close(started)
				<-r.Context().Done()
			}))
			defer server.Close()
			client := NewClientWithCustomEndpoints(server.URL, map[string]string{"Service": server.URL + "/service"})
			cached := StatusReport{OverallText: "Last known status", LastChecked: time.Unix(100, 0)}
			client.cachedReport = cached
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			type result struct {
				report StatusReport
				err    error
			}
			done := make(chan result, 1)
			go func() { report, err := client.FetchReportContext(ctx); done <- result{report, err} }()
			select {
			case <-started:
			case <-time.After(2 * time.Second):
				t.Fatal("request did not reach cancellation stage")
			}
			cancel()
			select {
			case got := <-done:
				if !errors.Is(got.err, context.Canceled) {
					t.Fatalf("expected wrapped cancellation, got %v", got.err)
				}
				if got.report.OverallText != cached.OverallText || got.report.Error == "" {
					t.Fatalf("expected cached fallback with error, got %+v", got.report)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("request did not stop after cancellation")
			}
			if got := client.GetCachedReport(); !reflect.DeepEqual(got, cached) {
				t.Fatalf("canceled fetch overwrote cache: %+v", got)
			}
		})
	}
}

func TestStatusReportSnapshotOwnership(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(reviewSummary))
	}))
	defer server.Close()
	client := NewClientWithCustomEndpoints(server.URL, map[string]string{"Service": server.URL})
	fetched, err := client.FetchReport()
	if err != nil {
		t.Fatal(err)
	}
	want := client.GetCachedReport()
	mutate := func(report StatusReport) {
		report.Services[0].Name = "changed"
		report.Services[0].Components[0].Name = "changed"
		report.ActiveIncidents[0].Name = "changed"
	}
	mutate(fetched)
	mutate(client.GetCachedReport())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fallback, err := client.FetchReportContext(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	mutate(fallback)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); mutate(client.GetCachedReport()) }()
	}
	wg.Wait()
	if got := client.GetCachedReport(); !reflect.DeepEqual(got, want) {
		t.Fatalf("caller edits mutated cached report: %+v", got)
	}
}

func TestServiceFailureRemainsBestEffort(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/failed/api/v2/summary.json" {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(reviewSummary))
	}))
	defer server.Close()
	client := NewClientWithCustomEndpoints(server.URL, map[string]string{
		"Available": server.URL, "Unavailable": server.URL + "/failed",
	})
	report, err := client.FetchReport()
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Services) != 1 || report.Services[0].Name != "Available" {
		t.Fatalf("expected successful service only, got %+v", report.Services)
	}
}
