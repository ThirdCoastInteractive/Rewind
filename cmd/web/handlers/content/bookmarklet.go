package content

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
)

func HandleBookmarklet(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		archivedByUUID, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(302, "/login")
		}

		url := strings.TrimSpace(c.QueryParam("url"))
		if url == "" {
			return c.String(400, "url parameter is required")
		}

		refresh := false
		if existing, err := dbc.Queries(c.Request().Context()).SelectVideoBySrc(c.Request().Context(), url); err == nil && existing != nil {
			refresh = true
		} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			slog.Error("failed to check existing video for bookmarklet", "error", err)
			return c.String(500, "Failed to create download job")
		}

		job, err := dbc.Queries(c.Request().Context()).EnqueueDownloadJob(c.Request().Context(), &db.EnqueueDownloadJobParams{
			URL:        url,
			ArchivedBy: archivedByUUID,
			Refresh:    refresh,
			ExtraArgs:  []string{},
		})
		if err != nil {
			slog.Error("failed to enqueue job from bookmarklet", "error", err, "url", url)
			return c.String(500, "Failed to create download job")
		}

		jobID := job.ID.String()
		slog.Info("job created from bookmarklet", "job_id", jobID, "url", url)
		return c.Redirect(302, fmt.Sprintf("/jobs/%s", jobID))
	}
}
