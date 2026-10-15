package window

import (
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
)

// OpenURL opens the given http(s) URL in the user's default browser.
// Other schemes are rejected: URLs can come from remote feeds (e.g. Statuspage
// incidents), and file: or app-specific schemes would launch local programs.
func OpenURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("refusing to open non-http(s) URL %q", rawURL)
	}

	var cmd string
	var args []string

	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
		args = []string{rawURL}
	case "windows":
		// Not "cmd /c start": cmd re-parses &, | and ^ from the query string.
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", rawURL}
	default: // Linux, BSD
		cmd = "xdg-open"
		args = []string{rawURL}
	}

	c := exec.Command(cmd, args...)
	if err := c.Start(); err != nil {
		return fmt.Errorf("failed to open URL %q: %w", rawURL, err)
	}
	return nil
}
