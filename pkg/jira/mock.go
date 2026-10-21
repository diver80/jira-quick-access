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

// GetMockChanges returns a fixed set of changelog entries for demo mode. IDs are
// stable, so repeated checks do not add them again.
func GetMockChanges(instanceID string) []ChangeEntry {
	now := time.Now()
	base := func(key, summary string) ChangeEntry {
		return ChangeEntry{InstanceID: instanceID, IssueKey: key, Summary: summary}
	}
	at := func(d time.Duration) time.Time { return now.Add(-d).Truncate(time.Minute) }

	e1 := base("PROJ-102", "Implement OAuth PKCE Flow for Edge Rail")
	e1.ID, e1.Kind, e1.Author, e1.At = "demo:1", ChangeComment, "Maria Keller", at(8*time.Minute)
	e1.Text, e1.Mentioned = "@Demo User could you double-check the redirect URI for the desktop client? The callback fails on the DC test instance.", true
	e2 := base("PROJ-102", "Implement OAuth PKCE Flow for Edge Rail")
	e2.ID, e2.Kind, e2.Author, e2.At = "demo:2", ChangeStatus, "Maria Keller", at(35*time.Minute)
	e2.Field, e2.From, e2.To = "Status", "To Do", "In Progress"
	e3 := base("PROJ-98", "Fix race condition in token poller background loop")
	e3.ID, e3.Kind, e3.Author, e3.At = "demo:3", ChangeField, "Jonas Weber", at(2*time.Hour)
	e3.Field, e3.From, e3.To = "assignee", "Jonas Weber", "Demo User"
	e4 := base("PROJ-98", "Fix race condition in token poller background loop")
	e4.ID, e4.Kind, e4.Author, e4.At, e4.Own = "demo:4", ChangeCommentEdited, "Demo User", at(3*time.Hour), true
	e4.Text = "Reproduced with 200 parallel refreshes; the mutex around the token cache fixes it."
	e5 := base("PROJ-77", "Update documentation for multi-instance setup")
	e5.ID, e5.Kind, e5.At = "demo:5", ChangeAdded, at(26*time.Hour)
	return []ChangeEntry{e1, e2, e3, e4, e5}
}
