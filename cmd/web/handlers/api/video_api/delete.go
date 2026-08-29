// package video_api provides video-related API handlers.
package video_api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
)

const maxBulkDelete = 200

// HandleDelete serves DELETE /videos/:id, soft-deleting a video and its associated data.
func HandleDelete(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		isDatastarRequest := strings.EqualFold(strings.TrimSpace(c.Request().Header.Get("Datastar-Request")), "true")

		accessLevel := fmt.Sprint(c.Get("accessLevel"))
		if accessLevel == "unauthenticated" {
			return c.String(401, "unauthorized")
		}

		userID, _, err := sm.GetSession(c.Request())
		if err != nil {
			return c.String(401, "unauthorized")
		}

		videoUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}

		// Fetch video for auth + to capture paths before deletion.
		videoRow, err := dbc.Queries(c.Request().Context()).GetVideoByID(c.Request().Context(), videoUUID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return c.String(404, "video not found")
			}
			return c.String(500, "failed to fetch video")
		}

		// Ownership check (admins can delete anything)
		if accessLevel != "admin" {
			if userID != videoRow.ArchivedBy.String() {
				return c.String(403, "forbidden")
			}
		}

		deleteDisk := isTruthyQueryParam(c.QueryParam("delete_disk"))
		deleteDir, ok := safeVideoDirForDeletion(videoUUID)
		if deleteDisk && !ok {
			return c.String(400, "refusing disk delete (no safe directory found for this video)")
		}

		ctx := c.Request().Context()
		if err := deleteVideoDB(ctx, dbc, videoUUID); err != nil {
			return c.String(500, "failed to delete video")
		}

		diskDeleted := false
		var diskError string
		if deleteDisk {
			if err := os.RemoveAll(deleteDir); err != nil {
				diskError = err.Error()
			} else {
				diskDeleted = true
			}
		}

		resp := map[string]any{"status": "deleted", "video_id": videoUUID.String(), "disk_deleted": diskDeleted}
		if diskError != "" {
			resp["disk_error"] = diskError
		}
		if isDatastarRequest {
			c.Response().Header().Set(echo.HeaderContentType, "text/javascript")
			return c.String(200, "window.location.href = '/videos';")
		}
		return c.JSON(200, resp)
	}
}

// HandleBulkDelete serves POST /videos/bulk-delete, deleting all selected library videos.
// Streams progress via SSE as each video finishes so the UI can show live feedback.
// DataStar signals: selectedVideoIds, bulkDeleteDisk, plus list filter signals for refresh.
// Note: do not use underscore-prefixed signal names for values the backend needs —
// Datastar excludes them from request payloads by default.
func HandleBulkDelete(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		accessLevel := fmt.Sprint(c.Get("accessLevel"))
		if accessLevel == "unauthenticated" {
			return c.String(401, "unauthorized")
		}

		userID, _, err := sm.GetSession(c.Request())
		if err != nil {
			return c.String(401, "unauthorized")
		}

		var sig struct {
			SelectedVideoIDs []string `json:"selectedVideoIds"`
			BulkDeleteDisk   bool     `json:"bulkDeleteDisk"`
			// List filters — used to re-render the grid after deletion.
			Query      string   `json:"q"`
			Sort       string   `json:"sort"`
			Duration   string   `json:"duration"`
			Uploader   string   `json:"uploader"`
			Tags       []string `json:"tags"`
			TagIDs     []string `json:"tagIds"`
			DateType   *string  `json:"dateType"`
			DateFrom   *string  `json:"dateFrom"`
			DateTo     *string  `json:"dateTo"`
			HasClips   bool     `json:"hasClips"`
			HasMarkers bool     `json:"hasMarkers"`
			Page       int      `json:"page"`
			PageSize   int      `json:"pageSize"`
		}
		// Must ReadSignals BEFORE opening the SSE stream (body is consumed).
		_ = datastar.ReadSignals(c.Request(), &sig)

		ids := parseVideoUUIDs(sig.SelectedVideoIDs)
		if len(ids) == 0 {
			return c.String(400, "no videos selected")
		}
		if len(ids) > maxBulkDelete {
			return c.String(400, fmt.Sprintf("too many videos selected (max %d)", maxBulkDelete))
		}

		ctx := c.Request().Context()
		q := dbc.Queries(ctx)
		total := len(ids)

		// Open SSE immediately so the client can stream progress while disk I/O runs.
		common.SetSSEHeaders(c)
		sse := datastar.NewSSE(c.Response().Writer, c.Request())

		// Acknowledge intent + clear selection so the bulk bar swaps to the progress UI.
		_ = patchBulkDeleteProgress(sse, bulkDeleteProgress{
			Active:  true,
			Done:    0,
			Total:   total,
			Skipped: 0,
			Status:  fmt.Sprintf("Deleting 0/%d…", total),
			// Drop selection so the action bar hides; progress bar takes over.
			ClearSelection: true,
		})

		deleted := 0
		skipped := 0
		diskDeleted := 0

		for i, videoUUID := range ids {
			if sse.IsClosed() {
				slog.Info("bulk video delete aborted (client closed SSE)",
					"deleted", deleted, "skipped", skipped, "requested", total, "user_id", userID)
				return nil
			}

			idStr := videoUUID.String()
			title := idStr
			videoRow, err := q.GetVideoByID(ctx, videoUUID)
			if err != nil {
				skipped++
				_ = patchBulkDeleteProgress(sse, bulkDeleteProgress{
					Active: true, Done: deleted, Total: total, Skipped: skipped,
					Status: fmt.Sprintf("Skipping missing video (%d/%d)", i+1, total),
				})
				_ = sse.RemoveElementf(`[data-video-id="%s"]`, idStr)
				continue
			}
			if videoRow.Title != "" {
				title = videoRow.Title
			}
			if accessLevel != "admin" && userID != videoRow.ArchivedBy.String() {
				skipped++
				_ = patchBulkDeleteProgress(sse, bulkDeleteProgress{
					Active: true, Done: deleted, Total: total, Skipped: skipped,
					Status: fmt.Sprintf("Skipping (no permission): %s", truncateRunes(title, 40)),
				})
				continue
			}

			_ = patchBulkDeleteProgress(sse, bulkDeleteProgress{
				Active: true, Done: deleted, Total: total, Skipped: skipped,
				Status: fmt.Sprintf("Deleting %d/%d — %s", i+1, total, truncateRunes(title, 48)),
			})

			if err := deleteVideoDB(ctx, dbc, videoUUID); err != nil {
				skipped++
				_ = patchBulkDeleteProgress(sse, bulkDeleteProgress{
					Active: true, Done: deleted, Total: total, Skipped: skipped,
					Status: fmt.Sprintf("Failed: %s", truncateRunes(title, 40)),
				})
				continue
			}
			deleted++

			// Drop the card as soon as the DB row is gone — disk I/O may still run.
			_ = sse.RemoveElementf(`[data-video-id="%s"]`, idStr)

			if sig.BulkDeleteDisk {
				_ = patchBulkDeleteProgress(sse, bulkDeleteProgress{
					Active: true, Done: deleted, Total: total, Skipped: skipped,
					Status: fmt.Sprintf("Removing files %d/%d — %s", deleted, total, truncateRunes(title, 40)),
				})
				if dir, ok := safeVideoDirForDeletion(videoUUID); ok {
					if err := os.RemoveAll(dir); err != nil {
						slog.Warn("bulk delete: disk remove failed", "video_id", videoUUID, "error", err)
					} else {
						diskDeleted++
					}
				}
			}

			_ = patchBulkDeleteProgress(sse, bulkDeleteProgress{
				Active: true, Done: deleted, Total: total, Skipped: skipped,
				Status: fmt.Sprintf("Deleted %d/%d", deleted, total),
			})
		}

		slog.Info("bulk video delete finished",
			"deleted", deleted,
			"skipped", skipped,
			"disk_deleted", diskDeleted,
			"requested", total,
			"user_id", userID,
		)

		// Final status before grid refresh.
		status := fmt.Sprintf("Done — deleted %d", deleted)
		if skipped > 0 {
			status = fmt.Sprintf("Done — deleted %d, skipped %d", deleted, skipped)
		}
		_ = patchBulkDeleteProgress(sse, bulkDeleteProgress{
			Active: true, Done: deleted, Total: total, Skipped: skipped, Status: status,
		})

		listSig := &videosListSignals{
			Query:      sig.Query,
			Sort:       sig.Sort,
			Duration:   sig.Duration,
			Uploader:   sig.Uploader,
			Tags:       sig.Tags,
			TagIDs:     sig.TagIDs,
			DateType:   sig.DateType,
			DateFrom:   sig.DateFrom,
			DateTo:     sig.DateTo,
			HasClips:   sig.HasClips,
			HasMarkers: sig.HasMarkers,
			Page:       sig.Page,
			PageSize:   sig.PageSize,
		}
		if err := patchVideosList(c, dbc, sse, listSig); err != nil {
			// Still clear the progress UI even if refresh fails.
			_ = patchBulkDeleteProgress(sse, bulkDeleteProgress{Active: false, Status: status + " (refresh failed)"})
			return err
		}

		_ = patchBulkDeleteProgress(sse, bulkDeleteProgress{
			Active:         false,
			Done:           deleted,
			Total:          total,
			Skipped:        skipped,
			Status:         "",
			ClearSelection: true,
			ClearDiskFlag:  true,
		})
		return nil
	}
}

type bulkDeleteProgress struct {
	Active         bool
	Done           int
	Total          int
	Skipped        int
	Status         string
	ClearSelection bool
	ClearDiskFlag  bool
}

func patchBulkDeleteProgress(sse *datastar.ServerSentEventGenerator, p bulkDeleteProgress) error {
	payload := map[string]any{
		"bulkDeleteActive":  p.Active,
		"bulkDeleteDone":    p.Done,
		"bulkDeleteTotal":   p.Total,
		"bulkDeleteSkipped": p.Skipped,
		"bulkDeleteStatus":  p.Status,
	}
	if p.ClearSelection {
		payload["selectedVideoIds"] = []string{}
	}
	if p.ClearDiskFlag {
		payload["bulkDeleteDisk"] = false
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return sse.PatchSignals(b)
}

func truncateRunes(s string, max int) string {
	if max <= 0 || s == "" {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 1 {
		return string(r[:max])
	}
	return string(r[:max-1]) + "…"
}

// deleteVideoDB removes a video and its non-cascading related rows in a transaction.
func deleteVideoDB(ctx context.Context, dbc *db.DatabaseConnection, videoUUID pgtype.UUID) error {
	tx, err := dbc.Begin(ctx)
	if err != nil {
		slog.Error("failed to begin transaction", "error", err, "video_id", videoUUID)
		return err
	}
	defer tx.Rollback(ctx)

	qtx := dbc.Queries(ctx).WithTx(tx)

	if err := qtx.ClearVideoFromJobs(ctx, videoUUID); err != nil {
		slog.Error("failed to clear video references from jobs", "error", err, "video_id", videoUUID)
		return err
	}
	if err := qtx.DeleteClipsByVideo(ctx, videoUUID); err != nil {
		slog.Error("failed to delete clips for video", "error", err, "video_id", videoUUID)
		return err
	}
	if err := qtx.DeleteMarkersByVideo(ctx, videoUUID); err != nil {
		slog.Error("failed to delete markers for video", "error", err, "video_id", videoUUID)
		return err
	}
	if err := qtx.DeleteVideo(ctx, videoUUID); err != nil {
		slog.Error("failed to delete video", "error", err, "video_id", videoUUID)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		slog.Error("failed to commit delete video transaction", "error", err, "video_id", videoUUID)
		return err
	}
	return nil
}

// parseVideoUUIDs parses id strings into a UUID slice, skipping invalid entries.
func parseVideoUUIDs(ids []string) []pgtype.UUID {
	out := make([]pgtype.UUID, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, s := range ids {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		g, err := uuid.Parse(s)
		if err != nil {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, pgtype.UUID{Bytes: [16]byte(g), Valid: true})
	}
	return out
}
