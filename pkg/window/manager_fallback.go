//go:build (!darwin && !windows) || (darwin && !cgo)

package window

import "sync"

type FallbackManager struct {
	mu            sync.RWMutex
	state         WindowState
	dockSide      DockSide
	monitorIndex  int
	posYRatio     float64
	currentWidth  int
	currentHeight int
}

func init() {
	if DefaultManager == nil {
		DefaultManager = &FallbackManager{
			state:         StateRest,
			dockSide:      DockSideRight,
			monitorIndex:  0,
			posYRatio:     0.5,
			currentWidth:  36,
			currentHeight: 224,
		}
	}
}

func (m *FallbackManager) InitEdgeRail(width, height int) error {
	m.mu.Lock()
	m.currentWidth = width
	m.currentHeight = height
	m.mu.Unlock()
	m.SetState(StateRest, width, height)
	return nil
}

func (m *FallbackManager) SetState(state WindowState, width, height int) {
	m.mu.Lock()
	m.state = state
	m.currentWidth = width
	m.currentHeight = height
	m.mu.Unlock()
	m.Dock(width, height)
}

func (m *FallbackManager) Dock(width, height int) {
	m.mu.Lock()
	m.currentWidth = width
	m.currentHeight = height
	m.mu.Unlock()
}

func (m *FallbackManager) DockToRightEdge(width, height int) {
	m.SetDockSide(DockSideRight)
	m.Dock(width, height)
}

func (m *FallbackManager) SetDockSide(side DockSide) {
	m.mu.Lock()
	m.dockSide = side
	m.mu.Unlock()
}

func (m *FallbackManager) GetDockSide() DockSide {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.dockSide
}

func (m *FallbackManager) SetMonitor(screenIndex int) {
	m.mu.Lock()
	m.monitorIndex = screenIndex
	m.mu.Unlock()
}

func (m *FallbackManager) GetSelectedMonitor() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.monitorIndex
}

func (m *FallbackManager) GetMonitors() []MonitorInfo {
	return []MonitorInfo{
		{Index: 0, Name: "Default Display", IsMain: true, Width: 1920, Height: 1080},
	}
}

func (m *FallbackManager) SetPositionRatio(ratio float64) {
	if ratio < 0.0 {
		ratio = 0.0
	}
	if ratio > 1.0 {
		ratio = 1.0
	}
	m.mu.Lock()
	m.posYRatio = ratio
	m.mu.Unlock()
}

func (m *FallbackManager) GetPositionRatio() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.posYRatio
}

func (m *FallbackManager) StartWindowDrag() {}

func (m *FallbackManager) OpenTicketURL(url string) error {
	return OpenURL(url)
}

func (m *FallbackManager) SetAlwaysOnTop(alwaysOnTop bool) {}
func (m *FallbackManager) GetAlwaysOnTop() bool            { return true }
func (m *FallbackManager) SetAutoHide(autoHide bool)       {}
func (m *FallbackManager) GetAutoHide() bool               { return false }
func (m *FallbackManager) SetTucked(tucked bool, w, h int) {}
func (m *FallbackManager) SetToolTip(tooltip string)       {}

func IsMouseInside() bool                                     { return true }
func SetMobileWebViewVisible(visible bool, width, height int) {}
func LoadMobileTicketView(url string)                         {}
func LoadMobileTicketHTML(html, baseURL string)               {}
func RegisterCollapseHandler(cb func())                       {}
func RegisterDockChangeHandler(cb DockChangeCallback)         {}
func NativeCopyText(text string)                              {}
func NativePasteText() (string, bool)                         { return "", false }

