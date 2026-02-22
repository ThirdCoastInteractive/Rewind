package crypto

import "thirdcoast.systems/rewind/pkg/encryption"

// NewNullEncryptedString creates a NULL EncryptedString.
func NewNullEncryptedString() EncryptedString {
	return encryption.EncryptNull[string]()
}

// EncryptString encrypts a string value using the provided manager.
func EncryptString(m *encryption.Manager, value string) (EncryptedString, error) {
	return encryption.Encrypt(m, value)
}

// EncryptStringOrNull encrypts a string pointer, returning NULL if the pointer is nil.
func EncryptStringOrNull(m *encryption.Manager, value *string) (EncryptedString, error) {
	if value == nil {
		return encryption.EncryptNull[string](), nil
	}
	return encryption.Encrypt(m, *value)
}

// DecryptString decrypts an EncryptedString and returns the plain string.
func DecryptString(m *encryption.Manager, encrypted *EncryptedString) (string, error) {
	if err := encryption.Decrypt(m, encrypted); err != nil {
		return "", err
	}
	value, valid := encrypted.Get()
	if !valid {
		return "", nil
	}
	return value, nil
}

// DecryptStringOrEmpty decrypts an EncryptedString and returns empty string if NULL.
func DecryptStringOrEmpty(m *encryption.Manager, encrypted EncryptedString) string {
	if err := encryption.Decrypt(m, &encrypted); err != nil {
		return ""
	}
	value, valid := encrypted.Get()
	if !valid {
		return ""
	}
	return value
}

// DecryptStringPtr decrypts an EncryptedString and returns a string pointer (nil if NULL).
func DecryptStringPtr(m *encryption.Manager, encrypted *EncryptedString) (*string, error) {
	if err := encryption.Decrypt(m, encrypted); err != nil {
		return nil, err
	}
	value, valid := encrypted.Get()
	if !valid {
		return nil, nil
	}
	return &value, nil
}
