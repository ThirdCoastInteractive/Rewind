package db

import (
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// IsUndefinedColumnErr reports whether err is a Postgres "undefined column" or "undefined table" error.
func IsUndefinedColumnErr(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		// 42703 = undefined_column
		// 42P01 = undefined_table
		return pgErr.Code == "42703" || pgErr.Code == "42P01"
	}
	return false
}

// IsUniqueViolationErr reports whether err is a Postgres unique-constraint violation.
func IsUniqueViolationErr(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// IsForeignKeyViolationErr reports whether err is a Postgres foreign-key violation.
func IsForeignKeyViolationErr(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}

// NilTimePtr converts a nullable pgtype.Timestamptz to a *time.Time, returning nil when invalid.
func NilTimePtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	return &t.Time
}
