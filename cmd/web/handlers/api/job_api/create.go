// package job_api provides download job API handlers.
package job_api

import (
	"errors"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
)

func HandleCreateDownload(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		archivedByUUID, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return err
		}

		var req struct {
			URL string `json:"url"`
		}
		if err := c.Bind(&req); err != nil {
			return c.String(400, "invalid json")
		}
		req.URL = strings.TrimSpace(req.URL)
		if req.URL == "" {
			return c.String(400, "url is required")
		}

		// Make first archive vs refresh transparent
		refresh := false
		if existing, err := dbc.Queries(c.Request().Context()).SelectVideoBySrc(c.Request().Context(), req.URL); err == nil && existing != nil {
			refresh = true
		} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			slog.Error("failed to check existing video", "error", err)
			return c.String(500, "failed to enqueue")
		}

		job, err := dbc.Queries(c.Request().Context()).EnqueueDownloadJob(c.Request().Context(), &db.EnqueueDownloadJobParams{
			URL:        req.URL,
			ArchivedBy: archivedByUUID,
			Refresh:    refresh,
			ExtraArgs:  []string{},
		})
		if err != nil {
			return c.String(500, "failed to enqueue")
		}

		resp := map[string]any{
			"id":      job.ID.String(),
			"status":  job.Status,
			"refresh": refresh,
		}
		return c.JSON(200, resp)
	}
}

// HandleRetry retries a failed download job.
