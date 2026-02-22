// package video_api provides video-related API handlers.
package video_api

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
)

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
		tx, err := dbc.Begin(ctx)
		if err != nil {
			slog.Error("failed to begin transaction", "error", err)
			return c.String(500, "failed to delete video")
		}
		defer tx.Rollback(ctx)

		qtx := dbc.Queries(ctx).WithTx(tx)

		// Ensure no non-cascading references block deletion.
		if err := qtx.ClearVideoFromJobs(ctx, videoUUID); err != nil {
			slog.Error("failed to clear video references from jobs", "error", err, "video_id", videoUUID)
			return c.String(500, "failed to delete video")
		}
		if err := qtx.ClearVideoFromPlayerSessions(ctx, videoUUID); err != nil {
			slog.Error("failed to clear video references from player sessions", "error", err, "video_id", videoUUID)
			return c.String(500, "failed to delete video")
		}
		if err := qtx.DeleteClipsByVideo(ctx, videoUUID); err != nil {
			slog.Error("failed to delete clips for video", "error", err, "video_id", videoUUID)
			return c.String(500, "failed to delete video")
		}
		if err := qtx.DeleteMarkersByVideo(ctx, videoUUID); err != nil {
			slog.Error("failed to delete markers for video", "error", err, "video_id", videoUUID)
			return c.String(500, "failed to delete video")
		}
		if err := qtx.DeleteVideo(ctx, videoUUID); err != nil {
			slog.Error("failed to delete video", "error", err, "video_id", videoUUID)
			return c.String(500, "failed to delete video")
		}

		if err := tx.Commit(ctx); err != nil {
			slog.Error("failed to commit delete video transaction", "error", err, "video_id", videoUUID)
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

// HandleStream streams the video file.
