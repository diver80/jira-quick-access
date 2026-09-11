package jira

import (
	"strings"
	"time"
)

// InstanceConfig represents an individual Jira workspace instance (e.g. avono cloud, avono DC).
type InstanceConfig struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	BaseURL  string `json:"base_url"`
	Email    string `json:"email"`
	APIToken string `json:"api_token"`
	JQLQuery string `json:"jql_query"`
	Color    string `json:"color,omitempty"` // User-defined profile accent color (hex string)
}

// MaskedAPIToken returns a masked representation of the instance API token for logging and UI display.
func (ic InstanceConfig) MaskedAPIToken() string {
	if len(ic.APIToken) <= 6 {
		return strings.Repeat("*", len(ic.APIToken))
	}
	return ic.APIToken[:3] + "..." + ic.APIToken[len(ic.APIToken)-3:]
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
	DemoMode     bool             `json:"demo_mode"`     // Mock / demo workflow without live Jira (default: false)
	DebugMode    bool             `json:"debug_mode"`    // Verbose logging of API requests & auth headers
	DockSide     int              `json:"dock_side"`     // 0: Right Edge, 1: Left Edge
	MonitorIndex int              `json:"monitor_index"` // Display index (0..N-1)
	PosYRatio    float64          `json:"pos_y_ratio"`   // Vertical anchor position ratio (0.0..1.0, default 0.5)
	AlwaysOnTop  bool             `json:"always_on_top"` // Float HUD window above all regular windows (default: true)
	AutoHide     bool             `json:"auto_hide"`     // macOS Dock-style auto-hide when resting (default: false)
}

// MaskedAPIToken returns a masked representation of the active/primary API token.
func (c Config) MaskedAPIToken() string {
	tok := c.APIToken
	if tok == "" && len(c.Instances) > 0 {
		tok = c.Instances[0].APIToken
	}
	if len(tok) <= 6 {
		return strings.Repeat("*", len(tok))
	}
	return tok[:3] + "..." + tok[len(tok)-3:]
}

// EnsureInstances ensures at least one valid instance exists from top-level or instances list.
func (c *Config) EnsureInstances() {
	if len(c.Instances) == 0 {
		name := "avono"
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
				Name:     "avono cloud",
				BaseURL:  "https://avono.atlassian.net",
				Email:    "frank.hess@avono.de",
				APIToken: "",
				JQLQuery: "assignee = currentUser() AND resolution = Unresolved ORDER BY updated DESC",
				Color:    "#38bdf8",
			},
			{
				ID:       "inst-2",
				Name:     "avono DC",
				BaseURL:  "https://jira.avono.de",
				Email:    "frank.hess@avono.de",
				APIToken: "",
				JQLQuery: "assignee = currentUser() AND resolution = Unresolved ORDER BY updated DESC",
				Color:    "#a855f7",
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
		DockSide:     0,
		MonitorIndex: 0,
		PosYRatio:    0.5,
		AlwaysOnTop:  true,
		AutoHide:     false,
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
