package cf

import (
	"fmt"
	"log"
	"os/exec"
	"runtime"
)

// OpenInBrowser opens the given URL in the user's default browser
func OpenInBrowser(url string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin": // macOS
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
// based on the cfInfo detection
func GetChallengeURL(info *CfInfo, originalURL string) string {
	// Priority 1: Meta redirect (most direct)
	if info.MetaRedirect != "" {
		return info.MetaRedirect
	}

	// Priority 2: Form action URL
	if info.FormAction != "" {
		return info.FormAction
	}

	// Priority 3: Just open the original URL
	// The browser will get the challenge naturally
	return originalURL
}
