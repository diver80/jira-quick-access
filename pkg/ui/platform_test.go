package ui

import (
	"testing"
	"time"

	"jira-quick-access/pkg/jira"
	"jira-quick-access/pkg/window"
)

// withoutNativeFeatures simulates the Windows/Linux build. Registered before the
// view is created, so the flags are restored only after its goroutines stopped.
func withoutNativeFeatures(t *testing.T) *[]string {
	t.Helper()
	embedded, tracking, open := window.EmbeddedTicketView, window.PointerTracking, openURL
	t.Cleanup(func() { window.EmbeddedTicketView, window.PointerTracking, openURL = embedded, tracking, open })
	window.EmbeddedTicketView, window.PointerTracking = false, false
	var opened []string
	openURL = func(u string) error { opened = append(opened, u); return nil }
	return &opened
}

func TestTicketsOpenInBrowserWithoutEmbeddedView(t *testing.T) {
	opened := withoutNativeFeatures(t)
	view := boardView(t)
	view.mu.Lock()
	for i := range view.issues {
		view.issues[i].BaseURL = "https://jira.test"
	}
	view.mu.Unlock()
	view.OpenBoard()

	l := newBoardLayout(expandedCardRect(window.DockSideRight))
	view.handleClick(boardCardRect(l.body[boardInProgress], 0, 0).Center())
	if len(*opened) != 1 || (*opened)[0] != "https://jira.test/browse/A-2" {
		t.Fatalf("the ticket should open in the browser, got %v", *opened)
	}
	if !view.IsBoardOpen() {
		t.Fatal("the board stays open instead of showing an empty ticket panel")
	}

	// Comment entries keep their focused-comment link.
	view.mu.Lock()
	view.changelog = jira.OpenChangeLog(t.TempDir() + "/c.json")
	view.mu.Unlock()
	view.openChange(jira.ChangeEntry{InstanceID: "a", IssueKey: "A-1", BaseURL: "https://jira.test", CommentID: "7"})
	if got := (*opened)[len(*opened)-1]; got != "https://jira.test/browse/A-1?focusedCommentId=7" {
		t.Fatalf("comment link lost: %s", got)
	}
}

func TestFanCollapsesAfterIdleWithoutPointerTracking(t *testing.T) {
	withoutNativeFeatures(t)
	view := newFeatureView(t)
	view.SetState(window.StateFan)
	view.mu.Lock()
	view.lastHover = time.Now().Add(-fanIdleCollapse - time.Second)
	view.mu.Unlock()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		view.mu.Lock()
		st := view.state
		view.mu.Unlock()
		if st == window.StateRest {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("an idle fan should collapse without pointer tracking")
}
