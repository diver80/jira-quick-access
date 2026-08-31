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

// WindowManager abstracts platform-specific window flags, docking, and edge level positioning.
type WindowManager interface {
	InitEdgeRail(width, height int) error
	SetState(state WindowState, width, height int)
	DockToRightEdge(width, height int)
	OpenTicketURL(url string) error
}

var DefaultManager WindowManager
