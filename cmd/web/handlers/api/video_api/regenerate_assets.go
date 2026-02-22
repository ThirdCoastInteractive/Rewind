package video_api

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

// validAssetScopes are the individual asset types that can be regenerated.
var validAssetScopes = map[string]bool{
	"thumbnail": true,
	"preview":   true,
	"seek":      true,
	"waveform":  true,
	"captions":  true,
	"hls":       true,
}

// HandleRegenerateAssets triggers regeneration of video assets.
// Query param ?scope=thumbnail|preview|seek|waveform limits to a single asset.
// Omitting scope regenerates all assets.
func HandleRegenerateAssets(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		_, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return err
		}

		videoUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}

		// Parse optional asset scope
		var assetScope *string
		if raw := strings.TrimSpace(c.QueryParam("scope")); raw != "" {
			if !validAssetScopes[raw] {
				return c.String(400, "invalid scope: must be thumbnail, preview, seek, or waveform")
			}
			assetScope = &raw
		}

		// Verify the video exists
		_, err = dbc.Queries(c.Request().Context()).GetVideoByID(c.Request().Context(), videoUUID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return c.String(404, "video not found")
			}
			slog.Error("failed to fetch video for asset regeneration", "error", err, "video_id", videoUUID)
			return c.String(500, "failed to fetch video")
		}

		// Create a special ingest job that will regenerate assets.
		// The ingest worker will discover the video file on disk even if video_path is NULL.
		job, err := dbc.Queries(c.Request().Context()).EnqueueAssetRegenerationJob(c.Request().Context(), &db.EnqueueAssetRegenerationJobParams{
			VideoID:    videoUUID,
			AssetScope: assetScope,
		})
		if err != nil {
			slog.Error("failed to create asset regeneration job", "error", err, "video_id", videoUUID, "scope", assetScope)
			return c.String(500, "failed to create regeneration job")
		}

		scopeLabel := "all"
		if assetScope != nil {
			scopeLabel = *assetScope
		}
		slog.Info("created asset regeneration job", "ingest_job_id", job.IngestJobID, "download_job_id", job.DownloadJobID, "video_id", videoUUID, "scope", scopeLabel)

		return c.JSON(200, map[string]any{
			"ingest_job_id":   job.IngestJobID.String(),
			"download_job_id": job.DownloadJobID.String(),
			"video_id":        job.VideoID.String(),
			"scope":           scopeLabel,
		})
	}
}
