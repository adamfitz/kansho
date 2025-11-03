package parser

import (
	"os"
	"path/filepath"
	"strings"
)

// LocalChapterList returns a list of all files from the provided rootDir.
// Optionally pass an exclusion list to skip certain file names.
func LocalChapterList(rootDir string, exclusionList ...string) ([]string, error) {
	// Expand ~ to home directory
	expandedPath, err := expandPath(rootDir)
	if err != nil {
		return nil, err
	}

	// Convert exclusionList slice to a map for fast lookup
	exclusions := make(map[string]struct{}, len(exclusionList))
	for _, name := range exclusionList {
		exclusions[name] = struct{}{}
	}

	fileList := make([]string, 0)

	entries, err := os.ReadDir(expandedPath)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			if _, skip := exclusions[entry.Name()]; !skip {
				fileList = append(fileList, entry.Name())
			}
		}
	}

	return fileList, nil
}

// ExpandPath expands ~ to the user's home directory, or returns the path as-is
func expandPath(path string) (string, error) {
	if strings.HasPrefix(path, "~/") {
		// Path starts with ~/ so expand it
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(homeDir, path[2:]), nil
	}
	// Path doesn't start with ~/ so return it unchanged
	return path, nil
}
