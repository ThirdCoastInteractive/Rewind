package shownote_api

import (
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/labstack/echo/v4"

	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
)

// HandleAddHost adds (or updates the role of) a host by username. Owner only.
func HandleAddHost(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		noteUUID, _, err := requireOwner(c, sm, dbc)
		if err != nil {
			return err
		}
		var req struct {
			Username string `json:"username"`
			Role     string `json:"role"`
		}
		if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
			return echo.NewHTTPError(400, "invalid body")
		}
		role := strings.TrimSpace(req.Role)
		if role != "host" && role != "viewer" {
			role = "host"
		}

		ctx := c.Request().Context()
		q := dbc.Queries(ctx)

		// Resolve the username; if unknown, fall through and just re-render the
		// (unchanged) roster so the input clears without an error.
		if user, err := q.SelectUserByUserName(ctx, strings.TrimSpace(req.Username)); err == nil {
			if _, err := q.AddHost(ctx, &db.AddHostParams{ShowNoteID: noteUUID, UserID: user.ID, Role: role}); err != nil {
				slog.Error("show note: add host failed", "error", err)
				return echo.NewHTTPError(500, "failed to add host")
			}
		}

		hosts, err := q.ListHostsForShowNote(ctx, noteUUID)
		if err != nil {
			return echo.NewHTTPError(500, "failed to load hosts")
		}
		return c.JSON(200, hosts)
	}
}

// HandleRemoveHost removes a host by user id. Owner only.
func HandleRemoveHost(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		noteUUID, _, err := requireOwner(c, sm, dbc)
		if err != nil {
			return err
		}
		userUUID, err := common.RequireUUIDParam(c, "userId")
		if err != nil {
			return err
		}
		ctx := c.Request().Context()
		q := dbc.Queries(ctx)
		if err := q.RemoveHost(ctx, &db.RemoveHostParams{ShowNoteID: noteUUID, UserID: userUUID}); err != nil {
			slog.Error("show note: remove host failed", "error", err)
			return echo.NewHTTPError(500, "failed to remove host")
		}
		hosts, err := q.ListHostsForShowNote(ctx, noteUUID)
		if err != nil {
			return echo.NewHTTPError(500, "failed to load hosts")
		}
		return c.JSON(200, hosts)
	}
}
