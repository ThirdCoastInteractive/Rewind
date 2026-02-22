package encryption

import (
	"bytes"
	"crypto/rand"
	"testing"

	"golang.org/x/crypto/chacha20poly1305"
)

func TestNewChaCha20Poly1305Cipher(t *testing.T) {
	t.Run("valid key", func(t *testing.T) {
		key := make([]byte, chacha20poly1305.KeySize)
		_, err := rand.Read(key)
		if err != nil {
			t.Fatalf("failed to generate key: %v", err)
		}

		cipher, err := NewChaCha20Poly1305Cipher(key)
		if err != nil {
			t.Fatalf("NewChaCha20Poly1305Cipher() error = %v", err)
		}

		if cipher.Type() != CipherChaCha20Poly1305 {
			t.Errorf("Type() = %v, want %v", cipher.Type(), CipherChaCha20Poly1305)
		}

		if cipher.NonceSize() != chacha20poly1305.NonceSize {
			t.Errorf("NonceSize() = %v, want %v", cipher.NonceSize(), chacha20poly1305.NonceSize)
		}
	})

	t.Run("invalid key size", func(t *testing.T) {
		key := make([]byte, 16) // Wrong size

		_, err := NewChaCha20Poly1305Cipher(key)
		if err == nil {
			t.Error("NewChaCha20Poly1305Cipher() expected error for invalid key size")
		}
	})

	t.Run("nil key", func(t *testing.T) {
		_, err := NewChaCha20Poly1305Cipher(nil)
		if err == nil {
			t.Error("NewChaCha20Poly1305Cipher() expected error for nil key")
		}
	})
}

func TestNewXChaCha20Poly1305Cipher(t *testing.T) {
	t.Run("valid key", func(t *testing.T) {
		key := make([]byte, chacha20poly1305.KeySize)
		_, err := rand.Read(key)
		if err != nil {
			t.Fatalf("failed to generate key: %v", err)
		}

		cipher, err := NewXChaCha20Poly1305Cipher(key)
		if err != nil {
			t.Fatalf("NewXChaCha20Poly1305Cipher() error = %v", err)
		}

		if cipher.Type() != CipherXChaCha20Poly1305 {
			t.Errorf("Type() = %v, want %v", cipher.Type(), CipherXChaCha20Poly1305)
		}

		if cipher.NonceSize() != chacha20poly1305.NonceSizeX {
			t.Errorf("NonceSize() = %v, want %v", cipher.NonceSize(), chacha20poly1305.NonceSizeX)
		}
	})

	t.Run("invalid key size", func(t *testing.T) {
		key := make([]byte, 16) // Wrong size

		_, err := NewXChaCha20Poly1305Cipher(key)
		if err == nil {
			t.Error("NewXChaCha20Poly1305Cipher() expected error for invalid key size")
		}
	})
}

func TestNewAES256GCMCipher(t *testing.T) {
	t.Run("valid key", func(t *testing.T) {
		key := make([]byte, 32)
		_, err := rand.Read(key)
		if err != nil {
			t.Fatalf("failed to generate key: %v", err)
		}

		cipher, err := NewAES256GCMCipher(key)
		if err != nil {
			t.Fatalf("NewAES256GCMCipher() error = %v", err)
		}

		if cipher.Type() != CipherAES256GCM {
			t.Errorf("Type() = %v, want %v", cipher.Type(), CipherAES256GCM)
		}

		if cipher.NonceSize() != 12 {
			t.Errorf("NonceSize() = %v, want 12", cipher.NonceSize())
		}
	})

	t.Run("invalid key size", func(t *testing.T) {
		key := make([]byte, 16) // Wrong size

		_, err := NewAES256GCMCipher(key)
		if err == nil {
			t.Error("NewAES256GCMCipher() expected error for invalid key size")
		}
	})
}

func TestCipherEncryptDecrypt(t *testing.T) {
	tests := []struct {
		name      string
		cipherFn  func([]byte) (*Cipher, error)
		keySize   int
		plaintext string
	}{
		{
			name:      "ChaCha20Poly1305",
			cipherFn:  NewChaCha20Poly1305Cipher,
			keySize:   chacha20poly1305.KeySize,
			plaintext: "Hello, World!",
		},
		{
			name:      "XChaCha20Poly1305",
			cipherFn:  NewXChaCha20Poly1305Cipher,
			keySize:   chacha20poly1305.KeySize,
			plaintext: "Hello, World!",
		},
		{
			name:      "AES256GCM",
			cipherFn:  NewAES256GCMCipher,
			keySize:   32,
			plaintext: "Hello, World!",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := make([]byte, tt.keySize)
			_, err := rand.Read(key)
			if err != nil {
				t.Fatalf("failed to generate key: %v", err)
			}

			cipher, err := tt.cipherFn(key)
			if err != nil {
				t.Fatalf("cipher creation error = %v", err)
			}

			// Encrypt
			ciphertext, err := cipher.Encrypt([]byte(tt.plaintext))
			if err != nil {
				t.Fatalf("Encrypt() error = %v", err)
			}

			// Ciphertext should be longer than plaintext (nonce + ciphertext + tag)
			if len(ciphertext) <= len(tt.plaintext) {
				t.Errorf("ciphertext length = %v, want > %v", len(ciphertext), len(tt.plaintext))
			}

			// Decrypt
			decrypted, err := cipher.Decrypt(ciphertext)
			if err != nil {
				t.Fatalf("Decrypt() error = %v", err)
			}

			// Verify decrypted matches original
			if string(decrypted) != tt.plaintext {
				t.Errorf("Decrypt() = %v, want %v", string(decrypted), tt.plaintext)
			}
		})
	}
}

func TestCipherEncryptUniqueness(t *testing.T) {
	key := make([]byte, chacha20poly1305.KeySize)
	_, err := rand.Read(key)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	cipher, err := NewChaCha20Poly1305Cipher(key)
	if err != nil {
		t.Fatalf("NewChaCha20Poly1305Cipher() error = %v", err)
	}

	plaintext := []byte("same plaintext")

	// Encrypt same plaintext twice
	ciphertext1, err := cipher.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	ciphertext2, err := cipher.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	// Ciphertexts should be different (random nonces)
	if bytes.Equal(ciphertext1, ciphertext2) {
		t.Error("Encrypt() produced identical ciphertexts for same plaintext (nonce reuse)")
	}

	// But both should decrypt to same plaintext
	decrypted1, _ := cipher.Decrypt(ciphertext1)
	decrypted2, _ := cipher.Decrypt(ciphertext2)

	if !bytes.Equal(decrypted1, plaintext) || !bytes.Equal(decrypted2, plaintext) {
		t.Error("Decrypt() failed to recover original plaintext")
	}
}

func TestCipherDecryptTamperedData(t *testing.T) {
	key := make([]byte, chacha20poly1305.KeySize)
	_, err := rand.Read(key)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	cipher, err := NewChaCha20Poly1305Cipher(key)
	if err != nil {
		t.Fatalf("NewChaCha20Poly1305Cipher() error = %v", err)
	}

	plaintext := []byte("authenticated data")
	ciphertext, err := cipher.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	// Tamper with ciphertext
	tampered := make([]byte, len(ciphertext))
	copy(tampered, ciphertext)
	tampered[len(tampered)-1] ^= 1 // Flip one bit

	// Decryption should fail authentication
	_, err = cipher.Decrypt(tampered)
	if err == nil {
		t.Error("Decrypt() expected error for tampered ciphertext")
	}
}

func TestCipherDecryptShortCiphertext(t *testing.T) {
	key := make([]byte, chacha20poly1305.KeySize)
	_, err := rand.Read(key)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	cipher, err := NewChaCha20Poly1305Cipher(key)
	if err != nil {
		t.Fatalf("NewChaCha20Poly1305Cipher() error = %v", err)
	}

	// Ciphertext too short (less than nonce size)
	shortCiphertext := make([]byte, cipher.NonceSize()-1)

	_, err = cipher.Decrypt(shortCiphertext)
	if err == nil {
		t.Error("Decrypt() expected error for ciphertext shorter than nonce size")
	}
}

func TestCipherDecryptWrongKey(t *testing.T) {
	key1 := make([]byte, chacha20poly1305.KeySize)
	key2 := make([]byte, chacha20poly1305.KeySize)
	_, err := rand.Read(key1)
	if err != nil {
		t.Fatalf("failed to generate key1: %v", err)
	}
	_, err = rand.Read(key2)
	if err != nil {
		t.Fatalf("failed to generate key2: %v", err)
	}

	cipher1, err := NewChaCha20Poly1305Cipher(key1)
	if err != nil {
		t.Fatalf("NewChaCha20Poly1305Cipher() error = %v", err)
	}

	cipher2, err := NewChaCha20Poly1305Cipher(key2)
	if err != nil {
		t.Fatalf("NewChaCha20Poly1305Cipher() error = %v", err)
	}

	plaintext := []byte("secret message")
	ciphertext, err := cipher1.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	// Try to decrypt with wrong key
	_, err = cipher2.Decrypt(ciphertext)
	if err == nil {
		t.Error("Decrypt() expected error when using wrong key")
	}
}

func TestCipherEmptyPlaintext(t *testing.T) {
	key := make([]byte, chacha20poly1305.KeySize)
	_, err := rand.Read(key)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	cipher, err := NewChaCha20Poly1305Cipher(key)
	if err != nil {
		t.Fatalf("NewChaCha20Poly1305Cipher() error = %v", err)
	}

	// Encrypt empty plaintext
	ciphertext, err := cipher.Encrypt([]byte{})
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	// Should still produce ciphertext (nonce + tag)
	if len(ciphertext) <= 0 {
		t.Error("Encrypt() produced empty ciphertext for empty plaintext")
	}

	// Decrypt
	decrypted, err := cipher.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}

	// Should decrypt to empty plaintext
	if len(decrypted) != 0 {
		t.Errorf("Decrypt() = %v, want empty", decrypted)
	}
}

func TestCipherLargePlaintext(t *testing.T) {
	key := make([]byte, chacha20poly1305.KeySize)
	_, err := rand.Read(key)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	cipher, err := NewChaCha20Poly1305Cipher(key)
	if err != nil {
		t.Fatalf("NewChaCha20Poly1305Cipher() error = %v", err)
	}

	// 1MB plaintext
	plaintext := make([]byte, 1024*1024)
	_, err = rand.Read(plaintext)
	if err != nil {
		t.Fatalf("failed to generate plaintext: %v", err)
	}

	// Encrypt
	ciphertext, err := cipher.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	// Decrypt
	decrypted, err := cipher.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}

	// Verify
	if !bytes.Equal(decrypted, plaintext) {
		t.Error("Decrypt() failed to recover large plaintext")
	}
}

func BenchmarkChaCha20Poly1305Encrypt(b *testing.B) {
	key := make([]byte, chacha20poly1305.KeySize)
	rand.Read(key)
	cipher, _ := NewChaCha20Poly1305Cipher(key)
	plaintext := make([]byte, 1024) // 1KB
	rand.Read(plaintext)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = cipher.Encrypt(plaintext)
	}
}

func BenchmarkChaCha20Poly1305Decrypt(b *testing.B) {
	key := make([]byte, chacha20poly1305.KeySize)
	rand.Read(key)
	cipher, _ := NewChaCha20Poly1305Cipher(key)
	plaintext := make([]byte, 1024) // 1KB
	rand.Read(plaintext)
	ciphertext, _ := cipher.Encrypt(plaintext)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = cipher.Decrypt(ciphertext)
	}
}

func BenchmarkAES256GCMEncrypt(b *testing.B) {
	key := make([]byte, 32)
	rand.Read(key)
	cipher, _ := NewAES256GCMCipher(key)
	plaintext := make([]byte, 1024) // 1KB
	rand.Read(plaintext)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = cipher.Encrypt(plaintext)
	}
}

func BenchmarkAES256GCMDecrypt(b *testing.B) {
	key := make([]byte, 32)
	rand.Read(key)
	cipher, _ := NewAES256GCMCipher(key)
	plaintext := make([]byte, 1024) // 1KB
	rand.Read(plaintext)
	ciphertext, _ := cipher.Encrypt(plaintext)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = cipher.Decrypt(ciphertext)
	}
}
