package stitch

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"io"
	"strings"

	_ "golang.org/x/image/webp"
	_ "image/jpeg"
	_ "image/png"
)

const maxAssetBytes int64 = 20 << 20

// ValidateAsset decodes an image and returns immutable bytes, format, and digest.
func ValidateAsset(r io.Reader) ([]byte, string, string, error) {
	b, err := io.ReadAll(io.LimitReader(r, maxAssetBytes+1))
	if err != nil {
		return nil, "", "", err
	}
	if int64(len(b)) > maxAssetBytes {
		return nil, "", "", fmt.Errorf("asset exceeds 20MB")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return nil, "", "", fmt.Errorf("invalid image: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > 8192 || cfg.Height > 8192 || int64(cfg.Width)*int64(cfg.Height) > 32_000_000 {
		return nil, "", "", fmt.Errorf("asset dimensions exceed limits")
	}
	switch strings.ToLower(format) {
	case "png", "jpeg", "webp":
	default:
		return nil, "", "", fmt.Errorf("unsupported image format %s", format)
	}
	decoded, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, "", "", fmt.Errorf("invalid image data: %w", err)
	}
	bounds := decoded.Bounds()
	if bounds.Dx() != cfg.Width || bounds.Dy() != cfg.Height {
		return nil, "", "", fmt.Errorf("invalid image dimensions")
	}
	h := sha256.Sum256(b)
	return b, format, hex.EncodeToString(h[:]), nil
}
