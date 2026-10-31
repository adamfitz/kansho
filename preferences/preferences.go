// Package preferences loads and saves Kansho's user preferences.
//
// Preferences always live in the default configuration directory
// (~/.config/kansho/kansho.conf) so Kansho can find them before it knows where
// the database has been moved to. The preferences file is YAML and currently
// stores only the database directory.
package preferences

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// DefaultDirectory is the default Kansho configuration directory.
	DefaultDirectory = "~/.config/kansho"

	// FileName is the YAML preferences file name inside DefaultDirectory.
	FileName = "kansho.conf"

	// DatabaseFileName is the fixed database file name inside the configured
	// database directory.
	DatabaseFileName = "kansho.db"

	// LegacyBookmarksFileName is the old bookmarks file that is migrated into
	// the database once.
	LegacyBookmarksFileName = "bookmarks.json"
)

// Preferences is the on-disk Kansho preferences structure.
type Preferences struct {
	DatabaseDirectory  string `yaml:"database_directory"`
	WindowsMangaRoot   string `yaml:"windows_manga_root,omitempty"`
	WindowsWebtoonRoot string `yaml:"windows_webtoon_root,omitempty"`
	LinuxMangaRoot     string `yaml:"linux_manga_root,omitempty"`
	LinuxWebtoonRoot   string `yaml:"linux_webtoon_root,omitempty"`
}

// Default returns the default preferences.
func Default() Preferences {
	return Preferences{DatabaseDirectory: DefaultDirectory}
}

// RootDirectory returns the configured default root for a platform and title type.
func (p Preferences) RootDirectory(platform, kind string) string {
	if platform == "windows" {
		if kind == "webtoon" {
			return p.WindowsWebtoonRoot
		}
		return p.WindowsMangaRoot
	}
	if kind == "webtoon" {
		return p.LinuxWebtoonRoot
	}
	return p.LinuxMangaRoot
}

// SetRootDirectory sets the default root for a platform and title type.
func (p *Preferences) SetRootDirectory(platform, kind, root string) {
	if platform == "windows" {
		if kind == "webtoon" {
			p.WindowsWebtoonRoot = root
		} else {
			p.WindowsMangaRoot = root
		}
		return
	}
	if kind == "webtoon" {
		p.LinuxWebtoonRoot = root
	} else {
		p.LinuxMangaRoot = root
	}
}

// ConfigDirectory returns the expanded default configuration directory and
// creates it when necessary.
func ConfigDirectory() (string, error) {
	dir, err := ExpandPath(DefaultDirectory)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create Kansho config directory: %w", err)
	}
	return dir, nil
}

// FilePath returns the path to kansho.conf.
func FilePath() (string, error) {
	dir, err := ConfigDirectory()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, FileName), nil
}

// Load reads kansho.conf. A missing file is created with the defaults. An empty
// or incomplete file falls back to the defaults.
func Load() (Preferences, error) {
	path, err := FilePath()
	if err != nil {
		return Preferences{}, err
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		prefs := Default()
		if saveErr := Save(prefs); saveErr != nil {
			return prefs, saveErr
		}
		return prefs, nil
	}
	if err != nil {
		return Preferences{}, fmt.Errorf("read Kansho preferences: %w", err)
	}

	prefs := Default()
	if len(strings.TrimSpace(string(data))) > 0 {
		if err := yaml.Unmarshal(data, &prefs); err != nil {
			return Preferences{}, fmt.Errorf("parse Kansho preferences: %w", err)
		}
	}
	if strings.TrimSpace(prefs.DatabaseDirectory) == "" {
		prefs.DatabaseDirectory = DefaultDirectory
	}
	return prefs, nil
}

// Save writes kansho.conf atomically.
func Save(prefs Preferences) error {
	if strings.TrimSpace(prefs.DatabaseDirectory) == "" {
		prefs.DatabaseDirectory = DefaultDirectory
	}

	path, err := FilePath()
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(prefs)
	if err != nil {
		return fmt.Errorf("encode Kansho preferences: %w", err)
	}

	header := []byte("# Kansho preferences. Edit with care.\n")
	temp := path + ".tmp"
	if err := os.WriteFile(temp, append(header, data...), 0644); err != nil {
		return fmt.Errorf("write Kansho preferences: %w", err)
	}
	if err := os.Rename(temp, path); err != nil {
		_ = os.Remove(temp)
		return fmt.Errorf("install Kansho preferences: %w", err)
	}
	return nil
}

// EnsureFile creates kansho.conf with defaults when it does not exist.
func EnsureFile() error {
	_, err := Load()
	return err
}

// DatabaseDir returns the expanded database directory.
func (p Preferences) DatabaseDir() (string, error) {
	dir := strings.TrimSpace(p.DatabaseDirectory)
	if dir == "" {
		dir = DefaultDirectory
	}
	return ExpandPath(dir)
}

// DatabasePath returns the expanded database file path.
func (p Preferences) DatabasePath() (string, error) {
	dir, err := p.DatabaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, DatabaseFileName), nil
}

// LegacyBookmarksPath returns the old bookmarks.json path in the default
// configuration directory.
func LegacyBookmarksPath() (string, error) {
	dir, err := ConfigDirectory()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, LegacyBookmarksFileName), nil
}

// ExpandPath expands a leading ~/ to the current user's home directory.
func ExpandPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("path is empty")
	}
	if path == "~" {
		return os.UserHomeDir()
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
}
