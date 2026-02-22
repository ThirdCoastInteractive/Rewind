// package job_api provides download job API handlers.
package job_api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
)

func HandleArchiveBatch(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		_, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return err
		}

		var req struct {
			JobIDs []string `json:"job_ids"`
		}
		if err := c.Bind(&req); err != nil {
			return c.String(400, "invalid json")
		}

		if len(req.JobIDs) == 0 {
			return c.String(400, "no job ids provided")
		}

		// Convert string UUIDs to pgtype.UUID array
		jobUUIDs := make([]pgtype.UUID, len(req.JobIDs))
		for i, idStr := range req.JobIDs {
			var uuid pgtype.UUID
			if err := uuid.Scan(idStr); err != nil {
				return c.String(400, fmt.Sprintf("invalid job id: %s", idStr))
			}

			jobRow, err := dbc.Queries(c.Request().Context()).GetDownloadJobByID(c.Request().Context(), uuid)
			if err != nil || jobRow == nil {
				return c.String(404, fmt.Sprintf("job not found: %s", idStr))
			}

			jobUUIDs[i] = uuid
		}

		if err := dbc.Queries(c.Request().Context()).ArchiveJobs(c.Request().Context(), jobUUIDs); err != nil {
			slog.Error("failed to archive jobs", "count", len(jobUUIDs), "error", err)
			return c.String(500, "failed to archive jobs")
		}

		return c.JSON(200, map[string]any{"archived": len(jobUUIDs)})
	}
}

// HandleStream streams job status updates via SSE.
func HandleStream(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		_, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return err
		}

		w := c.Response().Writer
		flusher, ok := w.(http.Flusher)
		if !ok {
			return c.String(500, "streaming unsupported")
		}

		c.Response().Header().Set(echo.HeaderContentType, "text/event-stream")
		c.Response().Header().Set(echo.HeaderCacheControl, "no-cache")
		c.Response().Header().Set(echo.HeaderConnection, "keep-alive")

		last := map[string]string{}

		send := func(jobID, status, jobsHTML, homeHTML string) error {
			payload := map[string]string{
				"job_id":         jobID,
				"status":         status,
				"jobs_card_html": jobsHTML,
				"home_card_html": homeHTML,
			}
			b, err := json.Marshal(payload)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(w, "event: download_job\n"); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
				return err
			}
			flusher.Flush()
			return nil
		}

		renderJobsCard := func(ctx context.Context, j *db.DownloadJob) (string, error) {
			var buf bytes.Buffer
			if err := templates.DownloadJobCard(j).Render(ctx, &buf); err != nil {
				return "", err
			}
			return buf.String(), nil
		}

		renderHomeCard := func(ctx context.Context, j *db.DownloadJob) (string, error) {
			job := templates.Job{
				ID:        j.ID.String(),
				URL:       j.URL,
				Status:    string(j.Status),
				Refresh:   j.Refresh,
				CreatedAt: j.CreatedAt.Time.Format("Jan 2, 2006 15:04"),
			}
			var buf bytes.Buffer
			if err := templates.JobCard(job).Render(ctx, &buf); err != nil {
				return "", err
			}
			return buf.String(), nil
		}

		// Initial comment so proxies start streaming.
		_, _ = fmt.Fprintf(w, ": connected\n\n")
		flusher.Flush()

		ticker := time.NewTicker(1200 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-c.Request().Context().Done():
				return nil
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(c.Request().Context(), 3*time.Second)
				dbJobs, err := dbc.Queries(ctx).ListRecentDownloadJobs(ctx)
				cancel()
				if err != nil {
					continue
				}

				for _, j := range dbJobs {
					id := j.ID.String()
					fingerprint := fmt.Sprintf("%s|%d|%v|%v|%s|%t|%s",
						j.Status,
						j.Attempts,
						db.NilTimePtr(j.StartedAt),
						db.NilTimePtr(j.FinishedAt),
						func() string {
							if j.LastError != nil {
								return *j.LastError
							}
							return ""
						}(),
						j.Refresh,
						j.UpdatedAt.Time.UTC().Format(time.RFC3339Nano),
					)
					if last[id] == fingerprint {
						continue
					}
					last[id] = fingerprint

					jobsHTML, err := renderJobsCard(c.Request().Context(), j)
					if err != nil {
						continue
					}
					homeHTML, err := renderHomeCard(c.Request().Context(), j)
					if err != nil {
						continue
					}
					_ = send(id, string(j.Status), jobsHTML, homeHTML)
				}
			}
		}
	}
}
