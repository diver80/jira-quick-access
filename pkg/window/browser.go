package window

import (
	"fmt"
	"os/exec"
	"runtime"
)

// OpenURL opens the given URL in the user's default browser or desktop handler.
func OpenURL(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
		args = []string{url}
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start", "", url}
	default: // Linux, BSD
		cmd = "xdg-open"
		args = []string{url}
	}

	c := exec.Command(cmd, args...)
	if err := c.Start(); err != nil {
		return fmt.Errorf("failed to open URL %q: %w", url, err)
	}
	return nil
}
