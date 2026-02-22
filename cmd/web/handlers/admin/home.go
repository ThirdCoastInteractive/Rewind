package admin

import (
	"encoding/json"
	"log/slog"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/utils/format"
)

func HandleAdminHomePage(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		username, _ := c.Get("currentUsername").(string)
		alertType := ""
		alertMsg := ""
		if errMsg := c.QueryParam("err"); errMsg != "" {
			alertType = "error"
			alertMsg = errMsg
		} else if msg := c.QueryParam("msg"); msg != "" {
			alertType = "success"
			alertMsg = msg
		}

		ctx := c.Request().Context()
		q := dbc.Queries(ctx)

		// Fetch dashboard overview
		var metrics templates.DashboardMetrics
		overview, err := q.GetDashboardOverview(ctx)
		if err != nil {
			slog.Error("failed to get dashboard overview", "error", err)
		} else {
			metrics.TotalVideos = overview.TotalVideos
			metrics.TotalClips = overview.TotalClips
			metrics.TotalMarkers = overview.TotalMarkers
			metrics.TotalUsers = overview.TotalUsers
			metrics.TotalComments = overview.TotalComments
			metrics.TotalStorageBytes = overview.TotalStorageBytes
			metrics.TotalDurationSeconds = overview.TotalDurationSeconds
			metrics.StorageDisplay = format.Bytes(overview.TotalStorageBytes)
			metrics.DurationDisplay = formatDurationDays(overview.TotalDurationSeconds)
		}

		// Fetch job statuses
		var jobStatuses []templates.JobStatusRow
		jobRows, err := q.GetJobStatusCounts(ctx)
		if err != nil {
			slog.Error("failed to get job status counts", "error", err)
		} else {
			for _, r := range jobRows {
				jobStatuses = append(jobStatuses, templates.JobStatusRow{
					JobType: r.JobType,
					Status:  r.Status,
					Count:   r.Count,
				})
			}
		}

		// Fetch chart data
		chartData := templates.DashboardChartData{}

		videosPerDay, err := q.GetVideosPerDay(ctx, 30)
		if err != nil {
			slog.Error("failed to get videos per day", "error", err)
		} else {
			for _, r := range videosPerDay {
				chartData.VideosPerDay = append(chartData.VideosPerDay, templates.DayCount{
					Day:   r.Day.Time.Format("2006-01-02"),
					Count: r.Count,
				})
			}
		}

		topSources, err := q.GetTopSources(ctx)
		if err != nil {
			slog.Error("failed to get top sources", "error", err)
		} else {
			for _, r := range topSources {
				chartData.TopSources = append(chartData.TopSources, templates.SourceCount{
					Source: r.Source,
					Count:  r.Count,
				})
			}
		}

		storageByUploader, err := q.GetStorageByUploader(ctx)
		if err != nil {
			slog.Error("failed to get storage by uploader", "error", err)
		} else {
			for _, r := range storageByUploader {
				chartData.StorageByUploader = append(chartData.StorageByUploader, templates.UploaderStorage{
					Uploader:   r.Uploader,
					TotalBytes: r.TotalBytes,
				})
			}
		}

		chartJSON, err := json.Marshal(chartData)
		if err != nil {
			slog.Error("failed to marshal chart data", "error", err)
			chartJSON = []byte("{}")
		}
		metrics.ChartDataJSON = string(chartJSON)

		return templates.AdminHome(username, alertType, alertMsg, metrics, jobStatuses).Render(c.Request().Context(), c.Response())
	}
}

// formatDurationDays formats total seconds into "Xd Yh" or "Yh Zm" display.
func formatDurationDays(totalSeconds int64) string {
	if totalSeconds <= 0 {
		return "0h"
	}
	days := totalSeconds / 86400
	hours := (totalSeconds % 86400) / 3600
	minutes := (totalSeconds % 3600) / 60

	if days > 0 {
		return format.Itoa64(days) + "d " + format.Itoa64(hours) + "h"
	}
	if hours > 0 {
		return format.Itoa64(hours) + "h " + format.Itoa64(minutes) + "m"
	}
	return format.Itoa64(minutes) + "m"
}

// HandleAdminSettingsPage redirects to the main settings page.
