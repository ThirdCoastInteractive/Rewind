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

// PasswordInput is a struct for validating password inputs
type PasswordInput struct {
	Password string `validate:"required,min=8,max=512"`
}

// NewPassword creates a new password hash, while enforcing some basic rules
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

// ComparePasswordAndHash compares the input to the password hash
func (p *Password) ComparePasswordAndHash(input PasswordInput) (bool, error) {
	return argon2id.ComparePasswordAndHash(input.Password, string(*p))
}

// String returns the string representation of the password
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

// IsArgonEncoded returns true if the input is an argon2id hash
func IsArgonEncoded(input string) bool {
	return strings.HasPrefix(input, "$argon2id$")
}

var (
	ErrPasswordTooShort = errors.New("password is too short")
	ErrPasswordTooLong  = errors.New("password is too long")
)

const (
	// MinPasswordLength is the minimum password length
	MinPasswordLength = 8
	// MaxPasswordLength is the maximum password length
	MaxPasswordLength = 512
)
