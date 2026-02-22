package video_api

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

// HandleDownloadFormat creates a download job for a specific yt-dlp format ID.
// POST /api/videos/:id/download-format
// Body: {"format_ids": "303,251"} — comma-separated yt-dlp format IDs
func HandleDownloadFormat(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		userUUID, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return err
		}

		videoUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}

		type requestBody struct {
			FormatIDs string `json:"format_ids"`
		}
		var req requestBody
		if err := c.Bind(&req); err != nil {
			return c.String(400, "invalid request body")
		}

		formatIDs := strings.TrimSpace(req.FormatIDs)
		if formatIDs == "" {
			return c.String(400, "format_ids is required")
		}

		// Validate format IDs look reasonable (alphanumeric + commas + plus)
		for _, ch := range formatIDs {
			if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == ',' || ch == '+' || ch == '-' || ch == '_') {
				return c.String(400, "invalid format_ids characters")
			}
		}

		videoRow, err := dbc.Queries(c.Request().Context()).GetVideoByID(c.Request().Context(), videoUUID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return c.String(404, "video not found")
			}
			slog.Error("failed to fetch video for format download", "error", err, "video_id", videoUUID)
			return c.String(500, "failed to fetch video")
		}

		// Build the yt-dlp format selector: "formatID1+formatID2/best"
		// This tells yt-dlp to download the specific format(s) requested.
		formatSelector := fmt.Sprintf("%s/best", formatIDs)

		job, err := dbc.Queries(c.Request().Context()).EnqueueDownloadJob(c.Request().Context(), &db.EnqueueDownloadJobParams{
			URL:        videoRow.Src,
			ArchivedBy: userUUID,
			Refresh:    false,
			ExtraArgs:  []string{"-f", formatSelector},
		})
		if err != nil {
			slog.Error("failed to create format download job", "error", err)
			return c.String(500, "failed to create download job")
		}

		slog.Info("created format download job",
			"job_id", job.ID, "video_id", videoUUID,
			"url", videoRow.Src, "format_ids", formatIDs)

		return c.JSON(200, map[string]any{
			"job_id":     job.ID.String(),
			"status":     job.Status,
			"format_ids": formatIDs,
		})
	}
}
