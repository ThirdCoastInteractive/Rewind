package job_api

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	"github.com/a-h/templ"
	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/events"
)

// HandleIndex sends an initial snapshot, then pushes committed job changes via LISTEN/NOTIFY.
func HandleIndex(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := common.RequireSessionUser(c, sm); err != nil {
			return err
		}
		// Subscribe before reading, so commits during the snapshot cannot be lost.
		wake, unsubscribe := events.Default.Subscribe("jobs_ui")
		defer unsubscribe()
		common.SetSSEHeaders(c)
		sse := datastar.NewSSE(c.Response().Writer, c.Request())
		last := map[string]string{}
		refresh := func() error {
			ctx, cancel := context.WithTimeout(c.Request().Context(), 5*time.Second)
			defer cancel()
			q := dbc.Queries(ctx)
			jobs, err := q.ListRecentDownloadJobs(ctx)
			if err != nil {
				return err
			}
			ml, err := q.ListRecentMLJobs(ctx)
			if err != nil {
				return err
			}
			health, err := q.ListMLRuntimeHealth(ctx)
			if err != nil {
				return err
			}
			counts, err := q.CountMLJobs(ctx)
			if err != nil {
				return err
			}
			for _, fragment := range []struct {
				key       string
				component templ.Component
			}{
				{"downloads", templates.JobsList(jobs)},
				{"summary", templates.JobsSummary(jobs)},
				{"ml", templates.MLJobsList(ml, health, counts, "")},
			} {
				var buf bytes.Buffer
				if err := fragment.component.Render(ctx, &buf); err != nil {
					return err
				}
				html := buf.String()
				if html != last[fragment.key] {
					if err := sse.PatchElements(html); err != nil {
						return err
					}
					last[fragment.key] = html
				}
			}
			b, _ := json.Marshal(map[string]any{"jobsLastSync": time.Now().UnixMilli(), "jobsHeartbeat": time.Now().UnixMilli()})
			return sse.PatchSignals(b)
		}
		if err := refresh(); err != nil {
			return err
		}
		if c.QueryParam("live") != "1" {
			return nil
		}
		heartbeat := time.NewTicker(15 * time.Second)
		defer heartbeat.Stop()
		for {
			select {
			case <-c.Request().Context().Done():
				return nil
			case <-wake:
				if err := refresh(); err != nil {
					return err
				}
			case <-heartbeat.C:
				// Transport liveness only: no database queries or job refresh.
				b, _ := json.Marshal(map[string]any{"jobsHeartbeat": time.Now().UnixMilli()})
				if err := sse.PatchSignals(b); err != nil {
					return nil
				}
			}
		}
	}
}

func jobActionResponse(c echo.Context, payload map[string]any, notice string) error {
	if c.QueryParam("render") != "1" {
		return c.JSON(200, payload)
	}
	b, _ := json.Marshal(map[string]any{"jobsNotice": notice, "job_ids": []string{}})
	return datastar.NewSSE(c.Response(), c.Request()).PatchSignals(b)
}
