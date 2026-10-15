package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSearchEndpointsPreferJQLOnCloud(t *testing.T) {
	cloud := searchEndpoints("https://acme.atlassian.net")
	if !strings.HasSuffix(cloud[0], "/rest/api/3/search/jql") {
		t.Errorf("Cloud must try /search/jql first, got %v", cloud)
	}
	dc := searchEndpoints("https://jira.example.com")
	if !strings.HasSuffix(dc[0], "/rest/api/2/search") {
		t.Errorf("Data Center must try /rest/api/2/search first, got %v", dc)
	}
}

func issueJSON(key string) string {
	return fmt.Sprintf(`{"id":"%s","key":"%s","fields":{"summary":"s","status":{"name":"Open","statusCategory":{"key":"new"}},"updated":"2026-09-01T10:00:00.000+0000"}}`, key, key)
}

func TestFetchPaginatesSearchJQL(t *testing.T) {
	var removedCalls int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/search/jql" {
			atomic.AddInt32(&removedCalls, 1)
			http.Error(w, "gone", http.StatusGone)
			return
		}
		var req map[string]interface{}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &req)
		if req["nextPageToken"] == nil {
			fmt.Fprintf(w, `{"issues":[%s],"nextPageToken":"p2","isLast":false}`, issueJSON("A-1"))
			return
		}
		fmt.Fprintf(w, `{"issues":[%s],"isLast":true}`, issueJSON("A-2"))
	}))
	defer ts.Close()

	client := NewClient(Config{Instances: []InstanceConfig{{ID: "i", BaseURL: ts.URL, APIToken: "t"}}})
	issues, err := client.FetchAssignedIssues(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 2 || issues[0].Key != "A-1" || issues[1].Key != "A-2" {
		t.Fatalf("expected both pages, got %+v", issues)
	}
	if removedCalls != 1 {
		t.Errorf("removed endpoint should be probed once (first page only), got %d", removedCalls)
	}
	if issues[0].Updated.IsZero() {
		t.Error("Jira timestamp with +0000 zone was not parsed")
	}
}

func TestFetchPaginatesLegacySearch(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			StartAt int `json:"startAt"`
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &req)
		key := fmt.Sprintf("DC-%d", req.StartAt+1)
		fmt.Fprintf(w, `{"startAt":%d,"total":3,"issues":[%s]}`, req.StartAt, issueJSON(key))
	}))
	defer ts.Close()

	client := NewClient(Config{Instances: []InstanceConfig{{ID: "i", BaseURL: ts.URL, APIToken: "t"}}})
	issues, err := client.FetchAssignedIssues(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 3 {
		t.Fatalf("expected 3 issues across pages, got %d", len(issues))
	}
}

func TestSearchDoesNotFallBackOnAuthError(t *testing.T) {
	var calls int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"errorMessages":["Client must be authenticated"]}`)
	}))
	defer ts.Close()

	client := NewClient(Config{Instances: []InstanceConfig{{ID: "i", BaseURL: ts.URL, APIToken: "bad"}}})
	_, err := client.FetchAssignedIssues(context.Background())
	if err == nil || !strings.Contains(err.Error(), "HTTP 401") || !strings.Contains(err.Error(), "Client must be authenticated") {
		t.Fatalf("expected the 401 with Jira's message, got %v", err)
	}
	if calls != 1 {
		t.Errorf("401 must not be retried on other endpoints, got %d calls", calls)
	}
}

func TestTransitionIsNotReplayedAfterTransportError(t *testing.T) {
	var calls int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		time.Sleep(200 * time.Millisecond) // Jira applied it, but the client gives up first
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	client := NewClient(Config{Instances: []InstanceConfig{{ID: "i", BaseURL: ts.URL, APIToken: "t"}}})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := client.DoTransition(ctx, Issue{Key: "A-1", InstanceID: "i"}, "11"); err == nil {
		t.Fatal("expected timeout error")
	}
	time.Sleep(250 * time.Millisecond)
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Errorf("transition POST must not be replayed on the v2 endpoint, got %d calls", n)
	}
}

func TestErrorDetailTruncatesHTML(t *testing.T) {
	page := "<html>" + strings.Repeat("x", 5000) + "</html>"
	if got := errorDetail([]byte(page)); len(got) > 210 {
		t.Errorf("error detail not truncated: %d bytes", len(got))
	}
}

func TestLoadConfigMigratesLegacySingleInstance(t *testing.T) {
	path := reviewConfigPath(t)
	writeReviewConfig(t, path, []byte(`{"base_url":"https://legacy.atlassian.net","email":"a@b.c","api_token":"tok"}`))

	cfg := LoadConfig()
	if len(cfg.Instances) != 1 {
		t.Fatalf("expected 1 migrated instance, got %+v", cfg.Instances)
	}
	inst := cfg.Instances[0]
	if inst.BaseURL != "https://legacy.atlassian.net" || inst.APIToken != "tok" {
		t.Errorf("legacy credentials not migrated: %+v", inst)
	}
}

func TestDotEnvCannotRedirectStoredToken(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeReviewConfig(t, dir+"/.env", []byte("JIRA_BASE_URL=https://evil.example\n"))

	cfg := Config{Instances: []InstanceConfig{{ID: "inst-1", BaseURL: "https://good.atlassian.net", APIToken: "secret"}}}
	if LoadFromDotEnv(&cfg) {
		t.Fatal(".env without a token must not be applied")
	}
	if cfg.Instances[0].BaseURL != "https://good.atlassian.net" {
		t.Fatalf("base URL redirected to %q", cfg.Instances[0].BaseURL)
	}

	writeReviewConfig(t, dir+"/.env", []byte("JIRA_BASE_URL=https://dev.atlassian.net\nJIRA_EMAIL=d@e.v\nJIRA_API_TOKEN=devtoken\n"))
	if !LoadFromDotEnv(&cfg) {
		t.Fatal("complete .env credentials should be applied")
	}
	if inst := cfg.Instances[0]; inst.BaseURL != "https://dev.atlassian.net" || inst.APIToken != "devtoken" || inst.Email != "d@e.v" {
		t.Fatalf("unexpected instance after import: %+v", inst)
	}
}
