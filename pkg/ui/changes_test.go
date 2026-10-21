package ui

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"

	"jira-quick-access/pkg/jira"
	"jira-quick-access/pkg/window"
)

// changesView is a board view with its own changelog file and a few entries for
// instance "a" (unread) and one for instance "b".
func changesView(t *testing.T) *AppView {
	t.Helper()
	// Created first so it is removed after view.Close has waited for background saves.
	dir := t.TempDir()
	view := boardView(t)
	log := jira.OpenChangeLog(filepath.Join(dir, "changelog.json"))
	now := time.Now()
	log.Merge("a", nil, now.Add(-time.Hour), true, now.Add(-time.Hour))
	log.Merge("a", []jira.ChangeEntry{
		{ID: "c:1", IssueKey: "A-2", Summary: "A-2 summary", Kind: jira.ChangeComment, Text: "Bitte Mapping prüfen", Author: "Maria", Mentioned: true, CommentID: "1", BaseURL: "https://jira.test", At: now.Add(-5 * time.Minute)},
		{ID: "h:1:0", IssueKey: "A-1", Kind: jira.ChangeStatus, From: "To Do", To: "In Progress", Author: "Frank", Own: true, At: now.Add(-10 * time.Minute)},
	}, time.Time{}, false, now)
	log.Merge("b", []jira.ChangeEntry{{ID: "h:9:0", IssueKey: "B-1", Kind: jira.ChangeField, Field: "assignee", To: "Maria", At: now}}, time.Time{}, false, now)
	view.mu.Lock()
	view.changelog = log
	view.mu.Unlock()
	return view
}

func drawnTexts(v *AppView) map[string]bool {
	canvas := &testMockCanvas{}
	v.Draw(&testMockContext{}, canvas)
	texts := map[string]bool{}
	for _, call := range canvas.getDrawTexts() {
		texts[call.text] = true
	}
	return texts
}

func TestMembershipEntriesOnlyForComparableInstances(t *testing.T) {
	prev := []jira.Issue{iss("a", "A-1", "To Do"), iss("b", "B-1", "To Do")}
	next := []jira.Issue{iss("a", "A-2", "To Do"), iss("b", "B-2", "To Do")}
	entries := membershipEntries(prev, next, map[string]bool{"a": true}, time.Now())
	if len(entries) != 2 {
		t.Fatalf("expected A-2 added and A-1 removed, got %+v", entries)
	}
	if entries[0].Kind != jira.ChangeAdded || entries[0].IssueKey != "A-2" || entries[1].Kind != jira.ChangeRemoved || entries[1].IssueKey != "A-1" {
		t.Fatalf("unexpected entries %+v", entries)
	}
	if entries[1].From != "To Do" {
		t.Fatalf("a removed ticket keeps its last status, got %q", entries[1].From)
	}
}

func TestCmdLOpensChangesForInstanceAndMarksRead(t *testing.T) {
	view := changesView(t)
	view.SetState(window.StateFan)
	if s := view.snapshot(); s.changesUnread != 2 {
		t.Fatalf("expected 2 unread changes for instance a, got %d", s.changesUnread)
	}

	view.handleKey(event.NewKeyEvent(event.KeyPress, event.KeyL, 'l', event.ModSuper))
	if !view.IsBoardOpen() || view.boardTab != boardTabChanges {
		t.Fatal("Cmd+L should open the changes tab")
	}
	texts := drawnTexts(view)
	for _, want := range []string{"Board", "Changes", "A-2", "A-1", "Comment", "Bitte Mapping prüfen", "@you", "To Do → In Progress", "You", "Today"} {
		if !texts[want] {
			t.Errorf("changes tab is missing %q", want)
		}
	}
	if texts["B-1"] {
		t.Error("changes must follow the board's instance filter")
	}
	s := view.snapshot()
	if s.changesUnread != 0 {
		t.Fatalf("opening the tab marks its changes read, still %d unread", s.changesUnread)
	}
	if len(s.changes) != 2 || !s.changes[0].At.After(s.changesReadAt["a"]) {
		t.Fatal("this visit must still show the entries as new")
	}

	view.handleKey(event.NewKeyEvent(event.KeyPress, event.KeyL, 'l', event.ModSuper))
	if view.IsBoardOpen() || view.state != window.StateFan {
		t.Fatal("Cmd+L again should return to the fan")
	}
}

func TestHideMineFiltersOwnChanges(t *testing.T) {
	view := changesView(t)
	view.OpenChanges()
	r := expandedCardRect(window.DockSideRight)
	view.handleClick(newBoardLayout(r).hideOwn.Center())
	s := view.snapshot()
	if !s.config.ChangesHideOwn || len(s.changes) != 1 || s.changes[0].IssueKey != "A-2" {
		t.Fatalf("hide mine should leave only A-2, got %+v", s.changes)
	}
}

func TestClickingCommentOpensTicketAtComment(t *testing.T) {
	view := changesView(t)
	view.OpenChanges()
	r := expandedCardRect(window.DockSideRight)
	rows, _ := layoutChangeRows(view.snapshot().changes, time.Now())
	var target geometry.Rect
	for _, row := range rows {
		if row.header == "" && row.entry.IssueKey == "A-2" {
			body := changesBody(r)
			target = geometry.NewRect(body.Min.X, body.Min.Y+row.y, body.Width(), row.h)
		}
	}
	// The tab bar and day header sit above the first row; a click there must not open anything.
	view.handleClick(newBoardLayout(r).tabs[boardTabChanges].Center())
	if !view.IsBoardOpen() {
		t.Fatal("clicking the active tab must keep the board open")
	}
	view.handleClick(target.Center())
	if view.IsBoardOpen() || view.state != window.StateExpanded || view.issues[view.activeIdx].Key != "A-2" {
		t.Fatalf("clicking the comment should show A-2, board open=%v", view.IsBoardOpen())
	}
	view.mu.Lock()
	left := view.ticketURLOverride
	view.mu.Unlock()
	if left != "" {
		t.Fatalf("the comment URL must be used by the ticket view, still pending %q", left)
	}
}

func TestChangelogCanBeTurnedOffPerInstance(t *testing.T) {
	view := changesView(t)
	view.mu.Lock()
	view.config.Instances[0].ChangesDisabled = true
	view.mu.Unlock()

	view.OpenChanges()
	s := view.snapshot()
	if !s.changesOff || len(s.changes) != 0 || s.changesUnread != 0 {
		t.Fatalf("a disabled instance shows no changes, got %+v unread=%d", s.changes, s.changesUnread)
	}
	if !drawnTexts(view)["Changelog is off for this instance"] {
		t.Fatal("the tab should explain that the changelog is off")
	}

	// Filter membership is not recorded for a disabled instance.
	view.mu.Lock()
	view.recordMembershipLocked(nil, []jira.Issue{iss("a", "A-9", "To Do")}, map[string]bool{"a": true})
	view.mu.Unlock()
	for _, e := range view.changelog.Entries("a") {
		if e.IssueKey == "A-9" {
			t.Fatal("disabled instance recorded a filter change")
		}
	}

	// The settings switch toggles the selected instance.
	view.OpenSettings()
	view.handleClick(settingsTestCenter(settingsTestLayout(view).instanceChanges))
	view.mu.Lock()
	off := view.config.Instances[view.selectedInstIdx].ChangesDisabled
	sel := view.selectedInstIdx
	view.mu.Unlock()
	if sel == 0 && off {
		t.Fatal("settings switch should turn the changelog back on")
	}
}

func TestFetchAllChangesSkipsDisabledInstance(t *testing.T) {
	cfg := twoInstanceConfig("http://127.0.0.1:1")
	cfg.Instances[0].ChangesDisabled = true
	cfg.Instances[1].ChangesDisabled = true
	res := jira.NewClient(cfg).FetchAllChanges(t.Context(), nil)
	if len(res.Instances) != 0 || len(res.Failed) != 0 {
		t.Fatalf("disabled instances must not be queried, got %+v", res)
	}
}

func TestDayLabels(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.Local)
	cases := map[time.Time]string{
		now.Add(-time.Hour):                             "Today",
		now.AddDate(0, 0, -1):                           "Yesterday",
		now.AddDate(0, 0, -3):                           "Sun 18 Oct",
		time.Date(2025, 12, 30, 9, 0, 0, 0, time.Local): "Tue 30 Dec 2025",
	}
	for at, want := range cases {
		if got := dayLabel(at, now); got != want {
			t.Errorf("dayLabel(%v) = %q, want %q", at, got, want)
		}
	}
}
