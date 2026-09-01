//go:build (!darwin && !windows) || (darwin && !cgo)

package window

import "sync"

type FallbackManager struct {
	mu    sync.RWMutex
	state WindowState
}

func init() {
	if DefaultManager == nil {
		DefaultManager = &FallbackManager{
			state: StateRest,
		}
	}
}

func (m *FallbackManager) InitEdgeRail(width, height int) error {
	m.SetState(StateRest, width, height)
	return nil
}

func (m *FallbackManager) SetState(state WindowState, width, height int) {
	m.mu.Lock()
	m.state = state
	m.mu.Unlock()
	m.DockToRightEdge(width, height)
}

func (m *FallbackManager) DockToRightEdge(width, height int) {
	m.mu.RLock()
	_ = m.state
	m.mu.RUnlock()
}
func (m *FallbackManager) OpenTicketURL(url string) error {
	return OpenURL(url)
}

func IsMouseInside() bool                                     { return true }
func SetMobileWebViewVisible(visible bool, width, height int) {}
func LoadMobileTicketView(url string)                         {}
func LoadMobileTicketHTML(html, baseURL string)               {}
func RegisterCollapseHandler(cb func())                       {}
