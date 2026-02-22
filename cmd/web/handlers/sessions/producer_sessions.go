package sessions

import (
	"log/slog"
	"time"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
)

func HandleProducerSessionManagePage(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		accessLevel := c.Get("accessLevel").(string)
		if accessLevel == "unauthenticated" {
			return c.Redirect(302, "/login")
		}

		userUUID, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(302, "/login")
		}

		// Get all sessions for this producer
		sessions, err := dbc.Queries(c.Request().Context()).ListSessionsByProducer(c.Request().Context(), userUUID)
		if err != nil {
			slog.Error("failed to list sessions", "error", err)
			return c.String(500, "Failed to load sessions")
		}

		sessionList := make([]templates.SessionInfo, len(sessions))
		for i, s := range sessions {
			sessionList[i] = templates.SessionInfo{
				ID:           s.ID.String(),
				SessionCode:  s.SessionCode,
				CreatedAt:    s.CreatedAt.Time,
				ExpiresAt:    s.ExpiresAt.Time,
				LastActivity: s.LastActivity.Time,
				IsExpired:    s.ExpiresAt.Time.Before(time.Now()),
			}
			if s.CurrentVideoID.Valid {
				sessionList[i].CurrentVideoID = s.CurrentVideoID.String()
			}
		}

		username := ""
		if u, ok := c.Request().Context().Value("username").(string); ok {
			username = u
		}
		return templates.Sessions(sessionList, username).Render(c.Request().Context(), c.Response())
	}
}

// HandleProducerCreateSession creates a new producer session.
