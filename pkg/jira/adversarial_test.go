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
	"sync/atomic"
	"testing"
)

func TestAuthHeaderFormatting(t *testing.T) {
	// Test email + token -> Basic auth
	h := authHeaderFor("user@test.com", "my-secret-token")
	expectedBasic := "Basic " + base64.StdEncoding.EncodeToString([]byte("user@test.com:my-secret-token"))
	if h != expectedBasic {
		t.Errorf("expected %q, got %q", expectedBasic, h)
	}

	// Test token only (PAT / Bearer) -> Bearer token
	hBearer := authHeaderFor("", "my-pat-token")
	if hBearer != "Bearer my-pat-token" {
		t.Errorf("expected 'Bearer my-pat-token', got %q", hBearer)
	}

	// Test whitespace trimmed
	hTrim := authHeaderFor("  user@test.com  ", "  token123  ")
	expectedTrim := "Basic " + base64.StdEncoding.EncodeToString([]byte("user@test.com:token123"))
	if hTrim != expectedTrim {
		t.Errorf("expected %q, got %q", expectedTrim, hTrim)
	}

	// Test empty token -> empty string
	if authHeaderFor("user@test.com", "") != "" {
		t.Errorf("expected empty auth header for empty token")
	}
	if authHeaderFor("", "") != "" {
		t.Errorf("expected empty auth header for empty email and token")
	}
}

func TestMaskForLog(t *testing.T) {
	if maskForLog("short") != "*****" {
		t.Errorf("expected '*****', got %q", maskForLog("short"))
	}
	if maskForLog("123456") != "******" {
		t.Errorf("expected '******', got %q", maskForLog("123456"))
	}
	masked := maskForLog("ATATT123456789xyz")
	if !strings.HasPrefix(masked, "ATA...") || !strings.HasSuffix(masked, "...xyz") {
		t.Errorf("expected ATA...xyz, got %q", masked)
	}
}

func TestConfigSaveLoadCycleRoundTrip(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "jira-config-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	configPath := filepath.Join(tempDir, ".jira-quick-access", "config.json")
	SetConfigFilePathForTesting(configPath)
	defer ResetConfigFilePathForTesting()

	cfg := Config{
		Instances: []InstanceConfig{
			{
				ID:       "inst-alpha",
				Name:     "Alpha Org",
				BaseURL:  "https://alpha.atlassian.net",
				Email:    "alpha@org.com",
				APIToken: "token-alpha",
				JQLQuery: "project = ALPHA ORDER BY rank ASC",
			},
			{
				ID:       "inst-beta",
				Name:     "Beta Data Center",
				BaseURL:  "https://jira.beta.local",
				Email:    "beta@local.net",
				APIToken: "token-beta",
				JQLQuery: "project = BETA",
			},
		},
		ActiveInstID: "inst-beta",
		BaseURL:      "https://alpha.atlassian.net",
		Email:        "alpha@org.com",
		APIToken:     "token-alpha",
		JQLQuery:     "project = ALPHA ORDER BY rank ASC",
		PollInterval: 120,
		BranchPrefix: "bugfix/",
		PinnedKeys:   []string{"ALPHA-1", "BETA-2"},
		DemoMode:     false,
		DebugMode:    true,
	}

	err = SaveConfig(cfg)
	if err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	// Verify file permissions (0600)
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("config file not found: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected 0600 file permissions, got %v", info.Mode().Perm())
	}

	// Load config back and verify field parity
	loaded := LoadConfig()
	if len(loaded.Instances) != 2 {
		t.Fatalf("expected 2 instances, got %d", len(loaded.Instances))
	}
	if loaded.ActiveInstID != "inst-beta" {
		t.Errorf("expected ActiveInstID 'inst-beta', got %q", loaded.ActiveInstID)
	}
	if loaded.PollInterval != 120 {
		t.Errorf("expected PollInterval 120, got %d", loaded.PollInterval)
	}
	if loaded.BranchPrefix != "bugfix/" {
		t.Errorf("expected BranchPrefix 'bugfix/', got %q", loaded.BranchPrefix)
	}
	if len(loaded.PinnedKeys) != 2 || loaded.PinnedKeys[0] != "ALPHA-1" || loaded.PinnedKeys[1] != "BETA-2" {
		t.Errorf("unexpected PinnedKeys: %v", loaded.PinnedKeys)
	}
	if loaded.Instances[1].Name != "Beta Data Center" || loaded.Instances[1].BaseURL != "https://jira.beta.local" {
		t.Errorf("instance 1 mismatch: %+v", loaded.Instances[1])
	}
}

func TestEnsureInstancesEdgeCases(t *testing.T) {
	// 1. Completely empty config
	var emptyCfg Config
	emptyCfg.EnsureInstances()
	if len(emptyCfg.Instances) != 1 {
		t.Fatalf("expected 1 default instance, got %d", len(emptyCfg.Instances))
	}
	if emptyCfg.Instances[0].ID != "inst-1" || emptyCfg.Instances[0].Name != "avono" {
		t.Errorf("unexpected default instance: %+v", emptyCfg.Instances[0])
	}
	if emptyCfg.ActiveInstID != "inst-1" {
		t.Errorf("expected ActiveInstID 'inst-1', got %q", emptyCfg.ActiveInstID)
	}

	// 2. Empty instances but primary BaseURL set
	primaryCfg := Config{
		BaseURL:  "https://custom.atlassian.net",
		Email:    "custom@user.com",
		APIToken: "custom-token",
		JQLQuery: "assignee = currentUser()",
	}
	primaryCfg.EnsureInstances()
	if len(primaryCfg.Instances) != 1 {
		t.Fatalf("expected 1 instance created from primary, got %d", len(primaryCfg.Instances))
	}
	if primaryCfg.Instances[0].Name != "Primary" || primaryCfg.Instances[0].BaseURL != "https://custom.atlassian.net" {
		t.Errorf("unexpected primary instance: %+v", primaryCfg.Instances[0])
	}

	// 3. Instances present but ActiveInstID is empty
	multiCfg := Config{
		Instances: []InstanceConfig{
			{ID: "inst-custom-1", Name: "One"},
			{ID: "inst-custom-2", Name: "Two"},
		},
	}
	multiCfg.EnsureInstances()
	if multiCfg.ActiveInstID != "inst-custom-1" {
		t.Errorf("expected ActiveInstID set to 'inst-custom-1', got %q", multiCfg.ActiveInstID)
	}
}

func TestResolveInstanceRoutingExhaustive(t *testing.T) {
	cfg := Config{
		BaseURL:  "https://primary.atlassian.net",
		Email:    "primary@test.com",
		APIToken: "token-primary",
		Instances: []InstanceConfig{
			{
				ID:       "inst-1",
				Name:     "Cloud Production",
				BaseURL:  "https://cloud-prod.atlassian.net",
				Email:    "prod@test.com",
				APIToken: "token-prod",
			},
			{
				ID:       "inst-2",
				Name:     "Internal DC",
				BaseURL:  "https://jira-dc.internal.corp",
				Email:    "dc@test.com",
				APIToken: "token-dc",
			},
		},
	}
	client := NewClient(cfg)

	// A. Match by exact InstanceID
	u, e, tok := client.resolveInstanceForIssue(Issue{Key: "PROD-1", InstanceID: "inst-1"})
	if u != "https://cloud-prod.atlassian.net" || e != "prod@test.com" || tok != "token-prod" {
		t.Errorf("Match by InstanceID failed: got %s, %s, %s", u, e, tok)
	}

	// B. Match by BaseURL with trailing slash on issue vs instance
	u, e, tok = client.resolveInstanceForIssue(Issue{Key: "DC-2", BaseURL: "https://jira-dc.internal.corp/"})
	if u != "https://jira-dc.internal.corp" || e != "dc@test.com" || tok != "token-dc" {
		t.Errorf("Match by BaseURL trailing slash failed: got %s, %s, %s", u, e, tok)
	}

	// C. Match by BaseURL prefix with browse subpath
	u, e, tok = client.resolveInstanceForIssue(Issue{Key: "DC-3", BaseURL: "https://jira-dc.internal.corp/browse/DC-3"})
	if u != "https://jira-dc.internal.corp" || e != "dc@test.com" || tok != "token-dc" {
		t.Errorf("Match by BaseURL prefix failed: got %s, %s, %s", u, e, tok)
	}

	// D. Match by Case-insensitive InstanceName
	u, e, tok = client.resolveInstanceForIssue(Issue{Key: "PROD-4", InstanceName: "cloud production"})
	if u != "https://cloud-prod.atlassian.net" || e != "prod@test.com" || tok != "token-prod" {
		t.Errorf("Match by case-insensitive InstanceName failed: got %s, %s, %s", u, e, tok)
	}

	// E. Fallback to primary config when issue has unknown BaseURL
	u, e, tok = client.resolveInstanceForIssue(Issue{Key: "UNK-5", BaseURL: "https://unknown.jira.com"})
	if u != "https://primary.atlassian.net" || e != "primary@test.com" || tok != "token-primary" {
		t.Errorf("Fallback to primary failed: got %s, %s, %s", u, e, tok)
	}

	// F. Client with empty primary BaseURL falls back to Instances[0]
	cfgNoPrimary := Config{
		Instances: []InstanceConfig{
			{
				ID:       "inst-first",
				Name:     "First",
				BaseURL:  "https://first.atlassian.net",
				Email:    "first@test.com",
				APIToken: "token-first",
			},
		},
	}
	clientNoPrimary := NewClient(cfgNoPrimary)
	u, e, tok = clientNoPrimary.resolveInstanceForIssue(Issue{Key: "UNK-6"})
	if u != "https://first.atlassian.net" || e != "first@test.com" || tok != "token-first" {
		t.Errorf("Fallback to Instances[0] failed: got %s, %s, %s", u, e, tok)
	}
}

func TestVerifyInstanceConnection(t *testing.T) {
	// 1. Success with DisplayName
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
	name, err := client.VerifyInstanceConnection(context.Background(), tsSuccess.URL, "frank.hess@avono.de", "token123")
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if name != "Frank Hess" {
		t.Errorf("expected 'Frank Hess', got %q", name)
	}

	// 2. Fallback to email when DisplayName is missing
	tsEmailFallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"displayName":"","emailAddress":"frank.hess@avono.de","name":""}`)
	}))
	defer tsEmailFallback.Close()

	name, err = client.VerifyInstanceConnection(context.Background(), tsEmailFallback.URL, "frank.hess@avono.de", "token123")
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if name != "frank.hess@avono.de" {
		t.Errorf("expected 'frank.hess@avono.de', got %q", name)
	}

	// 3. Missing BaseURL or Token error
	_, err = client.VerifyInstanceConnection(context.Background(), "", "user@test.com", "token123")
	if err == nil {
		t.Errorf("expected error for empty BaseURL")
	}
	_, err = client.VerifyInstanceConnection(context.Background(), "https://jira.com", "user@test.com", "")
	if err == nil {
		t.Errorf("expected error for empty Token")
	}

	// 4. HTTP 401 Unauthorized error
	ts401 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Unauthorized: Invalid API Token", http.StatusUnauthorized)
	}))
	defer ts401.Close()

	_, err = client.VerifyInstanceConnection(context.Background(), ts401.URL, "user@test.com", "bad-token")
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("expected 401 error, got %v", err)
	}
}

func TestDoTransitionJiraDataCenterV2Fallback(t *testing.T) {
	var v3Calls, v2Calls int32
	var receivedPayload map[string]interface{}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedPayload)

		switch r.URL.Path {
		case "/rest/api/3/issue/DC-500/transitions":
			atomic.AddInt32(&v3Calls, 1)
			// Return 405 Method Not Allowed on v3 for Jira Data Center
			http.Error(w, "REST v3 not supported on DC", http.StatusMethodNotAllowed)
		case "/rest/api/2/issue/DC-500/transitions":
			atomic.AddInt32(&v2Calls, 1)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "Not found", http.StatusNotFound)
		}
	}))
	defer ts.Close()

	cfg := Config{
		Instances: []InstanceConfig{
			{
				ID:       "inst-dc",
				BaseURL:  ts.URL,
				Email:    "dc@local.net",
				APIToken: "dc-pat",
			},
		},
	}
	client := NewClient(cfg)

	err := client.DoTransition(context.Background(), Issue{
		Key:        "DC-500",
		InstanceID: "inst-dc",
		BaseURL:    ts.URL,
	}, "31")

	if err != nil {
		t.Fatalf("DoTransition failed: %v", err)
	}
	if atomic.LoadInt32(&v3Calls) != 1 {
		t.Errorf("expected 1 call to v3, got %d", v3Calls)
	}
	if atomic.LoadInt32(&v2Calls) != 1 {
		t.Errorf("expected 1 call to v2 fallback, got %d", v2Calls)
	}

	// Verify transition ID was transmitted in payload
	transObj, ok := receivedPayload["transition"].(map[string]interface{})
	if !ok || transObj["id"] != "31" {
		t.Errorf("expected transition id '31' in request payload, got %+v", receivedPayload)
	}
}

func TestFetchAssignedIssuesPartialFailureTolerance(t *testing.T) {
	// Server 1: Healthy
	ts1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"issues":[{"id":"1","key":"OK-1","fields":{"summary":"Task OK","status":{"name":"Open","statusCategory":{"key":"new"}},"priority":{"name":"High"},"issuetype":{"name":"Bug"},"updated":"2026-04-17T12:00:00.000+0000"}}]}`)
	}))
	defer ts1.Close()

	// Server 2: Returns 500 Internal Server Error
	ts2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Database down", http.StatusInternalServerError)
	}))
	defer ts2.Close()

	// Server 3: Skipped (empty credentials)
	cfg := Config{
		Instances: []InstanceConfig{
			{
				ID:       "inst-1",
				Name:     "Healthy Instance",
				BaseURL:  ts1.URL,
				Email:    "u1@test.com",
				APIToken: "token1",
			},
			{
				ID:       "inst-2",
				Name:     "Faulty Instance",
				BaseURL:  ts2.URL,
				Email:    "u2@test.com",
				APIToken: "token2",
			},
			{
				ID:       "inst-3",
				Name:     "Unconfigured Instance",
				BaseURL:  "",
				APIToken: "",
			},
		},
	}
	client := NewClient(cfg)

	// FetchAssignedIssues should succeed and return issues from the healthy instance
	issues, err := client.FetchAssignedIssues(context.Background())
	if err != nil {
		t.Fatalf("expected partial aggregation success, got error: %v", err)
	}
	if len(issues) != 1 || issues[0].Key != "OK-1" {
		t.Fatalf("expected issue OK-1 from healthy instance, got %+v", issues)
	}
}

func TestFetchAssignedIssuesAllFailing(t *testing.T) {
	tsFail := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
	}))
	defer tsFail.Close()

	cfg := Config{
		Instances: []InstanceConfig{
			{
				ID:       "inst-fail",
				BaseURL:  tsFail.URL,
				Email:    "fail@test.com",
				APIToken: "tok",
			},
		},
	}
	client := NewClient(cfg)

	_, err := client.FetchAssignedIssues(context.Background())
	if err == nil {
		t.Fatalf("expected error when all instances fail, got nil")
	}
}

func TestClientThreadSafety(t *testing.T) {
	cfg := DefaultConfig()
	client := NewClient(cfg)

	done := make(chan bool)
	for i := 0; i < 20; i++ {
		go func(idx int) {
			for j := 0; j < 50; j++ {
				c := client.GetConfig()
				c.PollInterval = 100 + idx + j
				client.UpdateConfig(c)
				_ = client.authHeader()
			}
			done <- true
		}(i)
	}

	for i := 0; i < 20; i++ {
		<-done
	}
}
