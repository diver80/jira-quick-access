package jira

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestConcurrencyStressFetchAndConfig tests heavy parallel calls to FetchAssignedIssues,
// UpdateConfig, GetConfig, FetchTransitions, DoTransition, and VerifyInstanceConnection.
func TestConcurrencyStressFetchAndConfig(t *testing.T) {
	var requestCount int64

	ts1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&requestCount, 1)
		switch {
		case r.URL.Path == "/rest/api/3/search" || r.URL.Path == "/rest/api/2/search":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"issues":[{"id":"1","key":"SRV1-1","fields":{"summary":"Task 1","status":{"name":"Open","statusCategory":{"key":"new"}},"priority":{"name":"High"},"issuetype":{"name":"Bug"},"updated":"2026-04-17T12:00:00.000+0000"}}]}`)
		case r.URL.Path == "/rest/api/3/myself" || r.URL.Path == "/rest/api/2/myself":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"displayName":"User One","emailAddress":"user1@test.com"}`)
		case r.URL.Path == "/rest/api/3/issue/SRV1-1/transitions" || r.URL.Path == "/rest/api/2/issue/SRV1-1/transitions":
			w.Header().Set("Content-Type", "application/json")
			if r.Method == "POST" {
				w.WriteHeader(http.StatusOK)
			} else {
				fmt.Fprintf(w, `{"transitions":[{"id":"11","name":"In Progress","to":{"id":"2","name":"In Progress","statusCategory":{"key":"indeterminate"}}}]}`)
			}
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer ts1.Close()

	ts2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&requestCount, 1)
		switch {
		case r.URL.Path == "/rest/api/3/search" || r.URL.Path == "/rest/api/2/search":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"issues":[{"id":"2","key":"SRV2-2","fields":{"summary":"Task 2","status":{"name":"In Review","statusCategory":{"key":"indeterminate"}},"priority":{"name":"Medium"},"issuetype":{"name":"Task"},"updated":"2026-04-17T13:00:00.000+0000"}}]}`)
		case r.URL.Path == "/rest/api/3/myself" || r.URL.Path == "/rest/api/2/myself":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"displayName":"User Two","emailAddress":"user2@test.com"}`)
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer ts2.Close()

	cfg := Config{
		Instances: []InstanceConfig{
			{
				ID:       "inst-1",
				Name:     "Instance 1",
				BaseURL:  ts1.URL,
				Email:    "u1@test.com",
				APIToken: "token-1",
			},
			{
				ID:       "inst-2",
				Name:     "Instance 2",
				BaseURL:  ts2.URL,
				Email:    "u2@test.com",
				APIToken: "token-2",
			},
		},
	}

	client := NewClient(cfg)

	const numWorkers = 40
	const iterations = 50
	var wg sync.WaitGroup
	wg.Add(numWorkers)

	for w := 0; w < numWorkers; w++ {
		workerID := w
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				switch workerID % 5 {
				case 0:
					// FetchAssignedIssues
					issues, err := client.FetchAssignedIssues(ctx)
					if err != nil && ctx.Err() == nil {
						t.Errorf("FetchAssignedIssues error: %v", err)
					}
					if len(issues) > 0 && issues[0].Key == "" {
						t.Errorf("unexpected empty issue key")
					}
				case 1:
					// UpdateConfig and GetConfig
					c := client.GetConfig()
					c.DemoMode = (i%2 == 0)
					client.UpdateConfig(c)
				case 2:
					// FetchTransitions & DoTransition
					iss := Issue{
						Key:        "SRV1-1",
						InstanceID: "inst-1",
						BaseURL:    ts1.URL,
					}
					transitions, err := client.FetchTransitions(ctx, iss)
					if err == nil && len(transitions) > 0 {
						_ = client.DoTransition(ctx, iss, transitions[0].ID)
					}
				case 3:
					// VerifyConnection & VerifyInstanceConnection
					_, _ = client.VerifyConnection(ctx)
					_, _ = client.VerifyInstanceConnection(ctx, ts2.URL, "u2@test.com", "token-2")
				case 4:
					// Mock execution
					_ = client.ExecuteTransition(ctx, "PROJ-102", "41")
				}
				cancel()
			}
		}()
	}

	wg.Wait()

	if atomic.LoadInt64(&requestCount) == 0 {
		t.Errorf("expected requests to have been processed by mock servers")
	}
}

// TestGoroutineLeakAndContextCancellation tests that cancelling contexts during multi-instance fetch
// exits all background goroutines promptly without leaking.
func TestGoroutineLeakAndContextCancellation(t *testing.T) {
	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate a slow network response
		time.Sleep(100 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"issues":[]}`)
	}))
	defer slowServer.Close()

	cfg := Config{
		Instances: []InstanceConfig{
			{ID: "slow-1", Name: "Slow 1", BaseURL: slowServer.URL, Email: "s@test.com", APIToken: "tok"},
			{ID: "slow-2", Name: "Slow 2", BaseURL: slowServer.URL, Email: "s@test.com", APIToken: "tok"},
			{ID: "slow-3", Name: "Slow 3", BaseURL: slowServer.URL, Email: "s@test.com", APIToken: "tok"},
			{ID: "slow-4", Name: "Slow 4", BaseURL: slowServer.URL, Email: "s@test.com", APIToken: "tok"},
		},
	}
	client := NewClient(cfg)

	initialGoroutines := runtime.NumGoroutine()

	for i := 0; i < 20; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		_, _ = client.FetchAssignedIssues(ctx)
		cancel()
	}

	// Give a moment for any cancelled routines to finish draining
	time.Sleep(200 * time.Millisecond)

	finalGoroutines := runtime.NumGoroutine()
	// Allow small margin of 5 goroutines for runtime/GC/test runner
	if finalGoroutines > initialGoroutines+5 {
		t.Errorf("possible goroutine leak: initial=%d, final=%d", initialGoroutines, finalGoroutines)
	}
}
