package admin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/internal/db"
)

func HandleAdminRefreshAssets(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		slog.Info("Admin triggered bulk asset regeneration")

		q := dbc.Queries(c.Request().Context())

		// Get all videos that have a video_path (can regenerate)
		videos, err := q.ListVideosForAssetCatchup(c.Request().Context(), 10000)
		if err != nil {
			slog.Error("failed to list videos for bulk regeneration", "error", err)
			return c.Redirect(302, "/admin?err="+url.QueryEscape("Failed to start bulk regeneration"))
		}

		// Queue regeneration jobs for all videos
		queuedCount := 0
		for _, video := range videos {
			if video.VideoPath == nil || *video.VideoPath == "" {
				continue
			}

			var videoUUID pgtype.UUID
			if err := videoUUID.Scan(video.ID); err != nil {
				slog.Warn("invalid video ID", "video_id", video.ID, "error", err)
				continue
			}

			if _, err := q.EnqueueAssetRegenerationJob(c.Request().Context(), &db.EnqueueAssetRegenerationJobParams{VideoID: videoUUID}); err != nil {
				slog.Warn("failed to enqueue regeneration job", "video_id", video.ID, "error", err)
				continue
			}
			queuedCount++
		}

		slog.Info("bulk asset regeneration queued", "videos", queuedCount)
		return c.Redirect(302, "/admin?msg="+url.QueryEscape(fmt.Sprintf("Queued asset regeneration for %d videos", queuedCount)))
	}
}

// GetClipExportStorageLimitBytes retrieves the storage limit from instance settings.
func GetClipExportStorageLimitBytes(ctx context.Context, dbc *db.DatabaseConnection) (int64, error) {
	settings, err := dbc.Queries(ctx).GetInstanceSettings(ctx)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}
	if settings.ClipExportStorageLimitBytes < 0 {
		return 0, nil
	}
	return settings.ClipExportStorageLimitBytes, nil
}
