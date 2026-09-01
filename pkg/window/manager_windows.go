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
	mu    sync.RWMutex
	state WindowState
}

func init() {
	DefaultManager = &WindowsManager{
		state: StateRest,
	}
}

func (m *WindowsManager) InitEdgeRail(width, height int) error {
	m.SetState(StateRest, width, height)
	return nil
}

func (m *WindowsManager) SetState(state WindowState, width, height int) {
	m.mu.Lock()
	m.state = state
	m.mu.Unlock()
	m.DockToRightEdge(width, height)
}

func (m *WindowsManager) DockToRightEdge(width, height int) {
	m.mu.RLock()
	_ = m.state
	m.mu.RUnlock()
	hwnd, _, _ := procGetActiveWindow.Call()
	if hwnd == 0 {
		return
	}

	screenW, _, _ := procGetSystemMetrics.Call(uintptr(smCxScreen))
	screenH, _, _ := procGetSystemMetrics.Call(uintptr(smCyScreen))

	x := int(screenW) - width
	y := (int(screenH) - height) / 2

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

func (m *WindowsManager) OpenTicketURL(url string) error {
	return OpenURL(url)
}

func IsMouseInside() bool                                     { return true }
func SetMobileWebViewVisible(visible bool, width, height int) {}
func LoadMobileTicketView(url string)                         {}
func LoadMobileTicketHTML(html, baseURL string)               {}
func RegisterCollapseHandler(cb func())                       {}
