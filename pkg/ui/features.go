package ui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"jira-quick-access/pkg/jira"
	"jira-quick-access/pkg/update"
	"jira-quick-access/pkg/window"
)

const (
	// Status changes of tickets the user had open in the panel this recently are
	// most likely their own and are not announced.
	recentlyViewedWindow = 15 * time.Minute
	// More changes than this in one refresh are summarised in a single notification.
	maxSingleNotifications = 3
	updateCheckInterval    = 12 * time.Hour
)

// branchName builds a Git branch name such as "feature/OPS-12-fix-login-timeout".
func branchName(prefix string, iss jira.Issue) string {
	if iss.Key == "" {
		return ""
	}
	if prefix == "" {
		prefix = "feature/"
	}
	const maxSlug = 40
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(iss.Summary) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if len(slug) > maxSlug {
		// Cut at a word boundary when one is reasonably close.
		cut := strings.LastIndexByte(slug[:maxSlug+1], '-')
		if cut < maxSlug/2 {
			cut = maxSlug
		}
		slug = strings.TrimRight(slug[:cut], "-")
	}
	if slug == "" {
		return prefix + iss.Key
	}
	return prefix + iss.Key + "-" + slug
}

// ticketURL returns the browse link of an issue, or "" without a known site.
func ticketURL(iss jira.Issue, fallbackBase string) string {
	base := iss.BaseURL
	if base == "" {
		base = fallbackBase
	}
	if base == "" {
		return ""
	}
	return fmt.Sprintf("%s/browse/%s", strings.TrimRight(base, "/"), iss.Key)
}

// ---------------------------------------------------------------------------
// Global hotkey
// ---------------------------------------------------------------------------

// HandleGlobalHotKey toggles the ticket list from anywhere: it opens the fan with
// search focused, or collapses the HUD if it is already open. Runs on the UI thread.
func (v *AppView) HandleGlobalHotKey() {
	v.mu.Lock()
	st := v.state
	v.mu.Unlock()

	if st != window.StateRest {
		v.SetState(window.StateRest)
		return
	}
	v.SetState(window.StateFan)
	v.mu.Lock()
	v.searchActive = true
	v.mu.Unlock()
	window.ActivateApp()
	v.MarkNeedsLayout()
}

// SetGlobalHotKeyEnabled turns the ⌥⌘J shortcut on or off and saves the choice.
func (v *AppView) SetGlobalHotKeyEnabled(enabled bool) {
	v.mu.Lock()
	v.config.GlobalHotKey = enabled
	cfg := v.config.Clone()
	v.mu.Unlock()
	window.SetGlobalHotKey(enabled)
	_ = jira.SaveConfig(cfg)
	v.MarkNeedsLayout()
}

// ---------------------------------------------------------------------------
// Launch at login
// ---------------------------------------------------------------------------

func (v *AppView) refreshLoginItemStatus() {
	status := window.LaunchAtLoginStatus()
	v.mu.Lock()
	v.loginItem = status
	v.mu.Unlock()
}

func (v *AppView) toggleLaunchAtLogin() {
	v.mu.Lock()
	current := v.loginItem
	v.mu.Unlock()

	var msg string
	switch current {
	case window.LoginItemUnsupported:
		msg = "Launch at login needs macOS 13 and the installed app."
	default:
		enable := current == window.LoginItemDisabled
		if err := window.SetLaunchAtLogin(enable); err != nil {
			msg = "Launch at login: " + err.Error()
		}
	}
	v.refreshLoginItemStatus()

	v.mu.Lock()
	if msg == "" && v.loginItem == window.LoginItemNeedsApproval {
		msg = "Allow Jira Quick Access in System Settings › General › Login Items."
	}
	v.statusMsg = msg
	v.mu.Unlock()
	v.MarkNeedsLayout()
}

// ---------------------------------------------------------------------------
// Ticket order
// ---------------------------------------------------------------------------

// orderIssues applies the configured order. The Jira order groups tickets by
// instance in configuration order and keeps Jira's order within each instance.
func orderIssues(issues []jira.Issue, instances []jira.InstanceConfig, mode string) {
	if mode != jira.SortJira {
		jira.SortIssues(issues, mode)
		return
	}
	rank := make(map[string]int, len(instances))
	for i, inst := range instances {
		rank[inst.ID] = i
	}
	pos := func(iss jira.Issue) int {
		if r, ok := rank[iss.InstanceID]; ok {
			return r
		}
		return len(instances)
	}
	slices.SortStableFunc(issues, func(a, b jira.Issue) int { return pos(a) - pos(b) })
}

// SetSortMode changes the ticket order, keeping the selected ticket selected.
func (v *AppView) SetSortMode(mode string) {
	v.mu.Lock()
	v.config.SortMode = mode
	cfg := v.config.Clone()
	var active *jira.Issue
	if v.activeIdx >= 0 && v.activeIdx < len(v.issues) {
		a := v.issues[v.activeIdx]
		active = &a
	}
	issues := slices.Clone(v.issues)
	orderIssues(issues, v.config.Instances, mode)
	v.issues = issues
	if active != nil {
		if idx := indexOfIssue(issues, *active); idx >= 0 {
			v.activeIdx = idx
		}
	}
	v.invalidateFilterCacheLocked()
	v.mu.Unlock()
	_ = jira.SaveConfig(cfg)
	v.MarkNeedsLayout()
}

// ---------------------------------------------------------------------------
// Refresh results: partial failures and change notifications
// ---------------------------------------------------------------------------

type issueChange struct {
	issue     jira.Issue
	isNew     bool
	oldStatus string
}

// diffIssues lists tickets that are new or changed status. Only instances in
// comparable (loaded completely both times) are compared, so a site that just
// recovered from an error or was just added does not report all its tickets.
func diffIssues(prev, next []jira.Issue, comparable map[string]bool) []issueChange {
	var changes []issueChange
	for _, iss := range next {
		if !comparable[iss.InstanceID] {
			continue
		}
		idx := indexOfIssue(prev, iss)
		switch {
		case idx < 0:
			changes = append(changes, issueChange{issue: iss, isNew: true})
		case prev[idx].Status.Name != iss.Status.Name:
			changes = append(changes, issueChange{issue: iss, oldStatus: prev[idx].Status.Name})
		}
	}
	return changes
}

func issueID(iss jira.Issue) string {
	return iss.InstanceID + "/" + iss.Key
}

// applyFetchResult installs a refresh result. Tickets of instances that failed
// this time are kept from the previous refresh, so one unreachable site does not
// empty its part of the list. Called with v.mu held.
func (v *AppView) applyFetchResultLocked(res jira.FetchResult) (changes []issueChange, newFailures []string) {
	for id, err := range res.Failed {
		if _, known := v.instErrors[id]; !known {
			newFailures = append(newFailures, fmt.Sprintf("%s: %v", v.instanceNameLocked(id), err))
		}
	}
	slices.Sort(newFailures)
	v.instErrors = make(map[string]string, len(res.Failed))
	for id, err := range res.Failed {
		v.instErrors[id] = err.Error()
	}

	merged := slices.Clone(res.Issues)
	for _, iss := range v.issues {
		if _, failed := res.Failed[iss.InstanceID]; failed {
			merged = append(merged, iss)
		}
	}
	orderIssues(merged, v.config.Instances, v.config.SortMode)

	if v.loadedInstances != nil && !v.config.DemoMode {
		comparable := map[string]bool{}
		for id := range res.Loaded {
			if v.loadedInstances[id] {
				comparable[id] = true
			}
		}
		for _, c := range diffIssues(v.issues, merged, comparable) {
			if !c.isNew && time.Since(v.viewedAt[issueID(c.issue)]) < recentlyViewedWindow {
				continue
			}
			changes = append(changes, c)
		}
	}
	// Instances that failed keep their earlier baseline for the next comparison.
	loaded := map[string]bool{}
	for id := range res.Loaded {
		loaded[id] = true
	}
	for id := range res.Failed {
		if v.loadedInstances[id] {
			loaded[id] = true
		}
	}
	v.loadedInstances = loaded

	// Jira reorders results (e.g. ORDER BY updated), so keep the selection on the
	// same ticket rather than the same list position.
	newIdx := 0
	if v.activeIdx >= 0 && v.activeIdx < len(v.issues) {
		if idx := indexOfIssue(merged, v.issues[v.activeIdx]); idx >= 0 {
			newIdx = idx
		}
	}
	v.issues = merged
	v.activeIdx = newIdx
	if len(merged) > 0 {
		v.activeTheme = GetTicketTheme(v.activeIdx)
	}
	return changes, newFailures
}

func (v *AppView) instanceNameLocked(id string) string {
	for _, inst := range v.config.Instances {
		if inst.ID == id {
			return inst.Name
		}
	}
	return id
}

// notifyChanges posts notifications for changes, summarising large batches.
func notifyChanges(changes []issueChange) {
	if len(changes) == 0 {
		return
	}
	if len(changes) > maxSingleNotifications {
		keys := make([]string, 0, maxSingleNotifications)
		for _, c := range changes[:maxSingleNotifications] {
			keys = append(keys, c.issue.Key)
		}
		body := strings.Join(keys, ", ") + fmt.Sprintf(" and %d more", len(changes)-maxSingleNotifications)
		first := changes[0].issue
		window.Notify(fmt.Sprintf("%d tickets changed", len(changes)), body, first.Key, first.InstanceID)
		return
	}
	for _, c := range changes {
		iss := c.issue
		if c.isNew {
			window.Notify("New ticket · "+iss.Key, iss.Summary, iss.Key, iss.InstanceID)
		} else {
			window.Notify(fmt.Sprintf("%s → %s", iss.Key, iss.Status.Name),
				fmt.Sprintf("%s (was %s)", iss.Summary, c.oldStatus), iss.Key, iss.InstanceID)
		}
	}
}

// EnableNotifications requests permission and opens clicked tickets in the panel.
func (v *AppView) EnableNotifications() {
	window.InitNotifications(func(issueKey, instanceID string) {
		v.PostToUI(func() { v.OpenIssue(issueKey, instanceID) })
	})
}

// SetNotificationsEnabled turns change notifications on or off and saves the choice.
func (v *AppView) SetNotificationsEnabled(enabled bool) {
	v.mu.Lock()
	v.config.Notifications = enabled
	cfg := v.config.Clone()
	v.mu.Unlock()
	if enabled {
		v.EnableNotifications()
	}
	_ = jira.SaveConfig(cfg)
	v.MarkNeedsLayout()
}

// OpenIssue shows a ticket in the panel, switching the instance filter and
// clearing the search so it is part of the visible list.
func (v *AppView) OpenIssue(issueKey, instanceID string) {
	v.mu.Lock()
	idx := indexOfIssue(v.issues, jira.Issue{Key: issueKey, InstanceID: instanceID})
	if idx >= 0 {
		for i, inst := range v.config.Instances {
			if inst.ID == instanceID {
				v.activeInstIdx = i
			}
		}
		v.searchQuery, v.searchActive = "", false
		v.invalidateFilterCacheLocked()
	}
	v.mu.Unlock()
	if idx < 0 {
		return
	}
	v.Expand(idx)
	window.ActivateApp()
}

// ---------------------------------------------------------------------------
// Update check
// ---------------------------------------------------------------------------

// StartUpdateChecks looks for a newer release shortly after start and then twice
// a day, while the update check is enabled.
func (v *AppView) StartUpdateChecks(checker update.Checker) {
	v.mu.Lock()
	if v.ctx == nil || v.ctx.Err() != nil {
		v.mu.Unlock()
		return
	}
	v.updateChecker = checker
	v.wg.Add(1)
	v.mu.Unlock()

	go func() {
		defer v.wg.Done()
		timer := time.NewTimer(10 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-v.ctx.Done():
				return
			case <-timer.C:
			}
			v.mu.Lock()
			enabled := v.config.UpdateCheck
			v.mu.Unlock()
			if enabled {
				v.checkForUpdate(false)
			}
			timer.Reset(updateCheckInterval)
		}
	}()
}

// checkForUpdate queries GitHub once. manual reports the result in the settings.
func (v *AppView) checkForUpdate(manual bool) {
	v.mu.Lock()
	checker, current := v.updateChecker, v.version
	v.mu.Unlock()

	ctx, cancel := context.WithTimeout(v.ctx, 20*time.Second)
	defer cancel()
	rel, err := checker.Latest(ctx)
	if v.ctx.Err() != nil {
		return
	}

	v.mu.Lock()
	announce := false
	switch {
	case err != nil:
		if manual {
			v.statusMsg = "Update check failed: " + err.Error()
		}
	case update.IsNewer(rel.Version, current):
		announce = v.update.Version != rel.Version
		v.update = rel
		if manual {
			v.statusMsg = fmt.Sprintf("Version %s is available.", rel.Version)
		}
	default:
		v.update = update.Release{}
		if manual {
			v.statusMsg = fmt.Sprintf("Jira Quick Access %s is up to date.", current)
		}
	}
	if announce {
		v.toast = NewToast(fmt.Sprintf("Update %s available — see Settings", rel.Version))
	}
	v.mu.Unlock()
	v.postToUI(v.MarkNeedsLayout)
}

// checkForUpdateNow runs a manual check from the settings without blocking the UI.
func (v *AppView) checkForUpdateNow() {
	v.mu.Lock()
	if v.ctx == nil || v.ctx.Err() != nil {
		v.mu.Unlock()
		return
	}
	v.statusMsg = "Checking for updates..."
	v.wg.Add(1)
	v.mu.Unlock()
	v.MarkNeedsLayout()
	go func() {
		defer v.wg.Done()
		v.checkForUpdate(true)
	}()
}

// SetUpdateCheckEnabled turns the daily release check on or off and saves the choice.
func (v *AppView) SetUpdateCheckEnabled(enabled bool) {
	v.mu.Lock()
	v.config.UpdateCheck = enabled
	if !enabled {
		v.update = update.Release{}
	}
	cfg := v.config.Clone()
	v.mu.Unlock()
	_ = jira.SaveConfig(cfg)
	v.MarkNeedsLayout()
}
