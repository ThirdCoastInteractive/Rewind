package video_api

import (
	"log/slog"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/plugin"
)

// validAssetScopes are the individual asset types that can be regenerated.
var validAssetScopes = map[string]bool{
	"thumbnail": true,
	"preview":   true,
	"seek":      true,
	"waveform":  true,
	"captions":  true,
	"streams":   true,
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

		if _, err := common.RequireVideo(c, dbc.Queries(c.Request().Context()), videoUUID, plugin.ActionVideoWrite); err != nil {
			return err
		}

		// Create a special ingest job that will regenerate assets.
		// The ingest worker will discover the video file on disk even if video_path is NULL.
		active, err := dbc.Queries(c.Request().Context()).GetActiveAssetJobsForVideo(c.Request().Context(), videoUUID)
		if err != nil {
			return err
		}
		for _, existing := range active {
			existingScope := ""
			if existing.AssetScope != nil {
				existingScope = strings.TrimSpace(*existing.AssetScope)
			}
			if assetScope == nil || existingScope == "" || existingScope == "all" || *existing.AssetScope == *assetScope {
				if c.QueryParam("render") == "1" {
					return datastar.NewSSE(c.Response(), c.Request()).PatchElementTempl(templates.VideoProcessingNotice("This asset already has queued or running work. See Download and asset jobs for progress."))
				}
				return c.JSON(409, map[string]string{"error": "asset regeneration already queued or running"})
			}
		}
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
		if c.QueryParam("render") == "1" {
			if err := datastar.NewSSE(c.Response(), c.Request()).PatchElementTempl(templates.VideoProcessingNotice("Asset regeneration queued: " + scopeLabel + ". Track it under Download and asset jobs.")); err != nil {
				return err
			}
			return HandleJobs(sm, dbc)(c)
		}

		return c.JSON(200, map[string]any{
			"ingest_job_id":   job.IngestJobID.String(),
			"download_job_id": job.DownloadJobID.String(),
			"video_id":        job.VideoID.String(),
			"scope":           scopeLabel,
		})
	}
}
