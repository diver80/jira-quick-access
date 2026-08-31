package jira

import (
	"fmt"
	"sync"
	"time"
)

var (
	mockMu     sync.Mutex
	mockIssues = []Issue{
		{
			ID:      "10001",
			Key:     "PROJ-102",
			Summary: "Implement OAuth PKCE Flow for Edge Rail",
			Status: Status{
				ID:          "3",
				Name:        "In Progress",
				CategoryKey: "indeterminate",
			},
			Priority: Priority{
				ID:   "2",
				Name: "High",
			},
			IssueType: IssueType{
				ID:   "10001",
				Name: "Story",
			},
			Updated: time.Now().Add(-12 * time.Minute),
			Pinned:  true,
			URL:     "https://jira.example.com/browse/PROJ-102",
		},
		{
			ID:      "10002",
			Key:     "PROJ-98",
			Summary: "Fix race condition in token poller background loop",
			Status: Status{
				ID:          "4",
				Name:        "In Review",
				CategoryKey: "indeterminate",
			},
			Priority: Priority{
				ID:   "1",
				Name: "Highest",
			},
			IssueType: IssueType{
				ID:   "10002",
				Name: "Bug",
			},
			Updated: time.Now().Add(-35 * time.Minute),
			Pinned:  false,
			URL:     "https://jira.example.com/browse/PROJ-98",
		},
		{
			ID:      "10003",
			Key:     "PROJ-115",
			Summary: "Update deployment manifest & dynamic island assets",
			Status: Status{
				ID:          "1",
				Name:        "To Do",
				CategoryKey: "new",
			},
			Priority: Priority{
				ID:   "3",
				Name: "Medium",
			},
			IssueType: IssueType{
				ID:   "10003",
				Name: "Task",
			},
			Updated: time.Now().Add(-2 * time.Hour),
			Pinned:  false,
			URL:     "https://jira.example.com/browse/PROJ-115",
		},
		{
			ID:      "10004",
			Key:     "PROJ-84",
			Summary: "Design liquid-glass morphism HUD widget tree",
			Status: Status{
				ID:          "3",
				Name:        "In Progress",
				CategoryKey: "indeterminate",
			},
			Priority: Priority{
				ID:   "2",
				Name: "High",
			},
			IssueType: IssueType{
				ID:   "10001",
				Name: "Story",
			},
			Updated: time.Now().Add(-3 * time.Hour),
			Pinned:  true,
			URL:     "https://jira.example.com/browse/PROJ-84",
		},
		{
			ID:      "10005",
			Key:     "PROJ-77",
			Summary: "Cross-platform packaging script for OSX/Win/Linux",
			Status: Status{
				ID:          "5",
				Name:        "Done",
				CategoryKey: "done",
			},
			Priority: Priority{
				ID:   "3",
				Name: "Medium",
			},
			IssueType: IssueType{
				ID:   "10003",
				Name: "Task",
			},
			Updated: time.Now().Add(-5 * time.Hour),
			Pinned:  false,
			URL:     "https://jira.example.com/browse/PROJ-77",
		},
	}
)

// GetMockIssues returns a copy of mock issues with pinned status updated.
func GetMockIssues(pinnedKeys []string) []Issue {
	mockMu.Lock()
	defer mockMu.Unlock()

	pinnedMap := make(map[string]bool)
	for _, k := range pinnedKeys {
		pinnedMap[k] = true
	}

	result := make([]Issue, len(mockIssues))
	for i, iss := range mockIssues {
		result[i] = iss
		if len(pinnedKeys) > 0 {
			result[i].Pinned = pinnedMap[iss.Key]
		}
	}
	return result
}

// GetMockTransitions returns available transitions for mock issues.
func GetMockTransitions(issueKey string) []Transition {
	return []Transition{
		{ID: "11", Name: "To Do", To: Status{ID: "1", Name: "To Do", CategoryKey: "new"}},
		{ID: "21", Name: "In Progress", To: Status{ID: "3", Name: "In Progress", CategoryKey: "indeterminate"}},
		{ID: "31", Name: "In Review", To: Status{ID: "4", Name: "In Review", CategoryKey: "indeterminate"}},
		{ID: "41", Name: "Done", To: Status{ID: "5", Name: "Done", CategoryKey: "done"}},
	}
}

// ExecuteMockTransition updates an issue's status in memory.
func ExecuteMockTransition(issueKey, transitionID string) error {
	mockMu.Lock()
	defer mockMu.Unlock()

	for i := range mockIssues {
		if mockIssues[i].Key == issueKey {
			switch transitionID {
			case "11":
				mockIssues[i].Status = Status{ID: "1", Name: "To Do", CategoryKey: "new"}
			case "21":
				mockIssues[i].Status = Status{ID: "3", Name: "In Progress", CategoryKey: "indeterminate"}
			case "31":
				mockIssues[i].Status = Status{ID: "4", Name: "In Review", CategoryKey: "indeterminate"}
			case "41":
				mockIssues[i].Status = Status{ID: "5", Name: "Done", CategoryKey: "done"}
			default:
				mockIssues[i].Status = Status{ID: "3", Name: "In Progress", CategoryKey: "indeterminate"}
			}
			mockIssues[i].Updated = time.Now()
			return nil
		}
	}
	return fmt.Errorf("issue %s not found", issueKey)
}
