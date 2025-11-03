package parser

import (
	"bytes"
	"errors"
	"image"
	"image/jpeg"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/chai2010/webp"
)

// reads the magic bytes and returns the current image format string (like "jpeg", "png", "webp")
func DetectImageFormat(data []byte) (string, error) {
	if len(data) < 12 {
		return "", errors.New("data too short to determine format")
	}

	if bytes.HasPrefix(data, []byte{0xFF, 0xD8, 0xFF}) {
		return "jpeg", nil
	}
	if bytes.HasPrefix(data, []byte{0x89, 0x50, 0x4E, 0x47}) {
		return "png", nil
	}
	if bytes.HasPrefix(data, []byte("GIF87a")) || bytes.HasPrefix(data, []byte("GIF89a")) {
		return "gif", nil
	}
	if bytes.HasPrefix(data, []byte("RIFF")) && bytes.HasPrefix(data[8:], []byte("WEBP")) {
		return "webp", nil
	}

	return "", errors.New("unknown image format")
}

// downloads an image from imageURL, converts to JPG if needed, and saves it inside targetDir, returns error if any
func DownloadAndConvertToJPG(imageURL, targetDir string) error {
	resp, err := http.Get(imageURL)
	if err != nil {
		log.Fatalf("failed to download image: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Fatalf("bad response status: %s", resp.Status)
	}

	imgBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("failed to read image data: %v", err)
	}

	format, err := DetectImageFormat(imgBytes)
	if err != nil {
		log.Fatalf("failed to detect image format: %v", err)
	}

	base := filepath.Base(imageURL)
	ext := strings.ToLower(filepath.Ext(base))
	name := strings.TrimSuffix(base, ext)

	// pad the image filename to 3 digits
	padedFileName := padFileName(name + ".jpg")

	// join teh padded dir / filename back together
	outputFile := filepath.Join(targetDir, padedFileName)

	// If already JPEG, just save raw bytes directly
	if format == "jpeg" {
		err = os.WriteFile(outputFile, imgBytes, 0644)
		if err != nil {
			log.Fatalf("failed to save jpeg image: %v", err)
		}
		return nil
	}

	// Decode image according to detected format
	var img image.Image

	switch format {
	case "png", "gif":
		img, _, err = image.Decode(bytes.NewReader(imgBytes))
		if err != nil {
			log.Fatalf("failed to decode image: %v", err)
		}
	case "webp":
		img, err = webp.Decode(bytes.NewReader(imgBytes))
		if err != nil {
			log.Fatalf("failed to decode webp image: %v", err)
		}
	default:
		log.Fatalf("unsupported image format: %s", format)
	}

	// Convert and save as JPG
	outFile, err := os.Create(outputFile)
	if err != nil {
		log.Fatalf("failed to create output file: %v", err)
	}
	defer outFile.Close()

	opts := jpeg.Options{Quality: 90}
	err = jpeg.Encode(outFile, img, &opts)
	if err != nil {
		log.Fatalf("failed to encode jpeg: %v", err)
	}

	return nil
}
