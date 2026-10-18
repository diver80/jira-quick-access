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
	"net/url"
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
	cfg = cfg.Clone()
	cfg.EnsureInstances()
	// Clone the default transport so proxy settings (HTTPS_PROXY) and dial/TLS timeouts apply.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 20
	transport.MaxIdleConnsPerHost = 10
	return &Client{
		config: cfg,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   15 * time.Second,
		},
	}
}

// maxResponseBytes caps how much of a Jira response body is read into memory.
const maxResponseBytes = 8 << 20

// isMissingEndpoint reports whether a status means the API variant does not exist on
// this deployment (e.g. REST v3 on Data Center, removed search on Cloud), so the next
// variant may be tried. Any other status is the server's real answer.
func isMissingEndpoint(status int) bool {
	return status == http.StatusNotFound || status == http.StatusMethodNotAllowed || status == http.StatusGone
}

// errorDetail extracts Jira's errorMessages from a response body, falling back to a
// truncated raw body so proxy/SSO HTML pages don't flood logs and toasts.
func errorDetail(body []byte) string {
	var jiraErr struct {
		ErrorMessages []string          `json:"errorMessages"`
		Errors        map[string]string `json:"errors"`
	}
	if json.Unmarshal(body, &jiraErr) == nil {
		msgs := append([]string{}, jiraErr.ErrorMessages...)
		for field, msg := range jiraErr.Errors {
			msgs = append(msgs, field+": "+msg)
		}
		if len(msgs) > 0 {
			return strings.Join(msgs, "; ")
		}
	}
	detail := strings.TrimSpace(string(body))
	if len(detail) > 200 {
		detail = detail[:200] + "…"
	}
	return detail
}

// doFirstAvailable sends the request to each endpoint in order and returns the first
// response that is not a missing-endpoint status. Transport errors are returned
// immediately: the request may already have reached Jira, so it is never replayed.
func (c *Client) doFirstAvailable(ctx context.Context, method string, endpoints []string, auth string, payload []byte) (status int, body []byte, endpoint string, err error) {
	for _, endpoint = range endpoints {
		var reqBody io.Reader
		if payload != nil {
			reqBody = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, endpoint, reqBody)
		if err != nil {
			return 0, nil, endpoint, err
		}
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Accept", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return 0, nil, endpoint, err
		}
		body, err = io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		_ = resp.Body.Close()
		if err != nil {
			return 0, nil, endpoint, err
		}
		if !isMissingEndpoint(resp.StatusCode) {
			return resp.StatusCode, body, endpoint, nil
		}
		status = resp.StatusCode
	}
	return status, body, endpoint, nil
}

// UpdateConfig stores an independent snapshot of the client configuration.
func (c *Client) UpdateConfig(cfg Config) {
	cfg = cfg.Clone()
	cfg.EnsureInstances()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config = cfg
}

// GetConfig returns a configuration snapshot that callers can edit independently.
func (c *Client) GetConfig() Config {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.config.Clone()
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
	c.mu.RLock()
	defer c.mu.RUnlock()
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

	status, body, _, err := c.doFirstAvailable(ctx, http.MethodGet, endpoints, authHeaderFor(email, token), nil)
	if err != nil {
		return "", fmt.Errorf("connection failed: %w", err)
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("HTTP %d: %s", status, errorDetail(body))
	}

	var user struct {
		DisplayName  string `json:"displayName"`
		EmailAddress string `json:"emailAddress"`
		Name         string `json:"name"`
	}
	if err := json.Unmarshal(body, &user); err != nil {
		return email, nil
	}
	name := user.DisplayName
	if name == "" {
		name = user.EmailAddress
	}
	if name == "" {
		name = user.Name
	}
	return name, nil
}

// FetchAssignedIssues fetches assigned tickets across all configured instances concurrently.
func (c *Client) FetchAssignedIssues(ctx context.Context) ([]Issue, error) {
	res := c.FetchAllInstances(ctx)
	if len(res.Issues) == 0 && len(res.Failed) > 0 {
		return nil, res.FirstError()
	}
	return res.Issues, nil
}

// FetchResult is the outcome of one refresh across all configured instances.
type FetchResult struct {
	Issues []Issue
	// Loaded holds the IDs of instances whose tickets are complete in Issues.
	Loaded map[string]bool
	// Failed maps instance IDs to the error that kept their tickets out of Issues.
	Failed map[string]error
	// order lists failed instance IDs in configuration order for FirstError.
	order []string
}

// FirstError returns the error of the first failed instance in configuration order.
func (r FetchResult) FirstError() error {
	for _, id := range r.order {
		if err := r.Failed[id]; err != nil {
			return err
		}
	}
	return nil
}

// FetchAllInstances queries every configured instance concurrently. Tickets are
// returned in configuration order (and Jira's order within an instance), so the
// list does not reshuffle depending on which site answered first.
func (c *Client) FetchAllInstances(ctx context.Context) FetchResult {
	cfg := c.GetConfig()
	debug := cfg.DebugMode
	res := FetchResult{Loaded: map[string]bool{}, Failed: map[string]error{}}

	if cfg.DemoMode {
		res.Issues = GetMockIssues(cfg.PinnedKeys)
		for _, iss := range res.Issues {
			res.Loaded[iss.InstanceID] = true
		}
		return res
	}

	cfg.EnsureInstances()

	perInstance := make([][]Issue, len(cfg.Instances))
	errs := make([]error, len(cfg.Instances))
	var wg sync.WaitGroup
	for i, inst := range cfg.Instances {
		if inst.BaseURL == "" || inst.APIToken == "" {
			if debug {
				log.Printf("[Jira DEBUG] Skipping instance %q (baseURL=%q, tokenLen=%d)", inst.Name, inst.BaseURL, len(inst.APIToken))
			}
			continue
		}

		wg.Add(1)
		go func(i int, targetInst InstanceConfig) {
			defer wg.Done()
			if debug {
				log.Printf("[Jira DEBUG] Fetching issues for instance %q (%s)...", targetInst.Name, targetInst.BaseURL)
			}
			issues, err := c.fetchIssuesForInstance(ctx, targetInst)
			if err != nil {
				log.Printf("[Jira WARN] Failed to fetch issues for %s (%s): %v", targetInst.Name, targetInst.BaseURL, err)
				errs[i] = err
				return
			}
			if debug {
				log.Printf("[Jira DEBUG] Retrieved %d issues for %q", len(issues), targetInst.Name)
			}
			perInstance[i] = issues
		}(i, inst)
	}
	wg.Wait()

	for i, inst := range cfg.Instances {
		if errs[i] != nil {
			res.Failed[inst.ID] = errs[i]
			res.order = append(res.order, inst.ID)
			continue
		}
		if inst.BaseURL != "" && inst.APIToken != "" {
			res.Loaded[inst.ID] = true
		}
		res.Issues = append(res.Issues, perInstance[i]...)
	}
	return res
}

const (
	searchPageSize  = 100
	maxSearchIssues = 1000
)

// searchEndpoints orders the search API variants for a deployment. Jira Cloud removed
// /rest/api/{2,3}/search in favour of /rest/api/3/search/jql; Data Center only has
// /rest/api/2/search. Unknown hosts (custom Cloud domains) fall through via 404/410.
func searchEndpoints(baseURL string) []string {
	jqlEndpoint := baseURL + "/rest/api/3/search/jql"
	legacyEndpoint := baseURL + "/rest/api/2/search"
	if u, err := url.Parse(baseURL); err == nil {
		host := strings.ToLower(u.Hostname())
		if strings.HasSuffix(host, ".atlassian.net") || strings.HasSuffix(host, ".jira.com") {
			return []string{jqlEndpoint, legacyEndpoint}
		}
	}
	return []string{legacyEndpoint, jqlEndpoint}
}

func (c *Client) fetchIssuesForInstance(ctx context.Context, inst InstanceConfig) ([]Issue, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(inst.BaseURL), "/")
	email := strings.TrimSpace(inst.Email)
	token := strings.TrimSpace(inst.APIToken)
	jql := strings.TrimSpace(inst.JQLQuery)
	if jql == "" {
		jql = defaultJQL
	}
	auth := authHeaderFor(email, token)

	reqBody := map[string]interface{}{
		"jql":        jql,
		"maxResults": searchPageSize,
		"fields":     []string{"summary", "status", "priority", "issuetype", "updated"},
	}

	endpoints := searchEndpoints(baseURL)
	var issues []Issue
	for {
		bodyBytes, err := json.Marshal(reqBody)
		if err != nil {
			return nil, err
		}
		status, body, endpoint, err := c.doFirstAvailable(ctx, http.MethodPost, endpoints, auth, bodyBytes)
		if err != nil {
			return nil, err
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("HTTP %d on %s: %s", status, endpoint, errorDetail(body))
		}
		// Later pages must use the variant that answered, since pagination differs.
		endpoints = []string{endpoint}

		page, err := parseIssuesResponse(body, inst.ID, inst.Name, baseURL)
		if err != nil {
			return nil, err
		}
		issues = append(issues, page.Issues...)

		if len(issues) >= maxSearchIssues {
			log.Printf("[Jira WARN] %s: result capped at %d issues; narrow the JQL query", inst.Name, maxSearchIssues)
			return issues[:maxSearchIssues], nil
		}
		if len(page.Issues) == 0 {
			return issues, nil
		}
		if strings.HasSuffix(endpoint, "/search/jql") {
			if page.IsLast || page.NextPageToken == "" {
				return issues, nil
			}
			reqBody["nextPageToken"] = page.NextPageToken
		} else {
			next := page.StartAt + len(page.Issues)
			if next >= page.Total {
				return issues, nil
			}
			reqBody["startAt"] = next
		}
	}
}

// searchPage is one page of a search response with both pagination styles:
// token-based (/search/jql) and offset-based (/rest/api/2/search).
type searchPage struct {
	Issues        []Issue
	NextPageToken string
	IsLast        bool
	StartAt       int
	Total         int
}

// jiraTimeLayout is the timestamp format Jira returns (numeric zone without colon).
const jiraTimeLayout = "2006-01-02T15:04:05.000-0700"

func parseJiraTime(value string) time.Time {
	if t, err := time.Parse(jiraTimeLayout, value); err == nil {
		return t
	}
	t, _ := time.Parse(time.RFC3339, value)
	return t
}

func parseIssuesResponse(data []byte, instID, instName, baseURL string) (searchPage, error) {
	var searchResp struct {
		NextPageToken string `json:"nextPageToken"`
		IsLast        bool   `json:"isLast"`
		StartAt       int    `json:"startAt"`
		Total         int    `json:"total"`
		Issues        []struct {
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
		return searchPage{}, err
	}

	page := searchPage{
		NextPageToken: searchResp.NextPageToken,
		IsLast:        searchResp.IsLast,
		StartAt:       searchResp.StartAt,
		Total:         searchResp.Total,
	}
	for _, raw := range searchResp.Issues {
		page.Issues = append(page.Issues, Issue{
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
			Updated: parseJiraTime(raw.Fields.Updated),
			URL:     fmt.Sprintf("%s/browse/%s", baseURL, url.PathEscape(raw.Key)),
		})
	}
	return page, nil
}

func (c *Client) resolveInstanceForIssue(issue Issue) (baseURL, email, token string) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// 1. Try matching by InstanceID
	if issue.InstanceID != "" {
		for _, inst := range c.config.Instances {
			if inst.ID == issue.InstanceID {
				return inst.BaseURL, inst.Email, inst.APIToken
			}
		}
	}

	// 2. Try matching by BaseURL (exact or prefix match)
	if issue.BaseURL != "" {
		issueBase := strings.TrimRight(strings.TrimSpace(issue.BaseURL), "/")
		for _, inst := range c.config.Instances {
			instBase := strings.TrimRight(strings.TrimSpace(inst.BaseURL), "/")
			if instBase != "" && (strings.EqualFold(instBase, issueBase) || strings.HasPrefix(issueBase, instBase)) {
				return inst.BaseURL, inst.Email, inst.APIToken
			}
		}
	}

	// 3. Try matching by InstanceName
	if issue.InstanceName != "" {
		for _, inst := range c.config.Instances {
			if strings.EqualFold(inst.Name, issue.InstanceName) {
				return inst.BaseURL, inst.Email, inst.APIToken
			}
		}
	}

	// 4. Fallback to primary config if set, otherwise first instance
	if c.config.BaseURL != "" {
		return c.config.BaseURL, c.config.Email, c.config.APIToken
	}
	if len(c.config.Instances) > 0 {
		return c.config.Instances[0].BaseURL, c.config.Instances[0].Email, c.config.Instances[0].APIToken
	}

	return "", "", ""
}

// FetchTransitions returns available workflow transitions for an issue across REST v3 and v2.
func (c *Client) FetchTransitions(ctx context.Context, issue Issue) ([]Transition, error) {
	c.mu.RLock()
	demoMode := c.config.DemoMode
	c.mu.RUnlock()

	if demoMode {
		return GetMockTransitions(issue.Key), nil
	}

	baseURL, email, token := c.resolveInstanceForIssue(issue)
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("missing Jira Base URL for issue %s", issue.Key)
	}

	key := url.PathEscape(issue.Key)
	endpoints := []string{
		fmt.Sprintf("%s/rest/api/3/issue/%s/transitions", baseURL, key),
		fmt.Sprintf("%s/rest/api/2/issue/%s/transitions", baseURL, key),
	}

	status, body, endpoint, err := c.doFirstAvailable(ctx, http.MethodGet, endpoints, authHeaderFor(email, token), nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d on %s: %s", status, endpoint, errorDetail(body))
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
	if err := json.Unmarshal(body, &transResp); err != nil {
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

// DoTransition executes a workflow transition routing to the issue's parent instance with REST v3 & v2 fallback.
func (c *Client) DoTransition(ctx context.Context, issue Issue, transitionID string) error {
	c.mu.RLock()
	demoMode := c.config.DemoMode
	c.mu.RUnlock()

	if demoMode {
		return ExecuteMockTransition(issue.Key, transitionID)
	}

	baseURL, email, token := c.resolveInstanceForIssue(issue)
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return fmt.Errorf("missing Jira Base URL for issue %s", issue.Key)
	}

	key := url.PathEscape(issue.Key)
	endpoints := []string{
		fmt.Sprintf("%s/rest/api/3/issue/%s/transitions", baseURL, key),
		fmt.Sprintf("%s/rest/api/2/issue/%s/transitions", baseURL, key),
	}

	payload := map[string]interface{}{
		"transition": map[string]string{
			"id": transitionID,
		},
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	status, body, endpoint, err := c.doFirstAvailable(ctx, http.MethodPost, endpoints, authHeaderFor(email, token), bodyBytes)
	if err != nil {
		return fmt.Errorf("transition request failed: %w", err)
	}
	if status != http.StatusNoContent && status != http.StatusOK {
		return fmt.Errorf("transition failed (HTTP %d on %s): %s", status, endpoint, errorDetail(body))
	}
	return nil
}

// ExecuteTransition is an alias for DoTransition supporting mock and live workflow execution.
func (c *Client) ExecuteTransition(ctx context.Context, issueKey string, transitionID string) error {
	c.mu.RLock()
	cfg := c.config
	c.mu.RUnlock()

	if cfg.DemoMode {
		return ExecuteMockTransition(issueKey, transitionID)
	}
	return c.DoTransition(ctx, Issue{Key: issueKey}, transitionID)
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
