package encryption

import (
	"encoding/json"
	"fmt"
)

// Manager handles encryption and decryption operations.
type Manager struct {
	cipher *Cipher
}

// NewManager creates a new encryption manager with the specified cipher.
func NewManager(cipher *Cipher) *Manager {
	return &Manager{
		cipher: cipher,
	}
}

// NewManagerWithChaCha20Poly1305 creates a manager using ChaCha20-Poly1305 (recommended default).
func NewManagerWithChaCha20Poly1305(key []byte) (*Manager, error) {
	cipher, err := NewChaCha20Poly1305Cipher(key)
	if err != nil {
		return nil, err
	}
	return NewManager(cipher), nil
}

// NewManagerWithXChaCha20Poly1305 creates a manager using XChaCha20-Poly1305.
func NewManagerWithXChaCha20Poly1305(key []byte) (*Manager, error) {
	cipher, err := NewXChaCha20Poly1305Cipher(key)
	if err != nil {
		return nil, err
	}
	return NewManager(cipher), nil
}

// NewManagerWithAES256GCM creates a manager using AES-256-GCM.
func NewManagerWithAES256GCM(key []byte) (*Manager, error) {
	cipher, err := NewAES256GCMCipher(key)
	if err != nil {
		return nil, err
	}
	return NewManager(cipher), nil
}

// CipherType returns the cipher type used by this manager.
func (m *Manager) CipherType() CipherType {
	return m.cipher.Type()
}

// Encrypt encrypts a value of any serializable type and returns an EncryptedField.
func Encrypt[T any](m *Manager, value T) (EncryptedField[T], error) {
	// Serialize value to JSON
	plaintext, err := json.Marshal(value)
	if err != nil {
		return EncryptedField[T]{}, fmt.Errorf("marshal value: %w", err)
	}

	// Encrypt
	ciphertext, err := m.cipher.Encrypt(plaintext)
	if err != nil {
		return EncryptedField[T]{}, fmt.Errorf("encrypt: %w", err)
	}

	// Create encrypted field
	field := EncryptedField[T]{
		value:     value,
		encrypted: ciphertext,
		valid:     true,
		dirty:     false,
	}

	return field, nil
}

// EncryptNull creates a NULL EncryptedField.
func EncryptNull[T any]() EncryptedField[T] {
	return NewNullEncryptedField[T]()
}

// Decrypt decrypts an EncryptedField and populates its decrypted value.
func Decrypt[T any](m *Manager, field *EncryptedField[T]) error {
	if !field.valid {
		return fmt.Errorf("cannot decrypt NULL field")
	}

	if field.dirty {
		// Already decrypted
		return nil
	}

	// Decrypt
	plaintext, err := m.cipher.Decrypt(field.encrypted)
	if err != nil {
		return fmt.Errorf("decrypt: %w", err)
	}

	// Deserialize
	var value T
	if err := json.Unmarshal(plaintext, &value); err != nil {
		return fmt.Errorf("unmarshal value: %w", err)
	}

	field.value = value
	return nil
}

// DecryptValue is a convenience function that decrypts and returns the value in one call.
func DecryptValue[T any](m *Manager, field *EncryptedField[T]) (T, error) {
	var zero T
	if !field.valid {
		return zero, fmt.Errorf("cannot decrypt NULL field")
	}

	if err := Decrypt(m, field); err != nil {
		return zero, err
	}

	return field.value, nil
}
