package ui

import (
	"fmt"
	"strings"

	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/widget"

	"jira-quick-access/pkg/jira"
	"jira-quick-access/pkg/window"
)

// The board groups the tickets shown in the fan (configured JQL, instance filter
// and search) into three read-only columns by Jira status category.

const (
	boardToDo = iota
	boardInProgress
	boardDone
	boardColumnCount
)

var boardColumnTitles = [boardColumnCount]string{"To Do", "In Progress", "Done"}

var boardColumnColors = [boardColumnCount]widget.Color{ColorStatusToDo, ColorStatusInProgress, ColorStatusDone}

const (
	boardCardH   = float32(66)
	boardCardGap = float32(6)
)

// boardColumn maps a ticket to its column. Status categories are authoritative;
// the name checks cover mock data and servers that omit the category.
func boardColumn(iss jira.Issue) int {
	cat := strings.ToLower(iss.Status.CategoryKey)
	name := strings.ToLower(iss.Status.Name)
	switch {
	case cat == "indeterminate" || strings.Contains(name, "progress") || strings.Contains(name, "dev"):
		return boardInProgress
	case cat == "done" || strings.Contains(name, "done") || strings.Contains(name, "closed"):
		return boardDone
	}
	return boardToDo
}

// groupBoard splits tickets into columns, keeping the current sort order.
func groupBoard(issues []jira.Issue) [boardColumnCount][]jira.Issue {
	var cols [boardColumnCount][]jira.Issue
	for _, iss := range issues {
		c := boardColumn(iss)
		cols[c] = append(cols[c], iss)
	}
	return cols
}

type boardLayout struct {
	close   geometry.Rect
	tabs    [2]geometry.Rect                // Board / Changes switch
	hideOwn geometry.Rect                   // Changes tab: "Hide mine"
	cols    [boardColumnCount]geometry.Rect // column background incl. header
	body    [boardColumnCount]geometry.Rect // scrollable card area
}

func newBoardLayout(r geometry.Rect) boardLayout {
	l := boardLayout{close: geometry.NewRect(r.Max.X-54, r.Min.Y+14, 40, 26)}
	l.tabs[boardTabBoard] = geometry.NewRect(r.Min.X+16, r.Min.Y+10, 66, 24)
	l.tabs[boardTabChanges] = geometry.NewRect(r.Min.X+86, r.Min.Y+10, 100, 24)
	l.hideOwn = geometry.NewRect(l.close.Min.X-100, l.close.Min.Y, 92, 26)
	gap := float32(10)
	colW := (r.Width() - 32 - 2*gap) / boardColumnCount
	top := r.Min.Y + 56
	colH := r.Max.Y - 14 - top
	for i := range l.cols {
		x := r.Min.X + 16 + float32(i)*(colW+gap)
		l.cols[i] = geometry.NewRect(x, top, colW, colH)
		l.body[i] = geometry.NewRect(x+6, top+34, colW-12, colH-40)
	}
	return l
}

func boardCardRect(body geometry.Rect, scroll float32, idx int) geometry.Rect {
	return geometry.NewRect(body.Min.X, body.Min.Y-scroll+float32(idx)*(boardCardH+boardCardGap), body.Width(), boardCardH)
}

func boardMaxScroll(body geometry.Rect, n int) float32 {
	total := float32(n)*(boardCardH+boardCardGap) - boardCardGap
	if total <= body.Height() {
		return 0
	}
	return total - body.Height()
}

// boardHit returns the ticket card under pos, if any.
func boardHit(l boardLayout, cols [boardColumnCount][]jira.Issue, scroll [boardColumnCount]float32, pos geometry.Point) (jira.Issue, bool) {
	for c := range cols {
		if !l.body[c].Contains(pos) {
			continue
		}
		for i, iss := range cols[c] {
			if boardCardRect(l.body[c], scroll[c], i).Contains(pos) {
				return iss, true
			}
		}
	}
	return jira.Issue{}, false
}

// expandedCardRect is the content area next to the dock shelf in the expanded window.
func expandedCardRect(dockSide window.DockSide) geometry.Rect {
	w, h := float32(780), float32(580)
	tabBarWidth := float32(110)
	x := float32(8)
	if dockSide == window.DockSideLeft {
		x = tabBarWidth + 8
	}
	return geometry.NewRect(x, 8, w-tabBarWidth-14, h-16)
}

func (v *AppView) IsBoardOpen() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.showBoard && v.state == window.StateExpanded
}

func (v *AppView) OpenBoard() {
	v.mu.Lock()
	v.showBoard = true
	v.showSettings = false
	v.showStatus = false
	v.boardScroll = [boardColumnCount]float32{}
	v.hoveredBoardCard = ""
	v.mu.Unlock()
	v.SetState(window.StateExpanded)
}

func (v *AppView) ToggleBoard() {
	if v.IsBoardOpen() {
		v.SetState(window.StateFan)
		return
	}
	v.OpenBoard()
}

func (v *AppView) handleBoardClick(r geometry.Rect, pos geometry.Point) bool {
	l := newBoardLayout(r)
	if l.close.Contains(pos) {
		v.SetState(window.StateFan)
		return true
	}
	for tab, rect := range l.tabs {
		if rect.Contains(pos) {
			v.setBoardTab(tab)
			return true
		}
	}
	v.mu.Lock()
	tab := v.boardTab
	v.mu.Unlock()
	if tab == boardTabChanges {
		return v.handleChangesClick(r, pos)
	}
	v.mu.Lock()
	cols := groupBoard(v.getFilteredIssuesLocked())
	scroll := v.boardScroll
	all := v.issues
	v.mu.Unlock()
	if iss, ok := boardHit(l, cols, scroll, pos); ok {
		if idx := indexOfIssue(all, iss); idx >= 0 {
			v.Expand(idx)
		}
	}
	return true
}

// handleBoardHover updates the highlighted card and reports whether it changed.
func (v *AppView) handleBoardHover(r geometry.Rect, pos geometry.Point) bool {
	l := newBoardLayout(r)
	v.mu.Lock()
	if v.boardTab == boardTabChanges {
		v.mu.Unlock()
		return v.handleChangesHover(r, pos)
	}
	defer v.mu.Unlock()
	hovered := ""
	if iss, ok := boardHit(l, groupBoard(v.getFilteredIssuesLocked()), v.boardScroll, pos); ok {
		hovered = issueID(iss)
	}
	changed := hovered != v.hoveredBoardCard
	v.hoveredBoardCard = hovered
	return changed
}

func (v *AppView) handleBoardWheel(r geometry.Rect, pos geometry.Point, deltaY float32) {
	l := newBoardLayout(r)
	v.mu.Lock()
	if v.boardTab == boardTabChanges {
		v.mu.Unlock()
		v.handleChangesWheel(r, deltaY)
		return
	}
	defer v.mu.Unlock()
	cols := groupBoard(v.getFilteredIssuesLocked())
	for c := range cols {
		if !l.cols[c].Contains(pos) {
			continue
		}
		s := v.boardScroll[c] - deltaY*0.8
		s = max(0, min(s, boardMaxScroll(l.body[c], len(cols[c]))))
		v.boardScroll[c] = s
	}
}

func (v *AppView) drawBoard(canvas widget.Canvas, r geometry.Rect, s appViewStateSnapshot) {
	radius := float32(14)
	canvas.DrawRoundRect(r, widget.RGBA8(18, 24, 36, 250), radius)
	canvas.StrokeRoundRect(r, widget.RGBA8(255, 255, 255, 70), radius, 1.0)

	l := newBoardLayout(r)
	drawBoardTabs(canvas, l, s.boardTab, s.changesUnread)

	scope := "All instances"
	if s.activeInstIdx >= 0 && s.activeInstIdx < len(s.instances) {
		scope = s.instances[s.activeInstIdx].Name
	}
	sub := fmt.Sprintf("%s · %d tickets", scope, len(s.filteredIssues))
	if s.boardTab == boardTabChanges {
		sub = fmt.Sprintf("%s · %d changes", scope, len(s.changes))
	}
	if s.searchQuery != "" {
		sub += fmt.Sprintf(" · search \"%s\"", s.searchQuery)
	}
	canvas.DrawText(sub, geometry.NewRect(r.Min.X+22, r.Min.Y+38, r.Width()-120, 14), 9, widget.RGBA8(148, 163, 184, 255), false, widget.TextAlignLeft)

	canvas.DrawRoundRect(l.close, widget.RGBA8(38, 48, 68, 240), 5)
	canvas.StrokeRoundRect(l.close, widget.RGBA8(255, 255, 255, 60), 5, 1.0)
	canvas.DrawText("✕", l.close, 11, widget.RGBA8(240, 245, 255, 255), true, widget.TextAlignCenter)

	if s.boardTab == boardTabChanges {
		v.drawChanges(canvas, r, s)
		return
	}

	cols := groupBoard(s.filteredIssues)
	for c := range cols {
		col := l.cols[c]
		canvas.DrawRoundRect(col, widget.RGBA8(24, 32, 48, 230), 10)
		canvas.StrokeRoundRect(col, widget.RGBA8(255, 255, 255, 25), 10, 1.0)

		canvas.DrawCircle(geometry.Pt(col.Min.X+16, col.Min.Y+17), 4, boardColumnColors[c])
		canvas.DrawText(boardColumnTitles[c], geometry.NewRect(col.Min.X+26, col.Min.Y+10, col.Width()-70, 15), 11, widget.RGBA8(235, 245, 255, 255), true, widget.TextAlignLeft)
		countRect := geometry.NewRect(col.Max.X-38, col.Min.Y+8, 28, 18)
		canvas.DrawRoundRect(countRect, widget.RGBA8(40, 52, 74, 240), 9)
		canvas.DrawText(fmt.Sprintf("%d", len(cols[c])), countRect, 9, widget.RGBA8(220, 232, 250, 255), true, widget.TextAlignCenter)

		body := l.body[c]
		if len(cols[c]) == 0 {
			canvas.DrawText("No tickets", geometry.NewRect(body.Min.X, body.Min.Y+12, body.Width(), 14), 9, widget.RGBA8(110, 125, 150, 255), false, widget.TextAlignCenter)
			continue
		}

		scroll := s.boardScroll[c]
		canvas.PushClip(body)
		for i, iss := range cols[c] {
			card := boardCardRect(body, scroll, i)
			if card.Max.Y < body.Min.Y || card.Min.Y > body.Max.Y {
				continue
			}
			v.drawBoardCard(canvas, card, iss, s, issueID(iss) == s.hoveredBoardCard)
		}
		canvas.PopClip()

		if maxScroll := boardMaxScroll(body, len(cols[c])); maxScroll > 0 {
			thumbH := float32(28)
			thumbY := body.Min.Y + (scroll/maxScroll)*(body.Height()-thumbH)
			canvas.DrawRoundRect(geometry.NewRect(col.Max.X-4, thumbY, 3, thumbH), widget.RGBA8(255, 255, 255, 140), 1.5)
		}
	}
}

func (v *AppView) drawBoardCard(canvas widget.Canvas, card geometry.Rect, iss jira.Issue, s appViewStateSnapshot, hovered bool) {
	bg := widget.RGBA8(32, 42, 62, 240)
	border := widget.RGBA8(255, 255, 255, 30)
	if hovered {
		bg = widget.RGBA8(42, 56, 82, 250)
		border = widget.RGBA8(255, 255, 255, 170)
	}
	canvas.DrawRoundRect(card, bg, 7)
	canvas.StrokeRoundRect(card, border, 7, 1.0)

	dot := widget.RGBA8(148, 163, 184, 255)
	for idx, inst := range s.instances {
		if isIssueForInstance(iss, inst, idx) {
			dot, _, _, _, _, _, _ = GetInstanceColors(inst.Color, idx)
			break
		}
	}
	canvas.DrawCircle(geometry.Pt(card.Min.X+10, card.Min.Y+13), 3, dot)
	canvas.DrawText(iss.Key, geometry.NewRect(card.Min.X+18, card.Min.Y+6, card.Width()-90, 14), 10, widget.RGBA8(240, 247, 255, 255), true, widget.TextAlignLeft)
	if iss.Priority.Name != "" {
		canvas.DrawText(truncateSummary(iss.Priority.Name, 12), geometry.NewRect(card.Max.X-78, card.Min.Y+7, 70, 12), 8, widget.RGBA8(160, 175, 200, 255), false, widget.TextAlignRight)
	}

	textW := card.Width() - 20
	for i, line := range wrapText(iss.Summary, 9, textW, 2) {
		canvas.DrawText(line, geometry.NewRect(card.Min.X+10, card.Min.Y+22+float32(i)*13, textW, 13), 9, widget.RGBA8(205, 218, 235, 255), false, widget.TextAlignLeft)
	}
	canvas.DrawText(truncateSummary(iss.Status.Name, 30), geometry.NewRect(card.Min.X+10, card.Min.Y+50, textW, 12), 8, widget.RGBA8(130, 148, 175, 255), false, widget.TextAlignLeft)
}

// wrapText breaks text into at most maxLines lines that fit maxW; the last line
// ends with an ellipsis when text is left over.
func wrapText(text string, size, maxW float32, maxLines int) []string {
	words := strings.Fields(text)
	var lines []string
	for len(words) > 0 && len(lines) < maxLines {
		line := words[0]
		words = words[1:]
		for len(words) > 0 && measureTextWidth(line+" "+words[0], size) <= maxW {
			line += " " + words[0]
			words = words[1:]
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return nil
	}
	last := len(lines) - 1
	if len(words) > 0 {
		lines[last] = fitText(lines[last]+" "+strings.Join(words, " "), size, maxW)
	}
	for i, line := range lines {
		lines[i] = fitText(line, size, maxW)
	}
	return lines
}

// fitText shortens text with an ellipsis until it fits maxW.
func fitText(text string, size, maxW float32) string {
	if measureTextWidth(text, size) <= maxW {
		return text
	}
	runes := []rune(text)
	for n := len(runes) - 1; n > 0; n-- {
		cut := strings.TrimRight(string(runes[:n]), " ") + "…"
		if measureTextWidth(cut, size) <= maxW {
			return cut
		}
	}
	return "…"
}

// drawBoardButton draws the "Board" entry with a small three-column glyph.
func drawBoardButton(canvas widget.Canvas, r geometry.Rect, active, hovered, unread bool) {
	bg := widget.RGBA8(36, 46, 66, 230)
	border := widget.RGBA8(255, 255, 255, 45)
	if active {
		bg = widget.RGBA8(54, 70, 98, 240)
	}
	if hovered {
		bg = widget.RGBA8(52, 68, 96, 245)
		border = widget.RGBA8(255, 255, 255, 180)
	}
	canvas.DrawRoundRect(r, bg, 8)
	canvas.StrokeRoundRect(r, border, 8, 1.0)

	iconX := r.Min.X + 9
	iconY := r.Min.Y + r.Height()/2 - 5
	fg := widget.RGBA8(230, 240, 255, 255)
	for i, hgt := range []float32{10, 7, 4} {
		canvas.DrawRoundRect(geometry.NewRect(iconX+float32(i)*4, iconY, 3, hgt), fg, 1)
	}
	canvas.DrawText("Board", geometry.NewRect(iconX+14, r.Min.Y+r.Height()/2-8, r.Width()-25, 16), 10, fg, true, widget.TextAlignLeft)
	if unread {
		canvas.DrawCircle(geometry.Pt(r.Max.X-6, r.Min.Y+6), 3.5, colorChangeUnread)
	}
}

// drawSettingsIconButton draws the compact gear button next to the board button.
func drawSettingsIconButton(canvas widget.Canvas, r geometry.Rect, active, hovered, updateAvailable bool) {
	bg := widget.RGBA8(36, 46, 66, 230)
	border := widget.RGBA8(255, 255, 255, 45)
	if active {
		bg = widget.RGBA8(54, 70, 98, 240)
	}
	if hovered {
		bg = widget.RGBA8(52, 68, 96, 245)
		border = widget.RGBA8(255, 255, 255, 180)
	}
	canvas.DrawRoundRect(r, bg, 8)
	canvas.StrokeRoundRect(r, border, 8, 1.0)
	canvas.DrawText("⚙", r, 12, widget.RGBA8(230, 240, 255, 255), true, widget.TextAlignCenter)
	if updateAvailable {
		drawUpdateDot(canvas, geometry.Pt(r.Max.X-6, r.Min.Y+6))
	}
}

// Bottom row of the fan and of the expanded dock shelf: board button, then settings.
func fanBoardButtonRect(w, h float32) geometry.Rect    { return geometry.NewRect(7, h-42, 64, 34) }
func fanSettingsButtonRect(w, h float32) geometry.Rect { return geometry.NewRect(75, h-42, w-82, 34) }

func shelfBoardButtonRect(tabStartX, h float32) geometry.Rect {
	return geometry.NewRect(tabStartX+2, h-48, 60, 34)
}

func shelfSettingsButtonRect(tabStartX, h float32) geometry.Rect {
	return geometry.NewRect(tabStartX+66, h-48, 30, 34)
}
