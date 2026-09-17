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
	activeInstIdx   int // Currently viewed instance filter in Fan & Expanded states (0..n-1)
	selectedInstIdx int // Currently edited instance in Settings overlay

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
	activeField int       // 1..5
	cursorPos   int       // Cursor position inside active field
	selectAll   bool      // If true, all text in active field is selected
	lastModTime time.Time // Timestamp of last modifier action to prevent duplicate character insertion
	statusMsg   string

	// Callbacks
	onRedraw func()
	onResize func(w, h int)
}

type appViewStateSnapshot struct {
	bounds          geometry.Rect
	state           window.WindowState
	showSettings    bool
	dockSide        window.DockSide
	selectedMonitor int
	monitors        []window.MonitorInfo
	alwaysOnTop     bool
	autoHide        bool
	isTucked        bool
	issues          []jira.Issue
	filteredIssues  []jira.Issue
	activeIdx       int
	activeTheme     CardTheme
	toast           ToastNotification
	scrollY         float32
	searchQuery     string
	searchActive    bool
	activeInstIdx   int
	selectedInstIdx int
	hoveredTabIdx   int
	hoveredSettings bool
	instances       []jira.InstanceConfig
	nameVal         string
	urlVal          string
	emailVal        string
	tokenVal        string
	jqlVal          string
	intervalVal     string
	colorVal        string
	demoMode        bool
	debugMode       bool
	activeField     int
	cursorPos       int
	selectAll       bool
	statusMsg       string
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

	ctx, cancel := context.WithCancel(context.Background())

	v := &AppView{
		config:          cfg,
		client:          client,
		activeIdx:       0,
		activeTheme:     ThemeMint,
		state:           window.StateRest,
		showSettings:    false,
		hoveredTabIdx:   -1,
		hoveredSettings: false,
		dockSide:        window.DockSide(cfg.DockSide),
		selectedMonitor: cfg.MonitorIndex,
		alwaysOnTop:     cfg.AlwaysOnTop,
		autoHide:        cfg.AutoHide,
		activeInstIdx:   0,
		selectedInstIdx: 0,
		demoMode:        cfg.DemoMode,
		debugMode:       cfg.DebugMode,
		intervalVal:     fmt.Sprintf("%d", cfg.PollInterval),
		onRedraw:        onRedraw,
		ctx:             ctx,
		cancel:          cancel,
		cacheDirty:      true,
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
		v.mu.Unlock()

		if showSet {
			v.mu.Lock()
			v.showSettings = false
			v.mu.Unlock()
			v.SetState(window.StateFan)
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
		cfgToSave := v.config
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
	cfg := v.config
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
	cfg := v.config
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
	cfg := v.config
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
	cfg := v.config
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
	if !v.cacheDirty && v.filteredCache != nil {
		return v.filteredCache
	}

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
		v.filteredCache = base
		v.cacheDirty = false
		return base
	}

	q := strings.ToLower(v.searchQuery)
	var res []jira.Issue
	for _, iss := range base {
		if strings.Contains(strings.ToLower(iss.Key), q) ||
			strings.Contains(strings.ToLower(iss.Summary), q) ||
			strings.Contains(strings.ToLower(iss.Status.Name), q) {
			res = append(res, iss)
		}
	}
	v.filteredCache = res
	v.cacheDirty = false
	return res
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

	instances := make([]jira.InstanceConfig, len(v.config.Instances))
	copy(instances, v.config.Instances)

	issues := make([]jira.Issue, len(v.issues))
	copy(issues, v.issues)

	filtered := v.getFilteredIssuesLocked()
	filteredClone := make([]jira.Issue, len(filtered))
	copy(filteredClone, filtered)

	monitors := make([]window.MonitorInfo, len(v.monitors))
	copy(monitors, v.monitors)

	return appViewStateSnapshot{
		bounds:          v.Bounds(),
		state:           v.state,
		showSettings:    v.showSettings,
		dockSide:        v.dockSide,
		selectedMonitor: v.selectedMonitor,
		monitors:        monitors,
		alwaysOnTop:     v.alwaysOnTop,
		autoHide:        v.autoHide,
		isTucked:        v.isTucked,
		issues:          issues,
		filteredIssues:  filteredClone,
		activeIdx:       v.activeIdx,
		activeTheme:     v.activeTheme,
		toast:           v.toast,
		scrollY:         v.scrollY,
		searchQuery:     v.searchQuery,
		searchActive:    v.searchActive,
		activeInstIdx:   v.activeInstIdx,
		selectedInstIdx: v.selectedInstIdx,
		hoveredTabIdx:   v.hoveredTabIdx,
		hoveredSettings: v.hoveredSettings,
		instances:       instances,
		nameVal:         v.nameVal,
		urlVal:          v.urlVal,
		emailVal:        v.emailVal,
		tokenVal:        v.tokenVal,
		jqlVal:          v.jqlVal,
		intervalVal:     v.intervalVal,
		colorVal:        v.colorVal,
		demoMode:        v.demoMode,
		debugMode:       v.debugMode,
		activeField:     v.activeField,
		cursorPos:       v.cursorPos,
		selectAll:       v.selectAll,
		statusMsg:       v.statusMsg,
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
	if newState == window.StateFan {
		v.lastHover = time.Now()
	} else {
		v.lastHover = time.Time{}
		v.searchActive = false
	}
	showSettings := v.showSettings
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
	v.mu.Unlock()
	v.SetState(window.StateExpanded)
}

func (v *AppView) OpenSettings() {
	v.mu.Lock()
	v.showSettings = true
	v.loadInstanceFieldsLocked(v.selectedInstIdx)
	v.statusMsg = ""
	v.mu.Unlock()
	v.SetState(window.StateExpanded)
}

func (v *AppView) ToggleSettings() {
	v.mu.Lock()
	v.showSettings = !v.showSettings
	v.loadInstanceFieldsLocked(v.selectedInstIdx)
	v.statusMsg = ""
	v.mu.Unlock()
	v.SetState(window.StateExpanded)
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
		// Inset pill by 1.5px on all sides so the stroke is 100% visible, fully rounded and unclipped
		pillRect := geometry.NewRect(b.Min.X+1.5, b.Min.Y+1.5, w-3.0, h-3.0)
		pillRadius := float32((w - 3.0) / 2.0)

		// 1. Deep frosted glass backdrop
		canvas.DrawRoundRect(pillRect, widget.RGBA8(16, 22, 34, 245), pillRadius)
		// 2. Complete, crisp, prominent frosted white border all the way around
		canvas.StrokeRoundRect(pillRect, widget.RGBA8(255, 255, 255, 140), pillRadius, 1.5)

		// 3. Subtle top drag handle grip dots (···)
		centerX := b.Min.X + w/2
		canvas.DrawCircle(geometry.Pt(centerX-5, b.Min.Y+6), 1.2, widget.RGBA8(255, 255, 255, 120))
		canvas.DrawCircle(geometry.Pt(centerX, b.Min.Y+6), 1.2, widget.RGBA8(255, 255, 255, 120))
		canvas.DrawCircle(geometry.Pt(centerX+5, b.Min.Y+6), 1.2, widget.RGBA8(255, 255, 255, 120))

		instCount := len(s.instances)
		if instCount == 0 {
			instCount = 1
		}
		usableH := h - 34 // Reserve 34px for bottom divider and settings icon
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
			secY := b.Min.Y + float32(8+float32(idx)*instSectionH)

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

		// Divider before settings
		dividerY := b.Min.Y + h - 28
		canvas.DrawLine(geometry.Pt(b.Min.X+7, dividerY), geometry.Pt(b.Min.X+w-7, dividerY), widget.RGBA8(255, 255, 255, 45), 1.0)

		// Settings Cog Icon
		settingsCenter := geometry.Pt(centerX, b.Min.Y+h-14)
		canvas.DrawCircle(settingsCenter, 6.5, widget.RGBA8(180, 200, 230, 45))
		canvas.DrawCircle(settingsCenter, 3.8, widget.RGBA8(200, 220, 245, 240))
		return
	}

	// =========================================================================
	// 2. STATE FAN: Vertical Dock Tabs with Instance Header & Search (120xH)
	// =========================================================================
	if s.state == window.StateFan {
		railBackdrop := geometry.NewRect(b.Min.X+1.0, b.Min.Y+1.0, w-2.0, h-2.0)
		canvas.DrawRoundRect(railBackdrop, widget.RGBA8(20, 28, 44, 200), 14)
		canvas.StrokeRoundRect(railBackdrop, widget.RGBA8(255, 255, 255, 100), 14, 1.5)

		// 1. Top subtle Drag Handle Bar (grab affordance)
		dragBarRect := geometry.NewRect(b.Min.X+w/2-14, b.Min.Y+3, 28, 3)
		canvas.DrawRoundRect(dragBarRect, widget.RGBA8(255, 255, 255, 90), 1.5)

		// 2. Top Instance Header Pill
		instName := "All Instances"
		instBeaconColor := widget.RGBA8(56, 189, 248, 240)
		if s.activeInstIdx >= 0 && s.activeInstIdx < len(s.instances) {
			inst := s.instances[s.activeInstIdx]
			instName = inst.Name
			instBeaconColor, _, _, _, _, _, _ = GetInstanceColors(inst.Color, s.activeInstIdx)
		}
		if len(instName) > 14 {
			instName = instName[:12] + ".."
		}

		instHeaderRect := geometry.NewRect(b.Min.X+7, b.Min.Y+8, w-14, 22)
		canvas.DrawRoundRect(instHeaderRect, widget.RGBA8(34, 44, 66, 225), 5)
		canvas.StrokeRoundRect(instHeaderRect, widget.RGBA8(255, 255, 255, 40), 5, 1.0)

		canvas.DrawCircle(geometry.Pt(b.Min.X+16, b.Min.Y+19), 3.5, instBeaconColor)
		canvas.DrawText(instName, geometry.NewRect(b.Min.X+22, b.Min.Y+12, w-30, 14), 10, widget.RGBA8(235, 245, 255, 255), true, widget.TextAlignCenter)

		// 3. Search Bar with Clear Button & Blinking Cursor
		searchRect := geometry.NewRect(b.Min.X+7, b.Min.Y+34, w-14, 22)
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
		sTxtRect := geometry.NewRect(b.Min.X+12, b.Min.Y+38, w-38, 14)
		canvas.DrawText(searchTxt, sTxtRect, 9, searchColor, false, widget.TextAlignLeft)

		// Blinking search cursor
		if s.searchActive && (time.Now().UnixMilli()/500)%2 == 0 {
			curOffset := measureTextWidth(s.searchQuery, 9)
			curX := b.Min.X + 12 + curOffset + 1.0
			if curX < b.Min.X+w-28 {
				canvas.DrawLine(geometry.Pt(curX, b.Min.Y+37), geometry.Pt(curX, b.Min.Y+51), widget.RGBA8(255, 255, 255, 220), 1.5)
			}
		}

		// Clear search "✕" button
		if s.searchQuery != "" {
			clearBtnRect := geometry.NewRect(b.Min.X+w-24, b.Min.Y+38, 14, 14)
			canvas.DrawCircle(geometry.Pt(b.Min.X+w-17, b.Min.Y+45), 6.5, widget.RGBA8(255, 255, 255, 40))
			canvas.DrawText("✕", clearBtnRect, 8, widget.RGBA8(255, 255, 255, 220), true, widget.TextAlignCenter)
		}

		// 4. Tab Area (Smoothly clipped between top search and bottom settings tab)
		tabMinY := b.Min.Y + float32(60)
		tabMaxY := b.Min.Y + h - float32(50)
		tabStartY := tabMinY - s.scrollY
		tabHeight := float32(62)
		tabGap := float32(6)

		canvas.PushClip(geometry.NewRect(b.Min.X+1, tabMinY, w-2, tabMaxY-tabMinY))
		if len(s.filteredIssues) == 0 {
			noRect := geometry.NewRect(b.Min.X+8, b.Min.Y+74, w-16, 30)
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
						tabX = b.Min.X + 3  // Lift inward to the left
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

		// 5. Scroll Indicator
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

		// 6. Divider Line before Settings
		dividerY := b.Min.Y + h - 50
		canvas.DrawLine(geometry.Pt(b.Min.X+10, dividerY), geometry.Pt(b.Min.X+w-10, dividerY), widget.RGBA8(255, 255, 255, 55), 1.0)

		// 7. Settings Tab
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
		canvas.DrawText("Settings", setTxtRect, 10, widget.RGBA8(230, 240, 255, 255), true, widget.TextAlignCenter)
		return
	}

	// =========================================================================
	// 3. STATE EXPANDED: Side Dock Column + Multi-Instance Settings Overlay (780x580)
	// =========================================================================
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

	// Top subtle drag bar on the dock shelf
	shelfDragRect := geometry.NewRect(tabStartX+(tabBarWidth-32)/2, b.Min.Y+12, 28, 3)
	canvas.DrawRoundRect(shelfDragRect, widget.RGBA8(255, 255, 255, 90), 1.5)

	// Draw side tabs inside the dock shelf with strict bounds clipping
	tabMinY := b.Min.Y + float32(20)
	tabMaxY := b.Min.Y + h - float32(56)
	tabStartY := tabMinY - s.scrollY
	tabHeight := float32(64)
	tabGap := float32(7)

	var activeKey string
	if s.activeIdx >= 0 && s.activeIdx < len(s.issues) {
		activeKey = s.issues[s.activeIdx].Key
	}

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
	canvas.DrawText("Settings", setTxtRect, 10, widget.RGBA8(230, 240, 255, 255), true, widget.TextAlignCenter)

	// Settings Overlay (if active)
	if s.showSettings {
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

func (v *AppView) drawSettingsOverlay(ctx widget.Context, canvas widget.Canvas, r geometry.Rect, s appViewStateSnapshot) {
	radius := float32(14)
	canvas.DrawRoundRect(r, widget.RGBA8(18, 22, 32, 250), radius)
	canvas.StrokeRoundRect(r, widget.RGBA8(255, 255, 255, 70), radius, 1.0)

	// 1. Header Title & Top Controls
	hdrRect := geometry.NewRect(r.Min.X+20, r.Min.Y+14, 250, 22)
	canvas.DrawText("Jira Instances & Credentials", hdrRect, 14, widget.RGBA8(245, 250, 255, 255), true, widget.TextAlignLeft)

	// Load .env button
	envRect := geometry.NewRect(r.Min.X+r.Width()-155, r.Min.Y+12, 75, 24)
	canvas.DrawRoundRect(envRect, widget.RGBA8(48, 58, 78, 255), 4)
	canvas.DrawText("Load .env", envRect, 10, widget.RGBA8(220, 235, 255, 255), false, widget.TextAlignCenter)

	// Close button
	closeRect := geometry.NewRect(r.Min.X+r.Width()-70, r.Min.Y+12, 50, 24)
	canvas.DrawRoundRect(closeRect, widget.RGBA8(38, 48, 68, 255), 4)
	canvas.DrawText("Close", closeRect, 10, widget.RGBA8(240, 245, 255, 255), false, widget.TextAlignCenter)

	// 2. Dedicated Row for Instance Tabs & Add Button
	tabRowY := r.Min.Y + 44
	instStartX := r.Min.X + 20
	tabW := float32(110)
	tabGap := float32(6)

	for idx, inst := range s.instances {
		instTabX := instStartX + float32(idx)*(tabW+tabGap)
		instTabRect := geometry.NewRect(instTabX, tabRowY, tabW, 26)

		tabBg := widget.RGBA8(32, 40, 56, 255)
		tabBorder := widget.RGBA8(255, 255, 255, 30)
		if idx == s.selectedInstIdx {
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

	// ➕ Add Instance Button
	addInstX := instStartX + float32(len(s.instances))*(tabW+tabGap)
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
	rawVals := []string{s.nameVal, s.urlVal, s.emailVal, s.tokenVal, s.jqlVal}

	startY := sepY + 12
	blinkOn := (time.Now().UnixMilli()/500)%2 == 0

	for i, label := range labels {
		fIdx := i + 1
		y := startY + float32(i*45)
		isFocused := (s.activeField == fIdx)

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

		canvas.PushClip(geometry.NewRect(inpRect.Min.X+4, inpRect.Min.Y+2, inpRect.Width()-8, inpRect.Height()-4))
		if displayVal == "" && !isFocused {
			vRect := geometry.NewRect(textX, textY, inpRect.Width()-16, 14)
			canvas.DrawText("(click to type or paste with ⌘V)", vRect, 11, widget.RGBA8(100, 116, 139, 255), false, widget.TextAlignLeft)
		} else {
			if isFocused && s.selectAll && len(displayVal) > 0 {
				selWidth := measureTextWidth(displayVal, 11) + 4
				if selWidth > inpRect.Width()-16 {
					selWidth = inpRect.Width() - 16
				}
				selRect := geometry.NewRect(textX-2, textY-1, selWidth, 16)
				canvas.DrawRoundRect(selRect, widget.RGBA8(59, 130, 246, 180), 2)
			}

			vRect := geometry.NewRect(textX, textY, inpRect.Width()-16, 14)
			canvas.DrawText(displayVal, vRect, 11, widget.RGBA8(240, 245, 255, 255), false, widget.TextAlignLeft)

			if isFocused && blinkOn && !s.selectAll {
				cp := s.cursorPos
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
		canvas.PopClip()
	}

	// 4. Profile Accent Color Picker
	colorRowY := startY + float32(len(labels)*45) + 2
	colorLblRect := geometry.NewRect(r.Min.X+20, colorRowY, 200, 14)
	canvas.DrawText("Profile Accent Color", colorLblRect, 10, widget.RGBA8(148, 163, 184, 255), false, widget.TextAlignLeft)

	currColorHex := s.colorVal
	if currColorHex == "" && s.selectedInstIdx < len(s.instances) {
		currColorHex = s.instances[s.selectedInstIdx].Color
	}
	if currColorHex == "" {
		currColorHex = "#38bdf8"
	}

	for k, preset := range ProfilePresets {
		chipCenter := geometry.Pt(r.Min.X+30+float32(k)*28, colorRowY+24)
		chipColor := preset.Color
		isSelected := strings.EqualFold(currColorHex, preset.Hex)

		if isSelected {
			canvas.DrawCircle(chipCenter, 11, widget.RGBA8(255, 255, 255, 220))
			canvas.DrawCircle(chipCenter, 9, chipColor)
		} else {
			canvas.DrawCircle(chipCenter, 8.5, chipColor)
			canvas.StrokeCircle(chipCenter, 8.5, widget.RGBA8(255, 255, 255, 50), 1.0)
		}
	}

	hexRect := geometry.NewRect(r.Min.X+30+float32(len(ProfilePresets))*28+8, colorRowY+17, 70, 16)
	canvas.DrawText(currColorHex, hexRect, 10, widget.RGBA8(148, 163, 184, 255), false, widget.TextAlignLeft)

	// 5. Display & Docking Controls
	dockSecY := colorRowY + 40
	canvas.DrawLine(geometry.Pt(r.Min.X+20, dockSecY), geometry.Pt(r.Min.X+r.Width()-20, dockSecY), widget.RGBA8(255, 255, 255, 30), 1.0)

	dockLblRect := geometry.NewRect(r.Min.X+20, dockSecY+8, 250, 14)
	canvas.DrawText("Display, Docking & Window Controls", dockLblRect, 10, widget.RGBA8(148, 163, 184, 255), false, widget.TextAlignLeft)

	// Row 1: Edge Docking & Displays
	row1Y := dockSecY + 24

	leftEdgeRect := geometry.NewRect(r.Min.X+20, row1Y, 95, 24)
	leftBg := widget.RGBA8(32, 40, 56, 255)
	leftBorder := widget.RGBA8(255, 255, 255, 30)
	if s.dockSide == window.DockSideLeft {
		leftBg = widget.RGBA8(59, 130, 246, 200)
		leftBorder = widget.RGBA8(255, 255, 255, 180)
	}
	canvas.DrawRoundRect(leftEdgeRect, leftBg, 4)
	canvas.StrokeRoundRect(leftEdgeRect, leftBorder, 4, 1.0)
	canvas.DrawText("◧ Left Edge", leftEdgeRect, 10, widget.RGBA8(240, 245, 255, 255), true, widget.TextAlignCenter)

	rightEdgeRect := geometry.NewRect(r.Min.X+122, row1Y, 95, 24)
	rightBg := widget.RGBA8(32, 40, 56, 255)
	rightBorder := widget.RGBA8(255, 255, 255, 30)
	if s.dockSide == window.DockSideRight {
		rightBg = widget.RGBA8(59, 130, 246, 200)
		rightBorder = widget.RGBA8(255, 255, 255, 180)
	}
	canvas.DrawRoundRect(rightEdgeRect, rightBg, 4)
	canvas.StrokeRoundRect(rightEdgeRect, rightBorder, 4, 1.0)
	canvas.DrawText("◨ Right Edge", rightEdgeRect, 10, widget.RGBA8(240, 245, 255, 255), true, widget.TextAlignCenter)

	monStartX := r.Min.X + 228
	for m, mon := range s.monitors {
		monRect := geometry.NewRect(monStartX+float32(m)*86, row1Y, 80, 24)
		monBg := widget.RGBA8(32, 40, 56, 255)
		monBorder := widget.RGBA8(255, 255, 255, 30)
		if s.selectedMonitor == mon.Index {
			monBg = widget.RGBA8(16, 185, 129, 200)
			monBorder = widget.RGBA8(255, 255, 255, 180)
		}
		canvas.DrawRoundRect(monRect, monBg, 4)
		canvas.StrokeRoundRect(monRect, monBorder, 4, 1.0)
		canvas.DrawText(fmt.Sprintf("Disp %d", mon.Index+1), monRect, 10, widget.RGBA8(240, 245, 255, 255), true, widget.TextAlignCenter)
	}

	// Row 2: Always On Top & Auto-Hide Mode
	row2Y := dockSecY + 54

	aotRect := geometry.NewRect(r.Min.X+20, row2Y, 135, 24)
	aotBg := widget.RGBA8(32, 40, 56, 255)
	aotBorder := widget.RGBA8(255, 255, 255, 30)
	aotTxt := "Floating: Normal"
	if s.alwaysOnTop {
		aotBg = widget.RGBA8(59, 130, 246, 180)
		aotBorder = widget.RGBA8(255, 255, 255, 160)
		aotTxt = "✓ Always On Top"
	}
	canvas.DrawRoundRect(aotRect, aotBg, 4)
	canvas.StrokeRoundRect(aotRect, aotBorder, 4, 1.0)
	canvas.DrawText(aotTxt, aotRect, 10, widget.RGBA8(240, 245, 255, 255), false, widget.TextAlignCenter)

	ahRect := geometry.NewRect(r.Min.X+162, row2Y, 195, 24)
	ahBg := widget.RGBA8(32, 40, 56, 255)
	ahBorder := widget.RGBA8(255, 255, 255, 30)
	ahTxt := "Auto-Hide: Disabled"
	if s.autoHide {
		ahBg = widget.RGBA8(168, 85, 247, 180)
		ahBorder = widget.RGBA8(255, 255, 255, 160)
		ahTxt = "✓ Auto-Hide (macOS Dock)"
	}
	canvas.DrawRoundRect(ahRect, ahBg, 4)
	canvas.StrokeRoundRect(ahRect, ahBorder, 4, 1.0)
	canvas.DrawText(ahTxt, ahRect, 10, widget.RGBA8(240, 245, 255, 255), false, widget.TextAlignCenter)


	// Status Message
	if s.statusMsg != "" {
		stMsg := s.statusMsg
		if len(stMsg) > 75 {
			stMsg = stMsg[:72] + "..."
		}
		stRect := geometry.NewRect(r.Min.X+20, r.Min.Y+r.Height()-72, r.Width()-40, 16)
		canvas.DrawText(stMsg, stRect, 11, ColorStatusInProgress, false, widget.TextAlignLeft)
	}

	// 4. Bottom Action Row
	btnY := r.Min.Y + r.Height() - 44

	demoModeTxt := "● Live Jira"
	demoBg := widget.RGBA8(16, 185, 129, 200)
	demoFg := widget.RGBA8(255, 255, 255, 255)
	if s.demoMode {
		demoModeTxt = "⚠ Demo Mock Mode"
		demoBg = widget.RGBA8(217, 119, 6, 220)
		demoFg = widget.RGBA8(255, 255, 255, 255)
	}
	demoRect := geometry.NewRect(r.Min.X+20, btnY, 130, 28)
	canvas.DrawRoundRect(demoRect, demoBg, 6)
	canvas.DrawText(demoModeTxt, demoRect, 10, demoFg, false, widget.TextAlignCenter)

	debugTxt := "Debug: OFF"
	debugBg := widget.RGBA8(34, 42, 58, 255)
	if s.debugMode {
		debugTxt = "Debug: ON"
		debugBg = widget.RGBA8(70, 45, 95, 255)
	}
	debugRect := geometry.NewRect(r.Min.X+156, btnY, 86, 28)
	canvas.DrawRoundRect(debugRect, debugBg, 6)
	canvas.DrawText(debugTxt, debugRect, 10, widget.RGBA8(240, 245, 255, 255), false, widget.TextAlignCenter)

	testRect := geometry.NewRect(r.Min.X+248, btnY, 120, 28)
	canvas.DrawRoundRect(testRect, widget.RGBA8(34, 42, 58, 255), 6)
	canvas.DrawText("Test Connection", testRect, 10, widget.RGBA8(240, 245, 255, 255), false, widget.TextAlignCenter)

	// Delete Instance Button (if more than 1 instance)
	if len(s.instances) > 1 {
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
	scrollY := v.scrollY
	v.mu.Unlock()

	expW, expH := v.computeSize(st)
	w := float32(expW)
	h := float32(expH)
	b := geometry.NewRect(0, 0, w, h)

	// REST STATE: Hovering specific instance or settings dot
	if st == window.StateRest {
		v.mu.Lock()
		autoH := v.autoHide
		tucked := v.isTucked
		v.mu.Unlock()
		if autoH && tucked {
			v.setTucked(false)
		}

		if pos.Y >= b.Min.Y+h-28 {
			v.mu.Lock()
			v.hoveredSettings = true
			v.mu.Unlock()
			return true
		}

		v.mu.Lock()
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

		if instCount > 0 {
			usableH := h - 34
			instSectionH := usableH / float32(instCount)
			instIdx := int((pos.Y - (b.Min.Y + 6)) / instSectionH)
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

	// FAN STATE: Track hovered tab / settings button for interactive lift effect
	if st == window.StateFan {
		v.mu.Lock()
		v.lastHover = time.Now()
		prevHoverTab := v.hoveredTabIdx
		prevHoverSet := v.hoveredSettings
		v.mu.Unlock()

		newHoverTab := -1
		newHoverSet := false

		if pos.Y >= b.Min.Y+h-44 {
			newHoverSet = true
		} else {
			filtered := v.getFilteredIssues()
			tabMinY := b.Min.Y + float32(60)
			tabMaxY := b.Min.Y + h - float32(50)
			tabStartY := tabMinY - scrollY
			tabHeight := float32(62)
			tabGap := float32(6)

			for i := range filtered {
				tabY := tabStartY + float32(i)*(tabHeight+tabGap)
				if tabY < tabMinY-2 || tabY+tabHeight > tabMaxY+2 {
					continue
				}
				tabRect := geometry.NewRect(b.Min.X+2, tabY, w-4, tabHeight)
				if tabRect.Contains(pos) {
					newHoverTab = i
					break
				}
			}
		}

		if newHoverTab != prevHoverTab || newHoverSet != prevHoverSet {
			v.mu.Lock()
			v.hoveredSettings = newHoverSet
			v.mu.Unlock()
			v.setHoveredTab(newHoverTab)
			v.MarkNeedsLayout()
			if v.onRedraw != nil {
				v.onRedraw()
			}
		}
		return true
	}

	// EXPANDED STATE: Track hovered tab on the side dock shelf
	if st == window.StateExpanded {
		v.mu.Lock()
		prevHoverTab := v.hoveredTabIdx
		prevHoverSet := v.hoveredSettings
		dockSide := v.dockSide
		v.mu.Unlock()

		newHoverTab := -1
		newHoverSet := false

		tabBarWidth := float32(110)
		tabStartX := b.Min.X + w - tabBarWidth
		if dockSide == window.DockSideLeft {
			tabStartX = b.Min.X + 6
		}

		settingsTabRect := geometry.NewRect(tabStartX+2, b.Min.Y+h-48, tabBarWidth-14, 34)
		if settingsTabRect.Contains(pos) {
			newHoverSet = true
		} else {
			filtered := v.getFilteredIssues()
			tabMinY := b.Min.Y + float32(20)
			tabMaxY := b.Min.Y + h - float32(56)
			tabStartY := tabMinY - scrollY
			tabHeight := float32(64)
			tabGap := float32(7)

			for i := range filtered {
				tabY := tabStartY + float32(i)*(tabHeight+tabGap)
				if tabY < tabMinY-2 || tabY+tabHeight > tabMaxY+2 {
					continue
				}
				tabRect := geometry.NewRect(tabStartX, tabY, tabBarWidth-4, tabHeight)
				if tabRect.Contains(pos) {
					newHoverTab = i
					break
				}
			}
		}

		if newHoverTab != prevHoverTab || newHoverSet != prevHoverSet {
			v.mu.Lock()
			v.hoveredSettings = newHoverSet
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
	v.mu.Lock()
	st := v.state
	allIssues := v.issues
	showSettings := v.showSettings
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
		if pos.Y <= b.Min.Y+14 {
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
		if pos.Y <= b.Min.Y+12 {
			v.StartDrag()
			return true
		}

		// Clear search "✕" button
		v.mu.Lock()
		sq := v.searchQuery
		v.mu.Unlock()
		if sq != "" && pos.X >= b.Min.X+w-28 && pos.X <= b.Min.X+w-6 && pos.Y >= b.Min.Y+34 && pos.Y <= b.Min.Y+56 {
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
		instHeaderRect := geometry.NewRect(b.Min.X+7, b.Min.Y+8, w-14, 22)
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

		searchRect := geometry.NewRect(b.Min.X+7, b.Min.Y+34, w-14, 22)
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

		tabMinY := b.Min.Y + float32(60)
		tabMaxY := b.Min.Y + h - float32(50)
		tabStartY := tabMinY - scrollY
		tabHeight := float32(62)
		tabGap := float32(6)

		for i, fIss := range filteredIssues {
			tabY := tabStartY + float32(i)*(tabHeight+tabGap)
			if tabY < tabMinY-2 || tabY+tabHeight > tabMaxY+2 {
				continue
			}
			tabRect := geometry.NewRect(b.Min.X+7, tabY, w-14, tabHeight)
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
	tabBarWidth := float32(110)
	cardAreaWidth := w - tabBarWidth - 14

	tabStartX := b.Min.X + w - tabBarWidth
	cardStartX := b.Min.X + 8
	if v.dockSide == window.DockSideLeft {
		tabStartX = b.Min.X + 6
		cardStartX = b.Min.X + tabBarWidth + 8
	}

	// Top shelf drag affordance
	if pos.Y <= b.Min.Y+18 && pos.X >= tabStartX && pos.X <= tabStartX+tabBarWidth {
		v.StartDrag()
		return true
	}

	settingsTabRect := geometry.NewRect(tabStartX+2, b.Min.Y+h-48, tabBarWidth-14, 34)
	if settingsTabRect.Contains(pos) {
		v.ToggleSettings()
		return true
	}

	tabMinY := b.Min.Y + float32(20)
	tabMaxY := b.Min.Y + h - float32(56)
	tabStartY := tabMinY - scrollY
	tabHeight := float32(64)
	tabGap := float32(7)

	for i, iss := range filteredIssues {
		tabY := tabStartY + float32(i)*(tabHeight+tabGap)
		if tabY < tabMinY-2 || tabY+tabHeight > tabMaxY+2 {
			continue
		}
		tabRect := geometry.NewRect(tabStartX, tabY, tabBarWidth-4, tabHeight)
		if tabRect.Contains(pos) {
			for origIdx, oIss := range allIssues {
				if oIss.Key == iss.Key {
					if activeIdx == origIdx && !showSettings {
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

	// Settings Modal Clicks
	if showSettings {
		r := geometry.NewRect(cardStartX, b.Min.Y+8, cardAreaWidth, h-16)

		// Top Controls
		envRect := geometry.NewRect(r.Min.X+r.Width()-155, r.Min.Y+12, 75, 24)
		if envRect.Contains(pos) {
			v.mu.Lock()
			cfg := v.config
			loaded := jira.LoadFromDotEnv(&cfg)
			if loaded {
				v.config = cfg
				v.loadInstanceFieldsLocked(v.selectedInstIdx)
				v.statusMsg = "Credentials loaded from .env"
				v.invalidateFilterCacheLocked()
			}
			v.mu.Unlock()
			if loaded {
				v.showToast("✓ Loaded credentials from .env")
				v.MarkNeedsLayout()
			} else {
				v.showToast("No .env file found")
			}
			return true
		}

		closeRect := geometry.NewRect(r.Min.X+r.Width()-70, r.Min.Y+12, 50, 24)
		if closeRect.Contains(pos) {
			v.SetState(window.StateFan)
			return true
		}

		// Instance Tabs Row Clicks
		tabRowY := r.Min.Y + 44
		instStartX := r.Min.X + 20
		tabW := float32(110)
		tabGap := float32(6)

		v.mu.Lock()
		instLen := len(v.config.Instances)
		v.mu.Unlock()

		for idx := 0; idx < instLen; idx++ {
			instTabX := instStartX + float32(idx)*(tabW+tabGap)
			instTabRect := geometry.NewRect(instTabX, tabRowY, tabW, 26)
			if instTabRect.Contains(pos) {
				v.mu.Lock()
				v.saveCurrentInstanceFieldsLocked()
				v.loadInstanceFieldsLocked(idx)
				v.statusMsg = ""
				v.mu.Unlock()
				v.MarkNeedsLayout()
				return true
			}
		}

		// + Add Instance Button Click
		addInstX := instStartX + float32(instLen)*(tabW+tabGap)
		addRect := geometry.NewRect(addInstX, tabRowY, 80, 26)
		if addRect.Contains(pos) {
			v.mu.Lock()
			v.saveCurrentInstanceFieldsLocked()
			newIdx := len(v.config.Instances) + 1
			newInst := jira.InstanceConfig{
				ID:       fmt.Sprintf("inst-%d", time.Now().UnixNano()),
				Name:     fmt.Sprintf("Instance %d", newIdx),
				BaseURL:  "",
				Email:    v.emailVal,
				APIToken: "",
				JQLQuery: "assignee = currentUser() AND resolution = Unresolved ORDER BY updated DESC",
				Color:    "#38bdf8",
			}
			v.config.Instances = append(v.config.Instances, newInst)
			v.loadInstanceFieldsLocked(len(v.config.Instances) - 1)
			v.invalidateFilterCacheLocked()
			v.mu.Unlock()
			v.showToast("Added new Jira instance")
			v.MarkNeedsLayout()
			return true
		}

		// Profile Accent Color Chips Clicks
		labels := []string{
			"Instance Label / Name",
			"Jira Base URL (e.g. https://company.atlassian.net)",
			"User Email",
			"API Token (click to type or paste with ⌘V)",
			"Custom JQL Query",
		}
		sepY := tabRowY + 34
		startY := sepY + 12
		colorRowY := startY + float32(len(labels)*45) + 2

		for k, preset := range ProfilePresets {
			chipCenter := geometry.Pt(r.Min.X+30+float32(k)*28, colorRowY+24)
			dx := pos.X - chipCenter.X
			dy := pos.Y - chipCenter.Y
			if dx*dx+dy*dy <= 12*12 {
				v.mu.Lock()
				v.colorVal = preset.Hex
				if v.selectedInstIdx < len(v.config.Instances) {
					v.config.Instances[v.selectedInstIdx].Color = preset.Hex
				}
				v.mu.Unlock()
				v.MarkNeedsLayout()
				if v.onRedraw != nil {
					v.onRedraw()
				}
				return true
			}
		}

		// Display & Docking Controls Clicks
		dockSecY := colorRowY + 40
		row1Y := dockSecY + 24

		leftEdgeRect := geometry.NewRect(r.Min.X+20, row1Y, 95, 24)
		if leftEdgeRect.Contains(pos) {
			v.SetDockSide(window.DockSideLeft)
			return true
		}
		rightEdgeRect := geometry.NewRect(r.Min.X+122, row1Y, 95, 24)
		if rightEdgeRect.Contains(pos) {
			v.SetDockSide(window.DockSideRight)
			return true
		}

		monStartX := r.Min.X + 228
		v.mu.Lock()
		mons := v.monitors
		v.mu.Unlock()
		for m, mon := range mons {
			monRect := geometry.NewRect(monStartX+float32(m)*86, row1Y, 80, 24)
			if monRect.Contains(pos) {
				v.SetMonitor(mon.Index)
				return true
			}
		}

		// Row 2: Always On Top & Auto-Hide Mode
		row2Y := dockSecY + 54
		aotRect := geometry.NewRect(r.Min.X+20, row2Y, 135, 24)
		if aotRect.Contains(pos) {
			v.mu.Lock()
			newAOT := !v.alwaysOnTop
			v.mu.Unlock()
			v.SetAlwaysOnTop(newAOT)
			return true
		}

		ahRect := geometry.NewRect(r.Min.X+162, row2Y, 195, 24)
		if ahRect.Contains(pos) {
			v.mu.Lock()
			newAH := !v.autoHide
			v.mu.Unlock()
			v.SetAutoHide(newAH)
			return true
		}

		btnY := r.Min.Y + r.Height() - 44

		// Mode Toggle
		demoRect := geometry.NewRect(r.Min.X+20, btnY, 130, 28)
		if demoRect.Contains(pos) {
			v.mu.Lock()
			v.demoMode = !v.demoMode
			v.mu.Unlock()
			v.MarkNeedsLayout()
			return true
		}

		// Debug Toggle
		debugRect := geometry.NewRect(r.Min.X+156, btnY, 86, 28)
		if debugRect.Contains(pos) {
			v.mu.Lock()
			v.debugMode = !v.debugMode
			v.mu.Unlock()
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
		if instLen > 1 {
			delRect := geometry.NewRect(r.Min.X+374, btnY, 70, 28)
			if delRect.Contains(pos) {
				v.mu.Lock()
				cur := v.selectedInstIdx
				if cur >= 0 && cur < len(v.config.Instances) {
					v.config.Instances = append(v.config.Instances[:cur], v.config.Instances[cur+1:]...)
					if cur >= len(v.config.Instances) {
						cur = len(v.config.Instances) - 1
					}
					v.loadInstanceFieldsLocked(cur)
					v.invalidateFilterCacheLocked()
				}
				v.mu.Unlock()
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
		for i := 0; i < 5; i++ {
			y := startY + float32(i*45)
			inpRect := geometry.NewRect(r.Min.X+20, y+16, r.Width()-40, 24)
			if inpRect.Contains(pos) {
				v.mu.Lock()
				v.activeField = i + 1
				v.selectAll = false
				tgt := v.getActiveTargetLocked()
				if tgt != nil {
					clickRelX := pos.X - (inpRect.Min.X + 8)
					v.cursorPos = getCursorIndexFromX(*tgt, 11, clickRelX)
				} else {
					v.cursorPos = 0
				}
				v.mu.Unlock()
				v.MarkNeedsLayout()
				return true
			}
		}

		v.mu.Lock()
		v.activeField = 0
		v.selectAll = false
		v.mu.Unlock()
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
			v.invalidateFilterCacheLocked()
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

	v.mu.Lock()
	st := v.state
	showSettings := v.showSettings
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

	// Active ticket actions (when not editing settings)
	if !showSettings && (st == window.StateFan || st == window.StateExpanded) {
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

		// 8. Tab / Enter: Cycle next field
		if ev.Key == event.KeyEnter || ev.Key == event.KeyTab {
			v.activeField = (v.activeField % 5) + 1
			v.selectAll = false
			nextTgt := v.getActiveTargetLocked()
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
		if err != nil {
			v.statusMsg = fmt.Sprintf("Error: %v", err)
		} else {
			v.statusMsg = fmt.Sprintf("Connected as %s", user)
		}
		v.MarkNeedsLayout()
	}()
}

func (v *AppView) saveSettings() {
	v.mu.Lock()
	v.saveCurrentInstanceFieldsLocked()

	// If credentials are configured on any instance, automatically switch to Live Jira mode
	for _, inst := range v.config.Instances {
		if inst.BaseURL != "" && inst.APIToken != "" {
			v.demoMode = false
			break
		}
	}

	v.config.DemoMode = v.demoMode
	v.config.DebugMode = v.debugMode
	if len(v.config.Instances) > 0 {
		v.config.BaseURL = v.config.Instances[0].BaseURL
		v.config.Email = v.config.Instances[0].Email
		v.config.APIToken = v.config.Instances[0].APIToken
		v.config.JQLQuery = v.config.Instances[0].JQLQuery
	}
	cfg := v.config
	v.showSettings = false
	v.invalidateFilterCacheLocked()
	v.mu.Unlock()

	_ = jira.SaveConfig(cfg)
	v.client.UpdateConfig(cfg)
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

