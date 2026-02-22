package job_api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
)

// LogEntry represents a single log line from yt-dlp
type LogEntry struct {
	ID        int64     `json:"id"`
	Stream    string    `json:"stream"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

// LogsResponse is the response type for paginated job logs
type LogsResponse struct {
	Logs   []LogEntry `json:"logs"`
	Total  int64      `json:"total"`
	Limit  int        `json:"limit"`
	Offset int        `json:"offset"`
}

// HandleLogs returns paginated logs for a specific job (JSON)
func HandleLogs(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		_, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return err
		}

		jobUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}

		// Fetch the job
		_, err = dbc.Queries(c.Request().Context()).GetDownloadJobByID(c.Request().Context(), jobUUID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return c.JSON(404, map[string]string{"error": "Job not found"})
			}
			slog.Error("failed to fetch job", "error", err)
			return c.JSON(500, map[string]string{"error": "Failed to fetch job"})
		}

		// Get pagination params (default: last 50 lines)
		limit := 50
		offset := 0
		if limitStr := c.QueryParam("limit"); limitStr != "" {
			if _, err := fmt.Sscanf(limitStr, "%d", &limit); err == nil {
				if limit > 1000 {
					limit = 1000 // Cap at 1000
				}
			}
		}
		if offsetStr := c.QueryParam("offset"); offsetStr != "" {
			fmt.Sscanf(offsetStr, "%d", &offset)
		}

		// Fetch logs (DESC order, then reverse)
		logs, err := dbc.Queries(c.Request().Context()).GetYtdlpLogsForJobPaginated(c.Request().Context(), &db.GetYtdlpLogsForJobPaginatedParams{
			JobID:      jobUUID,
			PageLimit:  int32(limit),
			PageOffset: int32(offset),
		})
		if err != nil {
			slog.Error("failed to fetch ytdlp logs", "error", err)
			return c.JSON(500, map[string]string{"error": "Failed to fetch logs"})
		}

		// Get total count
		totalCount, err := dbc.Queries(c.Request().Context()).CountYtdlpLogsForJob(c.Request().Context(), jobUUID)
		if err != nil {
			slog.Error("failed to count logs", "error", err)
			totalCount = 0
		}

		// Reverse to show oldest first
		result := make([]LogEntry, len(logs))
		for i, log := range logs {
			result[len(logs)-1-i] = LogEntry{
				ID:        log.ID,
				Stream:    string(log.Stream),
				Message:   log.Message,
				CreatedAt: log.CreatedAt.Time,
			}
		}

		return c.JSON(200, LogsResponse{
			Logs:   result,
			Total:  totalCount,
			Limit:  limit,
			Offset: offset,
		})
	}
}

// HandleLogsStream streams logs via SSE as they're written
func HandleLogsStream(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		_, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return err
		}

		jobUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}

		// Fetch the job
		_, err = dbc.Queries(c.Request().Context()).GetDownloadJobByID(c.Request().Context(), jobUUID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return c.JSON(404, map[string]string{"error": "Job not found"})
			}
			slog.Error("failed to fetch job", "error", err)
			return c.JSON(500, map[string]string{"error": "Failed to fetch job"})
		}

		// Set SSE headers
		c.Response().Header().Set("Content-Type", "text/event-stream")
		c.Response().Header().Set("Cache-Control", "no-cache")
		c.Response().Header().Set("Connection", "keep-alive")
		c.Response().Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

		w := c.Response().Writer
		flusher, ok := w.(http.Flusher)
		if !ok {
			return c.JSON(500, map[string]string{"error": "Streaming not supported"})
		}

		// Start with epoch time to get all logs
		lastTimestamp := pgtype.Timestamptz{
			Time:  time.Unix(0, 0),
			Valid: true,
		}

		// Poll for new logs every 500ms
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		ctx := c.Request().Context()

		for {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
				// Fetch new logs since last timestamp
				logs, err := dbc.Queries(ctx).GetYtdlpLogsForJobSince(ctx, &db.GetYtdlpLogsForJobSinceParams{
					JobID: jobUUID,
					Since: lastTimestamp,
				})
				if err != nil {
					slog.Error("failed to fetch new ytdlp logs", "error", err)
					continue
				}

				for _, log := range logs {
					// Send each log as an SSE event
					c.Response().Write([]byte("event: log\n"))
					c.Response().Write([]byte("data: "))
					enc := json.NewEncoder(w)
					entry := LogEntry{
						ID:        log.ID,
						Stream:    string(log.Stream),
						Message:   log.Message,
						CreatedAt: log.CreatedAt.Time,
					}
					if err := enc.Encode(entry); err != nil {
						slog.Error("failed to encode log", "error", err)
						continue
					}
					c.Response().Write([]byte("\n\n"))
					flusher.Flush()

					// Update last timestamp
					lastTimestamp = log.CreatedAt
				}

				// Check if job is finished
				currentJob, err := dbc.Queries(ctx).GetDownloadJobByID(ctx, jobUUID)
				if err == nil {
					if currentJob.Status == db.JobStatusSucceeded || currentJob.Status == db.JobStatusFailed {
						// Job is done, send completion event and close
						c.Response().Write([]byte("event: complete\n"))
						c.Response().Write([]byte("data: {\"status\": \"" + string(currentJob.Status) + "\"}\n\n"))
						flusher.Flush()
						return nil
					}
				}
			}
		}
	}
}
