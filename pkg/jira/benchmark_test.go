package jira

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func generateBenchmarkIssues(count int) []Issue {
	issues := make([]Issue, count)
	now := time.Now()
	for i := 0; i < count; i++ {
		instID := fmt.Sprintf("inst-%d", i%3)
		issues[i] = Issue{
			InstanceID:   instID,
			InstanceName: fmt.Sprintf("Instance %d", i%3),
			BaseURL:      "https://benchmark.atlassian.net",
			ID:           fmt.Sprintf("%d", 10000+i),
			Key:          fmt.Sprintf("BENCH-%d", i+1),
			Summary:      fmt.Sprintf("Benchmark issue summary description number %d with some extra context keywords", i+1),
			Status: Status{
				ID:            fmt.Sprintf("%d", (i%4)+1),
				Name:          []string{"To Do", "In Progress", "In Review", "Done"}[i%4],
				CategoryKey:   []string{"new", "indeterminate", "indeterminate", "done"}[i%4],
				CategoryColor: []string{"blue", "yellow", "yellow", "green"}[i%4],
			},
			Priority: Priority{
				ID:      fmt.Sprintf("%d", (i%5)+1),
				Name:    []string{"Lowest", "Low", "Medium", "High", "Highest"}[i%5],
				IconURL: "https://benchmark.atlassian.net/images/icons/priority.svg",
			},
			IssueType: IssueType{
				ID:      fmt.Sprintf("%d", (i%3)+1),
				Name:    []string{"Bug", "Story", "Task"}[i%3],
				IconURL: "https://benchmark.atlassian.net/images/icons/type.svg",
				Subtask: i%10 == 0,
			},
			Updated: now.Add(time.Duration(-i) * time.Hour),
			Pinned:  i%20 == 0,
			URL:     fmt.Sprintf("https://benchmark.atlassian.net/browse/BENCH-%d", i+1),
		}
	}
	return issues
}

func BenchmarkIssueSerialization100(b *testing.B) {
	issues := generateBenchmarkIssues(100)
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		data, err := json.Marshal(issues)
		if err != nil {
			b.Fatalf("Marshal failed: %v", err)
		}
		var decoded []Issue
		if err := json.Unmarshal(data, &decoded); err != nil {
			b.Fatalf("Unmarshal failed: %v", err)
		}
	}
}

func BenchmarkIssueSerialization500(b *testing.B) {
	issues := generateBenchmarkIssues(500)
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		data, err := json.Marshal(issues)
		if err != nil {
			b.Fatalf("Marshal failed: %v", err)
		}
		var decoded []Issue
		if err := json.Unmarshal(data, &decoded); err != nil {
			b.Fatalf("Unmarshal failed: %v", err)
		}
	}
}

func BenchmarkFormatBranchName(b *testing.B) {
	prefix := "feature/"
	key := "PROJ-1234"
	summary := "Refactor Authentication and Token Renewal Logic for OAuth PKCE"

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = FormatBranchName(prefix, key, summary)
	}
}

func BenchmarkFormatBranchNameComplex(b *testing.B) {
	prefix := "bugfix/"
	key := "CORE-9999"
	summary := "Fix: [Crash] on nil pointer in AppView.Draw() & Event() --- (Critical: v1.27)!!!"

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = FormatBranchName(prefix, key, summary)
	}
}

func BenchmarkConfigSaveLoad(b *testing.B) {
	cfg := Config{
		Instances: []InstanceConfig{
			{ID: "inst-1", Name: "avono cloud", BaseURL: "https://avono.atlassian.net", Email: "frank.hess@avono.de", APIToken: "token1", JQLQuery: "assignee = currentUser() AND resolution = Unresolved ORDER BY updated DESC"},
			{ID: "inst-2", Name: "avono DC", BaseURL: "https://jira.avono.de", Email: "frank.hess@avono.de", APIToken: "token2", JQLQuery: "assignee = currentUser() AND resolution = Unresolved ORDER BY updated DESC"},
			{ID: "inst-3", Name: "sandbox", BaseURL: "https://sandbox.atlassian.net", Email: "frank.hess@avono.de", APIToken: "token3", JQLQuery: "assignee = currentUser() AND resolution = Unresolved ORDER BY updated DESC"},
		},
		ActiveInstID: "inst-1",
		BaseURL:      "https://avono.atlassian.net",
		Email:        "frank.hess@avono.de",
		APIToken:     "token1",
		PollInterval: 300,
		BranchPrefix: "feature/",
		PinnedKeys:   []string{"AVN-1", "AVN-2", "SBX-3"},
		DemoMode:     false,
		DebugMode:    true,
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		data, err := json.Marshal(cfg)
		if err != nil {
			b.Fatalf("Marshal failed: %v", err)
		}
		var loaded Config
		if err := json.Unmarshal(data, &loaded); err != nil {
			b.Fatalf("Unmarshal failed: %v", err)
		}
	}
}

func BenchmarkAuthHeaderFormatting(b *testing.B) {
	email := "frank.hess@avono.de"
	token := "mock-api-token-for-benchmark-testing-only"

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = authHeaderFor(email, token)
	}
}
