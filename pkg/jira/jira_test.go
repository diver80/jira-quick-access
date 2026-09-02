package jira

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Tier 1: Unit Tests for FormatBranchName
func TestFormatBranchName(t *testing.T) {
	tests := []struct {
		name     string
		prefix   string
		key      string
		summary  string
		expected string
	}{
		{
			name:     "standard feature branch",
			prefix:   "feature/",
			key:      "PROJ-102",
			summary:  "Implement OAuth PKCE Flow",
			expected: "feature/PROJ-102-implement-oauth-pkce-flow",
		},
		{
			name:     "fix prefix with punctuation",
			prefix:   "fix/",
			key:      "PROJ-98",
			summary:  "Fix race condition in token poller! (critical)",
			expected: "fix/PROJ-98-fix-race-condition-in-token-poller-critical",
		},
		{
			name:     "empty prefix defaults to feature/",
			prefix:   "",
			key:      "PROJ-115",
			summary:  "Update deployment pipeline",
			expected: "feature/PROJ-115-update-deployment-pipeline",
		},
		{
			name:     "hotfix prefix with multiple special characters",
			prefix:   "hotfix/",
			key:      "CORE-404",
			summary:  "Fix: [Crash] on nil pointer in AppView.Draw() & Event()",
			expected: "hotfix/CORE-404-fix-crash-on-nil-pointer-in-appviewdraw-event",
		},
		{
			name:     "chore prefix with consecutive dashes and underscores",
			prefix:   "chore/",
			key:      "INFRA-55",
			summary:  "upgrade___go--dependencies_v1.27",
			expected: "chore/INFRA-55-upgrade-go-dependencies-v127",
		},
		{
			name:     "summary with leading and trailing dashes and spaces",
			prefix:   "bugfix/",
			key:      "AVN-22",
			summary:  " --- Fix WebKit White Screen on macOS --- ",
			expected: "bugfix/AVN-22-fix-webkit-white-screen-on-macos",
		},
		{
			name:     "summary with numbers and alphanumeric symbols",
			prefix:   "feature/",
			key:      "DATA-77",
			summary:  "Jira Cloud REST v3 & DC REST v2 fallback 100%",
			expected: "feature/DATA-77-jira-cloud-rest-v3-dc-rest-v2-fallback-100",
		},
		{
			name:     "unicode and emojis stripped to clean ASCII",
			prefix:   "feat/",
			key:      "UI-9",
			summary:  "🚀 Add Toast Notifications & Blinking Cursor ⚡",
			expected: "feat/UI-9-add-toast-notifications-blinking-cursor",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatBranchName(tt.prefix, tt.key, tt.summary)
			if got != tt.expected {
				t.Errorf("FormatBranchName(%q, %q, %q) = %q; want %q", tt.prefix, tt.key, tt.summary, got, tt.expected)
			}
		})
	}
}

// Tier 1: Unit Tests for Issue Models, Status, Priority, IssueType Serialization
func TestIssueModelsAndSerialization(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	issue := Issue{
		InstanceID:   "inst-alpha",
		InstanceName: "Alpha Cloud",
		BaseURL:      "https://alpha.atlassian.net",
		ID:           "10050",
		Key:          "ALPHA-100",
		Summary:      "Refactor UI Layout Engine",
		Status: Status{
			ID:            "3",
			Name:          "In Progress",
			CategoryKey:   "indeterminate",
			CategoryColor: "yellow",
		},
		Priority: Priority{
			ID:      "2",
			Name:    "High",
			IconURL: "https://alpha.atlassian.net/images/icons/priorities/high.svg",
		},
		IssueType: IssueType{
			ID:      "10001",
			Name:    "Story",
			IconURL: "https://alpha.atlassian.net/images/icons/issuetypes/story.svg",
			Subtask: false,
		},
		Updated: now,
		Pinned:  true,
		URL:     "https://alpha.atlassian.net/browse/ALPHA-100",
	}

	// JSON roundtrip
	data, err := json.Marshal(issue)
	if err != nil {
		t.Fatalf("json.Marshal(Issue) failed: %v", err)
	}

	var unmarshaled Issue
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("json.Unmarshal(Issue) failed: %v", err)
	}

	if unmarshaled.Key != "ALPHA-100" || unmarshaled.Summary != "Refactor UI Layout Engine" {
		t.Errorf("unmarshaled issue mismatch: %+v", unmarshaled)
	}
	if unmarshaled.Status.Name != "In Progress" || unmarshaled.Status.CategoryKey != "indeterminate" {
		t.Errorf("unmarshaled status mismatch: %+v", unmarshaled.Status)
	}
	if unmarshaled.Priority.Name != "High" || unmarshaled.IssueType.Name != "Story" {
		t.Errorf("unmarshaled priority/type mismatch: %+v, %+v", unmarshaled.Priority, unmarshaled.IssueType)
	}
	if !unmarshaled.Pinned {
		t.Errorf("expected issue to be pinned")
	}
	if unmarshaled.URL != "https://alpha.atlassian.net/browse/ALPHA-100" {
		t.Errorf("expected URL https://alpha.atlassian.net/browse/ALPHA-100, got %s", unmarshaled.URL)
	}

	// Transition & TransitionHistory models
	trans := Transition{
		ID:   "21",
		Name: "Done",
		To: Status{
			ID:          "4",
			Name:        "Done",
			CategoryKey: "done",
		},
	}
	transData, err := json.Marshal(trans)
	if err != nil {
		t.Fatalf("json.Marshal(Transition) failed: %v", err)
	}
	var unmarshaledTrans Transition
	if err := json.Unmarshal(transData, &unmarshaledTrans); err != nil {
		t.Fatalf("json.Unmarshal(Transition) failed: %v", err)
	}
	if unmarshaledTrans.Name != "Done" || unmarshaledTrans.To.CategoryKey != "done" {
		t.Errorf("unmarshaled transition mismatch: %+v", unmarshaledTrans)
	}

	hist := TransitionHistory{
		IssueKey:     "ALPHA-100",
		FromStatusID: "3",
		ToStatusID:   "4",
		FromState:    "In Progress",
		ToState:      "Done",
		TransitionID: "21",
		Timestamp:    now,
	}
	if hist.IssueKey != "ALPHA-100" || hist.FromState != "In Progress" || hist.ToState != "Done" {
		t.Errorf("transition history mismatch: %+v", hist)
	}
}

// Tier 1: Unit Tests for Config.EnsureInstances and DefaultConfig
func TestConfigEnsureInstances(t *testing.T) {
	// Case 1: Empty config defaults to avono inst-1
	var emptyCfg Config
	emptyCfg.EnsureInstances()
	if len(emptyCfg.Instances) != 1 {
		t.Fatalf("expected 1 instance, got %d", len(emptyCfg.Instances))
	}
	if emptyCfg.Instances[0].ID != "inst-1" || emptyCfg.Instances[0].Name != "avono" {
		t.Errorf("unexpected instance config: %+v", emptyCfg.Instances[0])
	}
	if emptyCfg.ActiveInstID != "inst-1" {
		t.Errorf("expected ActiveInstID 'inst-1', got %q", emptyCfg.ActiveInstID)
	}

	// Case 2: Config with top-level primary credentials creates "Primary" instance
	primaryCfg := Config{
		BaseURL:  "https://custom-jira.atlassian.net",
		Email:    "dev@custom.com",
		APIToken: "secret-token",
		JQLQuery: "assignee = currentUser()",
	}
	primaryCfg.EnsureInstances()
	if len(primaryCfg.Instances) != 1 {
		t.Fatalf("expected 1 instance from primary, got %d", len(primaryCfg.Instances))
	}
	if primaryCfg.Instances[0].Name != "Primary" || primaryCfg.Instances[0].BaseURL != "https://custom-jira.atlassian.net" {
		t.Errorf("unexpected primary instance: %+v", primaryCfg.Instances[0])
	}
	if primaryCfg.ActiveInstID != "inst-1" {
		t.Errorf("expected ActiveInstID 'inst-1', got %q", primaryCfg.ActiveInstID)
	}

	// Case 3: Multiple instances present with empty ActiveInstID selects first instance
	multiCfg := Config{
		Instances: []InstanceConfig{
			{ID: "org-a", Name: "Org A", BaseURL: "https://a.atlassian.net"},
			{ID: "org-b", Name: "Org B", BaseURL: "https://b.atlassian.net"},
		},
	}
	multiCfg.EnsureInstances()
	if multiCfg.ActiveInstID != "org-a" {
		t.Errorf("expected ActiveInstID 'org-a', got %q", multiCfg.ActiveInstID)
	}

	// Case 4: DefaultConfig sanity check
	def := DefaultConfig()
	if len(def.Instances) < 2 {
		t.Errorf("expected at least 2 default instances in DefaultConfig, got %d", len(def.Instances))
	}
	if def.PollInterval != 300 {
		t.Errorf("expected default PollInterval 300, got %d", def.PollInterval)
	}
	if def.BranchPrefix != "feature/" {
		t.Errorf("expected default BranchPrefix 'feature/', got %q", def.BranchPrefix)
	}
	if def.ActiveInstID != "inst-1" {
		t.Errorf("expected default ActiveInstID 'inst-1', got %q", def.ActiveInstID)
	}
}

// Tier 1: Unit Tests for Config.MaskedAPIToken and InstanceConfig.MaskedAPIToken
func TestConfigMaskedAPIToken(t *testing.T) {
	// Empty token
	cfgEmpty := Config{APIToken: ""}
	if cfgEmpty.MaskedAPIToken() != "" {
		t.Errorf("expected empty masked token for empty string, got %q", cfgEmpty.MaskedAPIToken())
	}

	// Short tokens <= 6 characters masked with asterisks
	cfgShort := Config{APIToken: "abc"}
	if cfgShort.MaskedAPIToken() != "***" {
		t.Errorf("expected '***', got %q", cfgShort.MaskedAPIToken())
	}

	cfg6 := Config{APIToken: "123456"}
	if cfg6.MaskedAPIToken() != "******" {
		t.Errorf("expected '******', got %q", cfg6.MaskedAPIToken())
	}

	// Long token > 6 characters
	cfgLong := Config{APIToken: "ATATT123456789xyz"}
	masked := cfgLong.MaskedAPIToken()
	if masked != "ATA...xyz" {
		t.Errorf("expected 'ATA...xyz', got %q", masked)
	}

	// InstanceConfig MaskedAPIToken
	inst := InstanceConfig{APIToken: "secret-jira-token-99"}
	if inst.MaskedAPIToken() != "sec...-99" {
		t.Errorf("expected 'sec...-99', got %q", inst.MaskedAPIToken())
	}

	// Top-level fallback to Instances[0] when top-level is empty
	cfgFallback := Config{
		APIToken: "",
		Instances: []InstanceConfig{
			{ID: "inst-1", APIToken: "tok1234567"},
		},
	}
	if cfgFallback.MaskedAPIToken() != "tok...567" {
		t.Errorf("expected 'tok...567', got %q", cfgFallback.MaskedAPIToken())
	}
}

// Tier 2: Mock HTTP Integration Tests for Concurrent Multi-Instance Issue Fetching
func TestMockHTTPMultiInstanceFetchConcurrent(t *testing.T) {
	var (
		ts1Calls int32
		ts2Calls int32
		ts3Calls int32
	)

	// Server 1: Cloud REST v3 with Basic Auth header verification
	ts1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&ts1Calls, 1)
		auth := r.Header.Get("Authorization")
		expectedAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("user1@cloud.com:token-1"))
		if auth != expectedAuth {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		if r.URL.Path == "/rest/api/3/search" && r.Method == "POST" {
			body, _ := io.ReadAll(r.Body)
			var req map[string]interface{}
			_ = json.Unmarshal(body, &req)

			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{
				"issues": [
					{
						"id": "101",
						"key": "CLOUD-1",
						"fields": {
							"summary": "Implement Multi-Instance Auth",
							"status": {"id": "1", "name": "Open", "statusCategory": {"key": "new", "colorName": "blue"}},
							"priority": {"id": "1", "name": "Highest", "iconUrl": "https://icon.test/highest.svg"},
							"issuetype": {"id": "1", "name": "Bug", "subtask": false},
							"updated": "2026-09-01T10:00:00.000+0000"
						}
					},
					{
						"id": "102",
						"key": "CLOUD-2",
						"fields": {
							"summary": "Fix Edge Rail Layout",
							"status": {"id": "2", "name": "In Progress", "statusCategory": {"key": "indeterminate", "colorName": "yellow"}},
							"priority": {"id": "2", "name": "High", "iconUrl": "https://icon.test/high.svg"},
							"issuetype": {"id": "2", "name": "Story", "subtask": false},
							"updated": "2026-09-01T10:30:00.000+0000"
						}
					}
				]
			}`)
			return
		}
		http.Error(w, "Not found", http.StatusNotFound)
	}))
	defer ts1.Close()

	// Server 2: Data Center REST v2 with Bearer token authentication (REST v3 returns 404)
	ts2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&ts2Calls, 1)
		auth := r.Header.Get("Authorization")
		if auth != "Bearer pat-token-dc" {
			http.Error(w, "Unauthorized Bearer", http.StatusUnauthorized)
			return
		}

		if r.URL.Path == "/rest/api/3/search" {
			http.Error(w, "REST v3 not supported on DC", http.StatusNotFound)
			return
		}

		if r.URL.Path == "/rest/api/2/search" && r.Method == "POST" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{
				"issues": [
					{
						"id": "201",
						"key": "DC-10",
						"fields": {
							"summary": "Data Center Migration",
							"status": {"id": "3", "name": "In Review", "statusCategory": {"key": "indeterminate", "colorName": "yellow"}},
							"priority": {"id": "3", "name": "Medium", "iconUrl": "https://icon.test/medium.svg"},
							"issuetype": {"id": "3", "name": "Task", "subtask": false},
							"updated": "2026-09-01T11:00:00.000+0000"
						}
					},
					{
						"id": "202",
						"key": "DC-11",
						"fields": {
							"summary": "Verify Cgo Darwin Bindings",
							"status": {"id": "4", "name": "Done", "statusCategory": {"key": "done", "colorName": "green"}},
							"priority": {"id": "4", "name": "Low", "iconUrl": "https://icon.test/low.svg"},
							"issuetype": {"id": "4", "name": "Sub-task", "subtask": true},
							"updated": "2026-09-01T11:30:00.000+0000"
						}
					}
				]
			}`)
			return
		}
		http.Error(w, "Not found", http.StatusNotFound)
	}))
	defer ts2.Close()

	// Server 3: Faulty server returning 500 Internal Server Error
	ts3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&ts3Calls, 1)
		http.Error(w, "Internal Database Error", http.StatusInternalServerError)
	}))
	defer ts3.Close()

	cfg := Config{
		Instances: []InstanceConfig{
			{
				ID:       "inst-cloud",
				Name:     "Cloud Corp",
				BaseURL:  ts1.URL,
				Email:    "user1@cloud.com",
				APIToken: "token-1",
				JQLQuery: "assignee = currentUser() ORDER BY updated DESC",
			},
			{
				ID:       "inst-dc",
				Name:     "Data Center Corp",
				BaseURL:  ts2.URL,
				Email:    "", // Empty email -> Bearer token
				APIToken: "pat-token-dc",
			},
			{
				ID:       "inst-faulty",
				Name:     "Broken Server",
				BaseURL:  ts3.URL,
				Email:    "err@test.com",
				APIToken: "token-err",
			},
			{
				ID:       "inst-unconfigured",
				Name:     "Skipped Instance",
				BaseURL:  "",
				APIToken: "",
			},
		},
	}

	client := NewClient(cfg)

	// Fetch issues concurrently
	issues, err := client.FetchAssignedIssues(context.Background())
	if err != nil {
		t.Fatalf("FetchAssignedIssues failed: %v", err)
	}

	// Should have collected 4 issues total (2 from Cloud + 2 from DC) despite Server 3 failing
	if len(issues) != 4 {
		t.Fatalf("expected 4 aggregated issues from healthy instances, got %d", len(issues))
	}

	foundKeys := make(map[string]Issue)
	for _, iss := range issues {
		foundKeys[iss.Key] = iss
	}

	if iss, ok := foundKeys["CLOUD-1"]; !ok || iss.InstanceID != "inst-cloud" || iss.InstanceName != "Cloud Corp" {
		t.Errorf("CLOUD-1 issue tagging incorrect: %+v", iss)
	}
	if iss, ok := foundKeys["CLOUD-2"]; !ok || iss.Status.Name != "In Progress" {
		t.Errorf("CLOUD-2 status incorrect: %+v", iss)
	}
	if iss, ok := foundKeys["DC-10"]; !ok || iss.InstanceID != "inst-dc" || iss.BaseURL != ts2.URL {
		t.Errorf("DC-10 tagging incorrect: %+v", iss)
	}
	if iss, ok := foundKeys["DC-11"]; !ok || !iss.IssueType.Subtask || iss.Status.CategoryKey != "done" {
		t.Errorf("DC-11 subtask/status incorrect: %+v", iss)
	}

	if atomic.LoadInt32(&ts1Calls) == 0 || atomic.LoadInt32(&ts2Calls) == 0 || atomic.LoadInt32(&ts3Calls) == 0 {
		t.Errorf("all mock servers should have been contacted")
	}
}

// Tier 2: Mock HTTP Integration Tests for Transitions (REST v3 & REST v2 Fallback)
func TestMockHTTPTransitionsCloudAndDCFallback(t *testing.T) {
	var (
		v3TransitionCalls int32
		v2TransitionCalls int32
		v3DoCalls         int32
		v2DoCalls         int32
		receivedDoPayload map[string]interface{}
	)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		// REST v3 fetch transitions -> 404 Not Found (Simulating Data Center)
		case r.URL.Path == "/rest/api/3/issue/DC-99/transitions" && r.Method == "GET":
			atomic.AddInt32(&v3TransitionCalls, 1)
			http.Error(w, "REST v3 not supported", http.StatusNotFound)

		// REST v2 fetch transitions -> 200 OK
		case r.URL.Path == "/rest/api/2/issue/DC-99/transitions" && r.Method == "GET":
			atomic.AddInt32(&v2TransitionCalls, 1)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{
				"transitions": [
					{
						"id": "11",
						"name": "Start Work",
						"to": {
							"id": "2",
							"name": "In Progress",
							"statusCategory": {"key": "indeterminate"}
						}
					},
					{
						"id": "21",
						"name": "Resolve Issue",
						"to": {
							"id": "3",
							"name": "Done",
							"statusCategory": {"key": "done"}
						}
					}
				]
			}`)

		// REST v3 DoTransition -> 405 Method Not Allowed
		case r.URL.Path == "/rest/api/3/issue/DC-99/transitions" && r.Method == "POST":
			atomic.AddInt32(&v3DoCalls, 1)
			http.Error(w, "Method Not Allowed on v3", http.StatusMethodNotAllowed)

		// REST v2 DoTransition -> 204 No Content
		case r.URL.Path == "/rest/api/2/issue/DC-99/transitions" && r.Method == "POST":
			atomic.AddInt32(&v2DoCalls, 1)
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &receivedDoPayload)
			w.WriteHeader(http.StatusNoContent)

		default:
			http.Error(w, "Invalid route", http.StatusBadRequest)
		}
	}))
	defer ts.Close()

	cfg := Config{
		Instances: []InstanceConfig{
			{
				ID:       "inst-dc",
				Name:     "Data Center",
				BaseURL:  ts.URL,
				Email:    "dc@corp.local",
				APIToken: "token-dc",
			},
		},
	}
	client := NewClient(cfg)
	targetIssue := Issue{
		Key:        "DC-99",
		InstanceID: "inst-dc",
		BaseURL:    ts.URL,
	}

	// 1. FetchTransitions with fallback to REST v2
	transitions, err := client.FetchTransitions(context.Background(), targetIssue)
	if err != nil {
		t.Fatalf("FetchTransitions failed: %v", err)
	}
	if len(transitions) != 2 {
		t.Fatalf("expected 2 transitions, got %d", len(transitions))
	}
	if transitions[0].ID != "11" || transitions[0].Name != "Start Work" || transitions[0].To.CategoryKey != "indeterminate" {
		t.Errorf("unexpected transition 0: %+v", transitions[0])
	}
	if transitions[1].ID != "21" || transitions[1].Name != "Resolve Issue" || transitions[1].To.CategoryKey != "done" {
		t.Errorf("unexpected transition 1: %+v", transitions[1])
	}
	if atomic.LoadInt32(&v3TransitionCalls) != 1 || atomic.LoadInt32(&v2TransitionCalls) != 1 {
		t.Errorf("expected 1 v3 call and 1 v2 fallback call, got v3=%d, v2=%d", v3TransitionCalls, v2TransitionCalls)
	}

	// 2. DoTransition with fallback to REST v2
	err = client.DoTransition(context.Background(), targetIssue, "21")
	if err != nil {
		t.Fatalf("DoTransition failed: %v", err)
	}
	if atomic.LoadInt32(&v3DoCalls) != 1 || atomic.LoadInt32(&v2DoCalls) != 1 {
		t.Errorf("expected 1 v3 do call and 1 v2 do fallback call, got v3=%d, v2=%d", v3DoCalls, v2DoCalls)
	}
	transObj, ok := receivedDoPayload["transition"].(map[string]interface{})
	if !ok || transObj["id"] != "21" {
		t.Errorf("expected payload transition id '21', got %+v", receivedDoPayload)
	}
}

// Tier 2: Mock HTTP Connection Verification Scenarios (200, 401, 404, 500, Context Cancel)
func TestMockHTTPVerifyConnectionScenarios(t *testing.T) {
	// Scenario 1: 200 OK with displayName
	tsSuccess := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"displayName":"Frank Hess","emailAddress":"frank.hess@avono.de","name":"fhess"}`)
	}))
	defer tsSuccess.Close()

	client := NewClient(DefaultConfig())
	name, err := client.VerifyInstanceConnection(context.Background(), tsSuccess.URL, "frank.hess@avono.de", "tok123")
	if err != nil || name != "Frank Hess" {
		t.Fatalf("expected 'Frank Hess', got %q, err=%v", name, err)
	}

	// Scenario 2: 200 OK with missing displayName falls back to emailAddress
	tsEmailOnly := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"displayName":"","emailAddress":"frank.hess@avono.de","name":""}`)
	}))
	defer tsEmailOnly.Close()

	name, err = client.VerifyInstanceConnection(context.Background(), tsEmailOnly.URL, "frank.hess@avono.de", "tok123")
	if err != nil || name != "frank.hess@avono.de" {
		t.Fatalf("expected 'frank.hess@avono.de', got %q, err=%v", name, err)
	}

	// Scenario 3: 401 Unauthorized
	ts401 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Unauthorized: Invalid API Token", http.StatusUnauthorized)
	}))
	defer ts401.Close()

	_, err = client.VerifyInstanceConnection(context.Background(), ts401.URL, "frank.hess@avono.de", "bad-token")
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("expected 401 error, got %v", err)
	}

	// Scenario 4: 404 Not Found
	ts404 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Endpoint Not Found", http.StatusNotFound)
	}))
	defer ts404.Close()

	_, err = client.VerifyInstanceConnection(context.Background(), ts404.URL, "frank.hess@avono.de", "token")
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected 404 error, got %v", err)
	}

	// Scenario 5: 500 Internal Server Error
	ts500 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}))
	defer ts500.Close()

	_, err = client.VerifyInstanceConnection(context.Background(), ts500.URL, "frank.hess@avono.de", "token")
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("expected 500 error, got %v", err)
	}

	// Scenario 6: Missing BaseURL or APIToken
	_, err = client.VerifyInstanceConnection(context.Background(), "", "frank.hess@avono.de", "tok")
	if err == nil {
		t.Errorf("expected error for empty BaseURL")
	}
	_, err = client.VerifyInstanceConnection(context.Background(), "https://jira.test", "frank.hess@avono.de", "")
	if err == nil {
		t.Errorf("expected error for empty Token")
	}

	// Scenario 7: Context cancellation
	ctxCancel, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.VerifyInstanceConnection(ctxCancel, tsSuccess.URL, "frank.hess@avono.de", "tok123")
	if err == nil {
		t.Errorf("expected error on cancelled context")
	}
}

// Tier 2: Storage Roundtrip, File Permissions (0600), and .env Loading
func TestConfigStorageRoundtripAndPermissions(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "jira-storage-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	origHome := os.Getenv("HOME")
	_ = os.Setenv("HOME", tempDir)
	defer os.Setenv("HOME", origHome)

	cfg := Config{
		Instances: []InstanceConfig{
			{
				ID:       "inst-prod",
				Name:     "Production",
				BaseURL:  "https://prod.atlassian.net",
				Email:    "admin@prod.com",
				APIToken: "secret-token-prod",
				JQLQuery: "project = PROD ORDER BY rank ASC",
			},
			{
				ID:       "inst-stage",
				Name:     "Staging",
				BaseURL:  "https://stage.atlassian.net",
				Email:    "qa@stage.com",
				APIToken: "secret-token-stage",
				JQLQuery: "project = STAGE",
			},
		},
		ActiveInstID: "inst-stage",
		BaseURL:      "https://prod.atlassian.net",
		Email:        "admin@prod.com",
		APIToken:     "secret-token-prod",
		JQLQuery:     "project = PROD ORDER BY rank ASC",
		PollInterval: 180,
		BranchPrefix: "bugfix/",
		PinnedKeys:   []string{"PROD-10", "STAGE-20"},
		DemoMode:     false,
		DebugMode:    true,
	}

	if err := SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	// Verify file mode 0600
	cfgPath := filepath.Join(tempDir, ".jira-quick-access", "config.json")
	fileInfo, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatalf("config file was not created: %v", err)
	}
	if fileInfo.Mode().Perm() != 0600 {
		t.Errorf("expected 0600 file permissions, got %v", fileInfo.Mode().Perm())
	}

	// Load config back
	loaded := LoadConfig()
	if len(loaded.Instances) != 2 {
		t.Fatalf("expected 2 instances, got %d", len(loaded.Instances))
	}
	if loaded.ActiveInstID != "inst-stage" {
		t.Errorf("expected ActiveInstID 'inst-stage', got %q", loaded.ActiveInstID)
	}
	if loaded.PollInterval != 180 {
		t.Errorf("expected PollInterval 180, got %d", loaded.PollInterval)
	}
	if loaded.BranchPrefix != "bugfix/" {
		t.Errorf("expected BranchPrefix 'bugfix/', got %q", loaded.BranchPrefix)
	}
	if len(loaded.PinnedKeys) != 2 || loaded.PinnedKeys[0] != "PROD-10" || loaded.PinnedKeys[1] != "STAGE-20" {
		t.Errorf("unexpected PinnedKeys: %v", loaded.PinnedKeys)
	}
	if loaded.Instances[0].Name != "Production" || loaded.Instances[1].Name != "Staging" {
		t.Errorf("instance name mismatch: %+v", loaded.Instances)
	}
}

// Tier 3: Concurrency Race Tests with Parallel Operations
func TestClientConcurrencyRaceValidation(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "search"):
			fmt.Fprintf(w, `{"issues":[{"id":"1","key":"RACE-1","fields":{"summary":"Race Ticket","status":{"name":"Open","statusCategory":{"key":"new"}},"priority":{"name":"High"},"issuetype":{"name":"Task"},"updated":"2026-09-01T12:00:00.000+0000"}}]}`)
		case strings.Contains(r.URL.Path, "myself"):
			fmt.Fprintf(w, `{"displayName":"Race User","emailAddress":"race@test.com"}`)
		case strings.Contains(r.URL.Path, "transitions"):
			if r.Method == "POST" {
				w.WriteHeader(http.StatusOK)
			} else {
				fmt.Fprintf(w, `{"transitions":[{"id":"11","name":"In Progress","to":{"id":"2","name":"In Progress","statusCategory":{"key":"indeterminate"}}}]}`)
			}
		default:
			http.Error(w, "Not found", http.StatusNotFound)
		}
	}))
	defer ts.Close()

	cfg := Config{
		Instances: []InstanceConfig{
			{ID: "inst-1", Name: "Race Inst 1", BaseURL: ts.URL, Email: "u1@test.com", APIToken: "tok1"},
			{ID: "inst-2", Name: "Race Inst 2", BaseURL: ts.URL, Email: "u2@test.com", APIToken: "tok2"},
		},
	}
	client := NewClient(cfg)

	const numGoroutines = 30
	const opsPerGoroutine = 40
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for g := 0; g < numGoroutines; g++ {
		workerID := g
		go func() {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				switch workerID % 6 {
				case 0:
					// FetchAssignedIssues
					_, _ = client.FetchAssignedIssues(ctx)
				case 1:
					// UpdateConfig and GetConfig
					c := client.GetConfig()
					c.PollInterval = 100 + (i % 50)
					client.UpdateConfig(c)
				case 2:
					// FetchTransitions
					_, _ = client.FetchTransitions(ctx, Issue{Key: "RACE-1", InstanceID: "inst-1", BaseURL: ts.URL})
				case 3:
					// DoTransition
					_ = client.DoTransition(ctx, Issue{Key: "RACE-1", InstanceID: "inst-1", BaseURL: ts.URL}, "11")
				case 4:
					// VerifyInstanceConnection
					_, _ = client.VerifyInstanceConnection(ctx, ts.URL, "u1@test.com", "tok1")
				case 5:
					// authHeader and masked token
					_ = client.authHeader()
					_ = client.GetConfig().MaskedAPIToken()
				}
				cancel()
			}
		}()
	}

	wg.Wait()
}
