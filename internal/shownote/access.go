package shownote

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"thirdcoast.systems/rewind/internal/db"
)

// Access describes a user's permissions in a show-note workspace.
type Access struct {
	Allowed  bool
	ReadOnly bool
	Role     string
}

// UserAccess evaluates the show-note owner/host roster for one user.
func UserAccess(ctx context.Context, dbc *db.DatabaseConnection, noteID, userID pgtype.UUID) Access {
	q := dbc.Queries(ctx)
	note, err := q.GetShowNote(ctx, noteID)
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
