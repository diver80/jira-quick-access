package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	// changeBackfill is how far back the first changes check of an instance looks.
	changeBackfill = 7 * 24 * time.Hour
	// changeOverlap re-reads a short window before the last check to absorb clock
	// skew between this machine and Jira; duplicates are dropped by entry ID.
	changeOverlap = 2 * time.Minute
	// maxDeltaIssues caps one changes search; later polls catch up on the rest.
	maxDeltaIssues = 200
	// maxCommentFetches caps comment requests per instance and check; tickets left
	// over move the watermark back so the next check fetches them.
	maxCommentFetches = 20
	commentWorkers    = 4
	commentPageSize   = 50
)

// InstanceChanges is the outcome of one changes check for one instance.
type InstanceChanges struct {
	Entries []ChangeEntry
	// Watermark is the "since" value for the next check of this instance.
	Watermark time.Time
	// Backfill is set for the first check, whose entries are history rather than news.
	Backfill bool
}

// ChangesResult is the outcome of one changes check across all instances.
type ChangesResult struct {
	Instances map[string]InstanceChanges
	Failed    map[string]error
}

// FetchAllChanges collects changelog entries and comments of tickets in each
// instance's filter that changed since the given per-instance watermark. A missing
// or zero watermark starts with a backfill of the last changeBackfill.
func (c *Client) FetchAllChanges(ctx context.Context, since map[string]time.Time) ChangesResult {
	cfg := c.GetConfig()
	res := ChangesResult{Instances: map[string]InstanceChanges{}, Failed: map[string]error{}}
	if cfg.DemoMode {
		cfg.EnsureInstances()
		if cfg.Instances[0].ChangesDisabled {
			return res
		}
		id := cfg.Instances[0].ID
		res.Instances[id] = InstanceChanges{Entries: GetMockChanges(id), Watermark: time.Now(), Backfill: since[id].IsZero()}
		return res
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, inst := range cfg.Instances {
		if inst.BaseURL == "" || inst.APIToken == "" || inst.ChangesDisabled {
			continue
		}
		wg.Add(1)
		go func(inst InstanceConfig) {
			defer wg.Done()
			ch, err := c.fetchInstanceChanges(ctx, inst, since[inst.ID], time.Now())
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				log.Printf("[Jira WARN] Failed to fetch changes for %s: %v", inst.Name, err)
				res.Failed[inst.ID] = err
				return
			}
			res.Instances[inst.ID] = ch
		}(inst)
	}
	wg.Wait()
	return res
}

type deltaIssue struct {
	Key     string
	Summary string
	Updated time.Time
	History []changelogHistory
}

func (c *Client) fetchInstanceChanges(ctx context.Context, inst InstanceConfig, since, now time.Time) (InstanceChanges, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(inst.BaseURL), "/")
	auth := authHeaderFor(inst.Email, inst.APIToken)
	out := InstanceChanges{Watermark: now}
	if since.IsZero() || now.Sub(since) > changeBackfill {
		since = now.Add(-changeBackfill)
		out.Backfill = true
	}
	from := since.Add(-changeOverlap)

	me, err := c.identity(ctx, inst.ID, baseURL, auth)
	if err != nil {
		return out, err
	}
	jql := strings.TrimSpace(inst.JQLQuery)
	if jql == "" {
		jql = defaultJQL
	}
	issues, err := c.searchDelta(ctx, baseURL, auth, deltaJQL(jql, now.Sub(from)))
	if err != nil {
		return out, err
	}
	slices.SortStableFunc(issues, func(a, b deltaIssue) int { return a.Updated.Compare(b.Updated) })

	for _, iss := range issues {
		base := ChangeEntry{InstanceID: inst.ID, BaseURL: baseURL, IssueKey: iss.Key, Summary: iss.Summary}
		for _, h := range iss.History {
			if parseJiraTime(h.Created).After(from) {
				out.Entries = append(out.Entries, historyEntries(h, base, me)...)
			}
		}
	}

	// Comments need one request per ticket; fetch the oldest changes first so a
	// capped check can resume exactly where it stopped.
	todo := issues
	if len(todo) > maxCommentFetches {
		todo = issues[:maxCommentFetches]
		out.Watermark = resumeAt(out.Watermark, since, issues[maxCommentFetches])
	}
	comments := make([][]ChangeEntry, len(todo))
	errs := make([]error, len(todo))
	sem := make(chan struct{}, commentWorkers)
	var wg sync.WaitGroup
	for i, iss := range todo {
		wg.Add(1)
		go func(i int, iss deltaIssue) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			base := ChangeEntry{InstanceID: inst.ID, BaseURL: baseURL, IssueKey: iss.Key, Summary: iss.Summary}
			comments[i], errs[i] = c.fetchCommentEntries(ctx, baseURL, auth, base, me, from)
		}(i, iss)
	}
	wg.Wait()
	for i := range todo {
		if errs[i] != nil {
			// Retry this ticket on the next check instead of losing its comments.
			out.Watermark = resumeAt(out.Watermark, since, todo[i])
			log.Printf("[Jira WARN] %s: comments of %s: %v", inst.Name, todo[i].Key, errs[i])
			continue
		}
		out.Entries = append(out.Entries, comments[i]...)
	}
	return out, nil
}

// resumeAt moves the watermark back so the next check includes iss again. It only
// does so while that makes progress past since; otherwise (e.g. a bulk edit of many
// tickets in the same second, or a ticket whose comments always fail) the rest is
// skipped so the watermark cannot get stuck.
func resumeAt(watermark, since time.Time, iss deltaIssue) time.Time {
	w := iss.Updated.Add(-time.Second)
	if w.After(since) && w.Before(watermark) {
		return w
	}
	return watermark
}

// identity returns the API token's user for an instance, cached per credentials.
func (c *Client) identity(ctx context.Context, instID, baseURL, auth string) (userIdentity, error) {
	key := instID + "|" + baseURL + "|" + auth
	c.idMu.Lock()
	me, ok := c.identities[key]
	c.idMu.Unlock()
	if ok {
		return me, nil
	}
	status, body, endpoint, err := c.doFirstAvailable(ctx, http.MethodGet,
		[]string{baseURL + "/rest/api/3/myself", baseURL + "/rest/api/2/myself"}, auth, nil)
	if err != nil {
		return me, err
	}
	if status != http.StatusOK {
		return me, fmt.Errorf("HTTP %d on %s: %s", status, endpoint, errorDetail(body))
	}
	if err := json.Unmarshal(body, &me); err != nil {
		return me, err
	}
	c.idMu.Lock()
	if c.identities == nil {
		c.identities = map[string]userIdentity{}
	}
	c.identities[key] = me
	c.idMu.Unlock()
	return me, nil
}

// searchDelta runs a search that includes each ticket's changelog. The two search
// APIs disagree on the type of "expand", so the body is built per endpoint.
func (c *Client) searchDelta(ctx context.Context, baseURL, auth, jql string) ([]deltaIssue, error) {
	var issues []deltaIssue
	endpoints := searchEndpoints(baseURL)
	var nextToken string
	startAt := 0
	for {
		var status int
		var body []byte
		var endpoint string
		for _, ep := range endpoints {
			req := map[string]any{"jql": jql, "maxResults": searchPageSize, "fields": []string{"summary", "updated"}}
			if strings.HasSuffix(ep, "/search/jql") {
				req["expand"] = "changelog"
				if nextToken != "" {
					req["nextPageToken"] = nextToken
				}
			} else {
				req["expand"] = []string{"changelog"}
				req["startAt"] = startAt
			}
			payload, err := json.Marshal(req)
			if err != nil {
				return nil, err
			}
			status, body, endpoint, err = c.doFirstAvailable(ctx, http.MethodPost, []string{ep}, auth, payload)
			if err != nil {
				return nil, err
			}
			if !isMissingEndpoint(status) {
				break
			}
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("HTTP %d on %s: %s", status, endpoint, errorDetail(body))
		}
		endpoints = []string{endpoint}

		var resp struct {
			NextPageToken string `json:"nextPageToken"`
			IsLast        bool   `json:"isLast"`
			StartAt       int    `json:"startAt"`
			Total         int    `json:"total"`
			Issues        []struct {
				Key    string `json:"key"`
				Fields struct {
					Summary string `json:"summary"`
					Updated string `json:"updated"`
				} `json:"fields"`
				Changelog struct {
					Histories []changelogHistory `json:"histories"`
				} `json:"changelog"`
			} `json:"issues"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, err
		}
		for _, raw := range resp.Issues {
			issues = append(issues, deltaIssue{
				Key:     raw.Key,
				Summary: raw.Fields.Summary,
				Updated: parseJiraTime(raw.Fields.Updated),
				History: raw.Changelog.Histories,
			})
		}
		switch {
		case len(issues) >= maxDeltaIssues:
			return issues[:maxDeltaIssues], nil
		case len(resp.Issues) == 0:
			return issues, nil
		case strings.HasSuffix(endpoint, "/search/jql"):
			if resp.IsLast || resp.NextPageToken == "" {
				return issues, nil
			}
			nextToken = resp.NextPageToken
		default:
			startAt = resp.StartAt + len(resp.Issues)
			if startAt >= resp.Total {
				return issues, nil
			}
		}
	}
}

type commentPage struct {
	StartAt  int           `json:"startAt"`
	Total    int           `json:"total"`
	Comments []jiraComment `json:"comments"`
}

// fetchCommentEntries returns entries for comments of one ticket added or edited after since.
func (c *Client) fetchCommentEntries(ctx context.Context, baseURL, auth string, base ChangeEntry, me userIdentity, since time.Time) ([]ChangeEntry, error) {
	page, endpoint, err := c.fetchCommentPage(ctx, baseURL, auth, base.IssueKey, nil, 0)
	if err != nil {
		return nil, err
	}
	// Servers that ignore orderBy return the oldest comments first; read the last page.
	if n := len(page.Comments); n > 1 && page.Total > page.StartAt+n &&
		parseJiraTime(page.Comments[0].Created).Before(parseJiraTime(page.Comments[n-1].Created)) {
		page, _, err = c.fetchCommentPage(ctx, baseURL, auth, base.IssueKey, []string{endpoint}, max(0, page.Total-commentPageSize))
		if err != nil {
			return nil, err
		}
	}
	var out []ChangeEntry
	for _, cm := range page.Comments {
		if e, ok := commentEntry(cm, base, me, since); ok {
			out = append(out, e)
		}
	}
	return out, nil
}

func (c *Client) fetchCommentPage(ctx context.Context, baseURL, auth, key string, endpoints []string, startAt int) (commentPage, string, error) {
	if endpoints == nil {
		k := url.PathEscape(key)
		endpoints = []string{
			fmt.Sprintf("%s/rest/api/3/issue/%s/comment", baseURL, k),
			fmt.Sprintf("%s/rest/api/2/issue/%s/comment", baseURL, k),
		}
	}
	query := fmt.Sprintf("?orderBy=-created&maxResults=%d&startAt=%d", commentPageSize, startAt)
	withQuery := make([]string, len(endpoints))
	for i, ep := range endpoints {
		withQuery[i] = ep + query
	}
	status, body, endpoint, err := c.doFirstAvailable(ctx, http.MethodGet, withQuery, auth, nil)
	endpoint = strings.TrimSuffix(endpoint, query)
	if err != nil {
		return commentPage{}, endpoint, err
	}
	if status != http.StatusOK {
		return commentPage{}, endpoint, fmt.Errorf("HTTP %d on %s: %s", status, endpoint, errorDetail(body))
	}
	var page commentPage
	err = json.Unmarshal(body, &page)
	return page, endpoint, err
}
