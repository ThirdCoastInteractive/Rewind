package application

import (
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"thirdcoast.systems/rewind/pkg/encryption"
)

// InitEncryptionManager initializes the encryption manager from environment configuration.
// The encryption key should be a 64-character hex string (32 bytes).
// The cipher type can be "chacha20-poly1305" (default), "xchacha20-poly1305", or "aes-256-gcm".
func InitEncryptionManager() (*encryption.Manager, error) {
	// Get encryption key from environment
	keyHex := os.Getenv("ENCRYPTION_KEY")
	if keyHex == "" {
		return nil, fmt.Errorf("ENCRYPTION_KEY environment variable not set")
	}

	// Decode hex key
	key, err := hex.DecodeString(keyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid ENCRYPTION_KEY format (must be 64-char hex string): %w", err)
	}

	if len(key) != 32 {
		return nil, fmt.Errorf("ENCRYPTION_KEY must be exactly 32 bytes (64 hex chars), got %d bytes", len(key))
	}

	// Get cipher type from environment (default: chacha20-poly1305)
	cipherType := os.Getenv("ENCRYPTION_CIPHER")
	if cipherType == "" {
		cipherType = "chacha20-poly1305"
	}

	// Create manager based on cipher type
	var manager *encryption.Manager
	switch strings.ToLower(cipherType) {
	case "chacha20-poly1305":
		manager, err = encryption.NewManagerWithChaCha20Poly1305(key)
	case "xchacha20-poly1305":
		manager, err = encryption.NewManagerWithXChaCha20Poly1305(key)
	case "aes-256-gcm":
		manager, err = encryption.NewManagerWithAES256GCM(key)
	default:
		return nil, fmt.Errorf("unsupported ENCRYPTION_CIPHER: %s (must be chacha20-poly1305, xchacha20-poly1305, or aes-256-gcm)", cipherType)
	}

	if err != nil {
		return nil, fmt.Errorf("create encryption manager: %w", err)
	}

	return manager, nil
}
