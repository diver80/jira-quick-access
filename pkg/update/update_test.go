package update

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"1.1.0", "1.0.8", true},
		{"v1.0.10", "1.0.9", true},
		{"1.0.8", "1.0.8", false},
		{"1.0.7", "1.0.8", false},
		{"1.1", "1.0.9", true},
		{"1.0.8", "1.0.8-review", false},
		{"garbage", "1.0.0", false},
		{"1.0.0", "unknown", false},
	}
	for _, c := range cases {
		if got := IsNewer(c.a, c.b); got != c.want {
			t.Errorf("IsNewer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestLatestParsesGitHubRelease(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"tag_name":"v1.2.3","html_url":"https://github.com/x/y/releases/tag/v1.2.3"}`)
	}))
	defer ts.Close()

	rel, err := Checker{Endpoint: ts.URL}.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rel.Version != "1.2.3" || rel.URL == "" {
		t.Fatalf("unexpected release %+v", rel)
	}
}

func TestLatestReportsHTTPErrors(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusForbidden)
	}))
	defer ts.Close()

	if _, err := (Checker{Endpoint: ts.URL}).Latest(context.Background()); err == nil {
		t.Fatal("expected an error for HTTP 403")
	}
}
