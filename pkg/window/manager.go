package window

// WindowState represents the 3-state edge lifecycle inspired by noty / have-you-note-this.
type WindowState int

const (
	// StateRest: Discrete 14px capsule on screen edge with colored dashes.
	StateRest WindowState = iota
	// StateFan: Vertical colored tabs shingle out along the edge.
	StateFan
	// StateExpanded: Full floating card slides out level with the selected tab.
	StateExpanded
)

// DockSide defines which edge of the monitor the HUD is anchored to.
type DockSide int

const (
	DockSideRight DockSide = 0
	DockSideLeft  DockSide = 1
)

func (d DockSide) String() string {
	if d == DockSideLeft {
		return "Left"
	}
	return "Right"
}

// MonitorInfo describes a connected display monitor.
type MonitorInfo struct {
	Index  int    `json:"index"`
	Name   string `json:"name"`
	IsMain bool   `json:"is_main"`
	X      int    `json:"x"`
	Y      int    `json:"y"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// DockChangeCallback is invoked whenever the window is dragged or repositioned.
type DockChangeCallback func(side DockSide, monitorIndex int, posYRatio float64)

// WindowManager abstracts platform-specific window flags, docking, and edge level positioning.
type WindowManager interface {
	InitEdgeRail(width, height int) error
	SetState(state WindowState, width, height int)
	Dock(width, height int)
	DockToRightEdge(width, height int)
	SetDockSide(side DockSide)
	GetDockSide() DockSide
	SetMonitor(screenIndex int)
	GetSelectedMonitor() int
	GetMonitors() []MonitorInfo
	SetPositionRatio(ratio float64)
	GetPositionRatio() float64
	StartWindowDrag()
	OpenTicketURL(url string) error
	SetAlwaysOnTop(alwaysOnTop bool)
	GetAlwaysOnTop() bool
	SetAutoHide(autoHide bool)
	GetAutoHide() bool
	SetTucked(tucked bool, width, height int)
	SetToolTip(tooltip string)
}

var DefaultManager WindowManager

// SetToolTip updates the tooltip text on the window's content view.
func SetToolTip(tooltip string) {
	if DefaultManager != nil {
		DefaultManager.SetToolTip(tooltip)
	}
}

// SetAlwaysOnTop updates whether the window floats above all normal desktop windows.
func SetAlwaysOnTop(alwaysOnTop bool) {
	if DefaultManager != nil {
		DefaultManager.SetAlwaysOnTop(alwaysOnTop)
	}
}

// SetAutoHide toggles macOS Dock-style edge auto-hide.
func SetAutoHide(autoHide bool) {
	if DefaultManager != nil {
		DefaultManager.SetAutoHide(autoHide)
	}
}

// SetTucked animates the window in or out of edge-tucked state.
func SetTucked(tucked bool, width, height int) {
	if DefaultManager != nil {
		DefaultManager.SetTucked(tucked, width, height)
	}
}
