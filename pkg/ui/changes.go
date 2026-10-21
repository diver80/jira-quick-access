package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/widget"

	"jira-quick-access/pkg/jira"
	"jira-quick-access/pkg/window"
)

// The Changes tab of the board overlay lists what happened to the tickets of the
// instance filter: Jira changelog entries, comments and tickets entering or leaving
// the filter, newest first and grouped by day.

const (
	boardTabBoard = iota
	boardTabChanges
)

const (
	changeRowH       = float32(48)
	changeHeaderRowH = float32(24)
	changeRowGap     = float32(4)
)

var (
	colorChangeMention = widget.RGBA8(251, 191, 36, 255)
	colorChangeUnread  = widget.RGBA8(56, 189, 248, 255)
)

// ---------------------------------------------------------------------------
// Fetching and recording
// ---------------------------------------------------------------------------

// RefreshChanges checks all instances for changes since their last check, stores
// them and announces new mentions. Only one check runs at a time.
func (v *AppView) RefreshChanges() {
	v.mu.Lock()
	log := v.changelog
	if v.ctx == nil || v.ctx.Err() != nil || log == nil || !v.changesBusy.CompareAndSwap(false, true) {
		v.mu.Unlock()
		return
	}
	v.wg.Add(1)
	v.mu.Unlock()

	go func() {
		defer v.wg.Done()
		defer v.changesBusy.Store(false)
		res := v.client.FetchAllChanges(v.ctx, log.Watermarks())
		if v.ctx.Err() != nil {
			return
		}
		now := time.Now()
		var fresh []jira.ChangeEntry
		for id, ch := range res.Instances {
			added := log.Merge(id, ch.Entries, ch.Watermark, ch.Backfill, now)
			if !ch.Backfill {
				fresh = append(fresh, added...)
			}
		}

		v.mu.Lock()
		ids := v.changesInstancesLocked(-1)
		notify := v.config.Notifications
		v.mu.Unlock()

		// Disabled or removed instances lose their stored entries; turning the
		// changelog back on starts with a fresh backfill.
		log.Retain(ids)
		_ = log.Save()
		if notify {
			notifyMentions(fresh)
		}
		v.postToUI(v.MarkNeedsLayout)
	}()
}

// notifyMentions announces new comments that mention the user.
func notifyMentions(entries []jira.ChangeEntry) {
	for _, e := range entries {
		if e.Kind == jira.ChangeComment && e.Mentioned && !e.Own {
			window.Notify(fmt.Sprintf("%s · %s mentioned you", e.IssueKey, firstNonEmptyStr(e.Author, "Someone")), e.Text, e.IssueKey, e.InstanceID)
		}
	}
}

func firstNonEmptyStr(values ...string) string {
	for _, s := range values {
		if s != "" {
			return s
		}
	}
	return ""
}

// membershipEntries records tickets that entered or left an instance's filter
// between two complete refreshes. Jira's changelog does not cover this.
func membershipEntries(prev, next []jira.Issue, comparable map[string]bool, now time.Time) []jira.ChangeEntry {
	entry := func(iss jira.Issue, kind jira.ChangeKind) jira.ChangeEntry {
		return jira.ChangeEntry{
			ID:         fmt.Sprintf("m:%s:%s:%d", iss.Key, kind, now.Unix()),
			InstanceID: iss.InstanceID,
			BaseURL:    iss.BaseURL,
			IssueKey:   iss.Key,
			Summary:    iss.Summary,
			Kind:       kind,
			From:       iss.Status.Name,
			At:         now,
		}
	}
	var out []jira.ChangeEntry
	for _, iss := range next {
		if comparable[iss.InstanceID] && indexOfIssue(prev, iss) < 0 {
			out = append(out, entry(iss, jira.ChangeAdded))
		}
	}
	for _, iss := range prev {
		if comparable[iss.InstanceID] && indexOfIssue(next, iss) < 0 {
			out = append(out, entry(iss, jira.ChangeRemoved))
		}
	}
	return out
}

// recordMembershipLocked stores filter membership changes. Called with v.mu held.
func (v *AppView) recordMembershipLocked(prev, next []jira.Issue, comparable map[string]bool) {
	if v.changelog == nil {
		return
	}
	now := time.Now()
	enabled := v.changesInstancesLocked(-1)
	byInst := map[string][]jira.ChangeEntry{}
	for _, e := range membershipEntries(prev, next, comparable, now) {
		if slices.Contains(enabled, e.InstanceID) {
			byInst[e.InstanceID] = append(byInst[e.InstanceID], e)
		}
	}
	for id, entries := range byInst {
		v.changelog.Merge(id, entries, time.Time{}, false, now)
	}
}

// ---------------------------------------------------------------------------
// State
// ---------------------------------------------------------------------------

// changesInstancesLocked returns the IDs of instances with the changelog turned on,
// limited to the instance at idx when idx is valid (the board's instance filter).
func (v *AppView) changesInstancesLocked(idx int) []string {
	var ids []string
	for i, inst := range v.config.Instances {
		if !inst.ChangesDisabled && (idx < 0 || idx >= len(v.config.Instances) || i == idx) {
			ids = append(ids, inst.ID)
		}
	}
	return ids
}

// changesScopeLocked returns the instances whose changes the board shows.
func (v *AppView) changesScopeLocked() []string {
	return v.changesInstancesLocked(v.activeInstIdx)
}

// visibleChangesLocked applies the instance filter, the search and "hide mine".
func (v *AppView) visibleChangesLocked() []jira.ChangeEntry {
	scope := v.changesScopeLocked()
	if v.changelog == nil || len(scope) == 0 {
		return nil
	}
	all := v.changelog.Entries(scope...)
	q := strings.ToLower(v.searchQuery)
	out := all[:0]
	for _, e := range all {
		if v.config.ChangesHideOwn && e.Own {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(e.IssueKey+" "+e.Summary+" "+e.Author+" "+e.Text+" "+e.To), q) {
			continue
		}
		out = append(out, e)
	}
	return out
}

func (v *AppView) unreadChangesLocked() int {
	scope := v.changesScopeLocked()
	if v.changelog == nil || len(scope) == 0 {
		return 0
	}
	return v.changelog.Unread(v.config.ChangesHideOwn, scope...)
}

// showChangesTabLocked switches the board overlay to the Changes tab and marks its
// entries as read, keeping the previous read marks for this visit's unread dots.
func (v *AppView) showChangesTabLocked() {
	if v.boardTab != boardTabChanges || !v.showBoard {
		v.changesScroll = 0
	}
	v.boardTab = boardTabChanges
	v.hoveredChange = ""
	if v.changelog == nil {
		return
	}
	scope := v.changesScopeLocked()
	if len(scope) == 0 {
		return
	}
	v.changesReadAt = v.changelog.ReadAt()
	v.changelog.MarkRead(time.Now(), scope...)
	log := v.changelog
	v.wg.Add(1)
	go func() {
		defer v.wg.Done()
		_ = log.Save()
	}()
}

// OpenChanges opens the board overlay on its Changes tab.
func (v *AppView) OpenChanges() {
	v.mu.Lock()
	v.showChangesTabLocked()
	v.showBoard = true
	v.showSettings = false
	v.showStatus = false
	v.mu.Unlock()
	v.SetState(window.StateExpanded)
}

// ToggleChanges opens the Changes tab, or returns to the fan if it is showing.
func (v *AppView) ToggleChanges() {
	v.mu.Lock()
	open := v.showBoard && v.boardTab == boardTabChanges && v.state == window.StateExpanded
	v.mu.Unlock()
	if open {
		v.SetState(window.StateFan)
		return
	}
	v.OpenChanges()
}

func (v *AppView) setBoardTab(tab int) {
	v.mu.Lock()
	if tab == boardTabChanges {
		v.showChangesTabLocked()
	} else {
		v.boardTab = boardTabBoard
	}
	v.mu.Unlock()
	v.MarkNeedsLayout()
}

func (v *AppView) toggleHideOwnChanges() {
	v.mu.Lock()
	v.config.ChangesHideOwn = !v.config.ChangesHideOwn
	v.changesScroll = 0
	cfg := v.config.Clone()
	v.mu.Unlock()
	_ = jira.SaveConfig(cfg)
	v.MarkNeedsLayout()
}

// openChange shows the ticket of an entry in the panel (at the comment for comment
// entries). Tickets that left the filter open in the browser instead.
func (v *AppView) openChange(e jira.ChangeEntry) {
	v.mu.Lock()
	idx := -1
	for i, iss := range v.issues {
		if iss.Key == e.IssueKey && v.issueInInstanceLocked(iss, e.InstanceID) {
			idx = i
			break
		}
	}
	demo := v.config.DemoMode
	if idx >= 0 && !demo {
		v.ticketURLOverride = e.URL()
	}
	v.mu.Unlock()
	if idx >= 0 {
		v.Expand(idx)
		return
	}
	if u := e.URL(); u != "" && !demo {
		_ = window.OpenURL(u)
	}
}

func (v *AppView) issueInInstanceLocked(iss jira.Issue, instID string) bool {
	for i, inst := range v.config.Instances {
		if inst.ID == instID {
			return isIssueForInstance(iss, inst, i)
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Layout and input
// ---------------------------------------------------------------------------

type changeRow struct {
	header string // day label; empty for entry rows
	entry  jira.ChangeEntry
	y, h   float32 // offset from the top of the list
}

// layoutChangeRows groups entries (newest first) by local day.
func layoutChangeRows(entries []jira.ChangeEntry, now time.Time) ([]changeRow, float32) {
	var rows []changeRow
	y := float32(0)
	lastDay := ""
	for _, e := range entries {
		if day := dayLabel(e.At, now); day != lastDay {
			rows = append(rows, changeRow{header: day, y: y, h: changeHeaderRowH})
			y += changeHeaderRowH
			lastDay = day
		}
		rows = append(rows, changeRow{entry: e, y: y, h: changeRowH})
		y += changeRowH + changeRowGap
	}
	return rows, y
}

func dayLabel(t, now time.Time) string {
	t, now = t.Local(), now.Local()
	y1, m1, d1 := t.Date()
	y2, m2, d2 := now.Date()
	today := time.Date(y2, m2, d2, 0, 0, 0, 0, time.Local)
	day := time.Date(y1, m1, d1, 0, 0, 0, 0, time.Local)
	switch {
	case day.Equal(today):
		return "Today"
	case day.Equal(today.AddDate(0, 0, -1)):
		return "Yesterday"
	case y1 == y2:
		return t.Format("Mon 2 Jan")
	}
	return t.Format("Mon 2 Jan 2006")
}

func changesBody(r geometry.Rect) geometry.Rect {
	return geometry.NewRect(r.Min.X+16, r.Min.Y+56, r.Width()-32, r.Height()-70)
}

func changesMaxScroll(body geometry.Rect, total float32) float32 {
	return max(0, total-body.Height())
}

func changeHit(body geometry.Rect, rows []changeRow, scroll float32, pos geometry.Point) (jira.ChangeEntry, bool) {
	if !body.Contains(pos) {
		return jira.ChangeEntry{}, false
	}
	for _, row := range rows {
		if row.header == "" && geometry.NewRect(body.Min.X, body.Min.Y+row.y-scroll, body.Width(), row.h).Contains(pos) {
			return row.entry, true
		}
	}
	return jira.ChangeEntry{}, false
}

func (v *AppView) handleChangesClick(r geometry.Rect, pos geometry.Point) bool {
	if newBoardLayout(r).hideOwn.Contains(pos) {
		v.toggleHideOwnChanges()
		return true
	}
	v.mu.Lock()
	rows, _ := layoutChangeRows(v.visibleChangesLocked(), time.Now())
	scroll := v.changesScroll
	v.mu.Unlock()
	if e, ok := changeHit(changesBody(r), rows, scroll, pos); ok {
		v.openChange(e)
	}
	return true
}

func (v *AppView) handleChangesHover(r geometry.Rect, pos geometry.Point) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	rows, _ := layoutChangeRows(v.visibleChangesLocked(), time.Now())
	hovered := ""
	if e, ok := changeHit(changesBody(r), rows, v.changesScroll, pos); ok {
		hovered = e.InstanceID + "/" + e.ID
	}
	changed := hovered != v.hoveredChange
	v.hoveredChange = hovered
	return changed
}

func (v *AppView) handleChangesWheel(r geometry.Rect, deltaY float32) {
	v.mu.Lock()
	defer v.mu.Unlock()
	_, total := layoutChangeRows(v.visibleChangesLocked(), time.Now())
	v.changesScroll = max(0, min(v.changesScroll-deltaY*0.8, changesMaxScroll(changesBody(r), total)))
}

// ---------------------------------------------------------------------------
// Drawing
// ---------------------------------------------------------------------------

func (v *AppView) drawChanges(canvas widget.Canvas, r geometry.Rect, s appViewStateSnapshot) {
	l := newBoardLayout(r)
	drawToggleButton(canvas, l.hideOwn, "Hide mine", s.config.ChangesHideOwn)

	body := changesBody(r)
	if len(s.changes) == 0 {
		msg := "No changes yet"
		hint := "Status changes, field updates and comments of tickets in this filter appear here after the next sync."
		if s.changesOff {
			msg, hint = "Changelog is off for this instance", "Turn it on under Settings → Jira instances."
		} else if s.searchQuery != "" || s.config.ChangesHideOwn {
			msg, hint = "No matching changes", "Clear the search or show your own changes."
		}
		canvas.DrawText(msg, geometry.NewRect(body.Min.X, body.Min.Y+60, body.Width(), 18), 12, widget.RGBA8(200, 212, 230, 255), true, widget.TextAlignCenter)
		canvas.DrawText(fitText(hint, 10, body.Width()-40), geometry.NewRect(body.Min.X, body.Min.Y+84, body.Width(), 14), 10, widget.RGBA8(120, 136, 160, 255), false, widget.TextAlignCenter)
		return
	}

	rows, total := layoutChangeRows(s.changes, time.Now())
	canvas.PushClip(body)
	for _, row := range rows {
		y := body.Min.Y + row.y - s.changesScroll
		if y+row.h < body.Min.Y || y > body.Max.Y {
			continue
		}
		rect := geometry.NewRect(body.Min.X, y, body.Width(), row.h)
		if row.header != "" {
			canvas.DrawText(row.header, geometry.NewRect(rect.Min.X+4, rect.Min.Y+6, 200, 14), 10, widget.RGBA8(148, 163, 184, 255), true, widget.TextAlignLeft)
			continue
		}
		e := row.entry
		v.drawChangeRow(canvas, rect, e, s, s.hoveredChange == e.InstanceID+"/"+e.ID)
	}
	canvas.PopClip()

	if maxScroll := changesMaxScroll(body, total); maxScroll > 0 {
		thumbH := float32(32)
		thumbY := body.Min.Y + (s.changesScroll/maxScroll)*(body.Height()-thumbH)
		canvas.DrawRoundRect(geometry.NewRect(body.Max.X+5, thumbY, 3, thumbH), widget.RGBA8(255, 255, 255, 140), 1.5)
	}
}

func (v *AppView) drawChangeRow(canvas widget.Canvas, rect geometry.Rect, e jira.ChangeEntry, s appViewStateSnapshot, hovered bool) {
	bg := widget.RGBA8(28, 37, 56, 235)
	border := widget.RGBA8(255, 255, 255, 25)
	if e.Mentioned {
		border = colorChangeMention
	}
	if hovered {
		bg = widget.RGBA8(40, 53, 78, 250)
		border = widget.RGBA8(255, 255, 255, 170)
	}
	canvas.DrawRoundRect(rect, bg, 7)
	canvas.StrokeRoundRect(rect, border, 7, 1.0)

	accent := widget.RGBA8(148, 163, 184, 255)
	for idx, inst := range s.instances {
		if inst.ID == e.InstanceID {
			accent, _, _, _, _, _, _ = GetInstanceColors(inst.Color, idx)
			break
		}
	}
	canvas.DrawRoundRect(geometry.NewRect(rect.Min.X+3, rect.Min.Y+8, 3, rect.Height()-16), accent, 1.5)
	if e.At.After(s.changesReadAt[e.InstanceID]) {
		canvas.DrawCircle(geometry.Pt(rect.Min.X+14, rect.Min.Y+14), 3, colorChangeUnread)
	}

	primary := widget.RGBA8(240, 247, 255, 255)
	secondary := widget.RGBA8(150, 166, 190, 255)
	if e.Own {
		primary = widget.RGBA8(185, 198, 218, 255)
	}
	x := rect.Min.X + 22
	canvas.DrawText(e.At.Local().Format("15:04"), geometry.NewRect(x, rect.Min.Y+7, 34, 14), 9, secondary, false, widget.TextAlignLeft)
	keyW := min(measureTextWidth(e.IssueKey, 10)+10, 110)
	canvas.DrawText(e.IssueKey, geometry.NewRect(x+36, rect.Min.Y+6, keyW, 15), 10, primary, true, widget.TextAlignLeft)

	authorW := float32(130)
	author := e.Author
	if e.Own {
		author = "You"
	}
	if e.Mentioned {
		badge := geometry.NewRect(rect.Max.X-authorW-50, rect.Min.Y+6, 40, 15)
		canvas.DrawRoundRect(badge, colorChangeMention, 7)
		canvas.DrawText("@you", badge, 8, widget.RGBA8(30, 30, 30, 255), true, widget.TextAlignCenter)
	}
	canvas.DrawText(fitText(author, 9, authorW-10), geometry.NewRect(rect.Max.X-authorW, rect.Min.Y+7, authorW-10, 14), 9, secondary, false, widget.TextAlignRight)
	summaryX := x + 36 + keyW
	summaryW := rect.Max.X - authorW - 56 - summaryX
	canvas.DrawText(fitText(e.Summary, 9, summaryW), geometry.NewRect(summaryX, rect.Min.Y+7, summaryW, 14), 9, secondary, false, widget.TextAlignLeft)

	label, detail, color := describeChange(e)
	labelW := measureTextWidth(label, 9) + 12
	pill := geometry.NewRect(x, rect.Min.Y+27, labelW, 15)
	canvas.DrawRoundRect(pill, color.WithAlpha(0.2), 7)
	canvas.DrawText(label, pill, 9, color, true, widget.TextAlignCenter)
	detailW := rect.Max.X - 12 - (x + labelW + 8)
	canvas.DrawText(fitText(detail, 10, detailW), geometry.NewRect(x+labelW+8, rect.Min.Y+27, detailW, 15), 10, primary, false, widget.TextAlignLeft)
}

// describeChange returns the kind label, the one-line detail and the label color.
func describeChange(e jira.ChangeEntry) (label, detail string, color widget.Color) {
	switch e.Kind {
	case jira.ChangeStatus:
		return "Status", arrow(e.From, e.To), ColorStatusInProgress
	case jira.ChangeComment:
		return "Comment", e.Text, widget.RGBA8(167, 139, 250, 255)
	case jira.ChangeCommentEdited:
		return "Comment edited", e.Text, widget.RGBA8(167, 139, 250, 255)
	case jira.ChangeAdded:
		return "Entered filter", e.From, ColorStatusDone
	case jira.ChangeRemoved:
		return "Left filter", "Last status: " + firstNonEmptyStr(e.From, "unknown"), widget.RGBA8(248, 113, 113, 255)
	}
	label = fieldLabel(e.Field)
	switch {
	case e.From == "" && e.To == "":
		detail = "edited"
	case e.From == "":
		detail = "set to " + e.To
	case e.To == "":
		detail = "cleared (was " + e.From + ")"
	default:
		detail = arrow(e.From, e.To)
	}
	return label, detail, widget.RGBA8(125, 211, 252, 255)
}

func arrow(from, to string) string {
	return firstNonEmptyStr(from, "–") + " → " + firstNonEmptyStr(to, "–")
}

// fieldLabel capitalises Jira's lower-case system field names (e.g. "assignee").
func fieldLabel(field string) string {
	if field == "" {
		return "Field"
	}
	r := []rune(field)
	return strings.ToUpper(string(r[0])) + string(r[1:])
}

// drawToggleButton draws a small on/off pill such as "Hide mine".
func drawToggleButton(canvas widget.Canvas, r geometry.Rect, label string, on bool) {
	bg := widget.RGBA8(38, 48, 68, 240)
	fg := widget.RGBA8(200, 212, 230, 255)
	if on {
		bg = widget.RGBA8(30, 90, 130, 250)
		fg = widget.RGBA8(240, 248, 255, 255)
	}
	canvas.DrawRoundRect(r, bg, 6)
	canvas.StrokeRoundRect(r, widget.RGBA8(255, 255, 255, 60), 6, 1.0)
	mark := "○ "
	if on {
		mark = "● "
	}
	canvas.DrawText(mark+label, r, 9, fg, true, widget.TextAlignCenter)
}

// drawBoardTabs draws the Board / Changes switch in the overlay header.
func drawBoardTabs(canvas widget.Canvas, l boardLayout, active, unread int) {
	labels := [2]string{"Board", "Changes"}
	for i, rect := range l.tabs {
		bg := widget.RGBA8(30, 40, 58, 230)
		fg := widget.RGBA8(170, 184, 206, 255)
		if i == active {
			bg = widget.RGBA8(54, 70, 98, 250)
			fg = widget.RGBA8(245, 250, 255, 255)
		}
		canvas.DrawRoundRect(rect, bg, 7)
		canvas.StrokeRoundRect(rect, widget.RGBA8(255, 255, 255, 50), 7, 1.0)
		label := labels[i]
		if i == boardTabChanges && unread > 0 {
			canvas.DrawText(label, geometry.NewRect(rect.Min.X+10, rect.Min.Y, rect.Width()-36, rect.Height()), 11, fg, true, widget.TextAlignLeft)
			badge := geometry.NewRect(rect.Max.X-28, rect.Min.Y+5, 22, rect.Height()-10)
			canvas.DrawRoundRect(badge, colorChangeUnread, 7)
			canvas.DrawText(unreadLabel(unread), badge, 8, widget.RGBA8(10, 20, 30, 255), true, widget.TextAlignCenter)
			continue
		}
		canvas.DrawText(label, rect, 11, fg, true, widget.TextAlignCenter)
	}
}

func unreadLabel(n int) string {
	if n > 99 {
		return "99+"
	}
	return fmt.Sprintf("%d", n)
}
