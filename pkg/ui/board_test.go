package ui

import (
	"testing"

	"github.com/gogpu/ui/event"

	"jira-quick-access/pkg/jira"
	"jira-quick-access/pkg/window"
)

func boardIssue(key, status, category string) jira.Issue {
	return jira.Issue{InstanceID: "a", Key: key, Summary: key + " summary", Status: jira.Status{Name: status, CategoryKey: category}}
}

func TestBoardColumnUsesStatusCategory(t *testing.T) {
	cases := []struct {
		status, category string
		want             int
	}{
		{"Backlog", "new", boardToDo},
		{"In Review", "indeterminate", boardInProgress},
		{"Erledigt", "done", boardDone},
		{"In Progress", "", boardInProgress}, // no category: fall back to the name
		{"Closed", "", boardDone},
		{"Waiting", "", boardToDo},
	}
	for _, c := range cases {
		if got := boardColumn(boardIssue("A-1", c.status, c.category)); got != c.want {
			t.Errorf("%s/%s: got column %d, want %d", c.status, c.category, got, c.want)
		}
	}
}

func TestGroupBoardKeepsOrder(t *testing.T) {
	cols := groupBoard([]jira.Issue{
		boardIssue("A-3", "To Do", "new"),
		boardIssue("A-1", "Doing", "indeterminate"),
		boardIssue("A-2", "To Do", "new"),
	})
	if len(cols[boardToDo]) != 2 || cols[boardToDo][0].Key != "A-3" || cols[boardToDo][1].Key != "A-2" {
		t.Fatalf("to do column must keep the list order, got %+v", cols[boardToDo])
	}
	if len(cols[boardInProgress]) != 1 || len(cols[boardDone]) != 0 {
		t.Fatalf("unexpected columns %+v", cols)
	}
}

func TestWrapText(t *testing.T) {
	lines := wrapText("Fix the login timeout when the session cookie expires during a long upload", 9, 120, 2)
	if len(lines) != 2 {
		t.Fatalf("expected two lines, got %q", lines)
	}
	for _, l := range lines {
		if measureTextWidth(l, 9) > 120 {
			t.Errorf("line %q is wider than the card", l)
		}
	}
	if r := []rune(lines[1]); r[len(r)-1] != '…' {
		t.Errorf("cut text must end with an ellipsis, got %q", lines[1])
	}
	if got := wrapText("Short", 9, 120, 2); len(got) != 1 || got[0] != "Short" {
		t.Errorf("short text should stay on one line, got %q", got)
	}
	if got := wrapText("", 9, 120, 2); len(got) != 0 {
		t.Errorf("empty summary should produce no lines, got %q", got)
	}
}

func boardView(t *testing.T) *AppView {
	t.Helper()
	view := newFeatureView(t)
	view.mu.Lock()
	view.issues = []jira.Issue{
		boardIssue("A-1", "To Do", "new"),
		boardIssue("A-2", "In Progress", "indeterminate"),
		boardIssue("A-3", "Done", "done"),
		{InstanceID: "b", Key: "B-1", Status: jira.Status{Name: "To Do", CategoryKey: "new"}},
	}
	view.activeInstIdx = 0
	view.invalidateFilterCacheLocked()
	view.mu.Unlock()
	return view
}

func TestFanBoardButtonOpensBoardAndCardOpensTicket(t *testing.T) {
	view := boardView(t)
	view.SetState(window.StateFan)
	w, h := view.computeSize(window.StateFan)
	btn := fanBoardButtonRect(float32(w), float32(h))
	view.handleClick(btn.Center())
	if !view.IsBoardOpen() {
		t.Fatal("board button in the fan should open the board")
	}

	canvas := &testMockCanvas{}
	view.Draw(&testMockContext{}, canvas)
	labels := map[string]bool{}
	for _, call := range canvas.getDrawTexts() {
		labels[call.text] = true
	}
	for _, want := range []string{"Board", "To Do", "In Progress", "Done", "A-1", "A-2", "A-3"} {
		if !labels[want] {
			t.Errorf("board is missing %q", want)
		}
	}
	if labels["B-1"] {
		t.Error("board must follow the fan's instance filter")
	}

	// Click the first card of the "In Progress" column.
	l := newBoardLayout(expandedCardRect(window.DockSideRight))
	view.handleClick(boardCardRect(l.body[boardInProgress], 0, 0).Center())
	if view.IsBoardOpen() || view.state != window.StateExpanded {
		t.Fatalf("clicking a card should show the ticket, board open=%v state=%v", view.IsBoardOpen(), view.state)
	}
	if got := view.issues[view.activeIdx].Key; got != "A-2" {
		t.Fatalf("expected A-2 to be opened, got %s", got)
	}
}

func TestBoardCloseAndShortcut(t *testing.T) {
	view := boardView(t)
	view.SetState(window.StateFan)

	view.handleKey(event.NewKeyEvent(event.KeyPress, event.KeyB, 'b', event.ModSuper))
	if !view.IsBoardOpen() {
		t.Fatal("Cmd+B should open the board")
	}
	view.handleClick(newBoardLayout(expandedCardRect(window.DockSideRight)).close.Center())
	if view.IsBoardOpen() || view.state != window.StateFan {
		t.Fatalf("close should return to the fan, state %v", view.state)
	}

	view.OpenBoard()
	view.handleKey(event.NewKeyEvent(event.KeyPress, event.KeyEscape, 0, event.ModNone))
	if view.IsBoardOpen() || view.state != window.StateFan {
		t.Fatalf("Escape should return to the fan, state %v", view.state)
	}

	view.OpenBoard()
	view.OpenSettings()
	view.mu.Lock()
	open := view.showBoard
	view.mu.Unlock()
	if open {
		t.Fatal("opening settings must close the board")
	}
}

func TestBoardWheelScrollsOnlyThatColumn(t *testing.T) {
	view := newFeatureView(t)
	view.mu.Lock()
	for i := 0; i < 20; i++ {
		view.issues = append(view.issues, boardIssue("A-"+string(rune('a'+i)), "To Do", "new"))
	}
	view.invalidateFilterCacheLocked()
	view.mu.Unlock()
	view.OpenBoard()

	r := expandedCardRect(window.DockSideRight)
	l := newBoardLayout(r)
	view.handleBoardWheel(r, l.cols[boardToDo].Center(), -5000)
	view.handleBoardWheel(r, l.cols[boardDone].Center(), -5000)
	s := view.snapshot()
	if want := boardMaxScroll(l.body[boardToDo], 20); s.boardScroll[boardToDo] != want || want == 0 {
		t.Fatalf("to do column should scroll to its end %v, got %v", want, s.boardScroll[boardToDo])
	}
	if s.boardScroll[boardDone] != 0 {
		t.Fatalf("empty column must not scroll, got %v", s.boardScroll[boardDone])
	}

	// Hovering a card highlights it.
	if !view.handleBoardHover(r, boardCardRect(l.body[boardToDo], s.boardScroll[boardToDo], 19).Center()) {
		t.Fatal("hovering a card should change the highlight")
	}
}
