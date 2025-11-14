package cf

import (
	"bytes"
	"compress/gzip"
	"io"
	"log"

	"github.com/andybalholm/brotli"
	"github.com/gocolly/colly"
)

// DecompressResponse automatically detects and decompresses HTTP response bodies
// that are compressed with gzip or Brotli. It modifies the response body in-place.
//
// This function should be called in Colly's OnResponse callback to handle
// responses from servers that send compressed content.
//
// Parameters:
//   - r: The Colly response object to decompress
//   - logPrefix: Optional prefix for log messages (e.g., "<mgeko>", "<xbato>")
//
// Returns:
//   - bool: true if decompression was performed, false otherwise
//   - error: any error encountered during decompression
//
// Example usage:
//
//	c.OnResponse(func(r *colly.Response) {
//	    if decompressed, err := cf.DecompressResponse(r, "<mysite>"); err != nil {
//	        log.Printf("Decompression error: %v", err)
//	        return
//	    } else if decompressed {
//	        log.Printf("Response decompressed successfully")
//	    }
//	    // Continue with normal response processing...
//	})
func DecompressResponse(r *colly.Response, logPrefix string) (bool, error) {
	if r == nil || len(r.Body) == 0 {
		return false, nil
	}

	// Add default log prefix if none provided
	if logPrefix == "" {
		logPrefix = "<cf>"
	}

	originalSize := len(r.Body)

	// Debug: Show first bytes for troubleshooting
	if len(r.Body) >= 10 {
		log.Printf("%s DEBUG: First 10 bytes (hex): % x", logPrefix, r.Body[:10])
	}

	// Check compression type by magic bytes and Content-Encoding header
	contentEncoding := r.Headers.Get("Content-Encoding")

	// Try gzip first (most common)
	if len(r.Body) >= 2 && r.Body[0] == 0x1f && r.Body[1] == 0x8b {
		log.Printf("%s Detected gzip compression (magic bytes: 1f 8b)", logPrefix)
		return decompressGzip(r, logPrefix, originalSize)
	}

	// Try Brotli if Content-Encoding header indicates it
	if contentEncoding == "br" {
		log.Printf("%s Detected Brotli via Content-Encoding header", logPrefix)
		return decompressBrotli(r, logPrefix, originalSize)
	}

	// Heuristic: Try Brotli if first byte suggests compression
	// Brotli streams often start with bytes in range 0x80-0x8f, 0x00-0x0f
	if len(r.Body) >= 1 {
		firstByte := r.Body[0]

		// Common Brotli patterns (not foolproof but works for most cases)
		if firstByte >= 0x80 && firstByte <= 0x8f {
			log.Printf("%s Detected possible Brotli compression (first byte: %02x)", logPrefix, firstByte)
			decompressed, err := decompressBrotli(r, logPrefix, originalSize)
			if err != nil {
				// If Brotli fails, the content might not be compressed
				log.Printf("%s Brotli decompression failed, treating as uncompressed: %v", logPrefix, err)
				return false, nil
			}
			return decompressed, nil
		}
	}

	// Not compressed or unknown compression
	log.Printf("%s Response does not appear to be compressed", logPrefix)
	return false, nil
}

// decompressGzip handles gzip decompression
func decompressGzip(r *colly.Response, logPrefix string, originalSize int) (bool, error) {
	reader, err := gzip.NewReader(bytes.NewReader(r.Body))
	if err != nil {
		return false, err
	}
	defer reader.Close()

	decompressed, err := io.ReadAll(reader)
	if err != nil {
		return false, err
	}

	r.Body = decompressed
	log.Printf("%s ✓ Decompressed gzip: %d bytes → %d bytes", logPrefix, originalSize, len(decompressed))
	return true, nil
}

// decompressBrotli handles Brotli decompression
func decompressBrotli(r *colly.Response, logPrefix string, originalSize int) (bool, error) {
	reader := brotli.NewReader(bytes.NewReader(r.Body))

	decompressed, err := io.ReadAll(reader)
	if err != nil {
		return false, err
	}

	r.Body = decompressed
	log.Printf("%s ✓ Decompressed Brotli: %d bytes → %d bytes", logPrefix, originalSize, len(decompressed))
	return true, nil
}

// DecompressResponseBody is a convenience function that returns the decompressed body
// without modifying the original response. Useful when you need the decompressed data
// but want to keep the response object unchanged.
//
// Parameters:
//   - body: The compressed response body
//   - contentEncoding: The Content-Encoding header value (optional)
//
// Returns:
//   - []byte: The decompressed body
//   - bool: true if decompression was performed
//   - error: any error encountered during decompression
func DecompressResponseBody(body []byte, contentEncoding string) ([]byte, bool, error) {
	if len(body) == 0 {
		return body, false, nil
	}

	// Try gzip
	if len(body) >= 2 && body[0] == 0x1f && body[1] == 0x8b {
		reader, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, false, err
		}
		defer reader.Close()

		decompressed, err := io.ReadAll(reader)
		if err != nil {
			return nil, false, err
		}
		return decompressed, true, nil
	}

	// Try Brotli
	if contentEncoding == "br" || (len(body) >= 1 && body[0] >= 0x80 && body[0] <= 0x8f) {
		reader := brotli.NewReader(bytes.NewReader(body))
		decompressed, err := io.ReadAll(reader)
		if err != nil {
			// Not Brotli or corrupted
			return body, false, nil
		}
		return decompressed, true, nil
	}

	// Not compressed
	return body, false, nil
}
