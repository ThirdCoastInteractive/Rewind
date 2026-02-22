package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"

	"golang.org/x/crypto/chacha20poly1305"
)

// CipherType represents the encryption algorithm used.
type CipherType string

const (
	// CipherChaCha20Poly1305 uses ChaCha20-Poly1305 (IETF RFC 8439).
	// Recommended default: 3x faster than AES-GCM on mobile/ARM, constant-time.
	CipherChaCha20Poly1305 CipherType = "chacha20-poly1305"

	// CipherXChaCha20Poly1305 uses XChaCha20-Poly1305 (extended nonce variant).
	// Use for high-volume encryption where nonce exhaustion is a concern.
	CipherXChaCha20Poly1305 CipherType = "xchacha20-poly1305"

	// CipherAES256GCM uses AES-256-GCM.
	// Use when hardware acceleration (AES-NI) is available and required.
	CipherAES256GCM CipherType = "aes-256-gcm"
)

// Cipher wraps an AEAD cipher with metadata.
type Cipher struct {
	aead       cipher.AEAD
	cipherType CipherType
	nonceSize  int
}

// NewChaCha20Poly1305Cipher creates a ChaCha20-Poly1305 cipher.
// Key must be exactly 32 bytes (256 bits).
func NewChaCha20Poly1305Cipher(key []byte) (*Cipher, error) {
	if len(key) != chacha20poly1305.KeySize {
		return nil, fmt.Errorf("invalid key size: got %d, want %d", len(key), chacha20poly1305.KeySize)
	}

	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return nil, fmt.Errorf("create chacha20poly1305 cipher: %w", err)
	}

	return &Cipher{
		aead:       aead,
		cipherType: CipherChaCha20Poly1305,
		nonceSize:  aead.NonceSize(),
	}, nil
}

// NewXChaCha20Poly1305Cipher creates an XChaCha20-Poly1305 cipher.
// Key must be exactly 32 bytes (256 bits).
func NewXChaCha20Poly1305Cipher(key []byte) (*Cipher, error) {
	if len(key) != chacha20poly1305.KeySize {
		return nil, fmt.Errorf("invalid key size: got %d, want %d", len(key), chacha20poly1305.KeySize)
	}

	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("create xchacha20poly1305 cipher: %w", err)
	}

	return &Cipher{
		aead:       aead,
		cipherType: CipherXChaCha20Poly1305,
		nonceSize:  aead.NonceSize(),
	}, nil
}

// NewAES256GCMCipher creates an AES-256-GCM cipher.
// Key must be exactly 32 bytes (256 bits).
func NewAES256GCMCipher(key []byte) (*Cipher, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("invalid key size: got %d, want 32", len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create aes cipher: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create gcm: %w", err)
	}

	return &Cipher{
		aead:       aead,
		cipherType: CipherAES256GCM,
		nonceSize:  aead.NonceSize(),
	}, nil
}

// Encrypt encrypts plaintext and returns ciphertext with nonce prepended.
// Format: [nonce][ciphertext+tag]
func (c *Cipher) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, c.nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	ciphertext := c.aead.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// Decrypt decrypts ciphertext (with prepended nonce) and returns plaintext.
// Format: [nonce][ciphertext+tag]
func (c *Cipher) Decrypt(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) < c.nonceSize {
		return nil, fmt.Errorf("ciphertext too short: got %d, need at least %d", len(ciphertext), c.nonceSize)
	}

	nonce := ciphertext[:c.nonceSize]
	encrypted := ciphertext[c.nonceSize:]

	plaintext, err := c.aead.Open(nil, nonce, encrypted, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}

	return plaintext, nil
}

// Type returns the cipher type.
func (c *Cipher) Type() CipherType {
	return c.cipherType
}

// NonceSize returns the nonce size in bytes.
func (c *Cipher) NonceSize() int {
	return c.nonceSize
}
