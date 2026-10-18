// Package update checks GitHub for a newer release of Jira Quick Access.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// LatestReleaseURL is the GitHub API endpoint for the newest published release.
const LatestReleaseURL = "https://api.github.com/repos/diver80/jira-quick-access/releases/latest"

// Release describes a published version.
type Release struct {
	Version string // without the leading "v"
	URL     string // release page
}

// Checker queries the release endpoint. The zero value uses LatestReleaseURL.
type Checker struct {
	Endpoint string
	Client   *http.Client
}

// Latest returns the newest published release.
func (c Checker) Latest(ctx context.Context) (Release, error) {
	endpoint := c.Endpoint
	if endpoint == "" {
		endpoint = LatestReleaseURL
	}
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("release check: HTTP %d", resp.StatusCode)
	}
	var body struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return Release{}, fmt.Errorf("release check: %w", err)
	}
	if body.TagName == "" {
		return Release{}, fmt.Errorf("release check: no tag in response")
	}
	return Release{Version: strings.TrimPrefix(body.TagName, "v"), URL: body.HTMLURL}, nil
}

// IsNewer reports whether version a is newer than b. Both are dotted numeric
// versions; a pre-release or build suffix (e.g. "1.0.6-review") is ignored.
func IsNewer(a, b string) bool {
	pa, pb := parse(a), parse(b)
	if pa == nil || pb == nil {
		return false
	}
	for i := 0; i < max(len(pa), len(pb)); i++ {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			return x > y
		}
	}
	return false
}

func parse(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ".")
	nums := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil
		}
		nums[i] = n
	}
	return nums
}
