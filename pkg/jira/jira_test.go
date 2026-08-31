package jira

import (
	"context"
	"testing"
)

func TestFormatBranchName(t *testing.T) {
	tests := []struct {
		prefix   string
		key      string
		summary  string
		expected string
	}{
		{
			prefix:   "feature/",
			key:      "PROJ-102",
			summary:  "Implement OAuth PKCE Flow",
			expected: "feature/PROJ-102-implement-oauth-pkce-flow",
		},
		{
			prefix:   "fix/",
			key:      "PROJ-98",
			summary:  "Fix race condition in token poller!",
			expected: "fix/PROJ-98-fix-race-condition-in-token-poller",
		},
		{
			prefix:   "",
			key:      "PROJ-115",
			summary:  "Update deployment",
			expected: "feature/PROJ-115-update-deployment",
		},
	}

	for _, tt := range tests {
		got := FormatBranchName(tt.prefix, tt.key, tt.summary)
		if got != tt.expected {
			t.Errorf("FormatBranchName(%q, %q, %q) = %q; want %q", tt.prefix, tt.key, tt.summary, got, tt.expected)
		}
	}
}

func TestMockJiraWorkflow(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DemoMode = true
	client := NewClient(cfg)

	issues, err := client.FetchAssignedIssues(context.Background())
	if err != nil {
		t.Fatalf("expected no error fetching mock issues, got %v", err)
	}
	if len(issues) == 0 {
		t.Fatalf("expected mock issues, got 0")
	}

	// Verify transition execution
	err = client.ExecuteTransition(context.Background(), "PROJ-102", "41") // Move to Done
	if err != nil {
		t.Fatalf("failed to execute mock transition: %v", err)
	}

	issuesAfter, _ := client.FetchAssignedIssues(context.Background())
	found := false
	for _, iss := range issuesAfter {
		if iss.Key == "PROJ-102" {
			found = true
			if iss.Status.Name != "Done" {
				t.Errorf("expected PROJ-102 to be Done, got %s", iss.Status.Name)
			}
		}
	}
	if !found {
		t.Errorf("PROJ-102 not found in mock issues")
	}
}
