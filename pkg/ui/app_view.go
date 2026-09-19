package ui

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"jira-quick-access/pkg/jira"
	"jira-quick-access/pkg/status"
	"jira-quick-access/pkg/window"

	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/widget"
)

// AppView implements the 3-state edge architecture with multi-instance support & macOS Dock styling:
// 1. Rest: Sleek discrete macOS Dock capsule with complete crisp frosted border & proportional gauges (32x224)
// 2. Fan: Shingled vertical tabs down the edge with instant search & responsive hover magnification (120xH)
// 3. Expanded: Full floating card / native mobile webview level with its tab (780x580)
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

	// Goroutine lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Search & filter memoization cache
	filteredCache []jira.Issue
	cacheDirty    bool

	// Hover tracking for Fan mode dock interaction
	hoveredTabIdx   int  // Index of hovered tab in Fan mode (-1 if none)
	hoveredSettings bool // True if settings button is hovered in Fan mode

	// Dock and Display state
	dockSide        window.DockSide
	selectedMonitor int
	monitors        []window.MonitorInfo
	restHoverStart  time.Time
	alwaysOnTop     bool
	autoHide        bool
	isTucked        bool

	// Multi-instance filtering & settings tracking
	activeInstIdx   int             // Currently viewed instance filter in Fan & Expanded states (0..n-1)
	selectedInstIdx int             // Currently edited instance in Settings overlay
	settingsSection settingsSection // Current settings page: instances or application

	// Active instance field buffers (5 fields + color)
	nameVal     string
	urlVal      string
	emailVal    string
	tokenVal    string
	jqlVal      string
	intervalVal string
	colorVal    string
	demoMode    bool
	debugMode   bool
	activeField int       // 1..7
	cursorPos   int       // Cursor position inside active field
	selectAll   bool      // If true, all text in active field is selected
	lastModTime time.Time // Timestamp of last modifier action to prevent duplicate character insertion
	statusMsg   string

	// Status monitoring
	showStatus         bool
	statusClient       *status.Client
	statusReport       status.StatusReport
	hoveredStatus      bool
	statusIntervalVal  string
	statusOpenedFrom   window.WindowState
	settingsOpenedFrom window.WindowState
	version            string

	// Callbacks
	onRedraw       func()
	onResize       func(w, h int)
	onConfigReload func()
}

type appViewStateSnapshot struct {
	bounds            geometry.Rect
	state             window.WindowState
	showSettings      bool
	showStatus        bool
	statusReport      status.StatusReport
	hoveredStatus     bool
	dockSide          window.DockSide
	selectedMonitor   int
	monitors          []window.MonitorInfo
	alwaysOnTop       bool
	autoHide          bool
	isTucked          bool
	issues            []jira.Issue
	filteredIssues    []jira.Issue
	activeIdx         int
	activeTheme       CardTheme
	toast             ToastNotification
	scrollY           float32
	searchQuery       string
	searchActive      bool
	activeInstIdx     int
	selectedInstIdx   int
	hoveredTabIdx     int
	hoveredSettings   bool
	settingsSection   settingsSection
	instances         []jira.InstanceConfig
	nameVal           string
	urlVal            string
	emailVal          string
	tokenVal          string
	jqlVal            string
	intervalVal       string
	statusIntervalVal string
	colorVal          string
	demoMode          bool
	debugMode         bool
	activeField       int
	cursorPos         int
	selectAll         bool
	statusMsg         string
	version           string
	config            jira.Config
}

func NewAppView(
	cfg jira.Config,
	client *jira.Client,
	onRedraw func(),
) *AppView {
	cfg = cfg.Clone()
	cfg.ApplyDefaults()
	cfg.EnsureInstances()
	if cfg.Instances[0].APIToken == "" || cfg.Instances[0].Email == "" {
		jira.LoadFromDotEnv(&cfg)
	}

	ctx, cancel := context.WithCancel(context.Background())

	v := &AppView{
		config:             cfg,
		client:             client,
		activeIdx:          0,
		activeTheme:        ThemeMint,
		state:              window.StateRest,
		showSettings:       false,
		hoveredTabIdx:      -1,
		hoveredSettings:    false,
		dockSide:           window.DockSide(cfg.DockSide),
		selectedMonitor:    cfg.MonitorIndex,
		alwaysOnTop:        cfg.AlwaysOnTop,
		autoHide:           cfg.AutoHide,
		activeInstIdx:      0,
		selectedInstIdx:    0,
		demoMode:           cfg.DemoMode,
		debugMode:          cfg.DebugMode,
		intervalVal:        fmt.Sprintf("%d", cfg.PollInterval),
		statusIntervalVal:  fmt.Sprintf("%d", cfg.StatusPollInterval),
		statusOpenedFrom:   window.StateFan,
		settingsOpenedFrom: window.StateFan,
		statusClient:       status.NewClient(),
		statusReport:       status.StatusReport{OverallIndicator: status.IndicatorNone, OverallText: "All Systems Operational"},
		onRedraw:           onRedraw,
		ctx:                ctx,
		cancel:             cancel,
		cacheDirty:         true,
	}

	if window.DefaultManager != nil {
		window.DefaultManager.SetDockSide(v.dockSide)
		window.DefaultManager.SetMonitor(v.selectedMonitor)
		window.DefaultManager.SetAlwaysOnTop(cfg.AlwaysOnTop)
		window.DefaultManager.SetAutoHide(cfg.AutoHide)
		if cfg.PosYRatio > 0 && cfg.PosYRatio <= 1.0 {
			window.DefaultManager.SetPositionRatio(cfg.PosYRatio)
		}
		v.monitors = window.DefaultManager.GetMonitors()
	}

	v.mu.Lock()
	v.loadInstanceFieldsLocked(0)
	v.mu.Unlock()

	v.SetVisible(true)
	v.SetEnabled(true)
	v.SetBounds(geometry.NewRect(0, 0, 36, 224))

	// Register collapse handler (for native close button and Escape key)
	window.RegisterCollapseHandler(func() {
		v.mu.Lock()
		st := v.state
		showSet := v.showSettings
		showStat := v.showStatus
		v.mu.Unlock()

		if showStat {
			v.CloseStatus()
		} else if showSet {
			v.CloseSettings()
		} else if st == window.StateExpanded {
			v.SetState(window.StateFan)
		} else if st == window.StateFan {
			v.SetState(window.StateRest)
		}
	})

	// Register dock change handler (triggered when user drags window across displays/edges)
	window.RegisterDockChangeHandler(func(side window.DockSide, monIdx int, yRatio float64) {
		v.mu.Lock()
		v.dockSide = side
		v.selectedMonitor = monIdx
		v.config.DockSide = int(side)
		v.config.MonitorIndex = monIdx
		v.config.PosYRatio = yRatio
		cfgToSave := v.config.Clone()
		v.mu.Unlock()

		_ = jira.SaveConfig(cfgToSave)
		v.MarkNeedsLayout()
		if v.onRedraw != nil {
			v.onRedraw()
		}
	})

	// Cancelable OS-level mouse location poller for Fan auto-collapse
	v.wg.Add(1)
	go func() {
		defer v.wg.Done()
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-v.ctx.Done():
				return
			case <-ticker.C:
				v.mu.Lock()
				st := v.state
				lastH := v.lastHover
				searchAct := v.searchActive || v.searchQuery != ""
				v.mu.Unlock()

				inside := window.IsMouseInside()
				if st == window.StateFan && !searchAct {
					if inside {
						v.mu.Lock()
						v.lastHover = time.Now()
						v.mu.Unlock()
					} else {
						v.setHoveredTab(-1)
						if !lastH.IsZero() && time.Since(lastH) > 300*time.Millisecond {
							v.SetState(window.StateRest)
						}
					}
				} else if st == window.StateRest {
					if inside {
						v.mu.Lock()
						autoH := v.autoHide
						tucked := v.isTucked
						v.mu.Unlock()
						if autoH && tucked {
							v.setTucked(false)
						}
					} else {
						v.mu.Lock()
						v.restHoverStart = time.Time{}
						autoH := v.autoHide
						tucked := v.isTucked
						v.mu.Unlock()
						if autoH && !tucked {
							v.setTucked(true)
						}
					}
				}
			}
		}
	}()

	// Redraw timer for toast dismissal and focused text cursor blinking
	v.wg.Add(1)
	go func() {
		defer v.wg.Done()
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-v.ctx.Done():
				return
			case <-ticker.C:
				v.mu.Lock()
				showSet := v.showSettings
				hasToast := v.toast.IsActive()
				activeField := v.activeField
				v.mu.Unlock()

				if hasToast || (showSet && activeField > 0) {
					if v.onRedraw != nil {
						v.onRedraw()
					}
					v.MarkNeedsLayout()
				}
			}
		}
	}()

	// Synchronously initialize mock issues if demo mode is enabled
	if cfg.DemoMode {
		v.issues = jira.GetMockIssues(cfg.PinnedKeys)
		if len(v.issues) > 0 {
			v.activeTheme = GetTicketTheme(v.activeIdx)
		}
	}

	return v
}

// Close stops all background goroutines and cleans up resources.
func (v *AppView) Close() {
	v.mu.Lock()
	if v.cancel != nil {
		v.cancel()
	}
	v.mu.Unlock()
	v.wg.Wait()
}

// SetOnResize registers a callback to notify when the window/view changes dimensions.
func (v *AppView) SetOnResize(fn func(w, h int)) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.onResize = fn
}

// SetVersion sets the application version string displayed in UI overlays.
func (v *AppView) SetVersion(ver string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.version = ver
}

// SetBounds overrides widget.WidgetBase.SetBounds to enforce that bounds always match
// the authoritative size for the current WindowState, preventing external layout
// engines from overwriting the HUD dimensions with stale constraints.
func (v *AppView) SetBounds(r geometry.Rect) {
	v.mu.Lock()
	st := v.state
	v.mu.Unlock()

	expectedW, expectedH := v.computeSize(st)
	v.WidgetBase.SetBounds(geometry.NewRect(r.Min.X, r.Min.Y, float32(expectedW), float32(expectedH)))
}

// SetDockSide sets docking edge and smoothly repositions the window
func (v *AppView) SetDockSide(side window.DockSide) {
	v.mu.Lock()
	v.dockSide = side
	v.config.DockSide = int(side)
	cfg := v.config.Clone()
	v.mu.Unlock()

	_ = jira.SaveConfig(cfg)
	if window.DefaultManager != nil {
		window.DefaultManager.SetDockSide(side)
	}
	v.MarkNeedsLayout()
	if v.onRedraw != nil {
		v.onRedraw()
	}
}

// SetMonitor sets active monitor display and smoothly repositions the window
func (v *AppView) SetMonitor(monIdx int) {
	v.mu.Lock()
	v.selectedMonitor = monIdx
	v.config.MonitorIndex = monIdx
	cfg := v.config.Clone()
	v.mu.Unlock()

	_ = jira.SaveConfig(cfg)
	if window.DefaultManager != nil {
		window.DefaultManager.SetMonitor(monIdx)
	}
	v.MarkNeedsLayout()
	if v.onRedraw != nil {
		v.onRedraw()
	}
}

// StartDrag initiates Cocoa window dragging across displays
func (v *AppView) StartDrag() {
	if window.DefaultManager != nil {
		window.DefaultManager.StartWindowDrag()
	}
}

// SetAlwaysOnTop toggles whether the HUD floats above all windows.
func (v *AppView) SetAlwaysOnTop(alwaysOnTop bool) {
	v.mu.Lock()
	v.alwaysOnTop = alwaysOnTop
	v.config.AlwaysOnTop = alwaysOnTop
	cfg := v.config.Clone()
	v.mu.Unlock()

	window.SetAlwaysOnTop(alwaysOnTop)
	_ = jira.SaveConfig(cfg)
	v.MarkNeedsLayout()
	if v.onRedraw != nil {
		v.onRedraw()
	}
}

// SetAutoHide toggles macOS Dock-style edge auto-hide.
func (v *AppView) SetAutoHide(autoHide bool) {
	v.mu.Lock()
	v.autoHide = autoHide
	v.config.AutoHide = autoHide
	cfg := v.config.Clone()
	v.mu.Unlock()

	window.SetAutoHide(autoHide)
	if !autoHide {
		v.setTucked(false)
	}
	_ = jira.SaveConfig(cfg)
	v.MarkNeedsLayout()
	if v.onRedraw != nil {
		v.onRedraw()
	}
}

func (v *AppView) setTucked(tucked bool) {
	v.mu.Lock()
	if v.isTucked == tucked {
		v.mu.Unlock()
		return
	}
	v.isTucked = tucked
	st := v.state
	v.mu.Unlock()

	if st == window.StateRest {
		window.SetTucked(tucked, 36, 224)
	}
}

func (v *AppView) invalidateFilterCacheLocked() {
	v.cacheDirty = true
	v.filteredCache = nil
}

func (v *AppView) loadInstanceFieldsLocked(idx int) {
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
	v.colorVal = inst.Color
	v.activeField = 0
	v.cursorPos = len(inst.Name)
	v.selectAll = false
}

func (v *AppView) saveCurrentInstanceFieldsLocked() {
	if v.selectedInstIdx < 0 || v.selectedInstIdx >= len(v.config.Instances) {
		return
	}
	v.config.Instances[v.selectedInstIdx].Name = strings.TrimSpace(v.nameVal)
	v.config.Instances[v.selectedInstIdx].BaseURL = strings.TrimSpace(v.urlVal)
	v.config.Instances[v.selectedInstIdx].Email = strings.TrimSpace(v.emailVal)
	v.config.Instances[v.selectedInstIdx].APIToken = sanitizeToken(v.tokenVal)
	v.config.Instances[v.selectedInstIdx].JQLQuery = strings.TrimSpace(v.jqlVal)
	v.config.Instances[v.selectedInstIdx].Color = strings.TrimSpace(v.colorVal)
}

func isIssueForInstance(iss jira.Issue, inst jira.InstanceConfig, instIdx int) bool {
	// If both have an instance ID, it is the authoritative unique identifier.
	// Never fall back to BaseURL or Name when InstanceID is present, otherwise
	// multiple instances sharing the same BaseURL (e.g. different JQL queries)
	// will mistakenly cross-match and share counts.
	if inst.ID != "" && iss.InstanceID != "" {
		return iss.InstanceID == inst.ID
	}

	// Fallback for mock or legacy issues without an InstanceID:
	if iss.InstanceID == "" {
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
		if instIdx == 0 {
			return true
		}
	}

	return false
}

func (v *AppView) getFilteredIssuesLocked() []jira.Issue {
	// A nil result is a valid cached empty search, not a cache miss.
	if !v.cacheDirty {
		return v.filteredCache
	}

	filterInstance := v.activeInstIdx >= 0 && v.activeInstIdx < len(v.config.Instances)
	var targetInst jira.InstanceConfig
	if filterInstance {
		targetInst = v.config.Instances[v.activeInstIdx]
	}
	q := strings.ToLower(v.searchQuery)
	if !filterInstance && q == "" {
		v.filteredCache = v.issues
	} else {
		// Apply both predicates in one pass instead of copying an intermediate list.
		var result []jira.Issue
		for _, iss := range v.issues {
			if filterInstance && !isIssueForInstance(iss, targetInst, v.activeInstIdx) {
				continue
			}
			if q == "" || strings.Contains(strings.ToLower(iss.Key), q) ||
				strings.Contains(strings.ToLower(iss.Summary), q) ||
				strings.Contains(strings.ToLower(iss.Status.Name), q) {
				result = append(result, iss)
			}
		}
		v.filteredCache = result
	}
	v.cacheDirty = false
	return v.filteredCache
}

func (v *AppView) getFilteredIssues() []jira.Issue {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.getFilteredIssuesLocked()
}

func (v *AppView) setHoveredTab(idx int) {
	v.mu.Lock()
	if v.hoveredTabIdx == idx {
		v.mu.Unlock()
		return
	}
	v.hoveredTabIdx = idx
	var tip string
	filtered := v.getFilteredIssuesLocked()
	if idx >= 0 && idx < len(filtered) {
		iss := filtered[idx]
		tip = fmt.Sprintf("[%s] %s • %s", iss.Key, iss.Summary, iss.Status.Name)
	}
	v.mu.Unlock()
	window.SetToolTip(tip)
}

func (v *AppView) snapshot() appViewStateSnapshot {
	v.mu.Lock()
	defer v.mu.Unlock()

	cfg := v.config.Clone()
	instances := cfg.Instances

	// Fan mode only draws filtered issues; status mode draws no tickets.
	var issues []jira.Issue
	if v.state != window.StateFan && !v.showStatus {
		issues = make([]jira.Issue, len(v.issues))
		copy(issues, v.issues)
	}

	var filteredClone []jira.Issue
	if v.state != window.StateRest && !v.showStatus {
		filtered := v.getFilteredIssuesLocked()
		filteredClone = make([]jira.Issue, len(filtered))
		copy(filteredClone, filtered)
	}

	monitors := make([]window.MonitorInfo, len(v.monitors))
	copy(monitors, v.monitors)

	return appViewStateSnapshot{
		bounds:            v.Bounds(),
		state:             v.state,
		showSettings:      v.showSettings,
		showStatus:        v.showStatus,
		statusReport:      v.statusReport,
		hoveredStatus:     v.hoveredStatus,
		dockSide:          v.dockSide,
		selectedMonitor:   v.selectedMonitor,
		monitors:          monitors,
		alwaysOnTop:       v.alwaysOnTop,
		autoHide:          v.autoHide,
		isTucked:          v.isTucked,
		issues:            issues,
		filteredIssues:    filteredClone,
		activeIdx:         v.activeIdx,
		activeTheme:       v.activeTheme,
		toast:             v.toast,
		scrollY:           v.scrollY,
		searchQuery:       v.searchQuery,
		searchActive:      v.searchActive,
		activeInstIdx:     v.activeInstIdx,
		selectedInstIdx:   v.selectedInstIdx,
		hoveredTabIdx:     v.hoveredTabIdx,
		hoveredSettings:   v.hoveredSettings,
		settingsSection:   v.settingsSection,
		instances:         instances,
		nameVal:           v.nameVal,
		urlVal:            v.urlVal,
		emailVal:          v.emailVal,
		tokenVal:          v.tokenVal,
		jqlVal:            v.jqlVal,
		intervalVal:       v.intervalVal,
		statusIntervalVal: v.statusIntervalVal,
		colorVal:          v.colorVal,
		demoMode:          v.demoMode,
		debugMode:         v.debugMode,
		activeField:       v.activeField,
		cursorPos:         v.cursorPos,
		selectAll:         v.selectAll,
		statusMsg:         v.statusMsg,
		version:           v.version,
		config:            cfg,
	}
}

func (v *AppView) computeSize(st window.WindowState) (int, int) {
	switch st {
	case window.StateRest:
		return 36, 224

	case window.StateFan:
		issues := v.getFilteredIssues()
		issueCount := len(issues)
		if issueCount == 0 {
			issueCount = 2
		}
		h := issueCount*70 + 120
		if h > 720 {
			h = 720
		}
		if h < 270 {
			h = 270
		}
		return 120, h

	case window.StateExpanded:
		return 780, 580
	}
	return 36, 224
}

func (v *AppView) SetState(newState window.WindowState) {
	v.mu.Lock()
	v.state = newState
	v.hoveredTabIdx = -1
	v.hoveredSettings = false
	v.hoveredStatus = false
	if newState == window.StateFan {
		v.lastHover = time.Now()
	} else {
		v.lastHover = time.Time{}
		v.searchActive = false
	}
	if newState == window.StateRest || newState == window.StateFan {
		v.showSettings = false
		v.showStatus = false
	}
	showSettings := v.showSettings
	showStatus := v.showStatus
	v.mu.Unlock()

	if newState == window.StateRest {
		window.SetToolTip("")
	}

	if newState != window.StateRest {
		v.setTucked(false)
	}

	w, h := v.computeSize(newState)
	// Synchronously update bounds to eliminate any hit-test race conditions
	v.SetBounds(geometry.NewRect(0, 0, float32(w), float32(h)))

	if window.DefaultManager != nil {
		window.DefaultManager.SetState(newState, w, h)
	}

	v.mu.Lock()
	onResize := v.onResize
	v.mu.Unlock()
	if onResize != nil {
		onResize(w, h)
	}

	if newState == window.StateExpanded && !showSettings && !showStatus {
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
    background: #121826;
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
	v.showStatus = false
	v.mu.Unlock()
	v.SetState(window.StateExpanded)
}

func (v *AppView) OpenSettings() {
	v.mu.Lock()
	v.showSettings = true
	v.showStatus = false
	if v.state == window.StateRest || v.state == window.StateFan {
		v.settingsOpenedFrom = v.state
	}
	v.loadInstanceFieldsLocked(v.selectedInstIdx)
	v.statusMsg = ""
	v.mu.Unlock()
	v.SetState(window.StateExpanded)
}

func (v *AppView) ToggleSettings() {
	v.mu.Lock()
	v.showSettings = !v.showSettings
	if v.showSettings {
		v.showStatus = false
		if v.state == window.StateRest || v.state == window.StateFan {
			v.settingsOpenedFrom = v.state
		}
	}
	v.loadInstanceFieldsLocked(v.selectedInstIdx)
	v.statusMsg = ""
	st := v.showSettings
	targetState := v.settingsOpenedFrom
	if targetState != window.StateRest && targetState != window.StateFan {
		targetState = window.StateFan
	}
	v.mu.Unlock()
	if st {
		v.SetState(window.StateExpanded)
	} else {
		v.SetState(targetState)
	}
}

func (v *AppView) CloseSettings() {
	v.mu.Lock()
	v.showSettings = false
	targetState := v.settingsOpenedFrom
	if targetState != window.StateRest && targetState != window.StateFan {
		targetState = window.StateFan
	}
	v.mu.Unlock()
	v.SetState(targetState)
}

func (v *AppView) IsStatusOpen() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.showStatus
}

func (v *AppView) OpenStatus() {
	v.mu.Lock()
	v.showStatus = true
	v.showSettings = false
	if v.state == window.StateRest || v.state == window.StateFan {
		v.statusOpenedFrom = v.state
	}
	v.mu.Unlock()
	v.SetState(window.StateExpanded)
}

func (v *AppView) CloseStatus() {
	v.mu.Lock()
	v.showStatus = false
	targetState := v.statusOpenedFrom
	if targetState != window.StateRest && targetState != window.StateFan {
		targetState = window.StateFan
	}
	v.mu.Unlock()
	v.SetState(targetState)
}

func (v *AppView) ToggleStatus() {
	v.mu.Lock()
	v.showStatus = !v.showStatus
	if v.showStatus {
		v.showSettings = false
		if v.state == window.StateRest || v.state == window.StateFan {
			v.statusOpenedFrom = v.state
		}
	}
	st := v.showStatus
	targetState := v.statusOpenedFrom
	if targetState != window.StateRest && targetState != window.StateFan {
		targetState = window.StateFan
	}
	v.mu.Unlock()
	if st {
		v.SetState(window.StateExpanded)
	} else {
		v.SetState(targetState)
	}
}

func (v *AppView) RefreshStatus() {
	v.mu.Lock()
	if v.ctx == nil || v.ctx.Err() != nil {
		v.mu.Unlock()
		return
	}
	client := v.statusClient
	enabled := v.config.StatusCheckEnabled
	if client == nil || !enabled {
		v.mu.Unlock()
		return
	}
	v.wg.Add(1)
	v.mu.Unlock()

	go func() {
		defer v.wg.Done()
		rep, err := client.FetchReportContext(v.ctx)
		select {
		case <-v.ctx.Done():
			return
		default:
		}

		v.mu.Lock()
		v.statusReport = rep
		if err != nil {
			v.toast = NewToast(fmt.Sprintf("Status error: %v", err))
		}
		v.mu.Unlock()
		if v.onRedraw != nil {
			v.onRedraw()
		}
	}()
}

func (v *AppView) SetOnConfigReload(cb func()) {
	v.mu.Lock()
	v.onConfigReload = cb
	v.mu.Unlock()
}

func (v *AppView) RefreshIssues() {
	v.mu.Lock()
	if v.ctx == nil || v.ctx.Err() != nil {
		v.mu.Unlock()
		return
	}
	v.wg.Add(1)
	v.mu.Unlock()

	go func() {
		defer v.wg.Done()
		issues, err := v.client.FetchAssignedIssues(v.ctx)
		select {
		case <-v.ctx.Done():
			return
		default:
		}

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
		v.invalidateFilterCacheLocked()

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

func truncateSummary(text string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	clean := strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	runes := []rune(clean)
	if len(runes) <= maxLen {
		return clean
	}
	if maxLen <= 1 {
		return "…"
	}
	return string(runes[:maxLen-1]) + "…"
}

func getStatusBadgeInfo(ind status.Indicator) (icon string, text string, fg widget.Color, bg widget.Color, border widget.Color) {
	switch ind {
	case status.IndicatorMinor, status.IndicatorMaintenance:
		return "⚠", "Atlassian Notice",
			widget.RGBA8(245, 158, 11, 255),
			widget.RGBA8(36, 46, 66, 230),
			widget.RGBA8(255, 255, 255, 45)
	case status.IndicatorMajor, status.IndicatorCritical:
		return "⚠", "Atlassian Outage",
			widget.RGBA8(239, 68, 68, 255),
			widget.RGBA8(36, 46, 66, 230),
			widget.RGBA8(255, 255, 255, 45)
	default:
		return "✓", "Atlassian Cloud OK",
			widget.RGBA8(34, 197, 94, 255),
			widget.RGBA8(36, 46, 66, 230),
			widget.RGBA8(255, 255, 255, 45)
	}
}

func getStatusIndicatorColors(ind status.Indicator) (widget.Color, widget.Color) {
	switch ind {
	case status.IndicatorMinor, status.IndicatorMaintenance:
		return widget.RGBA8(245, 158, 11, 255), widget.RGBA8(245, 158, 11, 75)
	case status.IndicatorMajor, status.IndicatorCritical:
		return widget.RGBA8(239, 68, 68, 255), widget.RGBA8(239, 68, 68, 85)
	default:
		return widget.RGBA8(34, 197, 94, 255), widget.RGBA8(34, 197, 94, 70)
	}
}

func (v *AppView) Draw(ctx widget.Context, canvas widget.Canvas) {
	s := v.snapshot()
	expectedW, expectedH := v.computeSize(s.state)
	w := float32(expectedW)
	h := float32(expectedH)
	b := geometry.NewRect(s.bounds.Min.X, s.bounds.Min.Y, w, h)

	// =========================================================================
	// 1. STATE REST: macOS Dock Glass Capsule with Complete Crisp White Border
	// =========================================================================
	if s.state == window.StateRest {
		w = 36
		h = 224
		b = geometry.NewRect(s.bounds.Min.X, s.bounds.Min.Y, w, h)

		// Inset pill by 1.5px on all sides so the stroke is 100% visible, fully rounded and unclipped
		pillRect := geometry.NewRect(b.Min.X+1.5, b.Min.Y+1.5, w-3.0, h-3.0)
		pillRadius := float32((w - 3.0) / 2.0)

		// 1. Deep frosted glass backdrop
		canvas.DrawRoundRect(pillRect, widget.RGBA8(16, 22, 34, 245), pillRadius)
		// 2. Complete, crisp, prominent frosted white border all the way around
		canvas.StrokeRoundRect(pillRect, widget.RGBA8(255, 255, 255, 140), pillRadius, 1.5)

		// 3. Subtle top drag handle grip dots (···)
		centerX := b.Min.X + w/2
		canvas.DrawCircle(geometry.Pt(centerX-4, b.Min.Y+4.0), 1.0, widget.RGBA8(255, 255, 255, 100))
		canvas.DrawCircle(geometry.Pt(centerX, b.Min.Y+4.0), 1.0, widget.RGBA8(255, 255, 255, 100))
		canvas.DrawCircle(geometry.Pt(centerX+4, b.Min.Y+4.0), 1.0, widget.RGBA8(255, 255, 255, 100))

		topOffset := float32(6)
		bottomOffset := float32(31)

		// 4. Top Global Status Badge (Distinct icon badge, non-instance specific)
		if s.config.StatusCheckEnabled {
			topOffset = 31
			icon, _, fg, bg, border := getStatusBadgeInfo(s.statusReport.OverallIndicator)
			statBadgeRect := geometry.NewRect(centerX-12, b.Min.Y+7, 24, 18)
			stBg := bg
			stBorder := border
			if s.hoveredStatus {
				stBg = widget.RGBA8(52, 68, 96, 245)
				stBorder = widget.RGBA8(255, 255, 255, 120)
			}
			canvas.DrawRoundRect(statBadgeRect, stBg, 5)
			canvas.StrokeRoundRect(statBadgeRect, stBorder, 5, 1.0)
			canvas.DrawText(icon, statBadgeRect, 11, fg, true, widget.TextAlignCenter)

			// Top Divider Line below global status
			topDividerY := b.Min.Y + 29
			canvas.DrawLine(geometry.Pt(b.Min.X+7, topDividerY), geometry.Pt(b.Min.X+w-7, topDividerY), widget.RGBA8(255, 255, 255, 45), 1.0)
		}

		instCount := len(s.instances)
		if instCount == 0 {
			instCount = 1
		}
		usableH := h - topOffset - bottomOffset
		instSectionH := usableH / float32(instCount)

		// Calculate maxCount across instances to scale proportions (e.g. 23)
		maxCount := 1
		for idx, inst := range s.instances {
			c := 0
			for _, iss := range s.issues {
				if isIssueForInstance(iss, inst, idx) {
					c++
				}
			}
			if c > maxCount {
				maxCount = c
			}
		}

		for idx, inst := range s.instances {
			secY := b.Min.Y + topOffset + 2 + float32(idx)*instSectionH

			var instIssues []jira.Issue
			for _, iss := range s.issues {
				if isIssueForInstance(iss, inst, idx) {
					instIssues = append(instIssues, iss)
				}
			}
			count := len(instIssues)

			// User-defined profile accent color palette
			beaconColor, beaconGlow, inProgColor, todoColor, trackColor, badgeBg, badgeBorder := GetInstanceColors(inst.Color, idx)

			// Instance Beacon Core & Radiant Glow (Centered at capsule middle X)
			beaconCenter := geometry.Pt(centerX, secY+8)
			canvas.DrawCircle(beaconCenter, 6.0, beaconGlow)
			canvas.DrawCircle(beaconCenter, 3.5, beaconColor)

			// Edge marker gauge indicator line (placed on the screen-docked edge)
			markerX := b.Min.X + w - 4.5
			if s.dockSide == window.DockSideLeft {
				markerX = b.Min.X + 4.5
			}

			lineTopY := secY + 4
			lineBottomY := secY + instSectionH - 5
			lineTotalH := lineBottomY - lineTopY

			// 1. Background full track
			canvas.DrawLine(geometry.Pt(markerX, lineTopY), geometry.Pt(markerX, lineBottomY), trackColor, 2.0)

			// 2. Scale line height proportionally to maxCount
			ratio := float32(count) / float32(maxCount)
			fillH := ratio * lineTotalH
			if count > 0 && fillH < 4.0 {
				fillH = 4.0
			}

			if count > 0 {
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
				// In Progress segment
				if inProgCount > 0 {
					segH := fillH * (float32(inProgCount) / float32(count))
					canvas.DrawLine(geometry.Pt(markerX, curY), geometry.Pt(markerX, curY+segH), inProgColor, 2.0)
					curY += segH
				}
				// To Do segment
				if todoCount > 0 {
					segH := fillH * (float32(todoCount) / float32(count))
					canvas.DrawLine(geometry.Pt(markerX, curY), geometry.Pt(markerX, curY+segH), todoColor, 2.0)
					curY += segH
				}
				// Done segment
				if doneCount > 0 {
					segH := fillH * (float32(doneCount) / float32(count))
					canvas.DrawLine(geometry.Pt(markerX, curY), geometry.Pt(markerX, curY+segH), widget.RGBA8(34, 197, 94, 255), 2.0)
				}
			}

			// Clean Centered Ticket Count Badge Pill (aligned under beacon)
			countStr := fmt.Sprintf("%d", count)
			badgeW := float32(19)
			badgeH := float32(18)
			badgeX := centerX - badgeW/2
			badgeY := secY + 18
			countBox := geometry.NewRect(badgeX, badgeY, badgeW, badgeH)
			canvas.DrawRoundRect(countBox, badgeBg, 5)
			canvas.StrokeRoundRect(countBox, badgeBorder, 5, 1.0)
			canvas.DrawText(countStr, geometry.NewRect(badgeX, badgeY+2, badgeW, badgeH-2), 10, widget.RGBA8(245, 250, 255, 255), true, widget.TextAlignCenter)

			// Subtle Divider between instances
			if idx < len(s.instances)-1 {
				sepY := secY + instSectionH - 2
				canvas.DrawLine(geometry.Pt(b.Min.X+7, sepY), geometry.Pt(b.Min.X+w-7, sepY), widget.RGBA8(255, 255, 255, 30), 1.0)
			}
		}

		// Bottom Divider Line above settings
		bottomDividerY := b.Min.Y + h - 29
		canvas.DrawLine(geometry.Pt(b.Min.X+7, bottomDividerY), geometry.Pt(b.Min.X+w-7, bottomDividerY), widget.RGBA8(255, 255, 255, 45), 1.0)

		// Settings Gear Icon Badge (Distinct icon badge, non-instance specific)
		setBg := widget.RGBA8(36, 46, 66, 230)
		setBorder := widget.RGBA8(255, 255, 255, 45)
		if s.hoveredSettings {
			setBg = widget.RGBA8(52, 68, 96, 245)
			setBorder = widget.RGBA8(255, 255, 255, 120)
		}
		setRect := geometry.NewRect(centerX-12, b.Min.Y+h-25, 24, 18)
		canvas.DrawRoundRect(setRect, setBg, 5)
		canvas.StrokeRoundRect(setRect, setBorder, 5, 1.0)
		canvas.DrawText("⚙", setRect, 11, widget.RGBA8(220, 235, 255, 240), true, widget.TextAlignCenter)
		return
	}

	// =========================================================================
	// 2. STATE FAN: Vertical Dock Tabs with Instance Header & Search (120xH)
	// =========================================================================
	if s.state == window.StateFan {
		w = 120
		b = geometry.NewRect(s.bounds.Min.X, s.bounds.Min.Y, w, h)
		railBackdrop := geometry.NewRect(b.Min.X+1.0, b.Min.Y+1.0, w-2.0, h-2.0)
		canvas.DrawRoundRect(railBackdrop, widget.RGBA8(20, 28, 44, 200), 14)
		canvas.StrokeRoundRect(railBackdrop, widget.RGBA8(255, 255, 255, 100), 14, 1.5)

		// 1. Top subtle Drag Handle Bar (grab affordance)
		dragBarRect := geometry.NewRect(b.Min.X+w/2-14, b.Min.Y+3, 28, 3)
		canvas.DrawRoundRect(dragBarRect, widget.RGBA8(255, 255, 255, 90), 1.5)

		// 2. Top Instance Header Pill
		instHeaderY := b.Min.Y + 8
		searchY := b.Min.Y + 34
		tabMinY := b.Min.Y + float32(60)

		instName := "All Instances"
		instBeaconColor := widget.RGBA8(56, 189, 248, 240)
		if s.activeInstIdx >= 0 && s.activeInstIdx < len(s.instances) {
			inst := s.instances[s.activeInstIdx]
			instName = inst.Name
			instBeaconColor, _, _, _, _, _, _ = GetInstanceColors(inst.Color, s.activeInstIdx)
		}
		if len(instName) > 12 {
			instName = instName[:10] + ".."
		}

		instHeaderRect := geometry.NewRect(b.Min.X+7, instHeaderY, w-14, 22)
		canvas.DrawRoundRect(instHeaderRect, widget.RGBA8(34, 44, 66, 225), 5)
		canvas.StrokeRoundRect(instHeaderRect, widget.RGBA8(255, 255, 255, 40), 5, 1.0)

		canvas.DrawCircle(geometry.Pt(b.Min.X+16, instHeaderY+11), 3.5, instBeaconColor)
		labelRect := geometry.NewRect(b.Min.X+24, instHeaderY+4, w-46, 14)
		canvas.DrawText(instName, labelRect, 10, widget.RGBA8(235, 245, 255, 255), true, widget.TextAlignCenter)
		if len(s.instances) > 1 {
			canvas.DrawText("▾", geometry.NewRect(b.Min.X+w-20, instHeaderY+4, 12, 14), 9, widget.RGBA8(180, 200, 230, 200), false, widget.TextAlignCenter)
		}

		// 4. Search Bar with Clear Button & Blinking Cursor
		searchRect := geometry.NewRect(b.Min.X+7, searchY, w-14, 22)
		searchBg := widget.RGBA8(14, 20, 30, 220)
		searchBorder := widget.RGBA8(255, 255, 255, 35)
		if s.searchActive || s.searchQuery != "" {
			searchBg = widget.RGBA8(22, 32, 50, 240)
			searchBorder = ColorStatusToDo
		}
		canvas.DrawRoundRect(searchRect, searchBg, 5)
		canvas.StrokeRoundRect(searchRect, searchBorder, 5, 1.0)

		searchTxt := s.searchQuery
		searchColor := widget.RGBA8(240, 245, 255, 255)
		if searchTxt == "" {
			searchTxt = "Search..."
			searchColor = widget.RGBA8(140, 155, 180, 255)
		}
		sTxtRect := geometry.NewRect(b.Min.X+12, searchY+4, w-38, 14)
		canvas.DrawText(searchTxt, sTxtRect, 9, searchColor, false, widget.TextAlignLeft)

		// Blinking search cursor
		if s.searchActive && (time.Now().UnixMilli()/500)%2 == 0 {
			curOffset := measureTextWidth(s.searchQuery, 9)
			curX := b.Min.X + 12 + curOffset + 1.0
			if curX < b.Min.X+w-28 {
				canvas.DrawLine(geometry.Pt(curX, searchY+3), geometry.Pt(curX, searchY+17), widget.RGBA8(255, 255, 255, 220), 1.5)
			}
		}

		// Clear search "✕" button
		if s.searchQuery != "" {
			clearBtnRect := geometry.NewRect(b.Min.X+w-24, searchY+4, 14, 14)
			canvas.DrawCircle(geometry.Pt(b.Min.X+w-17, searchY+11), 6.5, widget.RGBA8(255, 255, 255, 40))
			canvas.DrawText("✕", clearBtnRect, 8, widget.RGBA8(255, 255, 255, 220), true, widget.TextAlignCenter)
		}

		// 5. Tab Area (Smoothly clipped between top search and bottom settings tab)
		tabMaxY := b.Min.Y + h - float32(50)
		tabStartY := tabMinY - s.scrollY
		tabHeight := float32(62)
		tabGap := float32(6)

		canvas.PushClip(geometry.NewRect(b.Min.X+1, tabMinY, w-2, tabMaxY-tabMinY))
		if len(s.filteredIssues) == 0 {
			noRect := geometry.NewRect(b.Min.X+8, tabMinY+14, w-16, 30)
			canvas.DrawText("No tickets", noRect, 10, widget.RGBA8(148, 163, 184, 255), false, widget.TextAlignCenter)
		} else {
			for i, iss := range s.filteredIssues {
				tabY := tabStartY + float32(i)*(tabHeight+tabGap)

				// Skip if completely out of viewport
				if tabY+tabHeight < tabMinY || tabY > tabMaxY {
					continue
				}

				tabTheme := GetTicketTheme(i)
				isHovered := (i == s.hoveredTabIdx)

				tabX := b.Min.X + 7
				tabW := w - 14
				if isHovered {
					if s.dockSide == window.DockSideLeft {
						tabX = b.Min.X + 11 // Lift inward to the right
					} else {
						tabX = b.Min.X + 3 // Lift inward to the left
					}
					tabW = w - 10
				}

				tabRect := geometry.NewRect(tabX, tabY, tabW, tabHeight)

				canvas.DrawRoundRect(tabRect, tabTheme.Background, 8)
				if isHovered {
					// Glowing animated Dock-style hover lift
					canvas.StrokeRoundRect(tabRect, widget.RGBA8(255, 255, 255, 240), 8, 1.5)
					// Vibrant indicator pill on edge
					pillX := tabX + tabW - 5
					if s.dockSide == window.DockSideLeft {
						pillX = tabX + 2
					}
					canvas.DrawRoundRect(geometry.NewRect(pillX, tabY+8, 3, tabHeight-16), tabTheme.Foreground, 1.5)
				} else {
					canvas.StrokeRoundRect(tabRect, tabTheme.Border, 8, 1.0)
				}

				// Line 1: Tab Key (11pt bold)
				keyRect := geometry.NewRect(tabX+2, tabY+5, tabW-4, 15)
				canvas.DrawText(iss.Key, keyRect, 11, tabTheme.Foreground, true, widget.TextAlignCenter)

				// Line 2: Tab Summary (9pt regular, truncated)
				summaryText := truncateSummary(iss.Summary, 17)
				summaryRect := geometry.NewRect(tabX+3, tabY+22, tabW-6, 15)
				summaryColor := tabTheme.Secondary
				if isHovered {
					summaryColor = widget.RGBA8(255, 255, 255, 255)
				}
				canvas.DrawText(summaryText, summaryRect, 9, summaryColor, isHovered, widget.TextAlignCenter)

				// Line 3: Tab Status (8pt regular)
				shortStatus := iss.Status.Name
				statusRunes := []rune(iss.Status.Name)
				if len(statusRunes) > 13 {
					shortStatus = string(statusRunes[:13])
				}
				statusRect := geometry.NewRect(tabX+2, tabY+41, tabW-4, 14)
				secColor := tabTheme.Secondary
				if isHovered {
					secColor = widget.RGBA8(255, 255, 255, 255)
				}
				canvas.DrawText(shortStatus, statusRect, 8, secColor, false, widget.TextAlignCenter)
			}
		}
		canvas.PopClip()

		// 6. Scroll Indicator
		totalTabH := float32(len(s.filteredIssues)) * (tabHeight + tabGap)
		viewTabH := tabMaxY - tabMinY
		if totalTabH > viewTabH && totalTabH > 0 {
			maxScroll := totalTabH - viewTabH
			scrollRatio := s.scrollY / maxScroll
			if scrollRatio < 0 {
				scrollRatio = 0
			}
			if scrollRatio > 1 {
				scrollRatio = 1
			}
			thumbH := float32(24)
			thumbY := tabMinY + scrollRatio*(viewTabH-thumbH)
			thumbRect := geometry.NewRect(b.Min.X+w-4, thumbY, 3, thumbH)
			canvas.DrawRoundRect(thumbRect, widget.RGBA8(255, 255, 255, 140), 1.5)
		}

		// 7. Divider Line before Settings
		dividerY := b.Min.Y + h - 50
		canvas.DrawLine(geometry.Pt(b.Min.X+10, dividerY), geometry.Pt(b.Min.X+w-10, dividerY), widget.RGBA8(255, 255, 255, 55), 1.0)

		// 8. Settings Tab
		settingsTabY := b.Min.Y + h - 42
		setX := b.Min.X + 7
		setW := w - 14
		setBg := widget.RGBA8(36, 46, 66, 230)
		setBorder := widget.RGBA8(255, 255, 255, 45)
		if s.hoveredSettings {
			setX = b.Min.X + 4
			setW = w - 11
			setBg = widget.RGBA8(52, 68, 96, 245)
			setBorder = widget.RGBA8(255, 255, 255, 180)
		}
		settingsTabRect := geometry.NewRect(setX, settingsTabY, setW, 34)
		canvas.DrawRoundRect(settingsTabRect, setBg, 8)
		canvas.StrokeRoundRect(settingsTabRect, setBorder, 8, 1.0)
		setTxtRect := geometry.NewRect(setX+2, settingsTabY+9, setW-4, 16)
		canvas.DrawText("⚙ Settings", setTxtRect, 10, widget.RGBA8(230, 240, 255, 255), true, widget.TextAlignCenter)
		return
	}

	// =========================================================================
	// 3. STATE EXPANDED: Side Dock Column + Multi-Instance Settings Overlay (780x580)
	// =========================================================================
	w = 780
	h = 580
	b = geometry.NewRect(s.bounds.Min.X, s.bounds.Min.Y, w, h)
	tabBarWidth := float32(110)
	cardAreaWidth := w - tabBarWidth - 14

	tabStartX := b.Min.X + w - tabBarWidth
	cardStartX := b.Min.X + 8
	if s.dockSide == window.DockSideLeft {
		tabStartX = b.Min.X + 6
		cardStartX = b.Min.X + tabBarWidth + 8
	}

	dockShelfRect := geometry.NewRect(tabStartX-2, b.Min.Y+8, tabBarWidth-4, h-16)
	canvas.DrawRoundRect(dockShelfRect, widget.RGBA8(20, 28, 44, 175), 12)
	canvas.StrokeRoundRect(dockShelfRect, widget.RGBA8(255, 255, 255, 80), 12, 1.5)

	// Top controls on the dock shelf: Global Status Button & Native Close Button
	if s.config.StatusCheckEnabled {
		icon, _, fg, bg, border := getStatusBadgeInfo(s.statusReport.OverallIndicator)
		statusBtnRect := geometry.NewRect(tabStartX+6, b.Min.Y+12, tabBarWidth-44, 22)
		stBg := bg
		stBorder := border
		if s.showStatus {
			stBg = widget.RGBA8(46, 62, 92, 255)
			stBorder = widget.RGBA8(255, 255, 255, 120)
		} else if s.hoveredStatus {
			stBg = widget.RGBA8(52, 68, 96, 245)
			stBorder = widget.RGBA8(255, 255, 255, 120)
		}
		canvas.DrawRoundRect(statusBtnRect, stBg, 6)
		canvas.StrokeRoundRect(statusBtnRect, stBorder, 6, 1.0)
		canvas.DrawText(icon+" Status", statusBtnRect, 9, fg, true, widget.TextAlignCenter)

		closeBtnRect := geometry.NewRect(tabStartX+tabBarWidth-34, b.Min.Y+12, 28, 22)
		canvas.DrawRoundRect(closeBtnRect, widget.RGBA8(28, 38, 58, 240), 6)
		canvas.StrokeRoundRect(closeBtnRect, widget.RGBA8(255, 255, 255, 140), 6, 1.0)
		canvas.DrawText("✕", closeBtnRect, 10, widget.RGBA8(255, 255, 255, 240), true, widget.TextAlignCenter)
	} else {
		closeBtnRect := geometry.NewRect(tabStartX+(tabBarWidth-36)/2, b.Min.Y+12, 36, 22)
		canvas.DrawRoundRect(closeBtnRect, widget.RGBA8(28, 38, 58, 240), 6)
		canvas.StrokeRoundRect(closeBtnRect, widget.RGBA8(255, 255, 255, 140), 6, 1.0)
		canvas.DrawText("✕", closeBtnRect, 10, widget.RGBA8(255, 255, 255, 240), true, widget.TextAlignCenter)
	}

	// Divider line below top dock controls
	topDividerY := b.Min.Y + 38
	canvas.DrawLine(geometry.Pt(tabStartX+8, topDividerY), geometry.Pt(tabStartX+tabBarWidth-12, topDividerY), widget.RGBA8(255, 255, 255, 45), 1.0)

	// Draw side tabs inside the dock shelf with strict bounds clipping
	tabMinY := b.Min.Y + float32(44)
	tabMaxY := b.Min.Y + h - float32(56)
	tabStartY := tabMinY - s.scrollY
	tabHeight := float32(64)
	tabGap := float32(7)

	var activeKey string
	if s.activeIdx >= 0 && s.activeIdx < len(s.issues) {
		activeKey = s.issues[s.activeIdx].Key
	}

	if !s.showStatus {
		canvas.PushClip(geometry.NewRect(tabStartX-2, tabMinY, tabBarWidth-4, tabMaxY-tabMinY))
		for i, iss := range s.filteredIssues {
			tabY := tabStartY + float32(i)*(tabHeight+tabGap)

			if tabY < tabMinY-2 || tabY+tabHeight > tabMaxY+2 {
				continue
			}

			tabTheme := GetTicketTheme(i)
			isActive := (iss.Key == activeKey && !s.showSettings)
			isHovered := (i == s.hoveredTabIdx)
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
				pillX := tabX + tabWidth - 5
				if s.dockSide == window.DockSideLeft {
					pillX = tabX + 2
				}
				canvas.DrawRoundRect(geometry.NewRect(pillX, tabY+8, 3, tabHeight-16), tabTheme.Foreground, 1.5)
			} else if isHovered {
				canvas.StrokeRoundRect(tabRect, widget.RGBA8(255, 255, 255, 200), 8, 1.2)
			} else {
				canvas.StrokeRoundRect(tabRect, tabTheme.Border, 8, 1.0)
			}

			// Line 1: Tab Key (11pt bold)
			keyRect := geometry.NewRect(tabX+2, tabY+6, tabWidth-4, 15)
			canvas.DrawText(iss.Key, keyRect, 11, tabTheme.Foreground, true, widget.TextAlignCenter)

			// Line 2: Tab Summary (9pt regular, truncated)
			summaryText := truncateSummary(iss.Summary, 17)
			summaryRect := geometry.NewRect(tabX+3, tabY+23, tabWidth-6, 15)
			summaryColor := tabTheme.Secondary
			if isActive || isHovered {
				summaryColor = widget.RGBA8(255, 255, 255, 255)
			}
			canvas.DrawText(summaryText, summaryRect, 9, summaryColor, isActive || isHovered, widget.TextAlignCenter)

			// Line 3: Tab Status (8pt regular)
			shortStatus := iss.Status.Name
			statusRunes := []rune(iss.Status.Name)
			if len(statusRunes) > 13 {
				shortStatus = string(statusRunes[:13])
			}
			statusRect := geometry.NewRect(tabX+2, tabY+43, tabWidth-4, 14)
			secColor := tabTheme.Secondary
			if isActive || isHovered {
				secColor = widget.RGBA8(255, 255, 255, 255)
			}
			canvas.DrawText(shortStatus, statusRect, 8, secColor, false, widget.TextAlignCenter)
		}
		canvas.PopClip()

		// Scroll Indicator in Expanded Rail
		totalRailH := float32(len(s.filteredIssues)) * (tabHeight + tabGap)
		viewRailH := tabMaxY - tabMinY
		if totalRailH > viewRailH && totalRailH > 0 {
			maxScroll := totalRailH - viewRailH
			scrollRatio := s.scrollY / maxScroll
			if scrollRatio < 0 {
				scrollRatio = 0
			}
			if scrollRatio > 1 {
				scrollRatio = 1
			}
			thumbH := float32(28)
			thumbY := tabMinY + scrollRatio*(viewRailH-thumbH)
			scrollThumbX := tabStartX + tabBarWidth - 8
			if s.dockSide == window.DockSideLeft {
				scrollThumbX = tabStartX + 2
			}
			thumbRect := geometry.NewRect(scrollThumbX, thumbY, 3, thumbH)
			canvas.DrawRoundRect(thumbRect, widget.RGBA8(255, 255, 255, 150), 1.5)
		}
	}

	// Dock Divider Line
	dividerY := b.Min.Y + h - 56
	canvas.DrawLine(geometry.Pt(tabStartX+8, dividerY), geometry.Pt(tabStartX+tabBarWidth-12, dividerY), widget.RGBA8(255, 255, 255, 55), 1.0)

	// Settings tab at bottom of dock shelf
	settingsTabY := b.Min.Y + h - 48
	settingsTabRect := geometry.NewRect(tabStartX+2, settingsTabY, tabBarWidth-14, 34)
	settingsBg := widget.RGBA8(36, 46, 66, 220)
	if s.showSettings {
		settingsBg = widget.RGBA8(54, 70, 98, 230)
	}
	canvas.DrawRoundRect(settingsTabRect, settingsBg, 8)
	canvas.StrokeRoundRect(settingsTabRect, widget.RGBA8(255, 255, 255, 45), 8, 1.0)
	setTxtRect := geometry.NewRect(tabStartX+4, settingsTabY+9, tabBarWidth-18, 16)
	canvas.DrawText("⚙ Settings", setTxtRect, 10, widget.RGBA8(230, 240, 255, 255), true, widget.TextAlignCenter)

	// Overlays (if active)
	if s.showStatus {
		cardRect := geometry.NewRect(cardStartX, b.Min.Y+8, cardAreaWidth, h-16)
		v.drawStatusPanel(ctx, canvas, cardRect, s)
	} else if s.showSettings {
		cardRect := geometry.NewRect(cardStartX, b.Min.Y+8, cardAreaWidth, h-16)
		v.drawSettingsOverlay(ctx, canvas, cardRect, s)
	}

	// Toast Notification
	if s.toast.IsActive() {
		msg := s.toast.Message
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
		if s.showSettings {
			toastY = b.Min.Y + h - 80
		}

		tRect := geometry.NewRect(toastX, toastY, toastW, toastH)
		canvas.DrawRoundRect(tRect, ColorToastBg, 15)
		canvas.StrokeRoundRect(tRect, ColorToastBorder, 15, 1.0)

		tTxtRect := geometry.NewRect(toastX+10, toastY+7, toastW-20, 16)
		canvas.DrawText(msg, tTxtRect, 11, widget.RGBA8(245, 250, 255, 255), true, widget.TextAlignCenter)
	}
}

type statusAppInfo struct {
	Name string
	Key  string
	URL  string
}

var atlassianCoreApps = []statusAppInfo{
	{"Jira Software", "jira-software", "https://jira-software.status.atlassian.com"},
	{"Jira Service Management", "jira-service-management", "https://jira-service-management.status.atlassian.com"},
	{"Confluence", "confluence", "https://confluence.status.atlassian.com"},
	{"Bitbucket", "bitbucket", "https://status.bitbucket.org"},
	{"Atlassian Migrations", "migrations", "https://migrations.status.atlassian.com"},
	{"Atlassian Analytics", "analytics", "https://analytics.status.atlassian.com"},
	{"Rovo", "rovo", "https://rovo.status.atlassian.com"},
	{"Rovo Dev", "rovodev", "https://rovodev.status.atlassian.com"},
}

func (v *AppView) drawStatusPanel(ctx widget.Context, canvas widget.Canvas, r geometry.Rect, s appViewStateSnapshot) {
	radius := float32(14)
	canvas.DrawRoundRect(r, widget.RGBA8(18, 24, 36, 250), radius)
	canvas.StrokeRoundRect(r, widget.RGBA8(255, 255, 255, 70), radius, 1.0)

	// 1. Top Header
	hdrRect := geometry.NewRect(r.Min.X+22, r.Min.Y+16, 280, 22)
	canvas.DrawText("Atlassian Cloud Status", hdrRect, 15, widget.RGBA8(245, 250, 255, 255), true, widget.TextAlignLeft)

	subHdrRect := geometry.NewRect(r.Min.X+22, r.Min.Y+38, 300, 14)
	canvas.DrawText("Live status & incident telemetry from status.atlassian.com", subHdrRect, 9, widget.RGBA8(148, 163, 184, 255), false, widget.TextAlignLeft)

	// Refresh button
	refRect := geometry.NewRect(r.Min.X+r.Width()-140, r.Min.Y+14, 76, 26)
	canvas.DrawRoundRect(refRect, widget.RGBA8(40, 52, 74, 240), 5)
	canvas.StrokeRoundRect(refRect, widget.RGBA8(255, 255, 255, 60), 5, 1.0)
	canvas.DrawText("↻ Refresh", refRect, 10, widget.RGBA8(225, 240, 255, 255), false, widget.TextAlignCenter)

	// Close button
	closeRect := geometry.NewRect(r.Min.X+r.Width()-54, r.Min.Y+14, 40, 26)
	canvas.DrawRoundRect(closeRect, widget.RGBA8(38, 48, 68, 240), 5)
	canvas.StrokeRoundRect(closeRect, widget.RGBA8(255, 255, 255, 60), 5, 1.0)
	canvas.DrawText("✕", closeRect, 11, widget.RGBA8(240, 245, 255, 255), true, widget.TextAlignCenter)

	// 2. Global Status Health Banner
	bannerY := r.Min.Y + 62
	bannerH := float32(48)
	bannerRect := geometry.NewRect(r.Min.X+20, bannerY, r.Width()-40, bannerH)

	statCol, statGlow := getStatusIndicatorColors(s.statusReport.OverallIndicator)
	bannerBg := widget.RGBA8(20, 48, 36, 220)
	bannerBorder := widget.RGBA8(34, 197, 94, 160)
	statusTitle := "All Systems Operational"

	switch s.statusReport.OverallIndicator {
	case status.IndicatorMinor, status.IndicatorMaintenance:
		bannerBg = widget.RGBA8(55, 42, 18, 220)
		bannerBorder = widget.RGBA8(245, 158, 11, 160)
		statusTitle = "Active Minor Service Outage / Maintenance"
	case status.IndicatorMajor, status.IndicatorCritical:
		bannerBg = widget.RGBA8(65, 22, 26, 220)
		bannerBorder = widget.RGBA8(239, 68, 68, 180)
		statusTitle = "Major Service Outage Detected"
	}
	if s.statusReport.OverallText != "" && s.statusReport.OverallIndicator != status.IndicatorNone {
		statusTitle = s.statusReport.OverallText
	}

	canvas.DrawRoundRect(bannerRect, bannerBg, 8)
	canvas.StrokeRoundRect(bannerRect, bannerBorder, 8, 1.0)

	bannerBeaconCenter := geometry.Pt(bannerRect.Min.X+22, bannerRect.Min.Y+24)
	canvas.DrawCircle(bannerBeaconCenter, 9.0, statGlow)
	canvas.DrawCircle(bannerBeaconCenter, 5.5, statCol)

	bTitleRect := geometry.NewRect(bannerRect.Min.X+40, bannerRect.Min.Y+9, bannerRect.Width()-180, 18)
	canvas.DrawText(statusTitle, bTitleRect, 13, widget.RGBA8(245, 250, 255, 255), true, widget.TextAlignLeft)

	lastCheckText := "Checked just now"
	if !s.statusReport.LastChecked.IsZero() {
		lastCheckText = fmt.Sprintf("Checked: %s", s.statusReport.LastChecked.Format("15:04:05"))
	}
	if s.statusReport.Error != "" {
		lastCheckText = fmt.Sprintf("Notice: %s", s.statusReport.Error)
	}
	bTimeRect := geometry.NewRect(bannerRect.Min.X+40, bannerRect.Min.Y+28, bannerRect.Width()-180, 14)
	canvas.DrawText(lastCheckText, bTimeRect, 9, widget.RGBA8(180, 195, 215, 255), false, widget.TextAlignLeft)

	// Direct link button on banner
	extLinkRect := geometry.NewRect(bannerRect.Max.X-135, bannerRect.Min.Y+12, 122, 24)
	canvas.DrawRoundRect(extLinkRect, widget.RGBA8(30, 42, 60, 220), 4)
	canvas.StrokeRoundRect(extLinkRect, widget.RGBA8(255, 255, 255, 40), 4, 1.0)
	canvas.DrawText("Statuspage ↗", extLinkRect, 10, widget.RGBA8(210, 230, 255, 255), false, widget.TextAlignCenter)

	// 3. Application Services Grid (Grouped by Application)
	gridY := bannerY + bannerH + 12
	secLblRect := geometry.NewRect(r.Min.X+22, gridY, 250, 14)
	canvas.DrawText("APPLICATION SERVICES", secLblRect, 9, widget.RGBA8(148, 163, 184, 255), true, widget.TextAlignLeft)

	gridCardsY := gridY + 16
	cardGap := float32(8)
	cardW := (r.Width() - 40 - cardGap) / 2
	cardH := float32(42)

	for i, app := range atlassianCoreApps {
		col := float32(i % 2)
		row := float32(i / 2)
		cardX := r.Min.X + 20 + col*(cardW+cardGap)
		cardY := gridCardsY + row*(cardH+cardGap)
		appRect := geometry.NewRect(cardX, cardY, cardW, cardH)

		appInd := status.IndicatorNone
		appDesc := "Operational"
		for _, serv := range s.statusReport.Services {
			if strings.EqualFold(serv.Name, app.Name) || strings.EqualFold(serv.PageID, app.Key) || strings.Contains(strings.ToLower(serv.Name), strings.ToLower(app.Name)) {
				appInd = serv.Indicator
				if serv.Description != "" {
					appDesc = serv.Description
				}
				break
			}
		}

		appCol, appGlow := getStatusIndicatorColors(appInd)
		canvas.DrawRoundRect(appRect, widget.RGBA8(22, 30, 46, 220), 7)
		canvas.StrokeRoundRect(appRect, widget.RGBA8(255, 255, 255, 30), 7, 1.0)

		canvas.DrawCircle(geometry.Pt(appRect.Min.X+16, appRect.Min.Y+15), 6.5, appGlow)
		canvas.DrawCircle(geometry.Pt(appRect.Min.X+16, appRect.Min.Y+15), 3.5, appCol)

		tRect := geometry.NewRect(appRect.Min.X+30, appRect.Min.Y+6, appRect.Width()-112, 15)
		canvas.DrawText(app.Name, tRect, 10, widget.RGBA8(240, 245, 255, 255), true, widget.TextAlignLeft)

		dRect := geometry.NewRect(appRect.Min.X+30, appRect.Min.Y+22, appRect.Width()-112, 13)
		canvas.DrawText(appDesc, dRect, 8, widget.RGBA8(148, 163, 184, 255), false, widget.TextAlignLeft)

		badgeW := float32(72)
		badgeRect := geometry.NewRect(appRect.Max.X-badgeW-8, appRect.Min.Y+11, badgeW, 20)
		badgeBg := widget.RGBA8(20, 52, 36, 220)
		badgeBorder := widget.RGBA8(34, 197, 94, 120)
		badgeTxt := "Operational"
		if appInd != status.IndicatorNone {
			badgeBg = widget.RGBA8(58, 38, 18, 220)
			badgeBorder = widget.RGBA8(245, 158, 11, 140)
			badgeTxt = "Notice"
			if appInd == status.IndicatorMajor || appInd == status.IndicatorCritical {
				badgeBg = widget.RGBA8(65, 24, 28, 220)
				badgeBorder = widget.RGBA8(239, 68, 68, 160)
				badgeTxt = "Outage"
			}
		}
		canvas.DrawRoundRect(badgeRect, badgeBg, 4)
		canvas.StrokeRoundRect(badgeRect, badgeBorder, 4, 1.0)
		canvas.DrawText(badgeTxt, badgeRect, 8, appCol, true, widget.TextAlignCenter)
	}

	// 4. Active Incidents Section
	gridRows := float32((len(atlassianCoreApps) + 1) / 2)
	incSecY := gridCardsY + gridRows*(cardH+cardGap) + 6
	incSecLbl := geometry.NewRect(r.Min.X+22, incSecY, 280, 14)
	canvas.DrawText("ACTIVE INCIDENTS & MAINTENANCE NOTICES", incSecLbl, 9, widget.RGBA8(148, 163, 184, 255), true, widget.TextAlignLeft)

	incListY := incSecY + 18
	if len(s.statusReport.ActiveIncidents) == 0 {
		emptyRect := geometry.NewRect(r.Min.X+20, incListY, r.Width()-40, 52)
		canvas.DrawRoundRect(emptyRect, widget.RGBA8(16, 24, 36, 180), 8)
		canvas.StrokeRoundRect(emptyRect, widget.RGBA8(255, 255, 255, 20), 8, 1.0)

		canvas.DrawCircle(geometry.Pt(emptyRect.Min.X+26, emptyRect.Min.Y+26), 7.0, widget.RGBA8(34, 197, 94, 60))
		canvas.DrawCircle(geometry.Pt(emptyRect.Min.X+26, emptyRect.Min.Y+26), 4.0, widget.RGBA8(34, 197, 94, 255))

		eTitleRect := geometry.NewRect(emptyRect.Min.X+44, emptyRect.Min.Y+11, emptyRect.Width()-55, 15)
		canvas.DrawText("No Active Incidents", eTitleRect, 10, widget.RGBA8(230, 240, 255, 255), true, widget.TextAlignLeft)

		eSubRect := geometry.NewRect(emptyRect.Min.X+44, emptyRect.Min.Y+27, emptyRect.Width()-55, 13)
		canvas.DrawText("All Atlassian Cloud services are operating normally with no active disruptions.", eSubRect, 8, widget.RGBA8(148, 163, 184, 255), false, widget.TextAlignLeft)
	} else {
		maxInc := 2
		if len(s.statusReport.ActiveIncidents) < maxInc {
			maxInc = len(s.statusReport.ActiveIncidents)
		}
		incH := float32(56)
		for idx := 0; idx < maxInc; idx++ {
			inc := s.statusReport.ActiveIncidents[idx]
			cardY := incListY + float32(idx)*(incH+6)
			incCardRect := geometry.NewRect(r.Min.X+20, cardY, r.Width()-40, incH)

			canvas.DrawRoundRect(incCardRect, widget.RGBA8(32, 22, 28, 220), 8)
			canvas.StrokeRoundRect(incCardRect, widget.RGBA8(239, 68, 68, 80), 8, 1.0)

			badgeW := float32(60)
			badgeRect := geometry.NewRect(incCardRect.Min.X+10, incCardRect.Min.Y+10, badgeW, 16)
			canvas.DrawRoundRect(badgeRect, widget.RGBA8(239, 68, 68, 200), 3)
			canvas.DrawText(strings.ToUpper(inc.Impact), badgeRect, 8, widget.RGBA8(255, 255, 255, 255), true, widget.TextAlignCenter)

			titleRect := geometry.NewRect(incCardRect.Min.X+78, incCardRect.Min.Y+10, incCardRect.Width()-170, 15)
			canvas.DrawText(truncateSummary(inc.Name, 45), titleRect, 10, widget.RGBA8(255, 240, 240, 255), true, widget.TextAlignLeft)

			statusTxt := fmt.Sprintf("Status: %s", inc.Status)
			if !inc.UpdatedAt.IsZero() {
				statusTxt += fmt.Sprintf(" • %s", inc.UpdatedAt.Format("15:04 MST"))
			}
			stRect := geometry.NewRect(incCardRect.Min.X+10, incCardRect.Min.Y+30, incCardRect.Width()-95, 13)
			canvas.DrawText(statusTxt, stRect, 8, widget.RGBA8(200, 180, 190, 255), false, widget.TextAlignLeft)

			if inc.URL != "" {
				linkRect := geometry.NewRect(incCardRect.Max.X-75, incCardRect.Min.Y+16, 65, 20)
				canvas.DrawRoundRect(linkRect, widget.RGBA8(50, 30, 40, 220), 4)
				canvas.StrokeRoundRect(linkRect, widget.RGBA8(255, 255, 255, 40), 4, 1.0)
				canvas.DrawText("Details ↗", linkRect, 8, widget.RGBA8(255, 220, 230, 255), false, widget.TextAlignCenter)
			}
		}
	}

	// 5. Footer Row
	footY := r.Min.Y + r.Height() - 34
	footTextRect := geometry.NewRect(r.Min.X+22, footY+2, r.Width()-210, 15)
	canvas.DrawText("Official incident feed & RSS updates available at status.atlassian.com", footTextRect, 9, widget.RGBA8(100, 116, 139, 255), false, widget.TextAlignLeft)

	footBtnRect := geometry.NewRect(r.Min.X+r.Width()-185, footY-1, 165, 22)
	canvas.DrawRoundRect(footBtnRect, widget.RGBA8(32, 42, 60, 240), 5)
	canvas.StrokeRoundRect(footBtnRect, widget.RGBA8(255, 255, 255, 40), 5, 1.0)
	canvas.DrawText("Open status.atlassian.com ↗", footBtnRect, 9, widget.RGBA8(210, 230, 255, 255), false, widget.TextAlignCenter)
}

func (v *AppView) Event(ctx widget.Context, e event.Event) bool {
	switch ev := e.(type) {
	case *event.MouseEvent:
		v.mu.Lock()
		st := v.state
		v.mu.Unlock()
		w, h := v.computeSize(st)
		currentRect := geometry.NewRect(0, 0, float32(w), float32(h))
		contains := currentRect.Contains(ev.Position)

		if ev.MouseType == event.MousePress {
			if contains {
				return v.handleClick(ev.Position)
			}
		} else if ev.MouseType == event.MouseMove {
			if contains {
				return v.handleHover(ev.Position)
			} else {
				v.setHoveredTab(-1)
			}
		}
	case *event.WheelEvent:
		filtered := v.getFilteredIssues()
		issueCount := len(filtered)

		v.mu.Lock()
		st := v.state
		v.mu.Unlock()

		if st == window.StateFan || st == window.StateExpanded {
			tabHeight := float32(62)
			tabGap := float32(6)
			if st == window.StateExpanded {
				tabHeight = float32(64)
				tabGap = float32(7)
			}
			totalH := float32(issueCount) * (tabHeight + tabGap)
			b := v.Bounds()
			viewH := b.Height() - 110
			maxScroll := totalH - viewH
			if maxScroll < 0 {
				maxScroll = 0
			}

			v.mu.Lock()
			v.scrollY -= ev.Delta.Y * 0.8
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
	statusEnabled := v.config.StatusCheckEnabled
	scrollY := v.scrollY
	v.mu.Unlock()

	expW, expH := v.computeSize(st)
	w := float32(expW)
	h := float32(expH)
	b := geometry.NewRect(0, 0, w, h)

	// REST STATE: Hovering specific instance, status badge, or settings badge
	if st == window.StateRest {
		v.mu.Lock()
		autoH := v.autoHide
		tucked := v.isTucked
		v.mu.Unlock()
		if autoH && tucked {
			v.setTucked(false)
		}

		if statusEnabled && pos.Y >= b.Min.Y+6 && pos.Y <= b.Min.Y+28 {
			v.mu.Lock()
			v.hoveredStatus = true
			v.hoveredSettings = false
			v.mu.Unlock()
			v.MarkNeedsLayout()
			if v.onRedraw != nil {
				v.onRedraw()
			}
			return true
		}

		if pos.Y >= b.Min.Y+h-28 {
			v.mu.Lock()
			v.hoveredSettings = true
			v.hoveredStatus = false
			v.mu.Unlock()
			v.MarkNeedsLayout()
			if v.onRedraw != nil {
				v.onRedraw()
			}
			return true
		}

		v.mu.Lock()
		if v.hoveredStatus || v.hoveredSettings {
			v.hoveredStatus = false
			v.hoveredSettings = false
			v.MarkNeedsLayout()
			if v.onRedraw != nil {
				v.onRedraw()
			}
		}

		if v.restHoverStart.IsZero() {
			v.restHoverStart = time.Now()
			v.mu.Unlock()
			return true
		}
		if time.Since(v.restHoverStart) < 150*time.Millisecond {
			v.mu.Unlock()
			return true
		}
		v.mu.Unlock()

		topOffset := float32(6)
		bottomOffset := float32(31)
		if statusEnabled {
			topOffset = float32(31)
		}
		if instCount > 0 {
			usableH := h - topOffset - bottomOffset
			instSectionH := usableH / float32(instCount)
			instIdx := int((pos.Y - (b.Min.Y + topOffset + 2)) / instSectionH)
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
			v.invalidateFilterCacheLocked()
			v.mu.Unlock()
		}

		v.SetState(window.StateFan)
		return true
	}

	// FAN STATE: Track hovered tab / settings button / status button for interactive lift effect
	if st == window.StateFan {
		v.mu.Lock()
		v.lastHover = time.Now()
		prevHoverTab := v.hoveredTabIdx
		prevHoverSet := v.hoveredSettings
		prevHoverStat := v.hoveredStatus
		v.mu.Unlock()

		newHoverTab := -1
		newHoverSet := false
		newHoverStat := false

		if pos.Y >= b.Min.Y+h-44 {
			newHoverSet = true
		} else {
			tabMinY := b.Min.Y + float32(60)
			tabMaxY := b.Min.Y + h - float32(50)
			tabStartY := tabMinY - scrollY
			tabHeight := float32(62)
			tabGap := float32(6)

			tabVisibleRect := geometry.NewRect(b.Min.X+2, tabMinY, w-4, tabMaxY-tabMinY)
			filtered := v.getFilteredIssues()
			for i := range filtered {
				tabY := tabStartY + float32(i)*(tabHeight+tabGap)
				tabRect := geometry.NewRect(b.Min.X+2, tabY, w-4, tabHeight)
				if tabRect.Contains(pos) && tabVisibleRect.Contains(pos) {
					newHoverTab = i
					break
				}
			}
		}

		if newHoverTab != prevHoverTab || newHoverSet != prevHoverSet || newHoverStat != prevHoverStat {
			v.mu.Lock()
			v.hoveredSettings = newHoverSet
			v.hoveredStatus = newHoverStat
			v.mu.Unlock()
			v.setHoveredTab(newHoverTab)
			v.MarkNeedsLayout()
			if v.onRedraw != nil {
				v.onRedraw()
			}
		}
		return true
	}

	// EXPANDED STATE: Track hovered tab / status / settings on the side dock shelf
	if st == window.StateExpanded {
		v.mu.Lock()
		prevHoverTab := v.hoveredTabIdx
		prevHoverSet := v.hoveredSettings
		prevHoverStat := v.hoveredStatus
		dockSide := v.dockSide
		showStatus := v.showStatus
		v.mu.Unlock()

		newHoverTab := -1
		newHoverSet := false
		newHoverStat := false

		tabBarWidth := float32(110)
		tabStartX := b.Min.X + w - tabBarWidth
		if dockSide == window.DockSideLeft {
			tabStartX = b.Min.X + 6
		}

		if statusEnabled {
			statusBtnRect := geometry.NewRect(tabStartX+6, b.Min.Y+12, tabBarWidth-44, 22)
			if statusBtnRect.Contains(pos) {
				newHoverStat = true
			}
		}

		settingsTabRect := geometry.NewRect(tabStartX+2, b.Min.Y+h-48, tabBarWidth-14, 34)
		if settingsTabRect.Contains(pos) {
			newHoverSet = true
		} else if !newHoverStat && !showStatus {
			filtered := v.getFilteredIssues()
			tabMinY := b.Min.Y + float32(44)
			tabMaxY := b.Min.Y + h - float32(56)
			tabStartY := tabMinY - scrollY
			tabHeight := float32(64)
			tabGap := float32(7)

			tabVisibleRect := geometry.NewRect(tabStartX, tabMinY, tabBarWidth-4, tabMaxY-tabMinY)
			for i := range filtered {
				tabY := tabStartY + float32(i)*(tabHeight+tabGap)
				tabRect := geometry.NewRect(tabStartX, tabY, tabBarWidth-4, tabHeight)
				if tabRect.Contains(pos) && tabVisibleRect.Contains(pos) {
					newHoverTab = i
					break
				}
			}
		}

		if newHoverTab != prevHoverTab || newHoverSet != prevHoverSet || newHoverStat != prevHoverStat {
			v.mu.Lock()
			v.hoveredSettings = newHoverSet
			v.hoveredStatus = newHoverStat
			v.mu.Unlock()
			v.setHoveredTab(newHoverTab)
			v.MarkNeedsLayout()
			if v.onRedraw != nil {
				v.onRedraw()
			}
		}
		return true
	}

	return false
}

func (v *AppView) getActiveTargetLocked() *string {
	if v.showSettings && ((v.settingsSection == settingsInstances && v.activeField > 5) ||
		(v.settingsSection == settingsApplication && v.activeField < 6)) {
		return nil
	}
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
	case 6:
		return &v.intervalVal
	case 7:
		return &v.statusIntervalVal
	default:
		return nil
	}
}

func (v *AppView) handleClick(pos geometry.Point) bool {
	v.mu.Lock()
	st := v.state
	allIssues := v.issues
	showSettings := v.showSettings
	showStatus := v.showStatus
	statusEnabled := v.config.StatusCheckEnabled
	scrollY := v.scrollY
	activeIdx := v.activeIdx
	v.mu.Unlock()

	expW, expH := v.computeSize(st)
	w := float32(expW)
	h := float32(expH)
	b := geometry.NewRect(0, 0, w, h)

	filteredIssues := v.getFilteredIssues()

	// REST STATE: Click affordances
	if st == window.StateRest {
		if statusEnabled && pos.Y >= b.Min.Y+6 && pos.Y <= b.Min.Y+28 {
			v.ToggleStatus()
			return true
		}
		if pos.Y <= b.Min.Y+6 {
			v.StartDrag()
			return true
		}
		if pos.Y >= b.Min.Y+h-28 {
			v.OpenSettings()
			return true
		}
		v.SetState(window.StateFan)
		return true
	}

	// 1. Fan State Clicks
	if st == window.StateFan {
		if pos.Y <= b.Min.Y+6 {
			v.StartDrag()
			return true
		}

		instHeaderY := b.Min.Y + 8
		searchY := b.Min.Y + 34
		tabMinY := b.Min.Y + float32(60)

		// Clear search "✕" button
		v.mu.Lock()
		sq := v.searchQuery
		v.mu.Unlock()
		if sq != "" && pos.X >= b.Min.X+w-28 && pos.X <= b.Min.X+w-6 && pos.Y >= searchY && pos.Y <= searchY+22 {
			v.mu.Lock()
			v.searchQuery = ""
			v.searchActive = true
			v.scrollY = 0
			v.invalidateFilterCacheLocked()
			v.mu.Unlock()
			v.MarkNeedsLayout()
			return true
		}

		// Click header to cycle instance filter
		instHeaderRect := geometry.NewRect(b.Min.X+7, instHeaderY, w-14, 22)
		v.mu.Lock()
		instLen := len(v.config.Instances)
		v.mu.Unlock()
		if instHeaderRect.Contains(pos) && instLen > 1 {
			v.mu.Lock()
			v.activeInstIdx = (v.activeInstIdx + 1) % len(v.config.Instances)
			v.scrollY = 0
			v.invalidateFilterCacheLocked()
			v.mu.Unlock()
			v.MarkNeedsLayout()
			return true
		}

		searchRect := geometry.NewRect(b.Min.X+7, searchY, w-14, 22)
		if searchRect.Contains(pos) {
			v.mu.Lock()
			v.searchActive = true
			v.mu.Unlock()
			v.MarkNeedsLayout()
			return true
		}

		settingsTabRect := geometry.NewRect(b.Min.X+7, b.Min.Y+h-42, w-14, 34)
		if settingsTabRect.Contains(pos) {
			v.OpenSettings()
			return true
		}

		tabMaxY := b.Min.Y + h - float32(50)
		tabStartY := tabMinY - scrollY
		tabHeight := float32(62)
		tabGap := float32(6)

		tabVisibleRect := geometry.NewRect(b.Min.X+7, tabMinY, w-14, tabMaxY-tabMinY)
		for i, fIss := range filteredIssues {
			tabY := tabStartY + float32(i)*(tabHeight+tabGap)
			tabRect := geometry.NewRect(b.Min.X+7, tabY, w-14, tabHeight)
			if tabRect.Contains(pos) && tabVisibleRect.Contains(pos) {
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
	tabBarWidth := float32(110)
	cardAreaWidth := w - tabBarWidth - 14

	tabStartX := b.Min.X + w - tabBarWidth
	cardStartX := b.Min.X + 8
	if v.dockSide == window.DockSideLeft {
		tabStartX = b.Min.X + 6
		cardStartX = b.Min.X + tabBarWidth + 8
	}

	// Top shelf close and status affordances
	if statusEnabled {
		statusBtnRect := geometry.NewRect(tabStartX+6, b.Min.Y+12, tabBarWidth-44, 22)
		if statusBtnRect.Contains(pos) {
			v.ToggleStatus()
			return true
		}
		closeBtnRect := geometry.NewRect(tabStartX+tabBarWidth-34, b.Min.Y+12, 28, 22)
		if closeBtnRect.Contains(pos) {
			if showStatus {
				v.CloseStatus()
				return true
			}
			if showSettings {
				v.CloseSettings()
				return true
			}
			v.SetState(window.StateFan)
			return true
		}
	} else {
		closeBtnRect := geometry.NewRect(tabStartX+(tabBarWidth-36)/2, b.Min.Y+12, 36, 22)
		if closeBtnRect.Contains(pos) {
			if showStatus {
				v.CloseStatus()
				return true
			}
			if showSettings {
				v.CloseSettings()
				return true
			}
			v.SetState(window.StateFan)
			return true
		}
	}

	settingsTabRect := geometry.NewRect(tabStartX+2, b.Min.Y+h-48, tabBarWidth-14, 34)
	if settingsTabRect.Contains(pos) {
		v.ToggleSettings()
		return true
	}

	if !showStatus {
		tabMinY := b.Min.Y + float32(44)
		tabMaxY := b.Min.Y + h - float32(56)
		tabStartY := tabMinY - scrollY
		tabHeight := float32(64)
		tabGap := float32(7)

		tabVisibleRect := geometry.NewRect(tabStartX, tabMinY, tabBarWidth-4, tabMaxY-tabMinY)
		for i, iss := range filteredIssues {
			tabY := tabStartY + float32(i)*(tabHeight+tabGap)
			tabRect := geometry.NewRect(tabStartX, tabY, tabBarWidth-4, tabHeight)
			if tabRect.Contains(pos) && tabVisibleRect.Contains(pos) {
				for origIdx, oIss := range allIssues {
					if oIss.Key == iss.Key {
						if activeIdx == origIdx && !showSettings && !showStatus {
							v.SetState(window.StateFan)
							return true
						}
						v.Expand(origIdx)
						return true
					}
				}
				v.Expand(i)
				return true
			}
		}
	}

	// Status Modal Clicks
	if showStatus {
		r := geometry.NewRect(cardStartX, b.Min.Y+8, cardAreaWidth, h-16)

		// Top Close button
		closeRect := geometry.NewRect(r.Min.X+r.Width()-54, r.Min.Y+14, 40, 26)
		if closeRect.Contains(pos) {
			v.CloseStatus()
			return true
		}

		// Top Refresh button
		refRect := geometry.NewRect(r.Min.X+r.Width()-140, r.Min.Y+14, 76, 26)
		if refRect.Contains(pos) {
			v.RefreshStatus()
			return true
		}

		// Banner Statuspage link
		bannerY := r.Min.Y + 62
		extLinkRect := geometry.NewRect(r.Min.X+r.Width()-40-135, bannerY+12, 122, 24)
		if extLinkRect.Contains(pos) {
			_ = window.OpenURL("https://status.atlassian.com")
			return true
		}

		// Application Service Cards Click -> Direct Statuspage link
		cardGap := float32(8)
		cardW := (r.Width() - 40 - cardGap) / 2
		cardH := float32(42)
		gridY := bannerY + 48 + 12
		gridCardsY := gridY + 16
		for i, app := range atlassianCoreApps {
			col := float32(i % 2)
			row := float32(i / 2)
			cardX := r.Min.X + 20 + col*(cardW+cardGap)
			cardY := gridCardsY + row*(cardH+cardGap)
			appRect := geometry.NewRect(cardX, cardY, cardW, cardH)
			if appRect.Contains(pos) && app.URL != "" {
				_ = window.OpenURL(app.URL)
				return true
			}
		}

		// Incident shortlink buttons
		v.mu.Lock()
		incidents := v.statusReport.ActiveIncidents
		v.mu.Unlock()

		gridRows := float32((len(atlassianCoreApps) + 1) / 2)
		incSecY := gridCardsY + gridRows*(cardH+cardGap) + 6
		incListY := incSecY + 18
		maxInc := 2
		if len(incidents) < maxInc {
			maxInc = len(incidents)
		}
		incH := float32(56)
		for idx := 0; idx < maxInc; idx++ {
			inc := incidents[idx]
			cardY := incListY + float32(idx)*(incH+6)
			incCardRect := geometry.NewRect(r.Min.X+20, cardY, r.Width()-40, incH)
			linkRect := geometry.NewRect(incCardRect.Max.X-75, incCardRect.Min.Y+16, 65, 20)
			if linkRect.Contains(pos) && inc.URL != "" {
				_ = window.OpenURL(inc.URL)
				return true
			}
		}

		// Footer link button
		footY := r.Min.Y + r.Height() - 34
		footBtnRect := geometry.NewRect(r.Min.X+r.Width()-185, footY-1, 165, 22)
		if footBtnRect.Contains(pos) {
			_ = window.OpenURL("https://status.atlassian.com")
			return true
		}

		return true
	}

	// Settings Modal Clicks
	if showSettings {
		r := geometry.NewRect(cardStartX, b.Min.Y+8, cardAreaWidth, h-16)
		return v.handleSettingsClick(r, pos)
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
			v.invalidateFilterCacheLocked()
			v.mu.Unlock()
			v.MarkNeedsLayout()
			return true
		}

		if st == window.StateExpanded {
			if v.IsStatusOpen() {
				v.CloseStatus()
				return true
			}
			v.mu.Lock()
			showSet := v.showSettings
			v.mu.Unlock()
			if showSet {
				v.CloseSettings()
				return true
			}
			v.SetState(window.StateFan)
			return true
		} else if st == window.StateFan {
			v.SetState(window.StateRest)
			return true
		}
		return true
	}

	v.mu.Lock()
	st := v.state
	showSettings := v.showSettings
	showStatus := v.showStatus
	activeField := v.activeField
	v.mu.Unlock()

	mods := ev.Modifiers()
	isCmdOrCtrl := mods.Has(event.ModSuper) || mods.Has(event.ModCtrl)
	isShift := mods.Has(event.ModShift)

	// Cmd+R / Ctrl+R: Refresh tickets immediately
	if isCmdOrCtrl && (ev.Key == event.KeyR || ev.Rune == 'r' || ev.Rune == 'R') {
		v.RefreshIssues()
		v.showToast("Syncing Jira issues...")
		return true
	}

	// Cmd+K / Cmd+F: Focus search
	if isCmdOrCtrl && (ev.Key == event.KeyK || ev.Rune == 'k' || ev.Key == event.KeyF || ev.Rune == 'f') {
		v.mu.Lock()
		v.searchActive = true
		v.mu.Unlock()
		if st == window.StateRest {
			v.SetState(window.StateFan)
		} else {
			v.MarkNeedsLayout()
		}
		return true
	}

	// Active ticket actions (when not editing settings or viewing status)
	if !showSettings && !showStatus && (st == window.StateFan || st == window.StateExpanded) {
		filtered := v.getFilteredIssues()

		// Arrow Up navigation
		if ev.Key == event.KeyUp {
			if len(filtered) > 0 {
				v.mu.Lock()
				curKey := ""
				if v.activeIdx >= 0 && v.activeIdx < len(v.issues) {
					curKey = v.issues[v.activeIdx].Key
				}
				curFIdx := 0
				for fi, iss := range filtered {
					if iss.Key == curKey {
						curFIdx = fi
						break
					}
				}
				newFIdx := curFIdx - 1
				if newFIdx < 0 {
					newFIdx = len(filtered) - 1
				}
				newKey := filtered[newFIdx].Key
				for origI, iss := range v.issues {
					if iss.Key == newKey {
						v.activeIdx = origI
						v.activeTheme = GetTicketTheme(origI)
						break
					}
				}
				v.mu.Unlock()
				if st == window.StateExpanded {
					v.loadActiveTicketInMobileView()
				}
				v.MarkNeedsLayout()
				if v.onRedraw != nil {
					v.onRedraw()
				}
				return true
			}
		}

		// Arrow Down navigation
		if ev.Key == event.KeyDown {
			if len(filtered) > 0 {
				v.mu.Lock()
				curKey := ""
				if v.activeIdx >= 0 && v.activeIdx < len(v.issues) {
					curKey = v.issues[v.activeIdx].Key
				}
				curFIdx := 0
				for fi, iss := range filtered {
					if iss.Key == curKey {
						curFIdx = fi
						break
					}
				}
				newFIdx := (curFIdx + 1) % len(filtered)
				newKey := filtered[newFIdx].Key
				for origI, iss := range v.issues {
					if iss.Key == newKey {
						v.activeIdx = origI
						v.activeTheme = GetTicketTheme(origI)
						break
					}
				}
				v.mu.Unlock()
				if st == window.StateExpanded {
					v.loadActiveTicketInMobileView()
				}
				v.MarkNeedsLayout()
				if v.onRedraw != nil {
					v.onRedraw()
				}
				return true
			}
		}

		// Cmd+O: Open active ticket in browser
		if isCmdOrCtrl && (ev.Key == event.KeyO || ev.Rune == 'o' || ev.Rune == 'O') {
			v.mu.Lock()
			var targetURL string
			if v.activeIdx >= 0 && v.activeIdx < len(v.issues) {
				iss := v.issues[v.activeIdx]
				baseURL := iss.BaseURL
				if baseURL == "" {
					baseURL = v.config.BaseURL
				}
				targetURL = fmt.Sprintf("%s/browse/%s", strings.TrimRight(baseURL, "/"), iss.Key)
			}
			v.mu.Unlock()
			if targetURL != "" {
				if window.DefaultManager != nil {
					_ = window.DefaultManager.OpenTicketURL(targetURL)
				}
				v.showToast("Opening ticket in browser")
				return true
			}
		}

		// Cmd+Shift+C: Copy Git branch name
		if isCmdOrCtrl && isShift && (ev.Key == event.KeyC || ev.Rune == 'c' || ev.Rune == 'C') {
			v.mu.Lock()
			var branchName string
			if v.activeIdx >= 0 && v.activeIdx < len(v.issues) {
				iss := v.issues[v.activeIdx]
				prefix := v.config.BranchPrefix
				if prefix == "" {
					prefix = "feature/"
				}
				slug := strings.ToLower(iss.Summary)
				slug = strings.Map(func(r rune) rune {
					if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
						return r
					}
					return '-'
				}, slug)
				slug = strings.Trim(slug, "-")
				if len(slug) > 30 {
					slug = slug[:30]
				}
				branchName = fmt.Sprintf("%s%s-%s", prefix, iss.Key, slug)
			}
			v.mu.Unlock()
			if branchName != "" {
				copyToClipboard(branchName)
				v.showToast("✓ Copied branch: " + branchName)
				return true
			}
		}

		// Cmd+C: Copy active ticket Key
		if isCmdOrCtrl && !isShift && (ev.Key == event.KeyC || ev.Rune == 'c' || ev.Rune == 'C') {
			v.mu.Lock()
			var keyToCopy string
			if v.activeIdx >= 0 && v.activeIdx < len(v.issues) {
				keyToCopy = v.issues[v.activeIdx].Key
			}
			v.mu.Unlock()
			if keyToCopy != "" {
				copyToClipboard(keyToCopy)
				v.showToast("✓ Copied key: " + keyToCopy)
				return true
			}
		}
	}

	// Handle Instant Search in Fan state (Modifier key guard: ignore modified keystrokes)
	if st == window.StateFan && !showSettings {
		mods := ev.Modifiers()
		hasMod := (mods != event.ModNone)

		if ev.Key == event.KeyBackspace {
			v.mu.Lock()
			if len(v.searchQuery) > 0 {
				v.searchQuery = v.searchQuery[:len(v.searchQuery)-1]
				v.scrollY = 0
				v.invalidateFilterCacheLocked()
			}
			v.mu.Unlock()
			v.MarkNeedsLayout()
			return true
		}
		if ev.Rune >= 32 && ev.Rune <= 126 && !hasMod {
			v.mu.Lock()
			v.searchQuery += string(ev.Rune)
			v.searchActive = true
			v.scrollY = 0
			v.invalidateFilterCacheLocked()
			v.mu.Unlock()
			v.MarkNeedsLayout()
			return true
		}
	}

	// Keyboard navigation stays within the visible settings page, including
	// when no field is focused after switching sections.
	if showSettings && !isCmdOrCtrl && (ev.Key == event.KeyTab || ev.Key == event.KeyEnter) {
		v.mu.Lock()
		first, last := 1, 5
		if v.settingsSection == settingsApplication {
			first, last = 6, 7
		}
		if v.activeField < first || v.activeField > last {
			v.activeField = first
			if isShift {
				v.activeField = last
			}
		} else if isShift {
			v.activeField--
			if v.activeField < first {
				v.activeField = last
			}
		} else {
			v.activeField++
			if v.activeField > last {
				v.activeField = first
			}
		}
		v.selectAll = false
		if target := v.getActiveTargetLocked(); target != nil {
			v.cursorPos = len(*target)
		}
		v.mu.Unlock()
		v.MarkNeedsLayout()
		return true
	}

	// Settings text editing
	if showSettings && activeField > 0 {
		v.mu.Lock()
		defer v.mu.Unlock()

		target := v.getActiveTargetLocked()
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
				v.toast = NewToast("✓ Copied to clipboard")
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

		// 9. Standard Printable Characters
		if ev.Rune >= 32 && !hasMod {
			if v.activeField == 6 || v.activeField == 7 {
				if ev.Rune < '0' || ev.Rune > '9' {
					return true
				}
			}
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
	v.mu.Lock()
	if v.ctx == nil || v.ctx.Err() != nil {
		v.mu.Unlock()
		return
	}
	v.saveCurrentInstanceFieldsLocked()
	nameVal := v.nameVal
	testBaseURL := strings.TrimSpace(v.urlVal)
	testEmail := strings.TrimSpace(v.emailVal)
	testToken := sanitizeToken(v.tokenVal)
	testedInstIdx := v.selectedInstIdx
	testedInstID := ""
	if testedInstIdx >= 0 && testedInstIdx < len(v.config.Instances) {
		testedInstID = v.config.Instances[testedInstIdx].ID
	}
	v.statusMsg = fmt.Sprintf("Testing connection for %s...", nameVal)
	v.wg.Add(1)
	v.mu.Unlock()
	v.MarkNeedsLayout()

	go func() {
		defer v.wg.Done()
		user, err := v.client.VerifyInstanceConnection(v.ctx, testBaseURL, testEmail, testToken)
		select {
		case <-v.ctx.Done():
			return
		default:
		}

		v.mu.Lock()
		defer v.mu.Unlock()
		// Only update statusMsg if same instance and credentials still selected
		if !v.showSettings || v.settingsSection != settingsInstances ||
			v.selectedInstIdx != testedInstIdx || testedInstIdx < 0 || testedInstIdx >= len(v.config.Instances) ||
			v.config.Instances[testedInstIdx].ID != testedInstID ||
			strings.TrimSpace(v.urlVal) != testBaseURL ||
			strings.TrimSpace(v.emailVal) != testEmail || sanitizeToken(v.tokenVal) != testToken {
			return
		}

		if err != nil {
			v.statusMsg = fmt.Sprintf("%s: connection failed", nameVal)
		} else {
			v.statusMsg = fmt.Sprintf("%s: connected as %s", nameVal, user)
		}
		v.MarkNeedsLayout()
	}()
}

func (v *AppView) saveSettings() {
	v.mu.Lock()
	v.saveCurrentInstanceFieldsLocked()

	// Parse intervals
	if pollInt, err := strconv.Atoi(strings.TrimSpace(v.intervalVal)); err == nil && pollInt > 0 {
		v.config.PollInterval = pollInt
	}
	if statInt, err := strconv.Atoi(strings.TrimSpace(v.statusIntervalVal)); err == nil && statInt > 0 {
		v.config.StatusPollInterval = statInt
	}
	v.config.ApplyDefaults()

	// Respect explicit demo mode choice; do not auto-override based on credentials
	v.config.DemoMode = v.demoMode
	v.config.DebugMode = v.debugMode
	if len(v.config.Instances) > 0 {
		v.config.BaseURL = v.config.Instances[0].BaseURL
		v.config.Email = v.config.Instances[0].Email
		v.config.APIToken = v.config.Instances[0].APIToken
		v.config.JQLQuery = v.config.Instances[0].JQLQuery
	}
	cfg := v.config.Clone()
	onReload := v.onConfigReload
	v.invalidateFilterCacheLocked()
	v.mu.Unlock()

	// Save config first, then update client
	if err := jira.SaveConfig(cfg); err != nil {
		v.mu.Lock()
		v.statusMsg = "Save failed"
		v.mu.Unlock()
		v.showToast("Settings save failed")
		v.MarkNeedsLayout()
		return
	}

	v.mu.Lock()
	v.showSettings = false
	v.mu.Unlock()

	v.client.UpdateConfig(cfg)
	if onReload != nil {
		onReload()
	}
	v.showToast("Settings & polling updated")
	v.SetState(window.StateFan)
	v.RefreshIssues()
	if cfg.StatusCheckEnabled {
		v.RefreshStatus()
	}
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
	if runtime.GOOS == "darwin" {
		window.NativeCopyText(text)
		return
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
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
	if runtime.GOOS == "darwin" {
		if text, ok := window.NativePasteText(); ok {
			return text
		}
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
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
