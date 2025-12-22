package config

import "time"

// These are injected at build time via -ldflags
var (
	Version   string
	GitCommit string
	BuildTime string
)

func init() {
	// Local / dev fallback
	if Version == "" {
		Version = "dev"
	}
	if GitCommit == "" {
		GitCommit = "local"
	}
	if BuildTime == "" {
		BuildTime = time.Now().Format("2006-01-02 15:04:05")
	}
}
