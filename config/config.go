package config

import (
	"fmt"
	"log"

	"gopkg.in/lumberjack.v3"

	"kansho/cf"
	"kansho/models"
	"kansho/preferences"
)

func init() {
	// setup logging in init func to avoid stdout printing by package files that implement init functions with log.Printf
	logging()
}

// Manga is kept as an alias so existing callers can continue to use
// config.Manga while bookmark persistence lives in the database store.
type Manga = models.Manga

// Bookmarks is kept as an alias for the shared bookmark record type.
type Bookmarks = models.Bookmarks

// logging configuration
func logging() error {
	configDir, configDirErr := preferences.ConfigDirectory()
	if configDirErr != nil {
		return configDirErr
	}

	logFilePath := fmt.Sprintf("%s/kansho.log", configDir)
	logWriter, err := lumberjack.New(
		lumberjack.WithFileName(logFilePath),
		lumberjack.WithMaxBytes(10*lumberjack.MB),
		lumberjack.WithMaxBackups(2), // Keep last 2 log files plus the current one == 3 log files total
		lumberjack.WithCompress(),    // compress the old logfiles
	)
	if err != nil {
		return fmt.Errorf("failed to initialise log rotation: %w", err)
	}

	log.SetOutput(logWriter)

	if err := preferences.EnsureFile(); err != nil {
		log.Printf("Failed to create Kansho preferences file: %v", err)
	}

	// Initialize CloudFlare debug logger
	if err := cf.InitCFLogger(configDir); err != nil {
		log.Printf("Failed to initialize CF debug logger: %v", err)
	}

	return nil
}

// CloseLoggers closes all logger file handles
// Should be called before application exit
func CloseLoggers() {
	cf.CloseCFLogger()
}
