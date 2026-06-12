package clip_api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates/components"
	"thirdcoast.systems/rewind/internal/db"
)

// HandleMulticamExport serves POST /clips/:clipId/multicam-export.
// It converts the clip's shot list + crops into stitch segments and creates a stitch job.
func HandleMulticamExport(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		userUUID, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.String(401, "unauthorized")
		}

		sse := datastar.NewSSE(c.Response().Writer, c.Request())
		common.SetSSEHeaders(c)

		sseError := func(msg string) error {
			_ = sse.PatchElementTempl(components.ExportStatus("multicam-export-status",msg, "error", ""))
			return nil
		}

		clipUUID, err := common.RequireUUIDParam(c, "clipId")
		if err != nil {
			return sseError("Invalid clip ID")
		}

		var req struct {
			Format     string `json:"format"`
			Quality    string `json:"quality"`
			Resolution int    `json:"resolution"` // target long-edge: 1080, 1440, 1920, etc.
		}
		if c.Request().ContentLength > 0 {
			_ = json.NewDecoder(c.Request().Body).Decode(&req)
		}

		format := strings.TrimSpace(req.Format)
		if format == "" {
			format = "mp4"
		}
		if format != "mp4" && format != "webm" {
			return sseError("Invalid format (mp4 or webm)")
		}
		quality := strings.TrimSpace(req.Quality)
		if quality == "" {
			quality = "high"
		}
		if quality != "high" && quality != "max" {
			return sseError("Invalid quality (high or max)")
		}

		ctx := c.Request().Context()
		q := dbc.Queries(ctx)

		clip, err := q.GetClip(ctx, clipUUID)
		if err != nil {
			return sseError("Clip not found")
		}

		if len(clip.ShotList) == 0 {
			return sseError("No shots defined — add crops and mark cut points first")
		}
		if len(clip.Crops) == 0 {
			return sseError("Clip has no crops defined")
		}

		if err := clip.ShotList.Validate(clip.StartTs, clip.EndTs, clip.Crops); err != nil {
			return sseError(fmt.Sprintf("Invalid shot list: %s", err))
		}

		// Build a single "multicam" segment envelope for the encoder
		mcSegment := map[string]any{
			"type":             "multicam",
			"video_id":         clip.VideoID.String(),
			"shots":            clip.ShotList,
			"crops":            clip.Crops,
			"clip_start":       clip.StartTs,
			"target_long_edge": req.Resolution,
		}

		// Wrap in array (stitch_jobs.segments is always a JSON array)
		segmentsJSON, err := json.Marshal([]any{mcSegment})
		if err != nil {
			return sseError("Failed to encode segments")
		}

		title := fmt.Sprintf("Multicam: %s", clip.Title)
		if clip.Title == "" {
			title = "Multicam export"
		}

		jobID, err := q.CreateStitchJob(ctx, &db.CreateStitchJobParams{
			CreatedBy:     userUUID,
			Title:         title,
			Format:        format,
			Quality:       quality,
			Segments:      segmentsJSON,
			GlobalFilters: []byte("[]"),
			ProjectID:     pgtype.UUID{},
		})
		if err != nil {
			slog.Error("failed to create multicam stitch job", "error", err)
			return sseError("Failed to queue export")
		}

		_, _ = dbc.Exec(ctx, "SELECT pg_notify('stitch_jobs', $1)", jobID.String())

		if err := sse.PatchElementTempl(components.ExportStatus("multicam-export-status","Queued...", "queued", "")); err != nil {
			return err
		}

		return streamMulticamStatus(c, sse, dbc, jobID)
	}
}

func streamMulticamStatus(c echo.Context, sse *datastar.ServerSentEventGenerator, dbc *db.DatabaseConnection, jobID pgtype.UUID) error {
	ctx := c.Request().Context()
	q := dbc.Queries(ctx)
	jobIDStr := jobID.String()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	lastPct := int32(-1)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			row, err := q.GetStitchJobStatus(ctx, jobID)
			if err != nil {
				_ = sse.PatchElementTempl(components.ExportStatus("multicam-export-status","Job not found", "error", ""))
				return nil
			}
			switch row.Status {
			case db.ExportStatusQueued:
				_ = sse.PatchElementTempl(components.ExportStatus("multicam-export-status","Queued...", "queued", ""))
			case db.ExportStatusProcessing:
				if row.ProgressPct != lastPct {
					lastPct = row.ProgressPct
					_ = sse.PatchElementTempl(components.ExportStatus("multicam-export-status",
						fmt.Sprintf("Rendering %d%%…", row.ProgressPct), "processing", ""))
				}
			case db.ExportStatusReady:
				downloadURL := "/api/stitch/" + jobIDStr + "/download"
				_ = sse.PatchElementTempl(components.ExportStatus("multicam-export-status","", "ready", downloadURL))
				_ = sse.ExecuteScript("window.location.href = '" + downloadURL + "';")
				return nil
			case db.ExportStatusError:
				errMsg := "Export failed"
				if row.LastError != nil && *row.LastError != "" {
					errMsg = *row.LastError
				}
				_ = sse.PatchElementTempl(components.ExportStatus("multicam-export-status",errMsg, "error", ""))
				return nil
			}
		}
	}
}
