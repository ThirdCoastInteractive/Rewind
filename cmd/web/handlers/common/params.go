package common

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/pkg/plugin"
)

// RequireUUIDParam extracts a UUID route parameter or returns a 400 error.
func RequireUUIDParam(c echo.Context, param string) (pgtype.UUID, error) {
	u, err := ParseUUID(c.Param(param))
	if err != nil {
		return u, echo.NewHTTPError(http.StatusBadRequest, "invalid "+param)
	}
	return u, nil
}

// ParseUUID scans a UUID string.
func ParseUUID(raw string) (pgtype.UUID, error) {
	var u pgtype.UUID
	if err := u.Scan(raw); err != nil {
		return u, err
	}
	return u, nil
}

// RequireSessionUser extracts the user UUID and username from the session.
// Returns 401 if not authenticated, 500 if the session user ID is corrupt.
func RequireSessionUser(c echo.Context, sm *auth.SessionManager) (pgtype.UUID, string, error) {
	userID, username, err := sm.GetSession(c.Request())
	if err != nil {
		return pgtype.UUID{}, "", echo.NewHTTPError(http.StatusUnauthorized)
	}
	if a := plugin.Auth(); a != nil {
		actor, aerr := a.Current(c.Request())
		if aerr != nil {
			return pgtype.UUID{}, "", echo.NewHTTPError(http.StatusUnauthorized)
		}
		if g := plugin.Guards(); g != nil && !g.Allow(c.Request().Context(), actor, plugin.ActionVideoRead, "") {
			return pgtype.UUID{}, "", echo.NewHTTPError(http.StatusForbidden)
		}
	}
	var u pgtype.UUID
	if err := u.Scan(userID); err != nil {
		return pgtype.UUID{}, "", echo.NewHTTPError(http.StatusInternalServerError, "invalid session")
	}
	return u, username, nil
}
