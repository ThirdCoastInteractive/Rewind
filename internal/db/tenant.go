package db

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
)

// OSSTenant is tenant_id for self-hosted rows (UUID nil).
func OSSTenant() pgtype.UUID {
	return pgtype.UUID{Valid: true}
}

// ParseTenant scans a tenant UUID. Empty string is OSSTenant.
func ParseTenant(s string) pgtype.UUID {
	if strings.TrimSpace(s) == "" {
		return OSSTenant()
	}
	var u pgtype.UUID
	if err := u.Scan(strings.TrimSpace(s)); err != nil {
		return pgtype.UUID{}
	}
	return u
}

const setVideoTenantID = `UPDATE videos SET tenant_id = $1 WHERE id = $2`

// SetVideoTenantID stamps tenant_id on the persisted video row.
func (q *Queries) SetVideoTenantID(ctx context.Context, id, tenant pgtype.UUID) error {
	_, err := q.db.Exec(ctx, setVideoTenantID, tenant, id)
	return err
}
