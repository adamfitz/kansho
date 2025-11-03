package parser

import (
	"archive/zip"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// LocalChapterList returns a list of all files from the provided rootDir.
// Optionally pass an exclusion list to skip certain file names.
func LocalChapterList(rootDir string, exclusionList ...string) ([]string, error) {
	// Expand ~ to home directory
	expandedPath, err := ExpandPath(rootDir)
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

	filteredFileList := filterCBZFiles(fileList)

	return filteredFileList, nil
}

// expands ~ to the user's home directory, or returns the path as-is
func ExpandPath(path string) (string, error) {
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

// filters out any non *.cbz file from the list
func filterCBZFiles(files []string) []string {
	var filtered []string
	for _, f := range files {
		if strings.EqualFold(filepath.Ext(f), ".cbz") {
			filtered = append(filtered, f)
		}
	}
	return filtered
}

// extract and sort map keys in ascending order, return string slice
func SortKeys(inputMap map[string]string) ([]string, error) {

	var sortedList []string

	for key := range inputMap {
		sortedList = append(sortedList, key)
	}

	sort.Strings(sortedList)

	return sortedList, nil
}

// pad the filename to 3 digits, the inputFileName must be a filename.ext and the filename must be string
// representation of a digit. The input filename will be an integer.jpg (or with some image extenstion), note the input
// file name must have an extension
func padFileName(inputFileName string) string {
	var outputFileName string

	if strings.Contains(inputFileName, ".") {
		// split the filename on the . to separate the extension while padding
		parts := strings.SplitN(inputFileName, ".", 2)

		// convert the fielname string to an integer
		fileNamePart, err := strconv.Atoi(parts[0])
		if err != nil {
			log.Printf("padFileName() - error when converting filename integer %v", err)
		}
		// pad the resulting integer
		padded := fmt.Sprintf("%03d", fileNamePart)

		// craete the final filename
		outputFileName = padded + "." + parts[1]

	} else {
		log.Fatal("padFileName() - inputFilename must contain an extension eg: filename.ext")
	}

	return outputFileName
}

// create cbz file from source directory that ONLY contains image files
// imput sourceDir is scanned and sorted to add files to cbz in order, note it is expected that the soureDir is the
// temp dir that ONLY contains image files
func CreateCbzFromDir(sourceDir, zipName string) error {
	// Read all directory entries
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		log.Fatalf("failed to read directory: %v", err)
	}

	// Collect all file names (skip directories)
	var files []string
	for _, entry := range entries {
		if !entry.IsDir() {
			files = append(files, entry.Name())
		}
	}

	// Sort files alphabetically for ordered inclusion
	sort.Strings(files)

	// Create output cbz (zip) file
	zipFile, err := os.Create(zipName)
	if err != nil {
		log.Fatalf("parser.CreateCbzFromDir() - failed to create cbz file: %v", err)
	}
	defer zipFile.Close()

	zipWriter := zip.NewWriter(zipFile)
	defer zipWriter.Close()

	// Add each file to the zip archive
	for _, file := range files {
		filePath := filepath.Join(sourceDir, file)

		err := func() error {
			f, err := os.Open(filePath)
			if err != nil {
				return err
			}
			defer f.Close()

			w, err := zipWriter.Create(file)
			if err != nil {
				return err
			}

			_, err = io.Copy(w, f)
			return err
		}()
		if err != nil {
			log.Fatalf("error adding %s to cbz: %v", filePath, err)
		}
	}

	return nil
}
