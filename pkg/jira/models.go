package jira

import (
	"time"
)

// InstanceConfig represents an individual Jira workspace instance (e.g. Avono, Sandbox, Sandbox).
type InstanceConfig struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	BaseURL  string `json:"base_url"`
	Email    string `json:"email"`
	APIToken string `json:"api_token"`
	JQLQuery string `json:"jql_query"`
}

// Config holds multi-instance Jira settings and global application preferences.
type Config struct {
	Instances    []InstanceConfig `json:"instances"`
	ActiveInstID string           `json:"active_instance_id"`
	BaseURL      string           `json:"base_url"`      // Backward-compatibility primary
	Email        string           `json:"email"`         // Backward-compatibility primary
	APIToken     string           `json:"api_token"`     // Backward-compatibility primary
	JQLQuery     string           `json:"jql_query"`     // Backward-compatibility primary
	PollInterval int              `json:"poll_interval"` // Sync frequency in seconds (default: 300 / 5 min)
	BranchPrefix string           `json:"branch_prefix"` // Git branch prefix (e.g., "feature/")
	PinnedKeys   []string         `json:"pinned_keys"`   // Pinned issue keys
	DemoMode     bool             `json:"demo_mode"`     // Mock / demo workflow without live Jira
	DebugMode    bool             `json:"debug_mode"`    // Verbose logging of API requests & auth headers
}

// EnsureInstances ensures at least one valid instance exists from top-level or instances list.
func (c *Config) EnsureInstances() {
	if len(c.Instances) == 0 {
		name := "Avono"
		if c.BaseURL != "" {
			name = "Primary"
		}
		c.Instances = []InstanceConfig{
			{
				ID:       "inst-1",
				Name:     name,
				BaseURL:  c.BaseURL,
				Email:    c.Email,
				APIToken: c.APIToken,
				JQLQuery: c.JQLQuery,
			},
		}
		c.ActiveInstID = "inst-1"
	}
	if c.ActiveInstID == "" && len(c.Instances) > 0 {
		c.ActiveInstID = c.Instances[0].ID
	}
}

// DefaultConfig returns reasonable default configuration with 5-minute polling.
func DefaultConfig() Config {
	return Config{
		Instances: []InstanceConfig{
			{
				ID:       "inst-1",
				Name:     "Avono",
				BaseURL:  "https://avono.atlassian.net",
				Email:    "frank.hess@avono.de",
				APIToken: "",
				JQLQuery: "assignee = currentUser() AND resolution = Unresolved ORDER BY updated DESC",
			},
			{
				ID:       "inst-2",
				Name:     "Sandbox Server",
				BaseURL:  "https://example.atlassian.net",
				Email:    "frank.hess@avono.de",
				APIToken: "",
				JQLQuery: "assignee = currentUser() AND resolution = Unresolved ORDER BY updated DESC",
			},
		},
		ActiveInstID: "inst-1",
		BaseURL:      "https://avono.atlassian.net",
		Email:        "frank.hess@avono.de",
		APIToken:     "",
		JQLQuery:     "assignee = currentUser() AND resolution = Unresolved ORDER BY updated DESC",
		PollInterval: 300, // 5 minutes (300 seconds)
		BranchPrefix: "feature/",
		PinnedKeys:   []string{},
		DemoMode:     false,
		DebugMode:    true,
	}
}

// Issue represents a Jira issue ticket tagged with its source instance.
type Issue struct {
	InstanceID   string    `json:"instance_id"`
	InstanceName string    `json:"instance_name"`
	BaseURL      string    `json:"base_url"`
	ID           string    `json:"id"`
	Key          string    `json:"key"`
	Summary      string    `json:"summary"`
	Status       Status    `json:"status"`
	Priority     Priority  `json:"priority"`
	IssueType    IssueType `json:"issue_type"`
	Updated      time.Time `json:"updated"`
	Pinned       bool      `json:"pinned"`
	URL          string    `json:"url"`
}

// Status represents ticket workflow state.
type Status struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	CategoryKey   string `json:"category_key"`
	CategoryColor string `json:"category_color"`
}

// Priority represents issue urgency.
type Priority struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	IconURL string `json:"iconUrl"`
}

// IssueType represents issue category (Bug, Story, Task, etc.).
type IssueType struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	IconURL string `json:"iconUrl"`
	Subtask bool   `json:"subtask"`
}

// Transition represents an available workflow movement.
type Transition struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	To   Status `json:"to"`
}

// TransitionHistory tracks status movements for undo/redo.
type TransitionHistory struct {
	IssueKey     string
	FromStatusID string
	ToStatusID   string
	FromState    string
	ToState      string
	TransitionID string
	Timestamp    time.Time
}
