//go:build windows

package window

import (
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

type WindowsManager struct{}

func init() {
	DefaultManager = &WindowsManager{}
}

func (m *WindowsManager) InitEdgeRail(width, height int) error {
	m.DockToRightEdge(width, height)
	return nil
}

func (m *WindowsManager) SetState(state WindowState, width, height int) {
	m.DockToRightEdge(width, height)
}

func (m *WindowsManager) DockToRightEdge(width, height int) {
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

func SetMobileWebViewVisible(visible bool, width, height int) {}
func LoadMobileTicketView(url string)                         {}
func LoadMobileTicketHTML(html, baseURL string)               {}
func RegisterCollapseHandler(cb func())                       {}
