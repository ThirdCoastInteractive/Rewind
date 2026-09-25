// Package shownote_api provides the HTTP/SSE handlers for the Show Notes
// workspace: host management, collaboration document, and live session APIs.
package shownote_api

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"

	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/shownote"
)

// canEditShowNote reports whether the user may edit the show note: the owner, or
// a roster member with the 'owner'/'host' role.
func canEditShowNote(ctx context.Context, dbc *db.DatabaseConnection, noteID, userID pgtype.UUID) bool {
	q := dbc.Queries(ctx)
	note, err := shownote.RequireTenant(ctx, dbc, noteID)
	if err != nil {
		return false
	}
	if note.OwnerID == userID {
		return true
	}
	role, err := q.GetHostRole(ctx, &db.GetHostRoleParams{ShowNoteID: noteID, UserID: userID})
	if err != nil {
		return false
	}
	return role == "owner" || role == "host"
}

// requireEditor resolves the session user and the :id show-note param, and
// verifies the user may edit that note. On failure it returns an echo error.
func requireEditor(c echo.Context, sm *auth.SessionManager, dbc *db.DatabaseConnection) (noteID pgtype.UUID, userID pgtype.UUID, err error) {
	userID, _, err = common.RequireSessionUser(c, sm)
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, err
	}
	noteID, err = common.RequireUUIDParam(c, "id")
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, err
	}
	if !canEditShowNote(c.Request().Context(), dbc, noteID, userID) {
		return pgtype.UUID{}, pgtype.UUID{}, echo.NewHTTPError(403, "forbidden")
	}
	return noteID, userID, nil
}

// requireOwner is like requireEditor but only permits the show note's owner.
func requireOwner(c echo.Context, sm *auth.SessionManager, dbc *db.DatabaseConnection) (noteID pgtype.UUID, userID pgtype.UUID, err error) {
	userID, _, err = common.RequireSessionUser(c, sm)
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, err
	}
	noteID, err = common.RequireUUIDParam(c, "id")
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, err
	}
	note, err := shownote.RequireTenant(c.Request().Context(), dbc, noteID)
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, echo.NewHTTPError(404, "not found")
	}
	if note.OwnerID != userID {
		return pgtype.UUID{}, pgtype.UUID{}, echo.NewHTTPError(403, "forbidden")
	}
	return noteID, userID, nil
}
