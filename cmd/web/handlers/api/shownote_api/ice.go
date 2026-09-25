package shownote_api

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/turn"
)

type iceConfigResponse struct {
	Servers []turn.Server `json:"iceServers"`
}

// HandleICEConfig returns short-lived ICE credentials to an authorized host or
// a public viewer while its show note is live. The Cloudflare API token is
// never included in this response.
func HandleICEConfig(sm *auth.SessionManager, dbc *db.DatabaseConnection, provider *turn.Provider) echo.HandlerFunc {
	return func(c echo.Context) error {
		noteID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		role := c.QueryParam("role")
		if role == "" {
			role = "host"
		}
		ctx := c.Request().Context()
		if role == "viewer" {
			note, lookupErr := dbc.Queries(ctx).GetShowNote(ctx, noteID)
			if lookupErr != nil || !note.IsLive {
				return c.String(http.StatusForbidden, "show is not live")
			}
		} else if role == "host" {
			userID, _, sessionErr := common.RequireSessionUser(c, sm)
			if sessionErr != nil {
				return c.String(http.StatusUnauthorized, "unauthorized")
			}
			if !canEditShowNote(ctx, dbc, noteID, userID) {
				return c.String(http.StatusForbidden, "forbidden")
			}
		} else {
			return c.String(http.StatusBadRequest, "invalid role")
		}
		if provider == nil {
			return c.String(http.StatusServiceUnavailable, "ICE service unavailable")
		}

		servers, err := provider.Servers(ctx)
		if err != nil {
			return c.String(http.StatusServiceUnavailable, "ICE service unavailable")
		}
		c.Response().Header().Set("Cache-Control", "private, no-store")
		return c.JSON(http.StatusOK, iceConfigResponse{Servers: servers})
	}
}
