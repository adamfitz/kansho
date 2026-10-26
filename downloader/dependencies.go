package downloader

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// MissingDependencyError reports that an external program Kansho needs for a
// feature is not installed or cannot be found. It carries actionable install
// instructions so both the log and the UI can tell the user how to fix it.
type MissingDependencyError struct {
	// Dependency is the human readable name of the missing program.
	Dependency string

	// Detail explains what Kansho was trying to do and what it searched for.
	Detail string

	// InstallInstructions tells the user how to install the dependency.
	InstallInstructions string
}

// Error implements error.
func (e *MissingDependencyError) Error() string {
	name := e.Dependency
	if name == "" {
		name = "required dependency"
	}

	msg := fmt.Sprintf("missing dependency: %s", name)
	if e.Detail != "" {
		msg += ": " + e.Detail
	}
	if e.InstallInstructions != "" {
		msg += "\n\n" + e.InstallInstructions
	}
	return msg
}

// IsMissingDependency reports whether err (or any error it wraps) is a
// MissingDependencyError. Callers use this to fail fast instead of retrying a
// problem that only the user can fix by installing software.
func IsMissingDependency(err error) bool {
	var target *MissingDependencyError
	return errors.As(err, &target)
}

const browserDependencyName = "Chromium or Google Chrome"

// browserExecutableCandidates returns the executable names and well-known
// absolute paths checked for a local Chrome/Chromium installation. The list
// mirrors chromedp's own lookup order, with a few extra standard Linux and
// Windows locations. KANSHO_CHROME_PATH is handled by findBrowserExecutable
// before these candidates are considered.
func browserExecutableCandidates() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"chromium",
			"google-chrome",
		}
	case "windows":
		var candidates []string
		candidates = append(candidates, "chrome", "chrome.exe")
		for _, dir := range []string{
			os.Getenv("ProgramFiles(x86)"),
			os.Getenv("ProgramFiles"),
			os.Getenv("LOCALAPPDATA"),
		} {
			if dir == "" {
				continue
			}
			candidates = append(candidates,
				filepath.Join(dir, `Google\Chrome\Application\chrome.exe`),
				filepath.Join(dir, `Chromium\Application\chrome.exe`),
			)
		}
		return candidates
	default:
		return []string{
			"headless_shell",
			"headless-shell",
			"chromium",
			"chromium-browser",
			"google-chrome",
			"google-chrome-stable",
			"google-chrome-beta",
			"google-chrome-unstable",
			"/usr/bin/google-chrome",
			"/usr/bin/google-chrome-stable",
			"/usr/bin/chromium",
			"/usr/bin/chromium-browser",
			"/usr/local/bin/chrome",
			"/snap/bin/chromium",
			"chrome",
		}
	}
}

// findBrowserExecutable locates a Chrome/Chromium executable suitable for
// chromedp. It returns a MissingDependencyError with platform-specific install
// instructions when none can be found.
func findBrowserExecutable() (string, error) {
	custom := strings.TrimSpace(os.Getenv("KANSHO_CHROME_PATH"))
	if custom != "" {
		path, err := exec.LookPath(custom)
		if err != nil {
			return "", newBrowserDependencyError(fmt.Sprintf(
				"KANSHO_CHROME_PATH is set to %q, but that is not an executable file",
				custom,
			))
		}
		return path, nil
	}

	for _, candidate := range browserExecutableCandidates() {
		if path, err := exec.LookPath(candidate); err == nil {
			return path, nil
		}
	}

	return "", newBrowserDependencyError(
		"no Chrome or Chromium executable was found in PATH or the standard install " +
			"locations. Kansho needs it for sites that require JavaScript rendering or a " +
			"browser fallback (for example Cloudflare-protected pages)",
	)
}

func newBrowserDependencyError(detail string) *MissingDependencyError {
	return &MissingDependencyError{
		Dependency:          browserDependencyName,
		Detail:              detail,
		InstallInstructions: browserInstallInstructions(),
	}
}

func browserInstallInstructions() string {
	var install string
	switch runtime.GOOS {
	case "darwin":
		install = "  brew install --cask chromium\n" +
			"  or install Google Chrome from https://www.google.com/chrome/"
	case "windows":
		install = "  install Google Chrome from https://www.google.com/chrome/\n" +
			"  or Chromium from https://www.chromium.org/getting-involved/download-chromium/"
	default:
		install = "  Debian/Ubuntu: sudo apt install chromium\n" +
			"  Fedora:        sudo dnf install chromium\n" +
			"  Arch:          sudo pacman -S chromium\n" +
			"  or install Google Chrome from https://www.google.com/chrome/"
	}

	return "Install a Chromium-based browser, for example:\n" + install + "\n\n" +
		"Then retry the action. If the browser is installed in a non-standard location, " +
		"restart Kansho with KANSHO_CHROME_PATH set to the full path of the executable."
}
