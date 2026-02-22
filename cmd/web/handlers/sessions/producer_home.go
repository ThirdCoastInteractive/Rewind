package sessions

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
)

func HandleProducerHomePage(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		accessLevel := c.Get("accessLevel").(string)
		if accessLevel == "unauthenticated" {
			return c.Redirect(302, "/login")
		}

		userUUID, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(302, "/login")
		}

		// Check if there's a video parameter - if so, create a new session and load it
		videoIDStr := c.QueryParam("video")
		if videoIDStr != "" {

			// Generate 6-digit session code
			code := fmt.Sprintf("%06d", time.Now().UnixNano()%1000000)
			expiresAt := time.Now().Add(4 * time.Hour)

			var videoUUID pgtype.UUID
			if err := videoUUID.Scan(videoIDStr); err != nil {
				return c.Redirect(302, "/producer")
			}

			session, err := dbc.Queries(c.Request().Context()).CreatePlayerSession(c.Request().Context(), &db.CreatePlayerSessionParams{
				ProducerID:  userUUID,
				SessionCode: code,
				ExpiresAt:   pgtype.Timestamptz{Time: expiresAt, Valid: true},
			})
			if err != nil {
				slog.Error("failed to create player session with video", "error", err)
				return c.String(500, "Failed to create session")
			}

			// Set the initial video for the session
			if err := dbc.Queries(c.Request().Context()).UpdatePlayerSessionVideo(c.Request().Context(), &db.UpdatePlayerSessionVideoParams{
				VideoID: videoUUID,
				ID:      session.ID,
			}); err != nil {
				slog.Error("failed to set initial video for session", "error", err)
			}

			// Redirect to producer with session code
			return c.Redirect(302, "/producer/"+code)
		}

		username := ""
		if u, ok := c.Request().Context().Value("username").(string); ok {
			username = u
		}
		return templates.Producer("", nil, "", username).Render(c.Request().Context(), c.Response())
	}
}

// HandleProducerSessionManagePage lists all sessions for the producer.
