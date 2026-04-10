package auth

import (
	"os/exec"
	"runtime"
)

// OpenURL attempts to open the given URL in the user's default browser.
//
// Implementation uses the OS's native URL handler via stdlib `os/exec`,
// intentionally avoiding a third-party dependency:
//   - darwin:  `open <url>`
//   - linux:   `xdg-open <url>` (may fail silently in WSL / headless)
//   - windows: `rundll32 url.dll,FileProtocolHandler <url>`
//   - other:   no-op, returns nil
//
// The command is launched with `cmd.Start()`, not `cmd.Run()`, so this
// function does not block waiting for the browser process to exit.
//
// IMPORTANT: callers must always print the URL to stdout BEFORE invoking
// this function. In headless/WSL/SSH environments the command may succeed
// silently without actually opening anything (or fail silently), and the
// user's only recourse is to copy the URL from the terminal. There is no
// reliable cross-platform way to detect "browser actually opened".
func OpenURL(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return nil
	}
	return cmd.Start()
}
