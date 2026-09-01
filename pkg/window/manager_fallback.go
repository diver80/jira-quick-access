//go:build (linux && !cgo) || (darwin && !cgo) || (!darwin && !windows && !linux)

package window

type FallbackManager struct{}

func init() {
	if DefaultManager == nil {
		DefaultManager = &FallbackManager{}
	}
}

func (m *FallbackManager) InitEdgeRail(width, height int) error          { return nil }
func (m *FallbackManager) SetState(state WindowState, width, height int) {}
func (m *FallbackManager) DockToRightEdge(width, height int)             {}
func (m *FallbackManager) OpenTicketURL(url string) error {
	return OpenURL(url)
}

func IsMouseInside() bool                                     { return true }
func SetMobileWebViewVisible(visible bool, width, height int) {}
func LoadMobileTicketView(url string)                         {}
func LoadMobileTicketHTML(html, baseURL string)               {}
func RegisterCollapseHandler(cb func())                       {}
