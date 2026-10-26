package downloader

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMissingDependencyErrorIncludesActionableMessage(t *testing.T) {
	err := &MissingDependencyError{
		Dependency:          "Chromium or Google Chrome",
		Detail:              "no executable was found",
		InstallInstructions: "sudo apt install chromium",
	}

	msg := err.Error()
	for _, want := range []string{
		"Chromium or Google Chrome",
		"no executable was found",
		"sudo apt install chromium",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error message %q does not contain %q", msg, want)
		}
	}
}

func TestIsMissingDependencyUnwraps(t *testing.T) {
	depErr := &MissingDependencyError{Dependency: "Chromium"}
	wrapped := fmt.Errorf("browser fetch failed: %w", depErr)

	if !IsMissingDependency(wrapped) {
		t.Fatal("IsMissingDependency did not recognize a wrapped MissingDependencyError")
	}
	if IsMissingDependency(errors.New("some other error")) {
		t.Fatal("IsMissingDependency matched an unrelated error")
	}
	if IsMissingDependency(nil) {
		t.Fatal("IsMissingDependency matched nil")
	}
}

func TestFindBrowserExecutableHonorsOverride(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test executable has no .exe suffix on Windows")
	}

	path := filepath.Join(t.TempDir(), "custom-chromium")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KANSHO_CHROME_PATH", path)

	got, err := findBrowserExecutable()
	if err != nil {
		t.Fatalf("findBrowserExecutable returned an error: %v", err)
	}
	if got != path {
		t.Fatalf("findBrowserExecutable returned %q, want %q", got, path)
	}
}

func TestFindBrowserExecutableRejectsInvalidOverride(t *testing.T) {
	t.Setenv("KANSHO_CHROME_PATH", filepath.Join(t.TempDir(), "does-not-exist"))

	_, err := findBrowserExecutable()
	if !IsMissingDependency(err) {
		t.Fatalf("findBrowserExecutable returned %v, want a MissingDependencyError", err)
	}
}

func TestNewBrowserSessionReportsMissingDependency(t *testing.T) {
	t.Setenv("KANSHO_CHROME_PATH", filepath.Join(t.TempDir(), "does-not-exist"))

	_, err := NewBrowserSession(context.Background(), "example.com", false)
	if !IsMissingDependency(err) {
		t.Fatalf("NewBrowserSession returned %v, want a MissingDependencyError", err)
	}
}
