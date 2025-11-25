package parser

import (
	"bytes"
	"errors"
	"image"
	"image/gif"
	"image/png"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/disintegration/imaging"
	"golang.org/x/image/webp"
)

// detectImageFormat reads the magic bytes and returns the current image format string
func detectImageFormat(data []byte) (string, error) {
	if len(data) < 12 {
		return "", errors.New("data too short to determine format")
	}

	if data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return "jpeg", nil
	}
	if data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 {
		return "png", nil
	}
	if string(data[0:6]) == "GIF87a" || string(data[0:6]) == "GIF89a" {
		return "gif", nil
	}
	if string(data[0:4]) == "RIFF" && len(data) >= 12 && string(data[8:12]) == "WEBP" {
		return "webp", nil
	}

	return "", errors.New("unknown image format")
}

// ConvertImageToJPEG converts image bytes to JPEG and saves to outputPath
// If already JPEG, saves directly without re-encoding
func ConvertImageToJPEG(imgBytes []byte, outputPath string) error {
	if len(imgBytes) == 0 {
		return errors.New("empty image data")
	}

	format, err := detectImageFormat(imgBytes)
	if err != nil {
		return err
	}

	// If already JPEG, just save raw bytes directly (no conversion needed)
	if format == "jpeg" {
		return saveRawBytes(imgBytes, outputPath)
	}

	// Decode the image based on format
	var img image.Image
	reader := bytes.NewReader(imgBytes)

	switch format {
	case "png":
		img, err = png.Decode(reader)
	case "gif":
		img, err = gif.Decode(reader)
	case "webp":
		img, err = webp.Decode(reader)
	default:
		return errors.New("unsupported image format: " + format)
	}

	if err != nil {
		return errors.New("failed to decode " + format + " image: " + err.Error())
	}

	// Save as JPEG with quality 90
	return imaging.Save(img, outputPath, imaging.JPEGQuality(90))
}

// downloadAndConvertToJPGWithRetry downloads with retry logic
func downloadAndConvertToJPGWithRetry(imageURL, targetDir string, maxRetries int) error {
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			log.Printf("Retry attempt %d/%d for: %s", attempt, maxRetries, imageURL)
		}

		err := downloadAndConvertToJPG(imageURL, targetDir)
		if err == nil {
			return nil
		}

		lastErr = err
		log.Printf("Download/conversion failed (attempt %d/%d): %v", attempt+1, maxRetries+1, err)
	}

	return lastErr
}

// downloadAndConvertToJPG downloads an image from imageURL, converts to JPG if needed, and saves it inside targetDir
func downloadAndConvertToJPG(imageURL, targetDir string) error {
	resp, err := http.Get(imageURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return errors.New("bad response status: " + resp.Status)
	}

	imgBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if len(imgBytes) == 0 {
		return errors.New("empty response body")
	}

	base := filepath.Base(imageURL)
	ext := strings.ToLower(filepath.Ext(base))
	name := strings.TrimSuffix(base, ext)

	// pad the image filename to 3 digits
	paddedFileName := padFileName(name + ".jpg")

	// join the padded dir / filename back together
	outputFile := filepath.Join(targetDir, paddedFileName)

	// Convert and save
	return ConvertImageToJPEG(imgBytes, outputFile)
}

// DownloadAndConvertToJPG is the public wrapper with retry logic
func DownloadAndConvertToJPG(imageURL, targetDir string) error {
	return downloadAndConvertToJPGWithRetry(imageURL, targetDir, 3)
}

// downloadConvertToJPGRename is the internal function without retry
func downloadConvertToJPGRename(filename, imageURL, targetDir string) error {
	resp, err := http.Get(imageURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return errors.New("bad response status: " + resp.Status)
	}

	imgBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if len(imgBytes) == 0 {
		return errors.New("empty response body")
	}

	// pad the image filename to 3 digits
	paddedFileName := padFileName(filename + ".jpg")

	// join the padded dir / filename back together
	outputFile := filepath.Join(targetDir, paddedFileName)

	// Convert and save
	return ConvertImageToJPEG(imgBytes, outputFile)
}

// DownloadConvertToJPGRename is the public wrapper with retry logic
func DownloadConvertToJPGRename(filename, imageURL, targetDir string) error {
	var lastErr error
	maxRetries := 3

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			log.Printf("Retry attempt %d/%d for: %s", attempt, maxRetries, imageURL)
		}

		err := downloadConvertToJPGRename(filename, imageURL, targetDir)
		if err == nil {
			return nil
		}

		lastErr = err
		log.Printf("Download/conversion failed (attempt %d/%d): %v", attempt+1, maxRetries+1, err)
	}

	return lastErr
}

// saveRawBytes saves bytes directly to file without conversion
func saveRawBytes(data []byte, outputPath string) error {
	return os.WriteFile(outputPath, data, 0644)
}
