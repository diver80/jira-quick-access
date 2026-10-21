package jira

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func jiraStamp(t time.Time) string { return t.Format(jiraTimeLayout) }

func TestDeltaJQLStripsOrderByAndUsesRelativeWindow(t *testing.T) {
	got := deltaJQL("project = UATHCM ORDER BY cf[10019] ASC", 10*time.Minute)
	want := `(project = UATHCM) AND updated >= "-11m" ORDER BY updated ASC`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := deltaJQL("order by created", time.Minute); got != `updated >= "-2m" ORDER BY updated ASC` {
		t.Fatalf("filter without condition: got %q", got)
	}
	// OR must stay inside the parentheses so the time window applies to every branch.
	if got := deltaJQL("assignee = a OR reporter = b", time.Minute); !strings.HasPrefix(got, "(assignee = a OR reporter = b) AND") {
		t.Fatalf("filter must be parenthesised, got %q", got)
	}
}

func TestCommentTextFromADFAndMentions(t *testing.T) {
	body := json.RawMessage(`{"type":"doc","content":[
		{"type":"paragraph","content":[
			{"type":"mention","attrs":{"id":"acc-me","text":"@Frank Hess"}},
			{"type":"text","text":" bitte das Mapping prüfen"},
			{"type":"hardBreak"},
			{"type":"text","text":"danke"}]},
		{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"Punkt eins"}]}]}]}]}`)
	text, mentions := commentText(body)
	if text != "@Frank Hess bitte das Mapping prüfen danke Punkt eins" {
		t.Fatalf("unexpected text %q", text)
	}
	if len(mentions) != 1 || mentions[0].AccountID != "acc-me" {
		t.Fatalf("unexpected mentions %+v", mentions)
	}
}

func TestCommentTextFromWikiMarkup(t *testing.T) {
	text, mentions := commentText(json.RawMessage(`"h2. Ergebnis\n*Wichtig*: siehe [Doku|https://x.test/doc] und !screen.png! {code}x := 1{code}\n[~fhess] snake_case bleibt"`))
	if text != "Ergebnis Wichtig: siehe Doku und [image] x := 1 @fhess snake_case bleibt" {
		t.Fatalf("unexpected text %q", text)
	}
	if len(mentions) != 1 || mentions[0].Name != "fhess" {
		t.Fatalf("unexpected mentions %+v", mentions)
	}
}

func TestExcerptCutsAtWordBoundary(t *testing.T) {
	if got := excerpt("eins zwei drei vier", 12); got != "eins zwei…" {
		t.Fatalf("got %q", got)
	}
	if got := excerpt("kurz", 12); got != "kurz" {
		t.Fatalf("got %q", got)
	}
}

func TestHistoryEntriesSkipNoiseAndShortenLongText(t *testing.T) {
	var h changelogHistory
	_ = json.Unmarshal([]byte(`{"id":"500","author":{"accountId":"acc-me","displayName":"Frank"},"created":"2026-10-20T09:00:00.000+0200","items":[
		{"field":"status","fromString":"To Do","toString":"In Progress"},
		{"field":"Rank","fromString":"","toString":"Ranked higher"},
		{"field":"description","fromString":"old","toString":"new"},
		{"field":"assignee","fromString":"","toString":"Maria"}]}`), &h)
	entries := historyEntries(h, ChangeEntry{IssueKey: "A-1"}, userIdentity{AccountID: "acc-me"})
	if len(entries) != 3 {
		t.Fatalf("expected status, description and assignee, got %+v", entries)
	}
	if e := entries[0]; e.Kind != ChangeStatus || e.From != "To Do" || e.To != "In Progress" || !e.Own || e.ID != "h:500:0" {
		t.Fatalf("unexpected status entry %+v", e)
	}
	if e := entries[1]; e.Field != "description" || e.From != "" || e.To != "" {
		t.Fatalf("description must be shown as edited only, got %+v", e)
	}
}

func TestCommentEntryNewEditedAndOld(t *testing.T) {
	since := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	me := userIdentity{AccountID: "acc-me"}
	mk := func(created, updated time.Time) jiraComment {
		return jiraComment{ID: "77", Author: jiraUser{DisplayName: "Maria"}, Body: json.RawMessage(`"Hallo [~accountid:acc-me]"`), Created: jiraStamp(created), Updated: jiraStamp(updated)}
	}
	e, ok := commentEntry(mk(since.Add(time.Minute), since.Add(time.Minute)), ChangeEntry{}, me, since)
	if !ok || e.Kind != ChangeComment || e.ID != "c:77" || !e.Mentioned || e.Own {
		t.Fatalf("new comment: %+v %v", e, ok)
	}
	e, ok = commentEntry(mk(since.Add(-time.Hour), since.Add(time.Minute)), ChangeEntry{}, me, since)
	if !ok || e.Kind != ChangeCommentEdited || !strings.HasPrefix(e.ID, "c:77:") {
		t.Fatalf("edited comment: %+v %v", e, ok)
	}
	if _, ok := commentEntry(mk(since.Add(-time.Hour), since.Add(-time.Hour)), ChangeEntry{}, me, since); ok {
		t.Fatal("old comment must not produce an entry")
	}
	if _, ok := commentEntry(mk(since.Add(-30*time.Second), since.Add(time.Second)), ChangeEntry{}, me, since); ok {
		t.Fatal("an update right after creation is not an edit")
	}
}

// fakeJira serves myself, a search with changelog and comments. cloud selects the
// /search/jql API (string expand) instead of /rest/api/2/search (array expand).
func fakeJira(t *testing.T, cloud bool, now time.Time, commentHits *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/myself"):
			_, _ = io.WriteString(w, `{"accountId":"acc-me","displayName":"Frank"}`)
		case r.URL.Path == "/rest/api/2/search" && cloud, r.URL.Path == "/rest/api/3/search/jql" && !cloud:
			w.WriteHeader(http.StatusNotFound)
		case strings.Contains(r.URL.Path, "/search"):
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			_, isString := req["expand"].(string)
			if isString != cloud {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"errorMessages":["wrong expand type"]}`)
				return
			}
			if jql, _ := req["jql"].(string); !strings.Contains(jql, `updated >= "-`) {
				t.Errorf("search must be limited to recent updates: %q", jql)
			}
			_, _ = io.WriteString(w, `{"isLast":true,"total":1,"issues":[{"key":"UAT-163","fields":{"summary":"Mapping","updated":"`+jiraStamp(now.Add(-time.Minute))+`"},
				"changelog":{"histories":[
					{"id":"1","author":{"displayName":"Maria"},"created":"`+jiraStamp(now.Add(-3*time.Minute))+`","items":[{"field":"status","fromString":"Open","toString":"In Arbeit"}]},
					{"id":"0","author":{"displayName":"Maria"},"created":"`+jiraStamp(now.Add(-30*24*time.Hour))+`","items":[{"field":"status","fromString":"New","toString":"Open"}]}]}}]}`)
		case strings.HasSuffix(r.URL.Path, "/comment"):
			commentHits.Add(1)
			if r.URL.Query().Get("orderBy") != "-created" {
				t.Errorf("comments must be requested newest first: %s", r.URL.RawQuery)
			}
			_, _ = io.WriteString(w, `{"startAt":0,"total":2,"comments":[
				{"id":"9","author":{"accountId":"other","displayName":"Maria"},"body":{"type":"doc","content":[{"type":"paragraph","content":[{"type":"mention","attrs":{"id":"acc-me","text":"@Frank"}},{"type":"text","text":" schau mal"}]}]},"created":"`+jiraStamp(now.Add(-2*time.Minute))+`","updated":"`+jiraStamp(now.Add(-2*time.Minute))+`"},
				{"id":"8","author":{"accountId":"acc-me"},"body":"alt","created":"`+jiraStamp(now.Add(-20*24*time.Hour))+`","updated":"`+jiraStamp(now.Add(-20*24*time.Hour))+`"}]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestFetchInstanceChangesCloudAndDataCenter(t *testing.T) {
	for _, cloud := range []bool{true, false} {
		now := time.Now()
		var hits atomic.Int32
		ts := fakeJira(t, cloud, now, &hits)
		c := NewClient(Config{})
		inst := InstanceConfig{ID: "i1", BaseURL: ts.URL, Email: "a@b.c", APIToken: "tok", JQLQuery: "project = UAT ORDER BY rank"}

		since := now.Add(-10 * time.Minute)
		res, err := c.fetchInstanceChanges(context.Background(), inst, since, now)
		ts.Close()
		if err != nil {
			t.Fatalf("cloud=%v: %v", cloud, err)
		}
		if res.Backfill || !res.Watermark.Equal(now) {
			t.Fatalf("cloud=%v: unexpected watermark/backfill %+v", cloud, res)
		}
		if len(res.Entries) != 2 {
			t.Fatalf("cloud=%v: expected the status change and the new comment, got %+v", cloud, res.Entries)
		}
		var sawStatus, sawComment bool
		for _, e := range res.Entries {
			switch e.Kind {
			case ChangeStatus:
				sawStatus = e.To == "In Arbeit" && e.IssueKey == "UAT-163" && e.InstanceID == "i1"
			case ChangeComment:
				sawComment = e.Mentioned && !e.Own && e.Text == "@Frank schau mal" && e.URL() == ts.URL+"/browse/UAT-163?focusedCommentId=9"
			}
		}
		if !sawStatus || !sawComment {
			t.Fatalf("cloud=%v: wrong entries %+v", cloud, res.Entries)
		}
		if hits.Load() != 1 {
			t.Fatalf("cloud=%v: expected one comment request, got %d", cloud, hits.Load())
		}
	}
}

func TestFetchInstanceChangesBackfillsOnFirstCheck(t *testing.T) {
	now := time.Now()
	var hits atomic.Int32
	ts := fakeJira(t, false, now, &hits)
	defer ts.Close()
	c := NewClient(Config{})
	res, err := c.fetchInstanceChanges(context.Background(), InstanceConfig{ID: "i1", BaseURL: ts.URL, APIToken: "tok"}, time.Time{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Backfill {
		t.Fatal("first check must be a backfill")
	}
	// The 30-day-old history and the 20-day-old comment are outside the 7-day backfill.
	if len(res.Entries) != 2 {
		t.Fatalf("expected two recent entries, got %+v", res.Entries)
	}
}

func TestResumeAtNeverStalls(t *testing.T) {
	since := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	now := since.Add(5 * time.Minute)
	if got := resumeAt(now, since, deltaIssue{Updated: since.Add(time.Minute)}); !got.Equal(since.Add(59 * time.Second)) {
		t.Fatalf("watermark should move back to the skipped ticket, got %v", got)
	}
	// Moving back to (or before) since would repeat the same check forever.
	if got := resumeAt(now, since, deltaIssue{Updated: since.Add(time.Second)}); !got.Equal(now) {
		t.Fatalf("watermark must keep progressing, got %v", got)
	}
}

func TestChangeLogMergeDedupesPrunesAndCountsUnread(t *testing.T) {
	path := filepath.Join(t.TempDir(), "changelog.json")
	l := OpenChangeLog(path)
	now := time.Now()
	e := func(id string, ago time.Duration, own bool) ChangeEntry {
		return ChangeEntry{ID: id, IssueKey: "A-1", Kind: ChangeComment, At: now.Add(-ago), Own: own}
	}

	added := l.Merge("i1", []ChangeEntry{e("1", time.Hour, false), e("old", 40*24*time.Hour, false)}, now, true, now)
	if len(added) != 1 {
		t.Fatalf("entries beyond the retention must be dropped, got %+v", added)
	}
	if l.Unread(false) != 0 {
		t.Fatal("a backfill counts as read")
	}
	added = l.Merge("i1", []ChangeEntry{e("1", time.Hour, false), e("2", -time.Second, false), e("3", -2*time.Second, true)}, now.Add(time.Minute), false, now)
	if len(added) != 2 || added[0].ID != "3" {
		t.Fatalf("expected the two new entries newest first, got %+v", added)
	}
	if got := l.Unread(false); got != 2 {
		t.Fatalf("expected 2 unread, got %d", got)
	}
	if got := l.Unread(true); got != 1 {
		t.Fatalf("own changes must not count when hidden, got %d", got)
	}
	if err := l.Save(); err != nil {
		t.Fatal(err)
	}

	reopened := OpenChangeLog(path)
	if got := reopened.Entries(); len(got) != 3 || got[0].ID != "3" || got[0].InstanceID != "i1" {
		t.Fatalf("entries must survive a restart newest first, got %+v", got)
	}
	if w := reopened.Watermarks()["i1"]; !w.Equal(now.Add(time.Minute)) {
		t.Fatalf("watermark lost: %v", w)
	}
	reopened.MarkRead(now.Add(time.Hour), "i1")
	if reopened.Unread(false) != 0 {
		t.Fatal("mark read must clear unread")
	}
	reopened.Retain([]string{"other"})
	if len(reopened.Entries()) != 0 {
		t.Fatal("entries of removed instances must be dropped")
	}
}

func TestFetchAllChangesDemoMode(t *testing.T) {
	c := NewClient(Config{DemoMode: true, Instances: []InstanceConfig{{ID: "demo"}}})
	res := c.FetchAllChanges(context.Background(), nil)
	ch := res.Instances["demo"]
	if len(ch.Entries) == 0 || !ch.Backfill {
		t.Fatalf("demo mode should return the mock changes as a backfill, got %+v", res)
	}
}
