package db

import (
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

func IsUndefinedColumnErr(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		// 42703 = undefined_column
		// 42P01 = undefined_table
		return pgErr.Code == "42703" || pgErr.Code == "42P01"
	}
	return false
}

func NilTimePtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	return &t.Time
}
