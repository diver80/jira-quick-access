package status

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"time"
)

// DefaultServiceEndpoints defines the default Atlassian public Statuspage URLs.
var DefaultServiceEndpoints = map[string]string{
	"Jira Software":           "https://jira-software.status.atlassian.com",
	"Jira Service Management": "https://jira-service-management.status.atlassian.com",
	"Confluence":              "https://confluence.status.atlassian.com",
	"Bitbucket":               "https://status.bitbucket.org",
	"Atlassian Migrations":    "https://migrations.status.atlassian.com",
	"Atlassian Analytics":     "https://analytics.status.atlassian.com",
	"Rovo":                    "https://rovo.status.atlassian.com",
	"Rovo Dev":                "https://rovodev.status.atlassian.com",
}

// Client interacts with Atlassian Statuspage REST endpoints.
type Client struct {
	httpClient   *http.Client
	baseURL      string
	serviceURLs  map[string]string
	mu           sync.RWMutex
	cachedReport StatusReport
}

// NewClient returns a production client configured for https://status.atlassian.com.
func NewClient() *Client {
	return NewClientWithCustomEndpoints("https://status.atlassian.com", DefaultServiceEndpoints)
}

// NewClientWithURL creates a client pointing to a custom base URL.
func NewClientWithURL(baseURL string) *Client {
	return NewClientWithCustomEndpoints(baseURL, nil)
}

// NewClientWithCustomEndpoints creates a client with customized endpoints for testing.
func NewClientWithCustomEndpoints(baseURL string, serviceURLs map[string]string) *Client {
	c := &Client{
		httpClient: &http.Client{
			Timeout: 8 * time.Second,
		},
		baseURL:     baseURL,
		serviceURLs: make(map[string]string),
	}
	for k, v := range serviceURLs {
		c.serviceURLs[k] = v
	}
	c.cachedReport = StatusReport{
		OverallIndicator: IndicatorNone,
		OverallText:      "All Systems Operational",
		LastChecked:      time.Time{},
	}
	return c
}

type statuspageSummaryResponse struct {
	Page struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		URL  string `json:"url"`
	} `json:"page"`
	Status struct {
		Indicator   string `json:"indicator"`
		Description string `json:"description"`
	} `json:"status"`
	Incidents []struct {
		ID              string `json:"id"`
		Name            string `json:"name"`
		Status          string `json:"status"`
		Impact          string `json:"impact"`
		Shortlink       string `json:"shortlink"`
		UpdatedAt       string `json:"updated_at"`
		IncidentUpdates []struct {
			Body string `json:"body"`
		} `json:"incident_updates"`
	} `json:"incidents"`
	Components []struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Status      string `json:"status"`
		Description string `json:"description"`
	} `json:"components"`
}

// FetchReport performs a fresh fetch of global and sub-service status.
// Preserves public API; delegates to FetchReportContext with background context.
func (c *Client) FetchReport() (StatusReport, error) {
	return c.FetchReportContext(context.Background())
}

// FetchReportContext performs a fresh fetch of global and sub-service status with context support.
// If context is cancelled, returns cached report with error wrapping ctx.Err().
// Ensures cancellation cannot publish partial report as success.
func (c *Client) FetchReportContext(ctx context.Context) (StatusReport, error) {
	url := fmt.Sprintf("%s/api/v2/summary.json", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		if ctx.Err() != nil {
			return c.fallbackWithError(fmt.Errorf("context error on status request: %w", ctx.Err()))
		}
		return c.fallbackWithError(fmt.Errorf("failed to create status request: %w", err))
	}
	req.Header.Set("User-Agent", "JiraQuickAccess-HealthMonitor/1.1")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return c.fallbackWithError(fmt.Errorf("context error on status HTTP request: %w", ctx.Err()))
		}
		return c.fallbackWithError(fmt.Errorf("status HTTP request failed: %w", err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.fallbackWithError(fmt.Errorf("status API returned HTTP %d", resp.StatusCode))
	}

	var parsed statuspageSummaryResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return c.fallbackWithError(fmt.Errorf("failed to decode status json: %w", err))
	}

	report := StatusReport{
		OverallIndicator: Indicator(parsed.Status.Indicator),
		OverallText:      parsed.Status.Description,
		LastChecked:      time.Now(),
	}
	if report.OverallIndicator == "" {
		report.OverallIndicator = IndicatorNone
	}
	if report.OverallText == "" {
		report.OverallText = "All Systems Operational"
	}

	// Parse active incidents
	for _, inc := range parsed.Incidents {
		body := ""
		if len(inc.IncidentUpdates) > 0 {
			body = inc.IncidentUpdates[0].Body
		}
		updated, _ := time.Parse(time.RFC3339, inc.UpdatedAt)
		report.ActiveIncidents = append(report.ActiveIncidents, Incident{
			ID:        inc.ID,
			Name:      inc.Name,
			Status:    inc.Status,
			Impact:    inc.Impact,
			Body:      body,
			UpdatedAt: updated,
			URL:       inc.Shortlink,
		})
	}

	// Fetch sub-services (if configured)
	if len(c.serviceURLs) > 0 {
		type svcResult struct {
			svc ServiceStatus
			err error
		}
		ch := make(chan svcResult, len(c.serviceURLs))

		for name, sURL := range c.serviceURLs {
			go func(serviceName, targetURL string) {
				sReq, sErr := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/api/v2/summary.json", targetURL), nil)
				if sErr != nil {
					ch <- svcResult{err: sErr}
					return
				}
				sReq.Header.Set("User-Agent", "JiraQuickAccess-HealthMonitor/1.1")
				sResp, doErr := c.httpClient.Do(sReq)
				if doErr != nil {
					ch <- svcResult{err: doErr}
					return
				}
				defer sResp.Body.Close()

				if sResp.StatusCode != http.StatusOK {
					ch <- svcResult{err: fmt.Errorf("HTTP %d", sResp.StatusCode)}
					return
				}

				var subParsed statuspageSummaryResponse
				if dErr := json.NewDecoder(sResp.Body).Decode(&subParsed); dErr != nil {
					ch <- svcResult{err: dErr}
					return
				}

				var comps []Component
				for _, comp := range subParsed.Components {
					comps = append(comps, Component{
						ID:          comp.ID,
						Name:        comp.Name,
						Status:      comp.Status,
						Description: comp.Description,
					})
				}

				svcInd := Indicator(subParsed.Status.Indicator)
				if svcInd == "" {
					svcInd = IndicatorNone
				}

				ch <- svcResult{
					svc: ServiceStatus{
						Name:        serviceName,
						PageID:      subParsed.Page.ID,
						Indicator:   svcInd,
						Description: subParsed.Status.Description,
						Components:  comps,
					},
				}
			}(name, sURL)
		}

		for i := 0; i < len(c.serviceURLs); i++ {
			select {
			case <-ctx.Done():
				return c.fallbackWithError(fmt.Errorf("status services canceled: %w", ctx.Err()))
			case res := <-ch:
				if res.err == nil && res.svc.Name != "" {
					report.Services = append(report.Services, res.svc)
				}
			}
		}
	} else if len(parsed.Components) > 0 {
		// Fallback when only base URL is queried: extract components as service entries
		for _, comp := range parsed.Components {
			ind := IndicatorNone
			if comp.Status != "operational" {
				ind = IndicatorMinor
			}
			report.Services = append(report.Services, ServiceStatus{
				Name:        comp.Name,
				PageID:      comp.ID,
				Indicator:   ind,
				Description: comp.Status,
			})
		}
	}

	// Check if context was cancelled before committing to cache
	if ctx.Err() != nil {
		return c.fallbackWithError(fmt.Errorf("context cancelled before caching report: %w", ctx.Err()))
	}

	c.mu.Lock()
	c.cachedReport = report
	c.mu.Unlock()

	// Clone report at API boundary for independence from internal mutable slices
	return c.cloneReport(report), nil
}

// GetCachedReport returns a clone of the last successfully queried status report.
// Data is cloned at API boundary to ensure independence from internal mutable slices.
func (c *Client) GetCachedReport() StatusReport {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cloneReport(c.cachedReport)
}

func (c *Client) fallbackWithError(err error) (StatusReport, error) {
	c.mu.RLock()
	rep := c.cloneReport(c.cachedReport)
	c.mu.RUnlock()

	rep.Error = err.Error()
	return rep, err
}

// cloneReport isolates all mutable slices at the client API boundary.
func (c *Client) cloneReport(report StatusReport) StatusReport {
	report.ActiveIncidents = slices.Clone(report.ActiveIncidents)
	report.Services = slices.Clone(report.Services)
	for i := range report.Services {
		report.Services[i].Components = slices.Clone(report.Services[i].Components)
	}
	return report
}
