package jira

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Client handles Jira REST API interactions for one or multiple instances.
type Client struct {
	config     Config
	httpClient *http.Client
	mu         sync.RWMutex
}

// NewClient creates a new Jira client.
func NewClient(cfg Config) *Client {
	cfg.EnsureInstances()
	return &Client{
		config: cfg,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// UpdateConfig updates the client configuration.
func (c *Client) UpdateConfig(cfg Config) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cfg.EnsureInstances()
	c.config = cfg
}

// GetConfig returns the current configuration.
func (c *Client) GetConfig() Config {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.config
}

func authHeaderFor(email, token string) string {
	email = strings.TrimSpace(email)
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	if email == "" {
		return "Bearer " + token
	}
	credentials := fmt.Sprintf("%s:%s", email, token)
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(credentials))
}

func (c *Client) authHeader() string {
	return authHeaderFor(c.config.Email, c.config.APIToken)
}

func maskForLog(token string) string {
	if len(token) <= 6 {
		return strings.Repeat("*", len(token))
	}
	return token[:3] + "..." + token[len(token)-3:]
}

// VerifyConnection checks if Jira credentials are valid for a given instance.
func (c *Client) VerifyConnection(ctx context.Context) (string, error) {
	c.mu.RLock()
	cfg := c.config
	c.mu.RUnlock()

	return c.VerifyInstanceConnection(ctx, cfg.BaseURL, cfg.Email, cfg.APIToken)
}

// VerifyInstanceConnection verifies credentials for a specific instance.
func (c *Client) VerifyInstanceConnection(ctx context.Context, baseURL, email, token string) (string, error) {
	c.mu.RLock()
	debug := c.config.DebugMode
	c.mu.RUnlock()

	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	email = strings.TrimSpace(email)
	token = strings.TrimSpace(token)

	if debug {
		log.Printf("[Jira DEBUG] === Starting Connection Verification for %s ===", baseURL)
		log.Printf("[Jira DEBUG] Email: %q (len=%d)", email, len(email))
		log.Printf("[Jira DEBUG] Token: %q (len=%d)", maskForLog(token), len(token))
	}

	if baseURL == "" || token == "" {
		return "", fmt.Errorf("missing Jira Base URL or API Token")
	}

	endpoints := []string{
		baseURL + "/rest/api/3/myself",
		baseURL + "/rest/api/2/myself",
	}

	var lastErr error
	for _, endpoint := range endpoints {
		req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
		if err != nil {
			return "", err
		}

		auth := authHeaderFor(email, token)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		req.Header.Set("Accept", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("connection failed: %w", err)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			var user struct {
				DisplayName  string `json:"displayName"`
				EmailAddress string `json:"emailAddress"`
				Name         string `json:"name"`
			}
			if err := json.Unmarshal(body, &user); err == nil {
				name := user.DisplayName
				if name == "" {
					name = user.EmailAddress
				}
				if name == "" {
					name = user.Name
				}
				return name, nil
			}
			return email, nil
		}

		lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return "", lastErr
}

// FetchAssignedIssues fetches assigned tickets across all configured instances.
func (c *Client) FetchAssignedIssues(ctx context.Context) ([]Issue, error) {
	c.mu.RLock()
	cfg := c.config
	debug := cfg.DebugMode
	c.mu.RUnlock()

	if cfg.DemoMode {
		return GetMockIssues(cfg.PinnedKeys), nil
	}

	cfg.EnsureInstances()

	var allIssues []Issue
	var lastErr error

	for _, inst := range cfg.Instances {
		if inst.BaseURL == "" || inst.APIToken == "" {
			if debug {
				log.Printf("[Jira DEBUG] Skipping instance %q (baseURL=%q, tokenLen=%d)", inst.Name, inst.BaseURL, len(inst.APIToken))
			}
			continue
		}

		if debug {
			log.Printf("[Jira DEBUG] Fetching issues for instance %q (%s)...", inst.Name, inst.BaseURL)
		}

		issues, err := c.fetchIssuesForInstance(ctx, inst)
		if err != nil {
			log.Printf("[Jira WARN] Failed to fetch issues for %s (%s): %v", inst.Name, inst.BaseURL, err)
			lastErr = err
			continue
		}

		if debug {
			log.Printf("[Jira DEBUG] Retrieved %d issues for %q", len(issues), inst.Name)
		}
		allIssues = append(allIssues, issues...)
	}

	if len(allIssues) == 0 && lastErr != nil {
		return nil, lastErr
	}

	return allIssues, nil
}

func (c *Client) fetchIssuesForInstance(ctx context.Context, inst InstanceConfig) ([]Issue, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(inst.BaseURL), "/")
	email := strings.TrimSpace(inst.Email)
	token := strings.TrimSpace(inst.APIToken)
	jql := strings.TrimSpace(inst.JQLQuery)
	if jql == "" {
		jql = "assignee = currentUser() AND resolution = Unresolved ORDER BY updated DESC"
	}

	reqBody := map[string]interface{}{
		"jql":        jql,
		"maxResults": 50,
		"fields":     []string{"summary", "status", "priority", "issuetype", "updated"},
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	// Try standard search endpoints in order: REST API v3, REST API v2, REST API v3 JQL endpoint
	endpoints := []string{
		baseURL + "/rest/api/3/search",
		baseURL + "/rest/api/2/search",
		baseURL + "/rest/api/3/search/jql",
	}

	auth := authHeaderFor(email, token)

	var lastErr error
	for _, endpoint := range endpoints {
		req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(bodyBytes))
		if err != nil {
			lastErr = err
			continue
		}

		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			return parseIssuesResponse(body, inst.ID, inst.Name, baseURL)
		}

		lastErr = fmt.Errorf("HTTP %d on %s: %s", resp.StatusCode, endpoint, string(body))
	}

	return nil, lastErr
}

func parseIssuesResponse(data []byte, instID, instName, baseURL string) ([]Issue, error) {
	var searchResp struct {
		Issues []struct {
			ID     string `json:"id"`
			Key    string `json:"key"`
			Fields struct {
				Summary string `json:"summary"`
				Status  struct {
					ID             string `json:"id"`
					Name           string `json:"name"`
					StatusCategory struct {
						Key   string `json:"key"`
						Color string `json:"colorName"`
					} `json:"statusCategory"`
				} `json:"status"`
				Priority struct {
					ID      string `json:"id"`
					Name    string `json:"name"`
					IconURL string `json:"iconUrl"`
				} `json:"priority"`
				IssueType struct {
					ID      string `json:"id"`
					Name    string `json:"name"`
					Subtask bool   `json:"subtask"`
				} `json:"issuetype"`
				Updated string `json:"updated"`
			} `json:"fields"`
		} `json:"issues"`
	}

	if err := json.Unmarshal(data, &searchResp); err != nil {
		return nil, err
	}

	var issues []Issue
	for _, raw := range searchResp.Issues {
		updated, _ := time.Parse(time.RFC3339, raw.Fields.Updated)
		issues = append(issues, Issue{
			InstanceID:   instID,
			InstanceName: instName,
			BaseURL:      baseURL,
			ID:           raw.ID,
			Key:          raw.Key,
			Summary:      raw.Fields.Summary,
			Status: Status{
				ID:            raw.Fields.Status.ID,
				Name:          raw.Fields.Status.Name,
				CategoryKey:   raw.Fields.Status.StatusCategory.Key,
				CategoryColor: raw.Fields.Status.StatusCategory.Color,
			},
			Priority: Priority{
				ID:      raw.Fields.Priority.ID,
				Name:    raw.Fields.Priority.Name,
				IconURL: raw.Fields.Priority.IconURL,
			},
			IssueType: IssueType{
				ID:      raw.Fields.IssueType.ID,
				Name:    raw.Fields.IssueType.Name,
				Subtask: raw.Fields.IssueType.Subtask,
			},
			Updated: updated,
			URL:     fmt.Sprintf("%s/browse/%s", baseURL, raw.Key),
		})
	}
	return issues, nil
}

// FetchTransitions returns available workflow transitions for an issue.
func (c *Client) FetchTransitions(ctx context.Context, issueKey string) ([]Transition, error) {
	c.mu.RLock()
	cfg := c.config
	c.mu.RUnlock()

	if cfg.DemoMode {
		return GetMockTransitions(issueKey), nil
	}

	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	url := fmt.Sprintf("%s/rest/api/3/issue/%s/transitions", baseURL, issueKey)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	auth := c.authHeader()
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var transResp struct {
		Transitions []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			To   struct {
				ID             string `json:"id"`
				Name           string `json:"name"`
				StatusCategory struct {
					Key string `json:"key"`
				} `json:"statusCategory"`
			} `json:"to"`
		} `json:"transitions"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&transResp); err != nil {
		return nil, err
	}

	var transitions []Transition
	for _, t := range transResp.Transitions {
		transitions = append(transitions, Transition{
			ID:   t.ID,
			Name: t.Name,
			To: Status{
				ID:          t.To.ID,
				Name:        t.To.Name,
				CategoryKey: t.To.StatusCategory.Key,
			},
		})
	}
	return transitions, nil
}

// DoTransition executes a workflow transition.
func (c *Client) DoTransition(ctx context.Context, issueKey string, transitionID string) error {
	c.mu.RLock()
	cfg := c.config
	c.mu.RUnlock()

	if cfg.DemoMode {
		return nil
	}

	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	url := fmt.Sprintf("%s/rest/api/3/issue/%s/transitions", baseURL, issueKey)

	payload := map[string]interface{}{
		"transition": map[string]string{
			"id": transitionID,
		},
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}

	auth := c.authHeader()
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("transition failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// ExecuteTransition is an alias for DoTransition supporting mock and live.
func (c *Client) ExecuteTransition(ctx context.Context, issueKey string, transitionID string) error {
	c.mu.RLock()
	cfg := c.config
	c.mu.RUnlock()

	if cfg.DemoMode {
		return ExecuteMockTransition(issueKey, transitionID)
	}
	return c.DoTransition(ctx, issueKey, transitionID)
}

// FormatBranchName generates a sanitized git branch name from ticket key and summary.
func FormatBranchName(prefix, key, summary string) string {
	if prefix == "" {
		prefix = "feature/"
	}
	cleanSummary := strings.ToLower(summary)
	cleanSummary = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		if r == ' ' || r == '-' || r == '_' {
			return '-'
		}
		return -1
	}, cleanSummary)

	for strings.Contains(cleanSummary, "--") {
		cleanSummary = strings.ReplaceAll(cleanSummary, "--", "-")
	}
	cleanSummary = strings.Trim(cleanSummary, "-")

	return fmt.Sprintf("%s%s-%s", prefix, key, cleanSummary)
}
