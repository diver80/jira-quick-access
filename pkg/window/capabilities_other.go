//go:build !darwin || !cgo

package window

// Platform capabilities: no embedded ticket view (tickets open in the browser) and
// no native pointer tracking (IsMouseInside always reports true).
var (
	EmbeddedTicketView = false
	PointerTracking    = false
)
