// Package passwords handles Argon2id password hashing, verification, and
// database persistence with configurable strength parameters.
package passwords

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"

	"github.com/alexedwards/argon2id"
	"github.com/go-playground/validator/v10"
	"github.com/jackc/pgx/v5/pgtype"
)

// Password is an Argon2id hash string that implements database Scanner/Valuer interfaces.
type Password string

var (
	params = &argon2id.Params{
		Memory:      128 * 1024,
		Iterations:  4,
		Parallelism: uint8(4),
		SaltLength:  32,
		KeyLength:   64,
	}
)

// PasswordInput carries a plaintext password through validation (8-512 chars required).
type PasswordInput struct {
	Password string `validate:"required,min=8,max=512"`
}

// NewPassword validates the input (8-512 chars) and returns its Argon2id hash.
func NewPassword(input PasswordInput) (Password, error) {
	err := validator.New().Struct(input)
	if err != nil {
		return "", err
	}

	hash, err := argon2id.CreateHash(input.Password, params)
	if err != nil {
		return "", err
	}

	return Password(hash), nil
}

// ComparePasswordAndHash returns true if the plaintext matches this hash.
func (p *Password) ComparePasswordAndHash(input PasswordInput) (bool, error) {
	return argon2id.ComparePasswordAndHash(input.Password, string(*p))
}

// String returns the raw hash string.
func (p *Password) String() string {
	return string(*p)
}

// Scan implements database/sql.Scanner.
func (p *Password) Scan(src any) error {
	if src == nil {
		*p = ""
		return nil
	}

	switch v := src.(type) {
	case string:
		*p = Password(v)
		return nil
	case []byte:
		*p = Password(string(v))
		return nil
	default:
		return fmt.Errorf("passwords.Password.Scan: expected string or []byte, got %T", src)
	}
}

// Value implements driver.Valuer.
func (p Password) Value() (driver.Value, error) {
	return string(p), nil
}

// ScanText implements the pgtype.TextScanner interface for pgx v5.
func (p *Password) ScanText(v pgtype.Text) error {
	if !v.Valid {
		*p = ""
		return nil
	}
	*p = Password(v.String)
	return nil
}

// TextValue implements the pgtype.TextValuer interface for pgx v5.
func (p Password) TextValue() (pgtype.Text, error) {
	return pgtype.Text{String: string(p), Valid: true}, nil
}

// IsArgonEncoded reports whether the string has the $argon2id$ prefix of an Argon2id hash.
func IsArgonEncoded(input string) bool {
	return strings.HasPrefix(input, "$argon2id$")
}

var (
	// ErrPasswordTooShort is returned when a password has fewer than MinPasswordLength characters.
	ErrPasswordTooShort = errors.New("password is too short")
	// ErrPasswordTooLong is returned when a password exceeds MaxPasswordLength characters.
	ErrPasswordTooLong = errors.New("password is too long")
)

const (
	// MinPasswordLength is the minimum acceptable password length in characters.
	MinPasswordLength = 8
	// MaxPasswordLength is the maximum acceptable password length in characters.
	MaxPasswordLength = 512
)
