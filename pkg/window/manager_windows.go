//go:build windows

package window

import (
	"sync"
	"syscall"
)

var (
	user32               = syscall.NewLazyDLL("user32.dll")
	procSetWindowPos     = user32.NewProc("SetWindowPos")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")
	procGetActiveWindow  = user32.NewProc("GetActiveWindow")
)

const (
	hwndTopMost   = ^uintptr(0) // -1
	swpShowWindow = 0x0040
	smCxScreen    = 0
	smCyScreen    = 1
)

type WindowsManager struct {
	mu            sync.RWMutex
	state         WindowState
	dockSide      DockSide
	monitorIndex  int
	posYRatio     float64
	currentWidth  int
	currentHeight int
}

func init() {
	DefaultManager = &WindowsManager{
		state:         StateRest,
		dockSide:      DockSideRight,
		monitorIndex:  0,
		posYRatio:     0.5,
		currentWidth:  32,
		currentHeight: 224,
	}
}

func (m *WindowsManager) InitEdgeRail(width, height int) error {
	m.mu.Lock()
	m.currentWidth = width
	m.currentHeight = height
	m.mu.Unlock()
	m.SetState(StateRest, width, height)
	return nil
}

func (m *WindowsManager) SetState(state WindowState, width, height int) {
	m.mu.Lock()
	m.state = state
	m.currentWidth = width
	m.currentHeight = height
	m.mu.Unlock()
	m.Dock(width, height)
}

func (m *WindowsManager) Dock(width, height int) {
	m.mu.Lock()
	m.currentWidth = width
	m.currentHeight = height
	side := m.dockSide
	ratio := m.posYRatio
	m.mu.Unlock()

	hwnd, _, _ := procGetActiveWindow.Call()
	if hwnd == 0 {
		return
	}

	screenW, _, _ := procGetSystemMetrics.Call(uintptr(smCxScreen))
	screenH, _, _ := procGetSystemMetrics.Call(uintptr(smCyScreen))

	x := int(screenW) - width
	if side == DockSideLeft {
		x = 0
	}
	availH := int(screenH) - height
	if availH < 0 {
		availH = 0
	}
	y := int(float64(availH) * ratio)

	procSetWindowPos.Call(
		hwnd,
		hwndTopMost,
		uintptr(x),
		uintptr(y),
		uintptr(width),
		uintptr(height),
		swpShowWindow,
	)
}

func (m *WindowsManager) DockToRightEdge(width, height int) {
	m.SetDockSide(DockSideRight)
	m.Dock(width, height)
}

func (m *WindowsManager) SetDockSide(side DockSide) {
	m.mu.Lock()
	m.dockSide = side
	w := m.currentWidth
	h := m.currentHeight
	m.mu.Unlock()
	m.Dock(w, h)
}

func (m *WindowsManager) GetDockSide() DockSide {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.dockSide
}

func (m *WindowsManager) SetMonitor(screenIndex int) {
	m.mu.Lock()
	m.monitorIndex = screenIndex
	w := m.currentWidth
	h := m.currentHeight
	m.mu.Unlock()
	m.Dock(w, h)
}

func (m *WindowsManager) GetSelectedMonitor() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.monitorIndex
}

func (m *WindowsManager) GetMonitors() []MonitorInfo {
	screenW, _, _ := procGetSystemMetrics.Call(uintptr(smCxScreen))
	screenH, _, _ := procGetSystemMetrics.Call(uintptr(smCyScreen))
	return []MonitorInfo{
		{
			Index:  0,
			Name:   "Primary Monitor",
			IsMain: true,
			Width:  int(screenW),
			Height: int(screenH),
		},
	}
}

func (m *WindowsManager) SetPositionRatio(ratio float64) {
	if ratio < 0.0 {
		ratio = 0.0
	}
	if ratio > 1.0 {
		ratio = 1.0
	}
	m.mu.Lock()
	m.posYRatio = ratio
	w := m.currentWidth
	h := m.currentHeight
	m.mu.Unlock()
	m.Dock(w, h)
}

func (m *WindowsManager) GetPositionRatio() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.posYRatio
}

func (m *WindowsManager) StartWindowDrag() {}

func (m *WindowsManager) OpenTicketURL(url string) error {
	return OpenURL(url)
}

func (m *WindowsManager) SetAlwaysOnTop(alwaysOnTop bool) {}
func (m *WindowsManager) GetAlwaysOnTop() bool            { return true }
func (m *WindowsManager) SetAutoHide(autoHide bool)       {}
func (m *WindowsManager) GetAutoHide() bool               { return false }
func (m *WindowsManager) SetTucked(tucked bool, w, h int) {}
func (m *WindowsManager) SetToolTip(tooltip string)       {}

func IsMouseInside() bool                                     { return true }
func SetMobileWebViewVisible(visible bool, width, height int) {}
func LoadMobileTicketView(url string)                         {}
func LoadMobileTicketHTML(html, baseURL string)               {}
func RegisterCollapseHandler(cb func())                       {}
func RegisterDockChangeHandler(cb DockChangeCallback)         {}
func NativeCopyText(text string)                              {}
func NativePasteText() (string, bool)                         { return "", false }

