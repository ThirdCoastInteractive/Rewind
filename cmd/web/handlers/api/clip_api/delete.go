// package clip_api provides clip-related API handlers.
package clip_api

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates/components"
	"thirdcoast.systems/rewind/internal/db"
)
// HandleDelete serves DELETE /clips/:id, removing a clip and its associated exports.
func HandleDelete(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()

		userUUID, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return err
		}

		clipUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}

		existing, err := dbc.Queries(ctx).GetClip(ctx, clipUUID)
		if err != nil || existing == nil {
			return c.String(404, "clip not found")
		}
		if err := requireClipMutate(c, sm, dbc.Queries(ctx), existing, userUUID); err != nil {
			return err
		}

		videoID := existing.VideoID

		removeClipExportFiles(ctx, dbc, clipUUID)

		if err := dbc.Queries(ctx).DeleteClip(ctx, clipUUID); err != nil {
			return c.String(500, "failed to delete clip")
		}

		// SSE response for DataStar
		common.SetSSEHeaders(c)

		sse := datastar.NewSSE(c.Response().Writer, c.Request())

		// Fetch updated clips list
		clips, err := dbc.Queries(ctx).ListClipsByVideo(ctx, videoID)
		if err != nil {
			clips = []*db.Clip{}
		}

		// Determine variant from referer or default to "watch"
		variant := "watch"
		referer := c.Request().Referer()
		if len(referer) > 0 && (strings.HasSuffix(referer, "/cut") || strings.Contains(referer, "/cut?")) {
			variant = "cut"
		}

		// Patch clip list - the JS MutationObserver on [data-clip-list]
		// will automatically reload clip data and re-render timelines.
		_ = sse.PatchElementTempl(
			components.ClipList(clips, variant),
			datastar.WithSelector("[data-clip-list]"),
			datastar.WithModeReplace(),
		)

		// Re-hydrate export status badges that were wiped by the full list replace.
		PatchClipExportStatuses(sse, ctx, dbc, clips)

		return nil
	}
}

func removeClipExportFiles(ctx context.Context, dbc *db.DatabaseConnection, clipID pgtype.UUID) {
	if dbc == nil {
		return
	}
	rows, err := dbc.Query(ctx, `SELECT file_path FROM clip_exports WHERE clip_id = $1 AND file_path <> ''`, clipID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil || strings.TrimSpace(p) == "" {
			continue
		}
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			slog.Warn("remove clip export", "path", p, "error", err)
		}
	}
}

// HandleUpdate updates a clip's metadata from DataStar signals.
