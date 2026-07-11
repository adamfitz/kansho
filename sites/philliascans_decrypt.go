package sites

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"log"
	"math/big"

	"github.com/disintegration/imaging"
)

// philliaDecryptAndDescramble decrypts a PhiliaScans encrypted image and reverses the tile scramble.
//
// The encrypted format (scheme 4, header 0xFF 0x04):
//   - Bytes 0-1: magic [0xFF, 0x04]
//   - Bytes 2-3: width (big-endian uint16)
//   - Bytes 4-5: height (big-endian uint16)
//   - Bytes 6+:  AES-CTR encrypted payload
//
// Decryption key derivation:
//
//	derivedKey = HMAC-SHA256(chapterKey, "aesctr4:" + pageIndex)
//
// Tile descrambling:
//
//	tileKey = HMAC-SHA256(chapterKey, "tiles:" + pageIndex)
//	permutation = fisherYatesShuffle(gridSize², tileKey)
//	unshuffle tiles using permutation
func philliaDecryptAndDescramble(chapterKey []byte, pageIndex int, gridSize int, encryptedData []byte) ([]byte, error) {
	if len(encryptedData) < 4 {
		return nil, fmt.Errorf("encrypted data too short: %d bytes", len(encryptedData))
	}

	// Check if the image is already unencrypted (starts with RIFF for WebP or FF D8 FF for JPEG)
	if encryptedData[0] == 'R' && encryptedData[1] == 'I' && encryptedData[2] == 'F' && encryptedData[3] == 'F' {
		log.Printf("[PhiliaScans] Image is unencrypted WebP (RIFF), returning as-is")
		return encryptedData, nil
	}
	if encryptedData[0] == 0xFF && encryptedData[1] == 0xD8 && encryptedData[2] == 0xFF {
		log.Printf("[PhiliaScans] Image is unencrypted JPEG, returning as-is")
		return encryptedData, nil
	}

	if len(encryptedData) < 6 {
		return nil, fmt.Errorf("encrypted data too short for header: %d bytes", len(encryptedData))
	}

	// Parse header
	if encryptedData[0] != 0xFF {
		return nil, fmt.Errorf("invalid magic byte: 0x%02X", encryptedData[0])
	}
	scheme := encryptedData[1]
	if scheme != 4 && scheme != 3 && scheme != 2 {
		return nil, fmt.Errorf("unsupported scheme: %d", scheme)
	}

	// For schemes 2/3/4, header is 6 bytes (2 magic + 4 dims)
	width := binary.BigEndian.Uint16(encryptedData[2:4])
	height := binary.BigEndian.Uint16(encryptedData[4:6])
	if width < 1 || height < 1 {
		return nil, fmt.Errorf("invalid dimensions: %dx%d", width, height)
	}

	payload := encryptedData[6:]
	log.Printf("[PhiliaScans] Decrypt: scheme=%d, %dx%d, payload=%d bytes", scheme, width, height, len(payload))

	// Scheme-specific decryption:
	//   Scheme 2 (0xFF 0x02): AES-CTR with "aesctr:" prefix + tile descrambling
	//   Scheme 3 (0xFF 0x03): ChaCha20 + tile descrambling (not implemented)
	//   Scheme 4 (0xFF 0x04): AES-CTR with "aesctr4:" prefix, NO tile descrambling
	var aesPrefix string
	var needsDescramble bool
	switch scheme {
	case 2:
		aesPrefix = "aesctr:"
		needsDescramble = true
	case 4:
		aesPrefix = "aesctr4:"
		needsDescramble = false
	default:
		return nil, fmt.Errorf("scheme %d decryption not implemented", scheme)
	}

	// AES-CTR decryption
	decrypted, err := philliaAESCTRDecryptWithPrefix(chapterKey, pageIndex, aesPrefix, payload)
	if err != nil {
		return nil, fmt.Errorf("AES-CTR decrypt failed: %w", err)
	}

	// Decode the decrypted image to get pixel data
	img, format, err := image.Decode(bytes.NewReader(decrypted))
	if err != nil {
		return nil, fmt.Errorf("decode decrypted image failed (format=%s): %w", format, err)
	}
	log.Printf("[PhiliaScans] Decrypted image: %dx%d, format=%s", img.Bounds().Dx(), img.Bounds().Dy(), format)

	// Tile descrambling (only for scheme 2)
	if needsDescramble && gridSize > 1 {
		img, err = philliaDescrambleTiles(chapterKey, pageIndex, gridSize, img)
		if err != nil {
			return nil, fmt.Errorf("tile descramble failed: %w", err)
		}
	}

	// Encode result as JPEG (reliable encoder)
	var buf bytes.Buffer
	if err := imaging.Encode(&buf, img, imaging.JPEG, imaging.JPEGQuality(95)); err != nil {
		return nil, fmt.Errorf("encode output failed: %w", err)
	}

	return buf.Bytes(), nil
}

// philliaAESCTRDecryptWithPrefix decrypts data using AES-CTR with a key derived from HMAC-SHA256.
// prefix is the HMAC message prefix (e.g. "aesctr:" for scheme 2, "aesctr4:" for scheme 4).
func philliaAESCTRDecryptWithPrefix(chapterKey []byte, pageIndex int, prefix string, encrypted []byte) ([]byte, error) {
	// Import chapterKey as HMAC-SHA256 key
	h := hmac.New(sha256.New, chapterKey)

	// Sign "{prefix}{pageIndex}" to derive the AES key
	msg := fmt.Sprintf("%s%d", prefix, pageIndex)
	h.Write([]byte(msg))
	derivedKey := h.Sum(nil) // 32 bytes

	// Create AES-CTR cipher with zero IV
	block, err := aes.NewCipher(derivedKey)
	if err != nil {
		return nil, fmt.Errorf("AES cipher creation failed: %w", err)
	}

	iv := make([]byte, 16) // counter = 0
	stream := cipher.NewCTR(block, iv)

	// Decrypt
	decrypted := make([]byte, len(encrypted))
	stream.XORKeyStream(decrypted, encrypted)

	return decrypted, nil
}

// philliaDescrambleTiles rearranges image tiles to undo the scrambling.
//
// The permutation is generated using Fisher-Yates shuffle seeded by:
//
//	HMAC-SHA256(HMAC-SHA256(chapterKey, "tiles:"+pageIndex), "perm:"+counter)
//
// For each destination tile position t, the source tile is at permutation[t].
func philliaDescrambleTiles(chapterKey []byte, pageIndex int, gridSize int, img image.Image) (image.Image, error) {
	bounds := img.Bounds()
	imgW := bounds.Dx()
	imgH := bounds.Dy()

	// Compute padded dimensions (must be multiples of gridSize)
	paddedW := ((imgW + gridSize - 1) / gridSize) * gridSize
	paddedH := ((imgH + gridSize - 1) / gridSize) * gridSize

	tileW := paddedW / gridSize
	tileH := paddedH / gridSize
	totalTiles := gridSize * gridSize

	if totalTiles < 2 {
		return img, nil
	}

	// Generate permutation
	permutation, err := philliaGeneratePermutation(chapterKey, pageIndex, gridSize)
	if err != nil {
		return nil, fmt.Errorf("permutation generation failed: %w", err)
	}
	log.Printf("[PhiliaScans] Permutation: %v", permutation)

	// Create padded source
	src := image.NewRGBA(image.Rect(0, 0, paddedW, paddedH))
	for y := 0; y < paddedH; y++ {
		for x := 0; x < paddedW; x++ {
			if x < imgW && y < imgH {
				src.Set(x, y, img.At(x+bounds.Min.X, y+bounds.Min.Y))
			} else {
				src.Set(x, y, color.White)
			}
		}
	}

	// Create result buffer
	dst := image.NewRGBA(image.Rect(0, 0, paddedW, paddedH))

	// Apply permutation: for each destination tile t, read from source tile permutation[t]
	for t := 0; t < totalTiles; t++ {
		srcTile := permutation[t]
		srcTileX := (srcTile % gridSize) * tileW
		srcTileY := (srcTile / gridSize) * tileH
		dstTileX := (t % gridSize) * tileW
		dstTileY := (t / gridSize) * tileH

		for dy := 0; dy < tileH; dy++ {
			for dx := 0; dx < tileW; dx++ {
				sx := srcTileX + dx
				sy := srcTileY + dy
				if sx < paddedW && sy < paddedH {
					dst.Set(dstTileX+dx, dstTileY+dy, src.At(sx, sy))
				}
			}
		}
	}

	return dst, nil
}

// philliaGeneratePermutation generates a tile permutation using HMAC-seeded Fisher-Yates shuffle.
func philliaGeneratePermutation(chapterKey []byte, pageIndex int, gridSize int) ([]int, error) {
	totalTiles := gridSize * gridSize
	if totalTiles < 2 {
		perm := make([]int, totalTiles)
		for i := range perm {
			perm[i] = i
		}
		return perm, nil
	}

	// Step 1: Derive tile key = HMAC-SHA256(chapterKey, "tiles:{pageIndex}")
	tileHMAC := hmac.New(sha256.New, chapterKey)
	tileHMAC.Write([]byte(fmt.Sprintf("tiles:%d", pageIndex)))
	tileKey := tileHMAC.Sum(nil) // 32 bytes

	// Step 2: Fisher-Yates shuffle seeded by HMAC-SHA256(tileKey, "perm:{counter}")
	perm := make([]int, totalTiles)
	for i := range perm {
		perm[i] = i
	}

	counter := 0
	hmacState := hmac.New(sha256.New, tileKey)
	hmacCache := make([]byte, 0, 32)
	hmacCacheIdx := 32 // force first generation

	getRandom := func() (uint32, error) {
		if hmacCacheIdx >= 32 {
			hmacState.Reset()
			hmacState.Write([]byte(fmt.Sprintf("perm:%d", counter)))
			hmacCache = hmacState.Sum(hmacCache[:0])
			hmacCacheIdx = 0
			counter++
		}
		val := binary.LittleEndian.Uint32(hmacCache[hmacCacheIdx : hmacCacheIdx+4])
		hmacCacheIdx += 4
		return val, nil
	}

	// Fisher-Yates shuffle (reverse)
	for i := totalTiles - 1; i >= 1; i-- {
		j64, err := getRandom()
		if err != nil {
			return nil, err
		}
		j := int(new(big.Int).Mod(new(big.Int).SetUint64(uint64(j64)), big.NewInt(int64(i+1))).Int64())
		perm[i], perm[j] = perm[j], perm[i]
	}

	return perm, nil
}
