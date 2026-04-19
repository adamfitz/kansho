package downloader

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

const (
	// linuxBundledPath is the fixed install location written by the deb package.
	linuxBundledPath = "/usr/lib/kansho/headless-shell"
)

// findChromePath returns the path to the Chromium headless-shell (or a
// suitable system Chrome) to use with chromedp.
//
// Search order:
//  1. Bundled binary shipped with kansho (preferred — known version, no
//     dependency on the user having Chrome installed)
//  2. System-installed Chrome / Chromium / Edge (fallback so the app still
//     works for developers running from a source checkout)
//
// Returning an empty string tells the caller to let chromedp use its own
// default search, which scans PATH for google-chrome / chromium-browser.
func findChromePath() string {
	// 1. Bundled binary
	if p := findBundled(); p != "" {
		log.Printf("[Chrome] Using bundled headless-shell: %s", p)
		return p
	}
	log.Printf("[Chrome] Bundled headless-shell not found, falling back to system Chrome")

	// 2. System install
	if p := findSystemChrome(); p != "" {
		log.Printf("[Chrome] Using system Chrome: %s", p)
		return p
	}

	// 3. Let chromedp try PATH itself
	log.Printf("[Chrome] No Chrome found via kansho search, letting chromedp use its defaults")
	return ""
}

// findBundled looks for the headless-shell binary that is shipped alongside
// kansho. The location differs by OS:
//
//   - Linux deb install: fixed path /usr/lib/kansho/headless-shell
//   - Windows zip:       headless-shell.exe in the same directory as kansho.exe
func findBundled() string {
	switch runtime.GOOS {
	case "linux":
		if _, err := os.Stat(linuxBundledPath); err == nil {
			return linuxBundledPath
		}
		return ""

	case "windows":
		exePath, err := os.Executable()
		if err != nil {
			log.Printf("[Chrome] Could not determine executable path: %v", err)
			return ""
		}
		// Resolve symlinks so we get the real directory
		exePath, err = filepath.EvalSymlinks(exePath)
		if err != nil {
			log.Printf("[Chrome] Could not resolve symlinks for executable path: %v", err)
			return ""
		}
		candidate := filepath.Join(filepath.Dir(exePath), "headless-shell.exe")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		return ""

	default:
		return ""
	}
}

// findSystemChrome checks well-known system installation paths as a fallback.
// On Linux we rely on chromedp's own PATH search (returning "" here is fine).
// On Windows Chrome is never in PATH so we check the standard install dirs.
func findSystemChrome() string {
	switch runtime.GOOS {
	case "windows":
		candidates := []string{
			// Per-user install (most common)
			filepath.Join(os.Getenv("LOCALAPPDATA"), `Google\Chrome\Application\chrome.exe`),
			// System-wide 64-bit
			filepath.Join(os.Getenv("PROGRAMFILES"), `Google\Chrome\Application\chrome.exe`),
			// System-wide 32-bit on 64-bit OS
			filepath.Join(os.Getenv("PROGRAMFILES(X86)"), `Google\Chrome\Application\chrome.exe`),
			// Microsoft Edge — ships with Windows 10/11, fully Chromium-based
			filepath.Join(os.Getenv("PROGRAMFILES(X86)"), `Microsoft\Edge\Application\msedge.exe`),
			filepath.Join(os.Getenv("PROGRAMFILES"), `Microsoft\Edge\Application\msedge.exe`),
			// Brave
			filepath.Join(os.Getenv("LOCALAPPDATA"), `BraveSoftware\Brave-Browser\Application\brave.exe`),
		}
		for _, p := range candidates {
			if p == "" {
				continue
			}
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
		// Last resort: PATH (unusual on Windows but possible in dev envs)
		for _, name := range []string{"chrome.exe", "msedge.exe"} {
			if p, err := exec.LookPath(name); err == nil {
				return p
			}
		}
		return ""

	default:
		// Linux / macOS: chromedp's DefaultExecAllocatorOptions already searches
		// PATH for google-chrome and chromium-browser. Return "" and let it do that.
		return ""
	}
}
