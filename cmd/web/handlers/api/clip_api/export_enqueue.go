package clip_api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates/components"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/ffmpeg"
)

// exportRequest is the JSON body sent by the export panel / DataStar action.
// It supports both the new spec-based format and the legacy ?variant= query param.
type exportRequest struct {
	Format  string              `json:"format"`
	Quality string              `json:"quality"`
	Filters []ffmpeg.FilterSpec `json:"filters"`
	Variant string              `json:"variant"` // Legacy compat: "full", "crop:<id>"
}

// HandleEnqueueExport enqueues a clip export job and streams status updates via SSE.
func HandleEnqueueExport(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		userUUID, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return err
		}

		clipUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}

		ctx := c.Request().Context()
		q := dbc.Queries(ctx)

		clipRow, err := q.GetClip(ctx, clipUUID)
		if err != nil || clipRow == nil {
			return c.String(404, "clip not found")
		}

		clipIDStr := clipRow.ID.String()

		// Parse export spec from JSON body or fall back to legacy ?variant= query param
		var req exportRequest
		if c.Request().ContentLength > 0 {
			_ = json.NewDecoder(c.Request().Body).Decode(&req)
		}

		// Determine variant string (for reuse matching and display)
		variant := strings.TrimSpace(req.Variant)
		if variant == "" {
			variant = strings.TrimSpace(c.QueryParam("variant"))
		}
		if variant == "" {
			variant = "full"
		}
		// Validate variant
		if variant != "full" && variant != "cropped" && !strings.HasPrefix(variant, "crop:") {
			return c.String(400, "invalid variant")
		}

		// Determine format with default
		format := strings.TrimSpace(req.Format)
		if format == "" {
			format = "mp4"
		}
		if format != "mp4" && format != "webm" && format != "gif" {
			return c.String(400, "invalid format")
		}

		// When variant is crop:<id>, inject a crop filter at the front of the
		// filter list so the encoder always applies it (even when other filters
		// are present and the spec-based pipeline takes precedence over legacy
		// variant handling).
		filters := req.Filters
		if strings.HasPrefix(variant, "crop:") {
			cropID := strings.TrimPrefix(variant, "crop:")
			cropFilter := ffmpeg.FilterSpec{
				Type:   "crop",
				Params: map[string]any{"crop_id": cropID},
			}
			// Prepend so crop is applied before other filters
			filters = append([]ffmpeg.FilterSpec{cropFilter}, filters...)
		}

		// Build ExportSpec JSON for storage
		var specJSON []byte
		if len(filters) > 0 || req.Format != "" || req.Quality != "" {
			spec := ffmpeg.ExportSpec{
				Format:  format,
				Quality: req.Quality,
				Filters: filters,
			}
			specJSON, _ = json.Marshal(spec)
		}

		// Start SSE response
		sse := datastar.NewSSE(c.Response().Writer, c.Request())

		// Check for existing ready export
		existingExport, reuseErr := q.FindReusableClipExport(ctx, &db.FindReusableClipExportParams{
			ClipID:    clipRow.ID,
			CreatedBy: userUUID,
			Format:    format,
			Variant:   variant,
		})
		if reuseErr == nil {
			if _, err := os.Stat(existingExport.FilePath); err == nil {
				_ = q.UpdateClipExportLastAccessed(ctx, existingExport.ID)
				cleanupClipExportsLRU(ctx, dbc)
				downloadURL := "/api/clip-exports/" + existingExport.ID.String() + "/download"
				if err := sse.PatchElementTempl(components.ClipExportStatus(clipIDStr, "Ready", "ready", downloadURL)); err != nil {
					slog.Error("failed to patch export status", "error", err)
				}
				return nil
			} else {
				// File is missing - requeue this export
				slog.Warn("reusable export file missing, requeuing", "export_id", existingExport.ID.String(), "file_path", existingExport.FilePath)
				if requeueErr := q.RequeueClipExport(ctx, existingExport.ID); requeueErr != nil {
					slog.Error("failed to requeue missing export", "export_id", existingExport.ID.String(), "error", requeueErr)
				}
				_, _ = dbc.Exec(ctx, "SELECT pg_notify('clip_exports', $1)", existingExport.ID.String())
				if err := sse.PatchElementTempl(components.ClipExportStatus(clipIDStr, "Queued...", "queued", "")); err != nil {
					slog.Error("failed to patch export status", "error", err)
				}
				return streamExportStatus(c, sse, dbc, existingExport.ID, clipIDStr)
			}
		}

		// Check for existing queued/processing export - stream its status
		pendingExport, pendingErr := q.FindOrCreatePendingClipExport(ctx, &db.FindOrCreatePendingClipExportParams{
			ClipID:    clipRow.ID,
			CreatedBy: userUUID,
			Format:    format,
			Variant:   variant,
		})
		if pendingErr == nil {
			return streamExportStatus(c, sse, dbc, pendingExport.ID, clipIDStr)
		}

		// Create new queued export
		exportID, err := q.CreateClipExport(ctx, &db.CreateClipExportParams{
			ClipID:        clipRow.ID,
			CreatedBy:     userUUID,
			Format:        format,
			Variant:       variant,
			Spec:          specJSON,
			ClipUpdatedAt: clipRow.UpdatedAt,
		})
		if err != nil {
			slog.Error("failed to create clip export", "error", err, "clip_id", clipIDStr)
			if patchErr := sse.PatchElementTempl(components.ClipExportStatus(clipIDStr, "Failed to queue", "error", "")); patchErr != nil {
				slog.Error("failed to patch export status", "error", patchErr)
			}
			return nil
		}

		// Notify encoder workers via NOTIFY
		_, _ = dbc.Exec(ctx, "SELECT pg_notify('clip_exports', $1)", exportID.String())

		// Patch initial queued status
		if err := sse.PatchElementTempl(components.ClipExportStatus(clipIDStr, "Queued...", "queued", "")); err != nil {
			slog.Error("failed to patch export status", "error", err)
			return err
		}

		// Stream status updates
		return streamExportStatus(c, sse, dbc, exportID, clipIDStr)
	}
}

// streamExportStatus polls the database for export status and patches the UI via SSE.
func streamExportStatus(c echo.Context, sse *datastar.ServerSentEventGenerator, dbc *db.DatabaseConnection, exportID pgtype.UUID, clipIDStr string) error {
	ctx := c.Request().Context()
	q := dbc.Queries(ctx)
	exportIDStr := exportID.String()
	statusID := "clip-export-status-" + clipIDStr

	patch := func(text, state, downloadURL string) error {
		return sse.PatchElementTempl(
			components.ClipExportStatus(clipIDStr, text, state, downloadURL),
			datastar.WithSelectorID(statusID),
		)
	}

	// Poll for status updates
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	lastPct := int32(-1)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			exportRow, err := q.GetClipExportStatus(ctx, exportID)
			if err != nil {
				if patchErr := patch("Export not found", "error", ""); patchErr != nil {
					return patchErr
				}
				return nil
			}

			switch exportRow.Status {
			case "queued":
				if err := patch("Queued…", "queued", ""); err != nil {
					return err
				}
			case "processing":
				if exportRow.ProgressPct != lastPct {
					lastPct = exportRow.ProgressPct
					if err := patch(fmt.Sprintf("Exporting %d%%…", exportRow.ProgressPct), "processing", ""); err != nil {
						return err
					}
				}
			case "ready":
				downloadURL := "/api/clip-exports/" + exportIDStr + "/download"
				if err := patch("", "ready", downloadURL); err != nil {
					return err
				}
				// Auto-download via script execution
				if err := sse.ExecuteScript("window.location.href = '" + downloadURL + "';"); err != nil {
					return err
				}
				return nil
			case "error":
				errMsg := "Export failed"
				if exportRow.LastError != nil && *exportRow.LastError != "" {
					errMsg = *exportRow.LastError
				}
				if err := patch(errMsg, "error", ""); err != nil {
					return err
				}
				return nil
			}
		}
	}
}

// HandleExportStatusStream streams export status updates via SSE.
