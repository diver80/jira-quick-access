package ui

import (
	"fmt"
	"testing"
	"time"

	"jira-quick-access/pkg/jira"
	"jira-quick-access/pkg/window"
)

func generateBenchmarkUIIssues(count int) []jira.Issue {
	issues := make([]jira.Issue, count)
	now := time.Now()
	for i := 0; i < count; i++ {
		instID := fmt.Sprintf("inst-%d", i%3)
		issues[i] = jira.Issue{
			InstanceID:   instID,
			InstanceName: fmt.Sprintf("Instance %d", i%3),
			BaseURL:      "https://benchmark.atlassian.net",
			ID:           fmt.Sprintf("%d", 10000+i),
			Key:          fmt.Sprintf("BENCH-%d", i+1),
			Summary:      fmt.Sprintf("Benchmark UI issue summary description %d with search keywords and layout data", i+1),
			Status: jira.Status{
				ID:            fmt.Sprintf("%d", (i%4)+1),
				Name:          []string{"To Do", "In Progress", "In Review", "Done"}[i%4],
				CategoryKey:   []string{"new", "indeterminate", "indeterminate", "done"}[i%4],
				CategoryColor: []string{"blue", "yellow", "yellow", "green"}[i%4],
			},
			Priority: jira.Priority{
				ID:      fmt.Sprintf("%d", (i%5)+1),
				Name:    []string{"Lowest", "Low", "Medium", "High", "Highest"}[i%5],
				IconURL: "https://benchmark.atlassian.net/images/icons/priority.svg",
			},
			IssueType: jira.IssueType{
				ID:      fmt.Sprintf("%d", (i%3)+1),
				Name:    []string{"Bug", "Story", "Task"}[i%3],
				IconURL: "https://benchmark.atlassian.net/images/icons/type.svg",
				Subtask: i%10 == 0,
			},
			Updated: now.Add(time.Duration(-i) * time.Hour),
			Pinned:  i%25 == 0,
			URL:     fmt.Sprintf("https://benchmark.atlassian.net/browse/BENCH-%d", i+1),
		}
	}
	return issues
}

func setupBenchmarkAppView(issueCount int) *AppView {
	cfg := jira.Config{
		DemoMode: true,
		Instances: []jira.InstanceConfig{
			{ID: "inst-0", Name: "Org 0", BaseURL: "https://inst0.test"},
			{ID: "inst-1", Name: "Org 1", BaseURL: "https://inst1.test"},
			{ID: "inst-2", Name: "Org 2", BaseURL: "https://inst2.test"},
		},
	}
	client := jira.NewClient(cfg)
	view := NewAppView(cfg, client, nil)
	view.mu.Lock()
	view.issues = generateBenchmarkUIIssues(issueCount)
	view.activeInstIdx = 0
	view.invalidateFilterCacheLocked()
	view.mu.Unlock()
	return view
}

// BenchmarkFilterIssues: 100 Issues Cold Cache
func BenchmarkFilterIssues100Cold(b *testing.B) {
	view := setupBenchmarkAppView(100)
	defer view.Close()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		view.mu.Lock()
		view.searchQuery = "layout"
		view.invalidateFilterCacheLocked()
		view.mu.Unlock()
		_ = view.getFilteredIssues()
	}
}

// BenchmarkFilterIssues: 100 Issues Warm Memoized Cache
func BenchmarkFilterIssues100Warm(b *testing.B) {
	view := setupBenchmarkAppView(100)
	defer view.Close()

	view.mu.Lock()
	view.searchQuery = "layout"
	view.invalidateFilterCacheLocked()
	view.mu.Unlock()
	_ = view.getFilteredIssues() // Prime cache

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = view.getFilteredIssues()
	}
}

// BenchmarkFilterIssues: 500 Issues Cold Cache
func BenchmarkFilterIssues500Cold(b *testing.B) {
	view := setupBenchmarkAppView(500)
	defer view.Close()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		view.mu.Lock()
		view.searchQuery = "search"
		view.invalidateFilterCacheLocked()
		view.mu.Unlock()
		_ = view.getFilteredIssues()
	}
}

// BenchmarkFilterIssues: 500 Issues Warm Memoized Cache
func BenchmarkFilterIssues500Warm(b *testing.B) {
	view := setupBenchmarkAppView(500)
	defer view.Close()

	view.mu.Lock()
	view.searchQuery = "search"
	view.invalidateFilterCacheLocked()
	view.mu.Unlock()
	_ = view.getFilteredIssues() // Prime cache

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = view.getFilteredIssues()
	}
}

// BenchmarkFilterIssues: 1000 Issues Cold Cache
func BenchmarkFilterIssues1000Cold(b *testing.B) {
	view := setupBenchmarkAppView(1000)
	defer view.Close()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		view.mu.Lock()
		view.searchQuery = "bench"
		view.invalidateFilterCacheLocked()
		view.mu.Unlock()
		_ = view.getFilteredIssues()
	}
}

// BenchmarkFilterIssues: 1000 Issues Warm Memoized Cache
func BenchmarkFilterIssues1000Warm(b *testing.B) {
	view := setupBenchmarkAppView(1000)
	defer view.Close()

	view.mu.Lock()
	view.searchQuery = "bench"
	view.invalidateFilterCacheLocked()
	view.mu.Unlock()
	_ = view.getFilteredIssues() // Prime cache

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = view.getFilteredIssues()
	}
}

// BenchmarkComputeSize across Window States
func BenchmarkComputeSizeRest(b *testing.B) {
	view := setupBenchmarkAppView(50)
	defer view.Close()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = view.computeSize(window.StateRest)
	}
}

func BenchmarkComputeSizeFan(b *testing.B) {
	view := setupBenchmarkAppView(50)
	defer view.Close()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = view.computeSize(window.StateFan)
	}
}

func BenchmarkComputeSizeExpanded(b *testing.B) {
	view := setupBenchmarkAppView(50)
	defer view.Close()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = view.computeSize(window.StateExpanded)
	}
}

func BenchmarkAppViewSnapshot(b *testing.B) {
	view := setupBenchmarkAppView(100)
	defer view.Close()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = view.snapshot()
	}
}

func BenchmarkGetStatusColors(b *testing.B) {
	categories := []string{"new", "indeterminate", "done"}
	statuses := []string{"To Do", "In Progress", "In Review", "Done"}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		cat := categories[i%len(categories)]
		st := statuses[i%len(statuses)]
		_, _, _ = GetStatusColors(cat, st)
	}
}
