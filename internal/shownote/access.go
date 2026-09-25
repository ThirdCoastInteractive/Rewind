package shownote

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgtype"

	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/plugin"
)

var ErrTenantRequired = errors.New("show-note tenant is required")

func TenantForContext(ctx context.Context) (pgtype.UUID, error) {
	if plugin.LiveIngest() == nil {
		return db.OSSTenant(), nil
	}
	raw, scoped := plugin.TenantScope(ctx)
	if !scoped || raw == "" {
		return pgtype.UUID{}, ErrTenantRequired
	}
	tenant := db.ParseTenant(raw)
	if !tenant.Valid || tenant == (pgtype.UUID{}) || isZeroTenant(tenant) {
		return pgtype.UUID{}, ErrTenantRequired
	}
	return tenant, nil
}

func isZeroTenant(tenant pgtype.UUID) bool {
	if !tenant.Valid {
		return true
	}
	for _, b := range tenant.Bytes {
		if b != 0 {
			return false
		}
	}
	return true
}

// RequireTenant verifies that a note belongs to the authenticated Live
// workspace. OSS callers retain the legacy zero-UUID behavior.
func RequireTenant(ctx context.Context, dbc *db.DatabaseConnection, noteID pgtype.UUID) (*db.ShowNote, error) {
	note, err := dbc.Queries(ctx).GetShowNote(ctx, noteID)
	if err != nil {
		return nil, err
	}
	if plugin.LiveIngest() == nil {
		return note, nil
	}
	tenant, err := TenantForContext(ctx)
	if err != nil || !note.TenantID.Valid || note.TenantID != tenant {
		return nil, ErrTenantRequired
	}
	return note, nil
}

// Access describes a user's permissions in a show-note workspace.
type Access struct {
	Allowed  bool
	ReadOnly bool
	Role     string
}

// UserAccess evaluates the show-note owner/host roster for one user.
func UserAccess(ctx context.Context, dbc *db.DatabaseConnection, noteID, userID pgtype.UUID) Access {
	q := dbc.Queries(ctx)
	note, err := RequireTenant(ctx, dbc, noteID)
	if err != nil {
		return Access{}
	}
	if note.OwnerID == userID {
		return Access{Allowed: true, Role: "owner"}
	}
	role, err := q.GetHostRole(ctx, &db.GetHostRoleParams{ShowNoteID: noteID, UserID: userID})
	if err != nil {
		return Access{}
	}
	return Access{Allowed: true, ReadOnly: role == "viewer", Role: role}
}
