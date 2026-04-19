package cf

import (
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"syscall"
)

// OpenInBrowser opens the given URL in the users default browser.
// On Windows, cmd /c start is used with CREATE_NEW_PROCESS_GROUP so the
// spawned browser is fully detached from chromedps process group. Without
// this, defer session.Close() kills the browser window before it opens.
func OpenInBrowser(url string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "windows":
		// cmd /c start "" <url> launches the default browser fully detached.
		// The empty string is a required title argument when the target is a URL.
		cmd = exec.Command("cmd", "/c", "start", "", url)
		cmd.SysProcAttr = &syscall.SysProcAttr{
			CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
		}
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	log.Printf("Opening URL in browser: %s", url)
	return cmd.Start()
}

// GetChallengeURL extracts the best URL to open in the browser
// based on the cfInfo detection.
func GetChallengeURL(info *CfInfo, originalURL string) string {
	if info.MetaRedirect != "" {
		return info.MetaRedirect
	}
	if info.FormAction != "" {
		return info.FormAction
	}
	return originalURL
}
