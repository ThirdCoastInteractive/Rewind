package clip_api

import (
	"context"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/templates/components"
	"thirdcoast.systems/rewind/internal/db"
)

// PatchClipExportStatuses re-hydrates the export status badges for every clip
// in the list.  Call this after any SSE operation that replaces the whole
// [data-clip-list] element, because the replacement renders blank
// ClipExportStatus placeholders and the one-shot data-init hydration won't
// re-fire.
func PatchClipExportStatuses(sse *datastar.ServerSentEventGenerator, ctx context.Context, dbc *db.DatabaseConnection, clips []*db.Clip) {
	if len(clips) == 0 {
		return
	}

	q := dbc.Queries(ctx)

	clipIDs := make([]pgtype.UUID, len(clips))
	for i, clip := range clips {
		clipIDs[i] = clip.ID
	}

	exports, err := q.ListActiveExportsForClips(ctx, clipIDs)
	if err != nil {
		slog.Warn("PatchClipExportStatuses: failed to list active exports", "error", err)
		return
	}
	if len(exports) == 0 {
		return
	}

	// Deduplicate by clip ID — keep the most relevant export per clip.
	clipExports := make(map[string]*db.ListActiveExportsForClipsRow)
	for _, exp := range exports {
		clipID := exp.ClipID.String()
		if existing, ok := clipExports[clipID]; !ok || exp.Status == "processing" || (existing.Status == "queued" && exp.Status != "queued") {
			clipExports[clipID] = exp
		}
	}

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
			if _, err := os.Stat(exp.FilePath); err != nil {
				slog.Warn("ready export file missing during re-hydration, requeuing",
					"export_id", exp.ID.String(), "file_path", exp.FilePath)
				if requeueErr := q.RequeueClipExport(ctx, exp.ID); requeueErr != nil {
					slog.Error("failed to requeue missing export",
						"export_id", exp.ID.String(), "error", requeueErr)
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
			slog.Error("PatchClipExportStatuses: failed to patch", "error", err, "clipID", clipID)
		}
	}
}
