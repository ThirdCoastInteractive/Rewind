package encryption

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// EncryptedField is a generic type that wraps encrypted data in the database.
// It handles automatic encryption/decryption when scanning from or writing to the database.
// T can be any serializable type (string, []byte, struct, etc.).
type EncryptedField[T any] struct {
	value     T
	encrypted []byte
	valid     bool // false if NULL
	dirty     bool // true if value changed but not yet encrypted
}

// NewEncryptedField creates a new EncryptedField with the given value.
func NewEncryptedField[T any](value T) EncryptedField[T] {
	return EncryptedField[T]{
		value: value,
		valid: true,
		dirty: true,
	}
}

// NewNullEncryptedField creates a NULL EncryptedField.
func NewNullEncryptedField[T any]() EncryptedField[T] {
	return EncryptedField[T]{
		valid: false,
	}
}

// Get returns the decrypted value and whether it's valid (not NULL).
func (e EncryptedField[T]) Get() (T, bool) {
	return e.value, e.valid
}

// Set updates the decrypted value and marks as dirty.
func (e *EncryptedField[T]) Set(value T) {
	e.value = value
	e.valid = true
	e.dirty = true
	e.encrypted = nil
}

// SetNull marks the field as NULL.
func (e *EncryptedField[T]) SetNull() {
	var zero T
	e.value = zero
	e.valid = false
	e.dirty = false
	e.encrypted = nil
}

// IsValid returns whether the field contains a non-NULL value.
func (e EncryptedField[T]) IsValid() bool {
	return e.valid
}

// MustValue returns the decrypted value, panicking if NULL.
func (e EncryptedField[T]) MustValue() T {
	if !e.valid {
		panic("EncryptedField is NULL")
	}
	return e.value
}

// Scan implements sql.Scanner for reading from the database.
// The encrypted data is stored but not decrypted until explicitly requested.
func (e *EncryptedField[T]) Scan(src any) error {
	if src == nil {
		e.SetNull()
		return nil
	}

	switch v := src.(type) {
	case []byte:
		e.encrypted = make([]byte, len(v))
		copy(e.encrypted, v)
		e.valid = true
		e.dirty = false
		return nil
	case string:
		e.encrypted = []byte(v)
		e.valid = true
		e.dirty = false
		return nil
	default:
		return fmt.Errorf("cannot scan %T into EncryptedField", src)
	}
}

// Value implements driver.Valuer for writing to the database.
// Returns the encrypted bytes, or nil if NULL.
func (e EncryptedField[T]) Value() (driver.Value, error) {
	if !e.valid {
		return nil, nil
	}

	if e.dirty {
		return nil, fmt.Errorf("EncryptedField contains unencrypted data - must encrypt before writing to database")
	}

	return e.encrypted, nil
}

// MarshalJSON implements json.Marshaler.
// Returns the decrypted value as JSON, or null if NULL.
func (e EncryptedField[T]) MarshalJSON() ([]byte, error) {
	if !e.valid {
		return []byte("null"), nil
	}
	return json.Marshal(e.value)
}

// UnmarshalJSON implements json.Unmarshaler.
// Sets the decrypted value and marks as dirty.
func (e *EncryptedField[T]) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		e.SetNull()
		return nil
	}

	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}

	e.Set(value)
	return nil
}

// String returns a string representation (for debugging).
func (e EncryptedField[T]) String() string {
	if !e.valid {
		return "NULL"
	}
	if e.dirty {
		return "[unencrypted]"
	}
	return fmt.Sprintf("[encrypted: %d bytes]", len(e.encrypted))
}
