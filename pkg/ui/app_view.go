package ui

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"jira-quick-access/pkg/jira"
	"jira-quick-access/pkg/window"

	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/widget"
)

// AppView implements the 3-state edge architecture with multi-instance support & macOS Dock styling:
// 1. Rest: Sleek discrete macOS Dock capsule with separate instance counts & dividers (26x210)
// 2. Fan: Shingled vertical tabs down the edge with instant search & scroll indicator (Hover)
// 3. Expanded: Full floating card / native mobile webview level with its tab (Click)
type AppView struct {
	widget.WidgetBase
	config       jira.Config
	client       *jira.Client
	issues       []jira.Issue
	history      []jira.TransitionHistory
	activeIdx    int
	activeTheme  CardTheme
	state        window.WindowState
	showSettings bool
	searchQuery  string
	searchActive bool
	toast        ToastNotification
	lastHover    time.Time
	scrollY      float32
	mu           sync.Mutex

	// Multi-instance filtering & settings tracking
	activeInstIdx   int // Currently viewed instance filter in Fan & Expanded states (0..n-1)
	selectedInstIdx int // Currently edited instance in Settings overlay

	// Active instance field buffers (5 fields)
	nameVal     string
	urlVal      string
	emailVal    string
	tokenVal    string
	jqlVal      string
	intervalVal string
	demoMode    bool
	debugMode   bool
	activeField int       // 1..5
	cursorPos   int       // Cursor position inside active field
	selectAll   bool      // If true, all text in active field is selected
	lastModTime time.Time // Timestamp of last modifier action to prevent duplicate character insertion
	statusMsg   string

	// Callbacks
	onRedraw func()
}

func NewAppView(
	cfg jira.Config,
	client *jira.Client,
	onRedraw func(),
) *AppView {
	cfg.EnsureInstances()
	if cfg.Instances[0].APIToken == "" || cfg.Instances[0].Email == "" {
		jira.LoadFromDotEnv(&cfg)
	}

	v := &AppView{
		config:          cfg,
		client:          client,
		activeIdx:       0,
		activeTheme:     ThemeMint,
		state:           window.StateRest,
		showSettings:    false,
		activeInstIdx:   0,
		selectedInstIdx: 0,
		demoMode:        cfg.DemoMode,
		debugMode:       cfg.DebugMode,
		intervalVal:     fmt.Sprintf("%d", cfg.PollInterval),
		onRedraw:        onRedraw,
	}

	v.loadInstanceFields(0)

	v.SetVisible(true)
	v.SetEnabled(true)

	// Background timer to collapse Fan state back to Rest when inactive
	go func() {
		for {
			time.Sleep(100 * time.Millisecond)
			v.mu.Lock()
			st := v.state
			lastH := v.lastHover
			searchAct := v.searchActive || v.searchQuery != ""
			v.mu.Unlock()

			if st == window.StateFan && !searchAct && !lastH.IsZero() && time.Since(lastH) > 380*time.Millisecond {
				v.SetState(window.StateRest)
			}
		}
	}()

	// Initial fetch
	v.RefreshIssues()

	return v
}

func (v *AppView) loadInstanceFields(idx int) {
	if idx < 0 || idx >= len(v.config.Instances) {
		return
	}
	v.selectedInstIdx = idx
	inst := v.config.Instances[idx]
	v.nameVal = inst.Name
	v.urlVal = inst.BaseURL
	v.emailVal = inst.Email
	v.tokenVal = inst.APIToken
	v.jqlVal = inst.JQLQuery
	v.activeField = 0
	v.selectAll = false
}

func (v *AppView) saveCurrentInstanceFields() {
	if v.selectedInstIdx < 0 || v.selectedInstIdx >= len(v.config.Instances) {
		return
	}
	v.config.Instances[v.selectedInstIdx].Name = strings.TrimSpace(v.nameVal)
	v.config.Instances[v.selectedInstIdx].BaseURL = strings.TrimSpace(v.urlVal)
	v.config.Instances[v.selectedInstIdx].Email = strings.TrimSpace(v.emailVal)
	v.config.Instances[v.selectedInstIdx].APIToken = sanitizeToken(v.tokenVal)
	v.config.Instances[v.selectedInstIdx].JQLQuery = strings.TrimSpace(v.jqlVal)
}

func isIssueForInstance(iss jira.Issue, inst jira.InstanceConfig, instIdx int) bool {
	if inst.ID != "" && iss.InstanceID == inst.ID {
		return true
	}
	if inst.BaseURL != "" && iss.BaseURL != "" {
		if strings.EqualFold(strings.TrimRight(inst.BaseURL, "/"), strings.TrimRight(iss.BaseURL, "/")) {
			return true
		}
	}
	if inst.Name != "" && iss.InstanceName != "" {
		if strings.EqualFold(inst.Name, iss.InstanceName) {
			return true
		}
	}
	if iss.InstanceID == "" && iss.InstanceName == "" && instIdx == 0 {
		return true
	}
	return false
}

func (v *AppView) getFilteredIssues() []jira.Issue {
	v.mu.Lock()
	defer v.mu.Unlock()

	var base []jira.Issue
	if v.activeInstIdx >= 0 && v.activeInstIdx < len(v.config.Instances) {
		targetInst := v.config.Instances[v.activeInstIdx]
		for _, iss := range v.issues {
			if isIssueForInstance(iss, targetInst, v.activeInstIdx) {
				base = append(base, iss)
			}
		}
	} else {
		base = v.issues
	}

	if v.searchQuery == "" {
		return base
	}

	q := strings.ToLower(v.searchQuery)
	var res []jira.Issue
	for _, iss := range base {
		if strings.Contains(strings.ToLower(iss.Key), q) || strings.Contains(strings.ToLower(iss.Summary), q) || strings.Contains(strings.ToLower(iss.Status.Name), q) {
			res = append(res, iss)
		}
	}
	return res
}

func (v *AppView) computeSize(st window.WindowState) (int, int) {
	switch st {
	case window.StateRest:
		return 26, 210

	case window.StateFan:
		issues := v.getFilteredIssues()
		issueCount := len(issues)
		if issueCount == 0 {
			issueCount = 2
		}
		h := issueCount*58 + 116
		if h > 660 {
			h = 660
		}
		if h < 260 {
			h = 260
		}
		return 112, h

	case window.StateExpanded:
		return 760, 560
	}
	return 26, 210
}

func (v *AppView) SetState(newState window.WindowState) {
	v.mu.Lock()
	v.state = newState
	if newState == window.StateFan {
		v.lastHover = time.Now()
	} else {
		v.lastHover = time.Time{}
		v.searchActive = false
	}
	showSettings := v.showSettings
	v.mu.Unlock()

	w, h := v.computeSize(newState)

	if window.DefaultManager != nil {
		window.DefaultManager.SetState(newState, w, h)
	}

	if newState == window.StateExpanded && !showSettings {
		window.SetMobileWebViewVisible(true, w, h)
		v.loadActiveTicketInMobileView()
	} else {
		window.SetMobileWebViewVisible(false, w, h)
	}

	if v.onRedraw != nil {
		v.onRedraw()
	}
	v.MarkNeedsLayout()
}

func (v *AppView) loadActiveTicketInMobileView() {
	v.mu.Lock()
	issues := v.issues
	activeIdx := v.activeIdx
	cfg := v.config
	v.mu.Unlock()

	if len(issues) == 0 || activeIdx >= len(issues) {
		return
	}

	iss := issues[activeIdx]

	if !cfg.DemoMode && (iss.BaseURL != "" || cfg.BaseURL != "") {
		baseURL := iss.BaseURL
		if baseURL == "" {
			baseURL = cfg.BaseURL
		}
		url := fmt.Sprintf("%s/browse/%s", strings.TrimRight(baseURL, "/"), iss.Key)
		window.LoadMobileTicketView(url)
	} else {
		html := generateMobileTicketHTML(iss)
		window.LoadMobileTicketHTML(html, "")
	}
}

func generateMobileTicketHTML(iss jira.Issue) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0, maximum-scale=1.0, user-scalable=no">
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; font-family: -apple-system, BlinkMacSystemFont, "SF Pro Text", "Segoe UI", Roboto, sans-serif; }
  body {
    background: #151b28;
    color: #f1f5f9;
    padding: 20px;
    height: 100vh;
    overflow-y: auto;
  }
  .header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 16px; }
  .key-badge { font-weight: 700; font-size: 16px; color: #38bdf8; letter-spacing: 0.5px; }
  .status-pill {
    font-size: 11px; font-weight: 600; padding: 4px 10px; border-radius: 12px;
    background: rgba(59, 130, 246, 0.25); color: #60a5fa; border: 1px solid rgba(96, 165, 250, 0.4);
  }
  .title { font-size: 17px; font-weight: 600; line-height: 1.4; color: #ffffff; margin-bottom: 16px; }
  .meta-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; margin-bottom: 18px; }
  .meta-item {
    background: rgba(255, 255, 255, 0.07); padding: 10px 14px; border-radius: 8px;
    border: 1px solid rgba(255, 255, 255, 0.1);
  }
  .meta-label { font-size: 10px; color: #94a3b8; text-transform: uppercase; margin-bottom: 3px; }
  .meta-val { font-size: 13px; font-weight: 500; color: #e2e8f0; }
  .desc-box {
    background: rgba(255, 255, 255, 0.05); border-radius: 10px; padding: 16px;
    border: 1px solid rgba(255, 255, 255, 0.08); margin-bottom: 20px; line-height: 1.5; font-size: 13px; color: #cbd5e1;
  }
</style>
</head>
<body>
  <div class="header">
    <div class="key-badge">● %s [%s]</div>
    <div class="status-pill">%s</div>
  </div>
  <div class="title">%s</div>
  <div class="meta-grid">
    <div class="meta-item">
      <div class="meta-label">Priority</div>
      <div class="meta-val">%s</div>
    </div>
    <div class="meta-item">
      <div class="meta-label">Issue Type</div>
      <div class="meta-val">%s</div>
    </div>
  </div>
  <div class="desc-box">
    <strong>Mobile Jira Overview</strong><br>
    Synced via Jira REST API & WebKit.
  </div>
</body>
</html>`,
		iss.Key,
		iss.InstanceName,
		iss.Status.Name,
		iss.Summary,
		iss.Priority.Name,
		iss.IssueType.Name,
	)
}

func (v *AppView) Expand(tabIdx int) {
	v.mu.Lock()
	v.activeIdx = tabIdx
	v.activeTheme = GetTicketTheme(tabIdx)
	v.showSettings = false
	v.mu.Unlock()
	v.SetState(window.StateExpanded)
}

func (v *AppView) OpenSettings() {
	v.mu.Lock()
	v.showSettings = true
	v.loadInstanceFields(v.selectedInstIdx)
	v.statusMsg = ""
	v.mu.Unlock()
	v.SetState(window.StateExpanded)
}

func (v *AppView) ToggleSettings() {
	v.mu.Lock()
	v.showSettings = !v.showSettings
	v.loadInstanceFields(v.selectedInstIdx)
	v.statusMsg = ""
	v.mu.Unlock()
	v.SetState(window.StateExpanded)
}

func (v *AppView) RefreshIssues() {
	go func() {
		issues, err := v.client.FetchAssignedIssues(context.Background())
		v.mu.Lock()
		defer v.mu.Unlock()

		if err != nil {
			v.toast = NewToast(fmt.Sprintf("Sync error: %v", err))
		} else {
			v.issues = issues
			if v.activeIdx >= len(issues) {
				v.activeIdx = 0
			}
			if len(issues) > 0 {
				v.activeTheme = GetTicketTheme(v.activeIdx)
			}
		}
		if v.onRedraw != nil {
			v.onRedraw()
		}
		v.MarkNeedsLayout()
	}()
}

func (v *AppView) Layout(ctx widget.Context, constraints geometry.Constraints) geometry.Size {
	v.mu.Lock()
	st := v.state
	v.mu.Unlock()

	w, h := v.computeSize(st)
	b := geometry.NewRect(0, 0, float32(w), float32(h))
	v.SetBounds(b)
	return constraints.Constrain(geometry.Sz(float32(w), float32(h)))
}

func measureTextWidth(text string, fontSize float32) float32 {
	if text == "" {
		return 0
	}
	var total float32
	scale := fontSize / 11.0
	for _, r := range text {
		var w float32
		switch r {
		case 'i', 'l', 'j', '1', ':', '.', '/', '\'', '|', '!':
			w = 3.2
		case 'r', 't', 'f', 'I', ' ', '-', '(', ')':
			w = 4.4
		case 'm', 'w', 'M', 'W', '@', '%', '&', '#':
			w = 8.8
		case 'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'J', 'K', 'L', 'N', 'O', 'P', 'Q', 'R', 'S', 'T', 'U', 'V', 'X', 'Y', 'Z':
			w = 7.2
		case '•':
			w = 5.2
		default:
			w = 5.8
		}
		total += w * scale
	}
	return total
}

func getCursorIndexFromX(text string, fontSize float32, clickX float32) int {
	if clickX <= 0 || text == "" {
		return 0
	}
	bestIdx := 0
	bestDiff := float32(9999)
	for i := 0; i <= len(text); i++ {
		curWidth := measureTextWidth(text[:i], fontSize)
		diff := clickX - curWidth
		if diff < 0 {
			diff = -diff
		}
		if diff < bestDiff {
			bestDiff = diff
			bestIdx = i
		}
	}
	return bestIdx
}

func (v *AppView) Draw(ctx widget.Context, canvas widget.Canvas) {
	b := v.Bounds()
	w := b.Width()
	h := b.Height()
	if w <= 0 {
		w = 26
	}
	if h <= 0 {
		h = 210
	}

	v.mu.Lock()
	allIssues := v.issues
	activeIdx := v.activeIdx
	st := v.state
	showSettings := v.showSettings
	toast := v.toast
	scrollY := v.scrollY
	searchQ := v.searchQuery
	searchAct := v.searchActive
	activeInstIdx := v.activeInstIdx
	instances := v.config.Instances
	v.mu.Unlock()

	filteredIssues := v.getFilteredIssues()

	// =========================================================================
	// 1. STATE REST: Discrete macOS Dock Capsule with Status Percentage Edge Indicator
	// =========================================================================
	if st == window.StateRest {
		pillRect := geometry.NewRect(b.Min.X, b.Min.Y, w, h)
		// Deep obsidian glass backdrop
		canvas.DrawRoundRect(pillRect, widget.RGBA8(16, 22, 34, 245), 13)
		canvas.StrokeRoundRect(pillRect, widget.RGBA8(255, 255, 255, 45), 13, 1.0)
		// Top specular highlight
		canvas.DrawLine(geometry.Pt(b.Min.X+5, b.Min.Y+2), geometry.Pt(b.Min.X+w-5, b.Min.Y+2), widget.RGBA8(255, 255, 255, 110), 1.0)

		instCount := len(instances)
		if instCount == 0 {
			instCount = 1
		}
		usableH := h - 34 // Reserve 34px for bottom divider and settings icon
		instSectionH := usableH / float32(instCount)

		for idx, inst := range instances {
			secY := b.Min.Y + float32(5+float32(idx)*instSectionH)

			var instIssues []jira.Issue
			for _, iss := range allIssues {
				if isIssueForInstance(iss, inst, idx) {
					instIssues = append(instIssues, iss)
				}
			}
			count := len(instIssues)

			// Distinct colors per instance
			beaconColor := widget.RGBA8(56, 189, 248, 255) // Cyan (Avono)
			beaconGlow := widget.RGBA8(56, 189, 248, 50)
			inProgColor := widget.RGBA8(56, 189, 248, 255) // Light Cyan
			todoColor := widget.RGBA8(14, 116, 144, 255)   // Darker Blue / Cyan
			badgeBg := widget.RGBA8(24, 34, 52, 240)
			badgeBorder := widget.RGBA8(56, 189, 248, 120)

			if idx == 1 {
				beaconColor = widget.RGBA8(168, 85, 247, 255) // Purple (Sandbox)
				beaconGlow = widget.RGBA8(168, 85, 247, 50)
				inProgColor = widget.RGBA8(192, 132, 252, 255) // Light Purple
				todoColor = widget.RGBA8(107, 33, 168, 255)    // Darker Violet
				badgeBg = widget.RGBA8(38, 26, 56, 240)
				badgeBorder = widget.RGBA8(168, 85, 247, 120)
			} else if idx == 2 {
				beaconColor = widget.RGBA8(234, 179, 8, 255) // Amber (Sandbox)
				beaconGlow = widget.RGBA8(234, 179, 8, 50)
				inProgColor = widget.RGBA8(250, 204, 21, 255) // Light Yellow/Amber
				todoColor = widget.RGBA8(161, 98, 7, 255)     // Darker Amber
				badgeBg = widget.RGBA8(48, 38, 20, 240)
				badgeBorder = widget.RGBA8(234, 179, 8, 120)
			} else if idx > 2 {
				beaconColor = widget.RGBA8(34, 197, 94, 255) // Emerald
				beaconGlow = widget.RGBA8(34, 197, 94, 50)
				inProgColor = widget.RGBA8(74, 222, 128, 255)
				todoColor = widget.RGBA8(21, 128, 61, 255)
				badgeBg = widget.RGBA8(20, 44, 30, 240)
				badgeBorder = widget.RGBA8(34, 197, 94, 120)
			}

			// Instance Beacon Core & Radiant Glow
			beaconCenter := geometry.Pt(b.Min.X+w/2, secY+7)
			canvas.DrawCircle(beaconCenter, 5.5, beaconGlow)
			canvas.DrawCircle(beaconCenter, 3.0, beaconColor)

			// Proportional Status Percentage Indicator on Left Edge
			lineTopY := secY + 3
			lineBottomY := secY + instSectionH - 4
			lineTotalH := lineBottomY - lineTopY

			if count == 0 {
				canvas.DrawLine(geometry.Pt(b.Min.X+1, lineTopY), geometry.Pt(b.Min.X+1, lineBottomY), beaconColor, 1.5)
			} else {
				inProgCount := 0
				todoCount := 0
				doneCount := 0
				for _, iss := range instIssues {
					cat := strings.ToLower(iss.Status.CategoryKey)
					name := strings.ToLower(iss.Status.Name)
					if cat == "indeterminate" || strings.Contains(name, "progress") || strings.Contains(name, "dev") {
						inProgCount++
					} else if cat == "done" || strings.Contains(name, "done") || strings.Contains(name, "closed") {
						doneCount++
					} else {
						todoCount++
					}
				}

				curY := lineTopY
				// In Progress segment (Lighter version of instance color)
				if inProgCount > 0 {
					segH := lineTotalH * (float32(inProgCount) / float32(count))
					canvas.DrawLine(geometry.Pt(b.Min.X+1, curY), geometry.Pt(b.Min.X+1, curY+segH), inProgColor, 2.0)
					curY += segH
				}
				// To Do segment (Darker version of instance color)
				if todoCount > 0 {
					segH := lineTotalH * (float32(todoCount) / float32(count))
					canvas.DrawLine(geometry.Pt(b.Min.X+1, curY), geometry.Pt(b.Min.X+1, curY+segH), todoColor, 2.0)
					curY += segH
				}
				// Done segment (Emerald Green)
				if doneCount > 0 {
					segH := lineTotalH * (float32(doneCount) / float32(count))
					canvas.DrawLine(geometry.Pt(b.Min.X+1, curY), geometry.Pt(b.Min.X+1, curY+segH), widget.RGBA8(34, 197, 94, 255), 2.0)
				}
			}

			// Compact Ticket Count Badge Pill
			countStr := fmt.Sprintf("%d", count)
			badgeW := w - 6
			badgeH := float32(17)
			countBox := geometry.NewRect(b.Min.X+3, secY+16, badgeW, badgeH)
			canvas.DrawRoundRect(countBox, badgeBg, 4)
			canvas.StrokeRoundRect(countBox, badgeBorder, 4, 1.0)
			canvas.DrawText(countStr, geometry.NewRect(b.Min.X+3, secY+17, badgeW, badgeH-2), 10, widget.RGBA8(240, 246, 255, 255), true, widget.TextAlignCenter)

			// Subtle Divider between instances
			if idx < len(instances)-1 {
				sepY := secY + instSectionH - 2
				canvas.DrawLine(geometry.Pt(b.Min.X+5, sepY), geometry.Pt(b.Min.X+w-5, sepY), widget.RGBA8(255, 255, 255, 30), 1.0)
			}
		}

		// Divider before settings
		dividerY := b.Min.Y + h - 28
		canvas.DrawLine(geometry.Pt(b.Min.X+5, dividerY), geometry.Pt(b.Min.X+w-5, dividerY), widget.RGBA8(255, 255, 255, 45), 1.0)

		// Settings Icon with clear visibility
		settingsCenter := geometry.Pt(b.Min.X+w/2, b.Min.Y+h-14)
		canvas.DrawCircle(settingsCenter, 6.0, widget.RGBA8(180, 200, 230, 40))
		canvas.DrawCircle(settingsCenter, 3.5, widget.RGBA8(200, 220, 245, 240))
		return
	}

	// =========================================================================
	// 2. STATE FAN: Vertical Dock Tabs with Instance Header & Search
	// =========================================================================
	if st == window.StateFan {
		railBackdrop := geometry.NewRect(b.Min.X, b.Min.Y, w, h)
		canvas.DrawRoundRect(railBackdrop, widget.RGBA8(24, 32, 48, 185), 14)
		canvas.StrokeRoundRect(railBackdrop, widget.RGBA8(255, 255, 255, 60), 14, 1.0)
		canvas.DrawLine(geometry.Pt(b.Min.X+6, b.Min.Y+2), geometry.Pt(b.Min.X+w-6, b.Min.Y+2), widget.RGBA8(255, 255, 255, 120), 1.0)

		// Top Instance Header Pill
		instName := "All Instances"
		instBeaconColor := widget.RGBA8(56, 189, 248, 240)
		if activeInstIdx >= 0 && activeInstIdx < len(instances) {
			instName = instances[activeInstIdx].Name
			if activeInstIdx == 1 {
				instBeaconColor = widget.RGBA8(168, 85, 247, 240)
			} else if activeInstIdx == 2 {
				instBeaconColor = widget.RGBA8(234, 179, 8, 240)
			} else if activeInstIdx > 2 {
				instBeaconColor = widget.RGBA8(34, 197, 94, 240)
			}
		}
		if len(instName) > 13 {
			instName = instName[:11] + ".."
		}

		instHeaderRect := geometry.NewRect(b.Min.X+6, b.Min.Y+7, w-12, 20)
		canvas.DrawRoundRect(instHeaderRect, widget.RGBA8(36, 46, 68, 220), 4)
		canvas.StrokeRoundRect(instHeaderRect, widget.RGBA8(255, 255, 255, 35), 4, 1.0)

		canvas.DrawCircle(geometry.Pt(b.Min.X+13, b.Min.Y+17), 3.0, instBeaconColor)
		canvas.DrawText(instName, geometry.NewRect(b.Min.X+18, b.Min.Y+11, w-24, 13), 9, widget.RGBA8(230, 240, 255, 255), true, widget.TextAlignCenter)

		// Search Bar
		searchRect := geometry.NewRect(b.Min.X+6, b.Min.Y+31, w-12, 22)
		searchBg := widget.RGBA8(14, 20, 30, 220)
		searchBorder := widget.RGBA8(255, 255, 255, 30)
		if searchAct || searchQ != "" {
			searchBg = widget.RGBA8(22, 32, 50, 240)
			searchBorder = ColorStatusToDo
		}
		canvas.DrawRoundRect(searchRect, searchBg, 5)
		canvas.StrokeRoundRect(searchRect, searchBorder, 5, 1.0)

		searchTxt := searchQ
		searchColor := widget.RGBA8(240, 245, 255, 255)
		if searchTxt == "" {
			searchTxt = "Search..."
			searchColor = widget.RGBA8(140, 155, 180, 255)
		}
		sTxtRect := geometry.NewRect(b.Min.X+10, b.Min.Y+35, w-20, 13)
		canvas.DrawText(searchTxt, sTxtRect, 9, searchColor, false, widget.TextAlignLeft)

		// Tab Area
		tabStartY := b.Min.Y + float32(58) - scrollY
		tabHeight := float32(48)
		tabGap := float32(6)

		if len(filteredIssues) == 0 {
			noRect := geometry.NewRect(b.Min.X+8, b.Min.Y+70, w-16, 30)
			canvas.DrawText("No tickets", noRect, 10, widget.RGBA8(148, 163, 184, 255), false, widget.TextAlignCenter)
		} else {
			for i, iss := range filteredIssues {
				tabTheme := GetTicketTheme(i)
				tabY := tabStartY + float32(i)*(tabHeight+tabGap)

				if tabY+tabHeight < b.Min.Y+56 || tabY > b.Min.Y+h-54 {
					continue
				}

				tabRect := geometry.NewRect(b.Min.X+6, tabY, w-12, tabHeight)

				canvas.DrawRoundRect(tabRect, tabTheme.Background, 8)
				canvas.StrokeRoundRect(tabRect, tabTheme.Border, 8, 1.0)

				// Tab Key
				keyRect := geometry.NewRect(b.Min.X+8, tabY+6, w-16, 15)
				canvas.DrawText(iss.Key, keyRect, 11, tabTheme.Foreground, true, widget.TextAlignCenter)

				// Tab Status
				shortStatus := iss.Status.Name
				if len(shortStatus) > 11 {
					shortStatus = shortStatus[:11]
				}
				statusRect := geometry.NewRect(b.Min.X+8, tabY+24, w-16, 14)
				canvas.DrawText(shortStatus, statusRect, 9, tabTheme.Secondary, false, widget.TextAlignCenter)
			}
		}

		// Scroll Indicator
		totalTabH := float32(len(filteredIssues)) * (tabHeight + tabGap)
		viewTabH := h - 114
		if totalTabH > viewTabH && totalTabH > 0 {
			scrollRatio := scrollY / (totalTabH - viewTabH)
			if scrollRatio < 0 {
				scrollRatio = 0
			}
			if scrollRatio > 1 {
				scrollRatio = 1
			}
			thumbH := float32(24)
			thumbY := b.Min.Y + float32(58) + scrollRatio*(viewTabH-thumbH)
			thumbRect := geometry.NewRect(b.Min.X+w-4, thumbY, 3, thumbH)
			canvas.DrawRoundRect(thumbRect, widget.RGBA8(255, 255, 255, 140), 1.5)
		}

		// Divider Line before Settings
		dividerY := b.Min.Y + h - 50
		canvas.DrawLine(geometry.Pt(b.Min.X+10, dividerY), geometry.Pt(b.Min.X+w-10, dividerY), widget.RGBA8(255, 255, 255, 60), 1.0)

		// Settings Tab
		settingsTabY := b.Min.Y + h - 42
		settingsTabRect := geometry.NewRect(b.Min.X+6, settingsTabY, w-12, 34)
		canvas.DrawRoundRect(settingsTabRect, widget.RGBA8(38, 48, 68, 230), 8)
		canvas.StrokeRoundRect(settingsTabRect, widget.RGBA8(255, 255, 255, 50), 8, 1.0)
		setTxtRect := geometry.NewRect(b.Min.X+8, settingsTabY+9, w-16, 16)
		canvas.DrawText("Settings", setTxtRect, 10, widget.RGBA8(230, 240, 255, 255), true, widget.TextAlignCenter)
		return
	}

	// =========================================================================
	// 3. STATE EXPANDED: Side Dock Column + Multi-Instance Settings Overlay
	// =========================================================================
	tabBarWidth := float32(104)
	cardAreaWidth := w - tabBarWidth - 14

	// Right-side Dock Shelf Column
	tabStartX := b.Min.X + w - tabBarWidth
	dockShelfRect := geometry.NewRect(tabStartX-2, b.Min.Y+8, tabBarWidth-4, h-16)
	canvas.DrawRoundRect(dockShelfRect, widget.RGBA8(24, 32, 48, 160), 12)
	canvas.StrokeRoundRect(dockShelfRect, widget.RGBA8(255, 255, 255, 55), 12, 1.0)
	canvas.DrawLine(geometry.Pt(tabStartX+4, b.Min.Y+10), geometry.Pt(b.Min.X+w-10, b.Min.Y+10), widget.RGBA8(255, 255, 255, 110), 1.0)

	// Draw side tabs inside the dock shelf with scroll support (Filtered to active instance)
	tabStartY := b.Min.Y + float32(16) - scrollY
	tabHeight := float32(50)
	tabGap := float32(7)

	var activeKey string
	if activeIdx >= 0 && activeIdx < len(allIssues) {
		activeKey = allIssues[activeIdx].Key
	}

	for i, iss := range filteredIssues {
		tabTheme := GetTicketTheme(i)
		tabY := tabStartY + float32(i)*(tabHeight+tabGap)

		if tabY+tabHeight < b.Min.Y+14 || tabY > b.Min.Y+h-58 {
			continue
		}

		isActive := (iss.Key == activeKey && !showSettings)
		tabWidth := tabBarWidth - 14
		tabX := tabStartX + 2
		if isActive {
			tabX = tabStartX
			tabWidth = tabBarWidth - 10
		}

		tabRect := geometry.NewRect(tabX, tabY, tabWidth, tabHeight)

		canvas.DrawRoundRect(tabRect, tabTheme.Background, 8)
		if isActive {
			canvas.StrokeRoundRect(tabRect, widget.RGBA8(255, 255, 255, 240), 8, 1.5)
		} else {
			canvas.StrokeRoundRect(tabRect, tabTheme.Border, 8, 1.0)
		}

		// Key & Status
		keyRect := geometry.NewRect(tabX+2, tabY+7, tabWidth-4, 15)
		canvas.DrawText(iss.Key, keyRect, 11, tabTheme.Foreground, true, widget.TextAlignCenter)

		shortStatus := iss.Status.Name
		if len(shortStatus) > 11 {
			shortStatus = shortStatus[:11]
		}
		statusRect := geometry.NewRect(tabX+2, tabY+26, tabWidth-4, 14)
		canvas.DrawText(shortStatus, statusRect, 9, tabTheme.Secondary, false, widget.TextAlignCenter)
	}

	// Scroll Indicator in Expanded Rail
	totalRailH := float32(len(filteredIssues)) * (tabHeight + tabGap)
	viewRailH := h - 76
	if totalRailH > viewRailH && totalRailH > 0 {
		scrollRatio := scrollY / (totalRailH - viewRailH)
		if scrollRatio < 0 {
			scrollRatio = 0
		}
		if scrollRatio > 1 {
			scrollRatio = 1
		}
		thumbH := float32(28)
		thumbY := b.Min.Y + float32(16) + scrollRatio*(viewRailH-thumbH)
		thumbRect := geometry.NewRect(b.Min.X+w-6, thumbY, 3, thumbH)
		canvas.DrawRoundRect(thumbRect, widget.RGBA8(255, 255, 255, 150), 1.5)
	}

	// Dock Divider Line
	dividerY := b.Min.Y + h - 56
	canvas.DrawLine(geometry.Pt(tabStartX+8, dividerY), geometry.Pt(b.Min.X+w-14, dividerY), widget.RGBA8(255, 255, 255, 60), 1.0)

	// Settings tab at bottom right
	settingsTabY := b.Min.Y + h - 48
	settingsTabRect := geometry.NewRect(tabStartX+2, settingsTabY, tabBarWidth-14, 34)
	settingsBg := widget.RGBA8(38, 48, 68, 220)
	if showSettings {
		settingsBg = widget.RGBA8(56, 72, 100, 230)
	}
	canvas.DrawRoundRect(settingsTabRect, settingsBg, 8)
	canvas.StrokeRoundRect(settingsTabRect, widget.RGBA8(255, 255, 255, 50), 8, 1.0)
	setTxtRect := geometry.NewRect(tabStartX+4, settingsTabY+9, tabBarWidth-18, 16)
	canvas.DrawText("Settings", setTxtRect, 10, widget.RGBA8(230, 240, 255, 255), true, widget.TextAlignCenter)

	// Settings Overlay (if active)
	if showSettings {
		cardRect := geometry.NewRect(b.Min.X+8, b.Min.Y+8, cardAreaWidth, h-16)
		v.drawSettingsOverlay(ctx, canvas, cardRect)
	}

	// Toast Notification
	if toast.IsActive() {
		msg := toast.Message
		toastW := measureTextWidth(msg, 11) + 32
		if toastW < 120 {
			toastW = 120
		}
		if toastW > w-40 {
			toastW = w - 40
		}
		toastH := float32(30)
		toastX := b.Min.X + (w-toastW)/2
		toastY := b.Min.Y + float32(20)
		if showSettings {
			toastY = b.Min.Y + h - 80
		}

		tRect := geometry.NewRect(toastX, toastY, toastW, toastH)
		canvas.DrawRoundRect(tRect, ColorToastBg, 15)
		canvas.StrokeRoundRect(tRect, ColorToastBorder, 15, 1.0)

		tTxtRect := geometry.NewRect(toastX+10, toastY+7, toastW-20, 16)
		canvas.DrawText(msg, tTxtRect, 11, widget.RGBA8(245, 250, 255, 255), true, widget.TextAlignCenter)
	}
}

func (v *AppView) drawSettingsOverlay(ctx widget.Context, canvas widget.Canvas, r geometry.Rect) {
	radius := float32(14)
	canvas.DrawRoundRect(r, widget.RGBA8(20, 24, 35, 245), radius)
	canvas.StrokeRoundRect(r, widget.RGBA8(255, 255, 255, 50), radius, 1.0)

	// 1. Header Title & Top Controls
	hdrRect := geometry.NewRect(r.Min.X+20, r.Min.Y+14, 250, 22)
	canvas.DrawText("Jira Instances & Credentials", hdrRect, 14, widget.RGBA8(245, 250, 255, 255), true, widget.TextAlignLeft)

	// Load .env button
	envRect := geometry.NewRect(r.Min.X+r.Width()-155, r.Min.Y+12, 75, 24)
	canvas.DrawRoundRect(envRect, widget.RGBA8(50, 60, 80, 255), 4)
	canvas.DrawText("Load .env", envRect, 10, widget.RGBA8(220, 235, 255, 255), false, widget.TextAlignCenter)

	// Close button
	closeRect := geometry.NewRect(r.Min.X+r.Width()-70, r.Min.Y+12, 50, 24)
	canvas.DrawRoundRect(closeRect, widget.RGBA8(40, 50, 70, 255), 4)
	canvas.DrawText("Close", closeRect, 10, widget.RGBA8(240, 245, 255, 255), false, widget.TextAlignCenter)

	// 2. Dedicated Row for Instance Tabs & Add Button
	tabRowY := r.Min.Y + 44
	instStartX := r.Min.X + 20
	tabW := float32(110)
	tabGap := float32(6)

	for idx, inst := range v.config.Instances {
		instTabX := instStartX + float32(idx)*(tabW+tabGap)
		instTabRect := geometry.NewRect(instTabX, tabRowY, tabW, 26)

		tabBg := widget.RGBA8(34, 42, 58, 255)
		tabBorder := widget.RGBA8(255, 255, 255, 30)
		if idx == v.selectedInstIdx {
			tabBg = widget.RGBA8(59, 130, 246, 220)
			tabBorder = widget.RGBA8(255, 255, 255, 160)
		}
		canvas.DrawRoundRect(instTabRect, tabBg, 5)
		canvas.StrokeRoundRect(instTabRect, tabBorder, 5, 1.0)

		name := inst.Name
		if name == "" {
			name = fmt.Sprintf("Inst %d", idx+1)
		}
		if len(name) > 14 {
			name = name[:12] + ".."
		}
		canvas.DrawText(name, geometry.NewRect(instTabX+4, tabRowY+6, tabW-8, 14), 10, widget.RGBA8(245, 250, 255, 255), true, widget.TextAlignCenter)
	}

	// ➕ Add Instance Button (always positioned immediately next to the last instance tab)
	addInstX := instStartX + float32(len(v.config.Instances))*(tabW+tabGap)
	addRect := geometry.NewRect(addInstX, tabRowY, 80, 26)
	canvas.DrawRoundRect(addRect, widget.RGBA8(40, 56, 78, 255), 5)
	canvas.StrokeRoundRect(addRect, widget.RGBA8(96, 165, 250, 100), 5, 1.0)
	canvas.DrawText("+ Add", addRect, 10, widget.RGBA8(210, 235, 255, 255), true, widget.TextAlignCenter)

	// Separator below instance tabs
	sepY := tabRowY + 34
	canvas.DrawLine(geometry.Pt(r.Min.X+20, sepY), geometry.Pt(r.Min.X+r.Width()-20, sepY), widget.RGBA8(255, 255, 255, 30), 1.0)

	// 3. 5 Interactive Fields for the selected instance
	labels := []string{
		"Instance Label / Name",
		"Jira Base URL (e.g. https://company.atlassian.net)",
		"User Email",
		"API Token (click to type or paste with ⌘V)",
		"Custom JQL Query",
	}
	rawVals := []string{v.nameVal, v.urlVal, v.emailVal, v.tokenVal, v.jqlVal}

	startY := sepY + 12
	blinkOn := (time.Now().UnixMilli()/500)%2 == 0

	for i, label := range labels {
		fIdx := i + 1
		y := startY + float32(i*45)
		isFocused := (v.activeField == fIdx)

		lblRect := geometry.NewRect(r.Min.X+20, y, r.Width()-40, 14)
		canvas.DrawText(label, lblRect, 10, widget.RGBA8(148, 163, 184, 255), false, widget.TextAlignLeft)

		inpRect := geometry.NewRect(r.Min.X+20, y+16, r.Width()-40, 24)
		bg := widget.RGBA8(14, 18, 26, 255)
		border := widget.RGBA8(255, 255, 255, 20)
		if isFocused {
			bg = widget.RGBA8(22, 28, 42, 255)
			border = ColorStatusToDo
		}
		canvas.DrawRoundRect(inpRect, bg, 4)
		canvas.StrokeRoundRect(inpRect, border, 4, 1.0)

		rawVal := rawVals[i]
		displayVal := rawVal
		if fIdx == 4 && !isFocused && len(rawVal) > 0 {
			displayVal = maskToken(rawVal)
		}

		textX := inpRect.Min.X + 8
		textY := inpRect.Min.Y + 5

		if displayVal == "" && !isFocused {
			vRect := geometry.NewRect(textX, textY, inpRect.Width()-16, 14)
			canvas.DrawText("(click to type or paste with ⌘V)", vRect, 11, widget.RGBA8(100, 116, 139, 255), false, widget.TextAlignLeft)
		} else {
			if isFocused && v.selectAll && len(displayVal) > 0 {
				selWidth := measureTextWidth(displayVal, 11) + 4
				if selWidth > inpRect.Width()-16 {
					selWidth = inpRect.Width() - 16
				}
				selRect := geometry.NewRect(textX-2, textY-1, selWidth, 16)
				canvas.DrawRoundRect(selRect, widget.RGBA8(59, 130, 246, 180), 2)
			}

			vRect := geometry.NewRect(textX, textY, inpRect.Width()-16, 14)
			canvas.DrawText(displayVal, vRect, 11, widget.RGBA8(240, 245, 255, 255), false, widget.TextAlignLeft)

			if isFocused && blinkOn && !v.selectAll {
				cp := v.cursorPos
				if cp > len(rawVal) {
					cp = len(rawVal)
				}
				cursorOffset := measureTextWidth(rawVal[:cp], 11)
				cursorX := textX + cursorOffset + 1.0
				if cursorX > inpRect.Max.X-6 {
					cursorX = inpRect.Max.X - 6
				}
				cursorTop := geometry.Pt(cursorX, inpRect.Min.Y+4)
				cursorBottom := geometry.Pt(cursorX, inpRect.Max.Y-4)
				canvas.DrawLine(cursorTop, cursorBottom, widget.RGBA8(255, 255, 255, 240), 1.5)
			}
		}
	}

	// Status Message
	if v.statusMsg != "" {
		stMsg := v.statusMsg
		if len(stMsg) > 75 {
			stMsg = stMsg[:72] + "..."
		}
		stRect := geometry.NewRect(r.Min.X+20, r.Min.Y+r.Height()-72, r.Width()-40, 16)
		canvas.DrawText(stMsg, stRect, 11, ColorStatusInProgress, false, widget.TextAlignLeft)
	}

	// 4. Bottom Action Row
	btnY := r.Min.Y + r.Height() - 44

	demoModeTxt := "Mode: Live Jira"
	if v.demoMode {
		demoModeTxt = "Mode: Demo Mock Data"
	}
	demoRect := geometry.NewRect(r.Min.X+20, btnY, 130, 28)
	canvas.DrawRoundRect(demoRect, widget.RGBA8(36, 44, 60, 255), 6)
	canvas.DrawText(demoModeTxt, demoRect, 10, widget.RGBA8(240, 245, 255, 255), false, widget.TextAlignCenter)

	debugTxt := "Debug: OFF"
	debugBg := widget.RGBA8(36, 44, 60, 255)
	if v.debugMode {
		debugTxt = "Debug: ON"
		debugBg = widget.RGBA8(70, 45, 95, 255)
	}
	debugRect := geometry.NewRect(r.Min.X+156, btnY, 86, 28)
	canvas.DrawRoundRect(debugRect, debugBg, 6)
	canvas.DrawText(debugTxt, debugRect, 10, widget.RGBA8(240, 245, 255, 255), false, widget.TextAlignCenter)

	testRect := geometry.NewRect(r.Min.X+248, btnY, 120, 28)
	canvas.DrawRoundRect(testRect, widget.RGBA8(36, 44, 60, 255), 6)
	canvas.DrawText("Test Connection", testRect, 10, widget.RGBA8(240, 245, 255, 255), false, widget.TextAlignCenter)

	// Delete Instance Button (if more than 1 instance)
	if len(v.config.Instances) > 1 {
		delRect := geometry.NewRect(r.Min.X+374, btnY, 70, 28)
		canvas.DrawRoundRect(delRect, widget.RGBA8(80, 30, 40, 255), 6)
		canvas.DrawText("Delete", delRect, 10, widget.RGBA8(255, 180, 190, 255), false, widget.TextAlignCenter)
	}

	saveRect := geometry.NewRect(r.Min.X+r.Width()-110, btnY, 90, 28)
	canvas.DrawRoundRect(saveRect, ColorStatusInProgress, 6)
	canvas.DrawText("Save All", saveRect, 11, widget.RGBA8(15, 20, 30, 255), true, widget.TextAlignCenter)
}

func (v *AppView) Event(ctx widget.Context, e event.Event) bool {
	switch ev := e.(type) {
	case *event.MouseEvent:
		contains := v.Bounds().Contains(ev.Position)
		if ev.MouseType == event.MousePress {
			return v.handleClick(ev.Position)
		} else if ev.MouseType == event.MouseMove {
			if contains {
				return v.handleHover(ev.Position)
			} else {
				v.mu.Lock()
				if v.state == window.StateFan && !v.lastHover.IsZero() {
					v.lastHover = time.Now().Add(-200 * time.Millisecond)
				}
				v.mu.Unlock()
			}
		}
	case *event.WheelEvent:
		filtered := v.getFilteredIssues()
		issueCount := len(filtered)

		v.mu.Lock()
		st := v.state
		v.mu.Unlock()

		if st == window.StateFan || st == window.StateExpanded {
			tabHeight := float32(56)
			totalH := float32(issueCount) * tabHeight
			b := v.Bounds()
			viewH := b.Height() - 74
			maxScroll := totalH - viewH
			if maxScroll < 0 {
				maxScroll = 0
			}

			v.mu.Lock()
			v.scrollY -= ev.Delta.Y * 0.7
			if v.scrollY < 0 {
				v.scrollY = 0
			}
			if v.scrollY > maxScroll {
				v.scrollY = maxScroll
			}
			v.mu.Unlock()
			v.MarkNeedsLayout()
			return true
		}
	case *event.KeyEvent:
		if ev.KeyType == event.KeyPress {
			return v.handleKey(ev)
		}
	}
	return false
}

func (v *AppView) handleHover(pos geometry.Point) bool {
	v.mu.Lock()
	st := v.state
	instCount := len(v.config.Instances)
	b := v.Bounds()
	h := b.Height()
	v.mu.Unlock()

	// REST STATE: Hovering specific instance or settings dot
	if st == window.StateRest {
		if pos.Y >= b.Min.Y+h-28 {
			v.OpenSettings()
			return true
		}

		if instCount > 0 {
			usableH := h - 34
			instSectionH := usableH / float32(instCount)
			instIdx := int((pos.Y - (b.Min.Y + 5)) / instSectionH)
			if instIdx < 0 {
				instIdx = 0
			}
			if instIdx >= instCount {
				instIdx = instCount - 1
			}
			v.mu.Lock()
			v.activeInstIdx = instIdx
			v.scrollY = 0
			v.searchQuery = ""
			v.mu.Unlock()
		}

		v.SetState(window.StateFan)
		return true
	}

	if st == window.StateFan {
		v.mu.Lock()
		v.lastHover = time.Now()
		v.mu.Unlock()
	}

	return false
}

func (v *AppView) getActiveTarget() *string {
	switch v.activeField {
	case 1:
		return &v.nameVal
	case 2:
		return &v.urlVal
	case 3:
		return &v.emailVal
	case 4:
		return &v.tokenVal
	case 5:
		return &v.jqlVal
	default:
		return nil
	}
}

func (v *AppView) handleClick(pos geometry.Point) bool {
	b := v.Bounds()
	w := b.Width()
	h := b.Height()

	v.mu.Lock()
	st := v.state
	allIssues := v.issues
	showSettings := v.showSettings
	scrollY := v.scrollY
	v.mu.Unlock()

	filteredIssues := v.getFilteredIssues()

	if st == window.StateRest {
		return v.handleHover(pos)
	}

	// 1. Fan State Clicks
	if st == window.StateFan {
		// Click header to cycle instance filter
		instHeaderRect := geometry.NewRect(b.Min.X+6, b.Min.Y+7, w-12, 20)
		if instHeaderRect.Contains(pos) && len(v.config.Instances) > 1 {
			v.mu.Lock()
			v.activeInstIdx = (v.activeInstIdx + 1) % len(v.config.Instances)
			v.scrollY = 0
			v.mu.Unlock()
			v.MarkNeedsLayout()
			return true
		}

		searchRect := geometry.NewRect(b.Min.X+6, b.Min.Y+31, w-12, 22)
		if searchRect.Contains(pos) {
			v.mu.Lock()
			v.searchActive = true
			v.mu.Unlock()
			v.MarkNeedsLayout()
			return true
		}

		settingsTabRect := geometry.NewRect(b.Min.X+6, b.Min.Y+h-42, w-12, 34)
		if settingsTabRect.Contains(pos) {
			v.OpenSettings()
			return true
		}

		tabStartY := b.Min.Y + float32(58) - scrollY
		tabHeight := float32(48)
		tabGap := float32(6)

		for i, fIss := range filteredIssues {
			tabY := tabStartY + float32(i)*(tabHeight+tabGap)
			if tabY+tabHeight < b.Min.Y+56 || tabY > b.Min.Y+h-54 {
				continue
			}
			tabRect := geometry.NewRect(b.Min.X+6, tabY, w-12, tabHeight)
			if tabRect.Contains(pos) {
				for origIdx, oIss := range allIssues {
					if oIss.Key == fIss.Key {
						v.Expand(origIdx)
						return true
					}
				}
				v.Expand(i)
				return true
			}
		}

		return false
	}

	// 2. Expanded State Clicks
	tabBarWidth := float32(104)
	cardAreaWidth := w - tabBarWidth - 14

	tabStartX := b.Min.X + w - tabBarWidth

	settingsTabRect := geometry.NewRect(tabStartX+2, b.Min.Y+h-48, tabBarWidth-14, 34)
	if settingsTabRect.Contains(pos) {
		v.ToggleSettings()
		return true
	}

	tabStartY := b.Min.Y + float32(16) - scrollY
	tabHeight := float32(50)
	tabGap := float32(7)

	for i, iss := range filteredIssues {
		tabY := tabStartY + float32(i)*(tabHeight+tabGap)
		if tabY+tabHeight < b.Min.Y+14 || tabY > b.Min.Y+h-58 {
			continue
		}
		tabRect := geometry.NewRect(tabStartX, tabY, tabBarWidth-4, tabHeight)
		if tabRect.Contains(pos) {
			for origIdx, oIss := range allIssues {
				if oIss.Key == iss.Key {
					v.Expand(origIdx)
					return true
				}
			}
			v.Expand(i)
			return true
		}
	}

	// Settings Modal Clicks
	if showSettings {
		r := geometry.NewRect(b.Min.X+8, b.Min.Y+8, cardAreaWidth, h-16)

		// Instance Tabs Row Clicks
		tabRowY := r.Min.Y + 44
		instStartX := r.Min.X + 20
		tabW := float32(110)
		tabGap := float32(6)

		for idx := range v.config.Instances {
			instTabX := instStartX + float32(idx)*(tabW+tabGap)
			instTabRect := geometry.NewRect(instTabX, tabRowY, tabW, 26)
			if instTabRect.Contains(pos) {
				v.saveCurrentInstanceFields()
				v.loadInstanceFields(idx)
				v.statusMsg = ""
				v.MarkNeedsLayout()
				return true
			}
		}

		// + Add Instance Button Click
		addInstX := instStartX + float32(len(v.config.Instances))*(tabW+tabGap)
		addRect := geometry.NewRect(addInstX, tabRowY, 80, 26)
		if addRect.Contains(pos) {
			v.saveCurrentInstanceFields()
			newIdx := len(v.config.Instances) + 1
			newInst := jira.InstanceConfig{
				ID:       fmt.Sprintf("inst-%d", time.Now().UnixNano()),
				Name:     fmt.Sprintf("Instance %d", newIdx),
				BaseURL:  "",
				Email:    v.emailVal,
				APIToken: "",
				JQLQuery: "assignee = currentUser() AND resolution = Unresolved ORDER BY updated DESC",
			}
			v.config.Instances = append(v.config.Instances, newInst)
			v.loadInstanceFields(len(v.config.Instances) - 1)
			v.showToast("Added new Jira instance")
			v.MarkNeedsLayout()
			return true
		}

		// Load .env
		envRect := geometry.NewRect(r.Min.X+r.Width()-155, r.Min.Y+12, 75, 24)
		if envRect.Contains(pos) {
			cfg := v.config
			if jira.LoadFromDotEnv(&cfg) {
				v.config = cfg
				v.loadInstanceFields(v.selectedInstIdx)
				v.showToast("✓ Loaded credentials from .env")
				v.statusMsg = "Credentials loaded from .env"
				v.MarkNeedsLayout()
			} else {
				v.showToast("No .env file found")
			}
			return true
		}

		// Close
		closeRect := geometry.NewRect(r.Min.X+r.Width()-70, r.Min.Y+12, 50, 24)
		if closeRect.Contains(pos) {
			v.SetState(window.StateFan)
			return true
		}

		btnY := r.Min.Y + r.Height() - 44

		// Mode Toggle
		demoRect := geometry.NewRect(r.Min.X+20, btnY, 130, 28)
		if demoRect.Contains(pos) {
			v.demoMode = !v.demoMode
			v.MarkNeedsLayout()
			return true
		}

		// Debug Toggle
		debugRect := geometry.NewRect(r.Min.X+156, btnY, 86, 28)
		if debugRect.Contains(pos) {
			v.debugMode = !v.debugMode
			v.MarkNeedsLayout()
			return true
		}

		// Test Connection
		testRect := geometry.NewRect(r.Min.X+248, btnY, 120, 28)
		if testRect.Contains(pos) {
			v.testConn()
			return true
		}

		// Delete Instance
		if len(v.config.Instances) > 1 {
			delRect := geometry.NewRect(r.Min.X+374, btnY, 70, 28)
			if delRect.Contains(pos) {
				cur := v.selectedInstIdx
				v.config.Instances = append(v.config.Instances[:cur], v.config.Instances[cur+1:]...)
				if cur >= len(v.config.Instances) {
					cur = len(v.config.Instances) - 1
				}
				v.loadInstanceFields(cur)
				v.showToast("Deleted instance")
				v.MarkNeedsLayout()
				return true
			}
		}

		// Save All
		saveRect := geometry.NewRect(r.Min.X+r.Width()-110, btnY, 90, 28)
		if saveRect.Contains(pos) {
			v.saveSettings()
			return true
		}

		// Field click focus
		sepY := tabRowY + 34
		startY := sepY + 12
		for i := 0; i < 5; i++ {
			y := startY + float32(i*45)
			inpRect := geometry.NewRect(r.Min.X+20, y+16, r.Width()-40, 24)
			if inpRect.Contains(pos) {
				v.activeField = i + 1
				v.selectAll = false
				tgt := v.getActiveTarget()
				if tgt != nil {
					clickRelX := pos.X - (inpRect.Min.X + 8)
					v.cursorPos = getCursorIndexFromX(*tgt, 11, clickRelX)
				} else {
					v.cursorPos = 0
				}
				v.MarkNeedsLayout()
				return true
			}
		}

		v.activeField = 0
		v.selectAll = false
		v.MarkNeedsLayout()
		return true
	}

	return false
}

func (v *AppView) handleKey(ev *event.KeyEvent) bool {
	if ev.Key == event.KeyEscape {
		v.mu.Lock()
		st := v.state
		searchQ := v.searchQuery
		v.mu.Unlock()

		if searchQ != "" {
			v.mu.Lock()
			v.searchQuery = ""
			v.searchActive = false
			v.scrollY = 0
			v.mu.Unlock()
			v.MarkNeedsLayout()
			return true
		}

		if st == window.StateExpanded {
			v.SetState(window.StateFan)
			return true
		} else if st == window.StateFan {
			v.SetState(window.StateRest)
			return true
		}
		return true
	}

	// Handle Instant Search in Fan state
	if v.state == window.StateFan && !v.showSettings {
		if ev.Key == event.KeyBackspace {
			v.mu.Lock()
			if len(v.searchQuery) > 0 {
				v.searchQuery = v.searchQuery[:len(v.searchQuery)-1]
				v.scrollY = 0
			}
			v.mu.Unlock()
			v.MarkNeedsLayout()
			return true
		}
		if ev.Rune >= 32 && ev.Rune <= 126 {
			v.mu.Lock()
			v.searchQuery += string(ev.Rune)
			v.searchActive = true
			v.scrollY = 0
			v.mu.Unlock()
			v.MarkNeedsLayout()
			return true
		}
	}

	// Settings text editing
	if v.showSettings && v.activeField > 0 {
		target := v.getActiveTarget()
		if target == nil {
			return true
		}

		val := *target
		mods := ev.Modifiers()
		hasMod := (mods != event.ModNone)

		// 1. Select All: Cmd+A / Ctrl+A
		if hasMod && (ev.Key == event.KeyA || ev.Rune == 'a' || ev.Rune == 'A') {
			v.selectAll = true
			v.cursorPos = len(val)
			v.lastModTime = time.Now()
			v.MarkNeedsLayout()
			return true
		}

		// 2. Copy: Cmd+C / Ctrl+C
		if hasMod && (ev.Key == event.KeyC || ev.Rune == 'c' || ev.Rune == 'C') {
			if val != "" {
				copyToClipboard(val)
				v.showToast("✓ Copied to clipboard")
			}
			v.lastModTime = time.Now()
			return true
		}

		// 3. Paste: Cmd+V / Ctrl+V
		if hasMod && (ev.Key == event.KeyV || ev.Rune == 'v' || ev.Rune == 'V') {
			pasted := strings.TrimSpace(pasteFromClipboard())
			if pasted != "" {
				if v.selectAll {
					*target = pasted
					v.cursorPos = len(pasted)
					v.selectAll = false
				} else {
					cp := v.cursorPos
					if cp > len(val) {
						cp = len(val)
					}
					*target = val[:cp] + pasted + val[cp:]
					v.cursorPos = cp + len(pasted)
				}
				v.lastModTime = time.Now()
				v.MarkNeedsLayout()
			}
			return true
		}

		if time.Since(v.lastModTime) < 350*time.Millisecond && (ev.Rune == 'v' || ev.Rune == 'V' || ev.Rune == 'c' || ev.Rune == 'C' || ev.Rune == 'a' || ev.Rune == 'A') {
			return true
		}

		// 4. Navigation: Arrow Left / Home
		if ev.Key == event.KeyLeft || ev.Key == event.KeyHome {
			v.selectAll = false
			if hasMod || ev.Key == event.KeyHome {
				v.cursorPos = 0
			} else if v.cursorPos > 0 {
				v.cursorPos--
			}
			v.MarkNeedsLayout()
			return true
		}

		// 5. Navigation: Arrow Right / End
		if ev.Key == event.KeyEnd || ev.Key == event.KeyRight {
			v.selectAll = false
			if hasMod || ev.Key == event.KeyEnd {
				v.cursorPos = len(val)
			} else if v.cursorPos < len(val) {
				v.cursorPos++
			}
			v.MarkNeedsLayout()
			return true
		}

		// 6. Deletion: Backspace
		if ev.Key == event.KeyBackspace {
			if v.selectAll {
				*target = ""
				v.cursorPos = 0
				v.selectAll = false
				v.MarkNeedsLayout()
				return true
			}
			if v.cursorPos > 0 && len(val) > 0 {
				cp := v.cursorPos
				if cp > len(val) {
					cp = len(val)
				}
				*target = val[:cp-1] + val[cp:]
				v.cursorPos = cp - 1
				v.MarkNeedsLayout()
			}
			return true
		}

		// 7. Deletion: Delete
		if ev.Key == event.KeyDelete {
			if v.selectAll {
				*target = ""
				v.cursorPos = 0
				v.selectAll = false
				v.MarkNeedsLayout()
				return true
			}
			if v.cursorPos < len(val) {
				cp := v.cursorPos
				*target = val[:cp] + val[cp+1:]
				v.MarkNeedsLayout()
			}
			return true
		}

		// 8. Tab / Enter: Cycle next field
		if ev.Key == event.KeyEnter || ev.Key == event.KeyTab {
			v.activeField = (v.activeField % 5) + 1
			v.selectAll = false
			nextTgt := v.getActiveTarget()
			if nextTgt != nil {
				v.cursorPos = len(*nextTgt)
			} else {
				v.cursorPos = 0
			}
			v.MarkNeedsLayout()
			return true
		}

		// 9. Standard Printable Characters
		if ev.Rune >= 32 && !hasMod {
			rStr := string(ev.Rune)
			if v.selectAll {
				*target = rStr
				v.cursorPos = 1
				v.selectAll = false
			} else {
				cp := v.cursorPos
				if cp > len(val) {
					cp = len(val)
				}
				*target = val[:cp] + rStr + val[cp:]
				v.cursorPos = cp + 1
			}
			v.MarkNeedsLayout()
			return true
		}

		return true
	}

	return false
}

func sanitizeToken(token string) string {
	token = strings.TrimSpace(token)
	if len(token) == 193 && strings.HasPrefix(token, "ATATT") && strings.HasSuffix(token, "v") {
		token = token[:192]
	}
	return token
}

func (v *AppView) testConn() {
	v.saveCurrentInstanceFields()
	v.statusMsg = fmt.Sprintf("Testing connection for %s...", v.nameVal)
	v.MarkNeedsLayout()

	testBaseURL := strings.TrimSpace(v.urlVal)
	testEmail := strings.TrimSpace(v.emailVal)
	testToken := sanitizeToken(v.tokenVal)

	go func() {
		user, err := v.client.VerifyInstanceConnection(context.Background(), testBaseURL, testEmail, testToken)
		v.mu.Lock()
		defer v.mu.Unlock()
		if err != nil {
			v.statusMsg = fmt.Sprintf("Error: %v", err)
		} else {
			v.statusMsg = fmt.Sprintf("Connected as %s", user)
		}
		v.MarkNeedsLayout()
	}()
}

func (v *AppView) saveSettings() {
	v.saveCurrentInstanceFields()
	v.config.DemoMode = v.demoMode
	v.config.DebugMode = v.debugMode
	if len(v.config.Instances) > 0 {
		v.config.BaseURL = v.config.Instances[0].BaseURL
		v.config.Email = v.config.Instances[0].Email
		v.config.APIToken = v.config.Instances[0].APIToken
		v.config.JQLQuery = v.config.Instances[0].JQLQuery
	}
	_ = jira.SaveConfig(v.config)
	v.client.UpdateConfig(v.config)
	v.showSettings = false
	v.showToast("All instances saved")
	v.SetState(window.StateFan)
	v.RefreshIssues()
}

func (v *AppView) showToast(msg string) {
	v.mu.Lock()
	v.toast = NewToast(msg)
	v.mu.Unlock()
	v.MarkNeedsLayout()
}

func maskToken(token string) string {
	token = sanitizeToken(token)
	if token == "" {
		return ""
	}
	if len(token) <= 4 {
		return "****"
	}
	return token[:2] + "••••••••••••" + token[len(token)-2:]
}

func copyToClipboard(text string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
		cmd.Stdin = strings.NewReader(text)
		_ = cmd.Run()
	case "windows":
		cmd = exec.Command("clip")
		cmd.Stdin = strings.NewReader(text)
		_ = cmd.Run()
	case "linux":
		cmd = exec.Command("xclip", "-selection", "clipboard")
		cmd.Stdin = strings.NewReader(text)
		_ = cmd.Run()
	}
}

func pasteFromClipboard() string {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbpaste")
	case "windows":
		cmd = exec.Command("powershell", "-NoProfile", "-Command", "Get-Clipboard")
	case "linux":
		cmd = exec.Command("xclip", "-selection", "clipboard", "-o")
	default:
		return ""
	}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(out), "\r\n")
}
