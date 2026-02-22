package clip_api

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates/components"
	"thirdcoast.systems/rewind/internal/db"
)

func HandleBankExportStatus(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		_, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return err
		}

		videoUUID, err := common.RequireUUIDParam(c, "videoId")
		if err != nil {
			return err
		}

		ctx := c.Request().Context()
		q := dbc.Queries(ctx)

		// Get all clips for this video
		clips, err := q.ListClipsByVideo(ctx, videoUUID)
		if err != nil {
			return c.String(500, "failed to list clips")
		}

		if len(clips) == 0 {
			return c.NoContent(204)
		}

		// Collect clip IDs
		clipIDs := make([]pgtype.UUID, len(clips))
		for i, clip := range clips {
			clipIDs[i] = clip.ID
		}

		// Get active exports for these clips
		exports, err := q.ListActiveExportsForClips(ctx, clipIDs)
		if err != nil {
			slog.Warn("failed to list active exports", "error", err)
			return c.NoContent(204)
		}

		if len(exports) == 0 {
			return c.NoContent(204)
		}

		// SSE headers
		common.SetSSEHeaders(c)

		w := c.Response().Writer
		flusher, ok := w.(http.Flusher)
		if !ok {
			return c.String(500, "streaming unsupported")
		}

		sse := datastar.NewSSE(w, c.Request())

		// Deduplicate by clip ID (latest export per clip)
		clipExports := make(map[string]*db.ListActiveExportsForClipsRow)
		for _, exp := range exports {
			clipID := exp.ClipID.String()
			if existing, ok := clipExports[clipID]; !ok || exp.Status == "processing" || (existing.Status == "queued" && exp.Status != "queued") {
				clipExports[clipID] = exp
			}
		}

		// Patch each clip's export status
		for clipID, exp := range clipExports {
			statusID := "clip-export-status-" + clipID
			var text, state, downloadURL string

			switch exp.Status {
			case "queued":
				text = "Queued…"
				state = "queued"
			case "processing":
				text = "Exporting " + itoa(int(exp.ProgressPct)) + "%…"
				state = "processing"
			case "ready":
				// Verify file actually exists before showing download button
				if _, err := os.Stat(exp.FilePath); err != nil {
					slog.Warn("ready export file missing during hydration, requeuing", "export_id", exp.ID.String(), "file_path", exp.FilePath)
					if requeueErr := q.RequeueClipExport(ctx, exp.ID); requeueErr != nil {
						slog.Error("failed to requeue missing export", "export_id", exp.ID.String(), "error", requeueErr)
					}
					_, _ = dbc.Exec(ctx, "SELECT pg_notify('clip_exports', $1)", exp.ID.String())
					continue
				}
				state = "ready"
				downloadURL = "/api/clip-exports/" + exp.ID.String() + "/download"
			}

			if err := sse.PatchElementTempl(
				components.ClipExportStatus(clipID, text, state, downloadURL),
				datastar.WithSelectorID(statusID),
				datastar.WithModeReplace(),
			); err != nil {
				slog.Error("failed to patch clip export status", "error", err, "clipID", clipID)
			}
		}

		flusher.Flush()
		return nil
	}
}

func itoa(i int) string {
	if i < 0 {
		return "0"
	}
	if i > 99 {
		return "99"
	}
	if i < 10 {
		return string(rune('0' + i))
	}
	return string(rune('0'+i/10)) + string(rune('0'+i%10))
}
