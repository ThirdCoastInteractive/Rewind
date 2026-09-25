// package video_api provides video-related API handlers.
package video_api

import (
	"errors"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/plugin"
)

// HandleJobs returns all download + ingest jobs for a video via SSE.
func HandleJobs(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		_, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return err
		}

		videoUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}

		ctx := c.Request().Context()
		q := dbc.Queries(ctx)

		video, err := common.RequireVideo(c, q, videoUUID, plugin.ActionVideoRead)
		if err != nil {
			return err
		}

		jobs, err := q.ListDownloadJobsByVideoID(ctx, &db.ListDownloadJobsByVideoIDParams{
			VideoID:  videoUUID,
			VideoSrc: video.Src,
		})
		if err != nil {
			slog.Error("failed to fetch jobs for video", "error", err, "video_id", videoUUID)
			return c.String(500, "failed to fetch jobs")
		}

		// Collect download job IDs to fetch associated ingest jobs
		var djIDs []pgtype.UUID
		for _, j := range jobs {
			djIDs = append(djIDs, j.ID)
		}

		ingestJobsByDJ := map[string][]*db.IngestJob{}
		if len(djIDs) > 0 {
			ingestJobs, err := q.ListIngestJobsByDownloadJobIDs(ctx, djIDs)
			if err != nil {
				slog.Warn("failed to fetch ingest jobs", "error", err)
			} else {
				for _, ij := range ingestJobs {
					key := ij.DownloadJobID.String()
					ingestJobsByDJ[key] = append(ingestJobsByDJ[key], ij)
				}
			}
		}

		mlJobs, err := q.ListMLJobsForVideo(ctx, videoUUID)
		if err != nil {
			return err
		}
		health, err := q.ListMLRuntimeHealth(ctx)
		if err != nil {
			return err
		}
		transcript, err := q.GetVideoTranscript(ctx, videoUUID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		hasTranscript := transcript != nil && strings.TrimSpace(transcript.Text) != ""
		windows, err := q.ListContextWindowsByVideo(ctx, videoUUID)
		if err != nil {
			return err
		}
		sse := datastar.NewSSE(c.Response().Writer, c.Request())
		if err := sse.PatchElementTempl(templates.VideoProcessing(videoUUID.String(), mlJobs, health, hasTranscript, len(windows))); err != nil {
			return err
		}
		return sse.PatchElementTempl(
			templates.VideoJobsList(jobs, ingestJobsByDJ),
			datastar.WithSelectorID("video-jobs-list"),
			datastar.WithModeInner(),
		)
	}
}
