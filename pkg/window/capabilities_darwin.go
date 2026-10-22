//go:build darwin && cgo

package window

// Platform capabilities: the macOS build embeds the ticket in a WKWebView and
// tracks the pointer natively. Variables so tests can exercise the fallbacks.
var (
	EmbeddedTicketView = true
	PointerTracking    = true
)
