package stitch_api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates/components"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/ffmpeg"
)

// stitchRequest is the JSON body sent by the stitch page.
type stitchRequest struct {
	Title         string              `json:"title"`
	Format        string              `json:"format"`
	Quality       string              `json:"quality"`
	Segments      []json.RawMessage   `json:"segments"`
	GlobalFilters []ffmpeg.FilterSpec `json:"global_filters"`
	ProjectID     string              `json:"project_id"`
}

// stitchSegmentForValidation is a minimal view of a segment for server-side validation.
type stitchSegmentForValidation struct {
	Type          string          `json:"type"`
	Duration      float64         `json:"duration"`
	RawTransition json.RawMessage `json:"transition"`
}

// HandleStitchEnqueue validates and enqueues a stitch job, then streams status via SSE.
func HandleStitchEnqueue(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		userUUID, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.String(401, "unauthorized")
		}

		// Helper to return validation errors as SSE so the UI updates correctly.
		sseError := func(msg string) error {
			sse := datastar.NewSSE(c.Response().Writer, c.Request())
			common.SetSSEHeaders(c)
			_ = sse.PatchElementTempl(components.StitchExportStatus(msg, "error", ""))
			return nil
		}

		var req stitchRequest
		if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
			slog.Error("stitch request decode failed", "error", err)
			return sseError("Invalid request body")
		}

		if len(req.Segments) == 0 {
			return sseError("At least one segment is required")
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

		// Validate segment transition durations don't exceed segment durations.
		for i, rawSeg := range req.Segments {
			var seg stitchSegmentForValidation
			if err := json.Unmarshal(rawSeg, &seg); err != nil {
				slog.Error("stitch segment unmarshal failed", "index", i, "error", err)
				return sseError(fmt.Sprintf("Invalid segment[%d]", i))
			}
			// Parse transition — RawTransition may be null, "", or a JSON object.
			var trDuration float64
			if len(seg.RawTransition) > 0 && seg.RawTransition[0] == '{' {
				var tr struct {
					Duration float64 `json:"duration"`
				}
				if err := json.Unmarshal(seg.RawTransition, &tr); err == nil {
					trDuration = tr.Duration
				}
			}
			if i > 0 && trDuration > 0 {
				if seg.Duration > 0 && trDuration >= seg.Duration {
					return sseError(fmt.Sprintf("Segment[%d] transition (%.1fs) must be shorter than duration (%.1fs)",
						i, trDuration, seg.Duration))
				}
			}
		}

		// Marshal segments and global filters for storage.
		segmentsJSON, err := json.Marshal(req.Segments)
		if err != nil {
			return sseError("Failed to encode segments")
		}
		globalFiltersJSON := []byte("[]")
		if len(req.GlobalFilters) > 0 {
			globalFiltersJSON, _ = json.Marshal(req.GlobalFilters)
		}

		ctx := c.Request().Context()
		q := dbc.Queries(ctx)

		title := strings.TrimSpace(req.Title)

		// Parse optional project_id
		var projectID pgtype.UUID
		if req.ProjectID != "" {
			parsed, err := uuid.Parse(req.ProjectID)
			if err == nil {
				projectID = pgtype.UUID{Bytes: parsed, Valid: true}
			}
		}

		jobID, err := q.CreateStitchJob(ctx, &db.CreateStitchJobParams{
			CreatedBy:     userUUID,
			Title:         title,
			Format:        format,
			Quality:       quality,
			Segments:      segmentsJSON,
			GlobalFilters: globalFiltersJSON,
			ProjectID:     projectID,
		})
		if err != nil {
			slog.Error("failed to create stitch job", "error", err)
			return sseError("Failed to queue stitch job")
		}

		// Notify encoder worker.
		_, _ = dbc.Exec(ctx, "SELECT pg_notify('stitch_jobs', $1)", jobID.String())

		sse := datastar.NewSSE(c.Response().Writer, c.Request())
		common.SetSSEHeaders(c)

		if err := sse.PatchElementTempl(components.StitchExportStatus("Queued...", "queued", "")); err != nil {
			slog.Error("failed to patch stitch status", "error", err)
			return err
		}

		return streamStitchStatus(c, sse, dbc, jobID, projectID)
	}
}

// streamStitchStatus polls the DB and patches the export status component via SSE until complete.
func streamStitchStatus(c echo.Context, sse *datastar.ServerSentEventGenerator, dbc *db.DatabaseConnection, jobID pgtype.UUID, projectID pgtype.UUID) error {
	ctx := c.Request().Context()
	q := dbc.Queries(ctx)
	jobIDStr := jobID.String()

	// Helper to refresh the export history list after a terminal state.
	refreshHistory := func() {
		if !projectID.Valid {
			return
		}
		jobs, err := q.ListStitchJobsByProject(ctx, projectID)
		if err != nil {
			slog.Error("failed to refresh export history", "error", err)
			return
		}
		_ = sse.PatchElementTempl(components.StitchExportHistory(jobs))
	}

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
				_ = sse.PatchElementTempl(components.StitchExportStatus("Job not found", "error", ""))
				return nil
			}

			switch row.Status {
			case db.ExportStatusQueued:
				_ = sse.PatchElementTempl(components.StitchExportStatus("Queued...", "queued", ""))
			case db.ExportStatusProcessing:
				if row.ProgressPct != lastPct {
					lastPct = row.ProgressPct
					_ = sse.PatchElementTempl(components.StitchExportStatus(
						fmt.Sprintf("Stitching %d%%…", row.ProgressPct), "processing", ""))
				}
			case db.ExportStatusReady:
				downloadURL := "/api/stitch/" + jobIDStr + "/download"
				_ = sse.PatchElementTempl(components.StitchExportStatus("", "ready", downloadURL))
				_ = sse.ExecuteScript("window.location.href = '" + downloadURL + "';")
				refreshHistory()
				return nil
			case db.ExportStatusError:
				errMsg := "Export failed"
				if row.LastError != nil && *row.LastError != "" {
					errMsg = *row.LastError
				}
				_ = sse.PatchElementTempl(components.StitchExportStatus(errMsg, "error", ""))
				refreshHistory()
				return nil
			}
		}
	}
}
