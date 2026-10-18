package jira

import (
	"slices"
	"strconv"
	"strings"
)

// SortIssues orders issues in place by the given mode. The sort is stable, so ties
// (and the Jira mode) keep the order the instances returned.
func SortIssues(issues []Issue, mode string) {
	switch mode {
	case SortUpdated:
		slices.SortStableFunc(issues, func(a, b Issue) int {
			return b.Updated.Compare(a.Updated)
		})
	case SortPriority:
		slices.SortStableFunc(issues, func(a, b Issue) int {
			return priorityRank(a.Priority) - priorityRank(b.Priority)
		})
	case SortStatus:
		slices.SortStableFunc(issues, func(a, b Issue) int {
			if d := statusRank(a.Status) - statusRank(b.Status); d != 0 {
				return d
			}
			return strings.Compare(strings.ToLower(a.Status.Name), strings.ToLower(b.Status.Name))
		})
	}
}

// Priority names of Jira's default and legacy schemes, most urgent first.
var priorityNames = map[string]int{
	"blocker": 0, "highest": 1, "critical": 2, "high": 3, "major": 4,
	"medium": 5, "normal": 5, "low": 6, "minor": 7, "lowest": 8, "trivial": 9,
}

// priorityRank maps a priority to a sortable rank; lower is more urgent. Custom
// priorities fall back to their numeric ID (Jira numbers its defaults 1..5 from
// highest), and issues without a priority come last.
func priorityRank(p Priority) int {
	if rank, ok := priorityNames[strings.ToLower(strings.TrimSpace(p.Name))]; ok {
		return rank * 10
	}
	if id, err := strconv.Atoi(p.ID); err == nil {
		return id*20 + 5
	}
	return 1000
}

// statusRank puts work in progress first, then open tickets, then finished ones.
func statusRank(s Status) int {
	switch strings.ToLower(s.CategoryKey) {
	case "indeterminate":
		return 0
	case "new":
		return 1
	case "done":
		return 2
	}
	return 1
}
