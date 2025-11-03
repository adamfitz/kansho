package config

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"kansho/parser"
)

type Manga struct {
	Manga []Bookmarks `json:"manga"`
}

type Bookmarks struct {
	Title     string `json:"title"`
	Url       string `json:"url"`
	Chapters  string `json:"chapters"`
	Location  string `json:"location"`
	Site      string `json:"site"`
	Shortname string `json:"shortname"`
}

// load bookmarks return custom struct
// load bookmarks return custom struct
func LoadBookmarks() Manga {
	bookmarksLocation, err := verifyConfigFiles()
	if err != nil {
		log.Printf("error verifying config files: %v", err)
		return Manga{}
	}

	file, err := os.Open(bookmarksLocation)
	if err != nil {
		log.Printf("error loading bookmarks file: %v", err)
		return Manga{}
	}
	defer file.Close()

	byteValues, err := io.ReadAll(file)
	if err != nil {
		log.Printf("error reading bookmarks file: %v", err)
		return Manga{}
	}

	var mangaStruct Manga
	if err := json.Unmarshal(byteValues, &mangaStruct); err != nil {
		log.Printf("error unmarshalling bookmarks: %v", err)
	}

	return mangaStruct
}

// Save bookmark to file (always saves to ~/.config/kansho/bookmarks.json)
func SaveBookmarks(data Manga) error {
	bookmarksDir, err := verifyConfigDirectory()
	if err != nil {
		log.Fatalf("error verifying config directory: %v", err)
	}

	bookmarksFile := filepath.Join(bookmarksDir, "bookmarks.json")

	// Marshal to JSON
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}

	// Write to file
	return os.WriteFile(bookmarksFile, jsonData, 0644)
}

// check config directory exists or create it
func verifyConfigDirectory() (string, error) {
	configDirectory, expandError := parser.ExpandPath("~/.config/kansho")
	if expandError != nil {
		log.Fatalf("cannot verify local configuration directory: %v", expandError)
	}

	// Check if the directory exists
	_, err := os.Stat(configDirectory)

	if os.IsNotExist(err) {
		// Create the directory with read/write/execute permissions for owner, and read/execute for others
		err := os.MkdirAll(configDirectory, 0755)
		if err != nil {
			log.Fatalf("error creating directory %s: %v", configDirectory, err)
		}
		log.Printf("Directory %s created successfully.\n", configDirectory)
	} else if err != nil {
		log.Fatalf("error checking directory %s: %v", configDirectory, err)
	}

	return configDirectory, nil
}

// check config files exist or create them
func verifyConfigFiles() (string, error) {
	bookmarksDir, err := verifyConfigDirectory()
	if err != nil {
		return "", err
	}

	bookmarksFile := filepath.Join(bookmarksDir, "bookmarks.json")
	//templateFile := filepath.Join(bookmarksDir, "bookmarks.template")

	// Check if the bookmarks file exists
	_, err = os.Stat(bookmarksFile)

	if os.IsNotExist(err) {
		// File does not exist, create a barebones template
		log.Printf("Bookmarks file not found, creating template at '%s'\n", bookmarksFile)

		// Create barebones template data
		templateData := Manga{
			Manga: []Bookmarks{},
		}

		// Save the template to bookmarks.json
		if saveErr := SaveBookmarks(templateData); saveErr != nil {
			log.Fatalf("error creating bookmarks file: %v", saveErr)
		}
		log.Printf("File '%s' created successfully.\n", bookmarksFile)

	} else if err != nil {
		// An error occurred other than the file not existing
		log.Fatalf("error checking file existence: %v", err)
	} else {
		// File exists
		log.Printf("File '%s' already exists.\n", bookmarksFile)
	}

	return bookmarksFile, nil
}

// logging configuration
func Logging() error {
	// local config dir
	configDir, configDirErr := verifyConfigDirectory()
	if configDirErr != nil {
		return configDirErr
	}
	// open log file or creat it if it does not exist
	logFilePath := fmt.Sprintf("%s/kansho.log", configDir)
	logFile, logFileErr := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if logFileErr != nil {
		log.Fatalf("Failed to open log file: %v", logFileErr)
	}

	log.SetOutput(logFile)

	return nil
}
