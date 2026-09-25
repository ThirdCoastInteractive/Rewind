package common

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/pkg/plugin"
)

// Private echo.Context key for ActorFrom cache (do not cache nil).
const actorContextKey = "common.plugin.actor"

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
	if plugin.Auth() != nil {
		actor := ActorFrom(c)
		if actor == nil {
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

// ActorFrom is the request actor. Caches on echo.Context after the first
// plugin.Auth().Current. Nil if unauthenticated or no plugin.
func ActorFrom(c echo.Context) *plugin.Actor {
	if v := c.Get(actorContextKey); v != nil {
		if a, ok := v.(*plugin.Actor); ok {
			return a
		}
	}
	a := plugin.Auth()
	if a == nil {
		return nil
	}
	actor, err := a.Current(c.Request())
	if err != nil || actor == nil {
		return nil
	}
	c.Set(actorContextKey, actor)
	return actor
}

// ActorTenantID is plugin.Actor.TenantID, or empty for OSS / staff.
func ActorTenantID(c echo.Context) string {
	actor := ActorFrom(c)
	if actor == nil {
		return ""
	}
	return actor.TenantID
}

// AllowVideo is Authz.Allow for a video id. Missing guards mean OSS (allow).
func AllowVideo(c echo.Context, action, videoID string) bool {
	g := plugin.Guards()
	if g == nil {
		return true
	}
	if plugin.Auth() == nil {
		return true
	}
	actor := ActorFrom(c)
	if actor == nil {
		return false
	}
	return g.Allow(c.Request().Context(), actor, action, videoID)
}
