package admin

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
)

func HandleAdminAssetHealthPage(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		username, _ := c.Get("currentUsername").(string)
		ctx := c.Request().Context()
		q := dbc.Queries(ctx)

		alertType := ""
		alertMsg := ""
		if errMsg := c.QueryParam("err"); errMsg != "" {
			alertType = "error"
			alertMsg = errMsg
		} else if msg := c.QueryParam("msg"); msg != "" {
			alertType = "success"
			alertMsg = msg
		}

		count, err := q.CountVideosWithAssetErrors(ctx)
		if err != nil {
			slog.Error("failed to count videos with asset errors", "error", err)
			return templates.AdminAssetHealth(username, 0, nil, "error", "Failed to load asset errors.").Render(ctx, c.Response().Writer)
		}

		var rows []*templates.AdminAssetErrorRow
		if count > 0 {
			dbRows, err := q.ListVideosWithAssetErrors(ctx, 200)
			if err != nil {
				slog.Error("failed to list videos with asset errors", "error", err)
				return templates.AdminAssetHealth(username, count, nil, "error", "Failed to load error details.").Render(ctx, c.Response().Writer)
			}
			rows = make([]*templates.AdminAssetErrorRow, 0, len(dbRows))
			for _, r := range dbRows {
				row := &templates.AdminAssetErrorRow{
					ID:             r.ID,
					Title:          r.Title,
					ErrorCount:     extractInt(r.AssetsStatus, "_error_count"),
					LastErrorAt:    formatErrorTime(r.AssetsStatus, "_last_error_at"),
					Errors:         extractErrorMap(r.AssetsStatus, "_errors"),
					AssetsBooleans: extractBoolMap(r.AssetsStatus),
				}
				rows = append(rows, row)
			}
		}

		return templates.AdminAssetHealth(username, count, rows, alertType, alertMsg).Render(ctx, c.Response().Writer)
	}
}

func HandleAdminAssetHealthRetry(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		videoID := c.Param("id")
		ctx := c.Request().Context()
		q := dbc.Queries(ctx)

		var uuid pgtype.UUID
		if err := uuid.Scan(videoID); err != nil {
			return c.Redirect(302, "/admin/asset-health?err=Invalid video ID")
		}

		if err := q.ClearVideoAssetErrors(ctx, uuid); err != nil {
			slog.Error("failed to clear video asset errors", "error", err, "video_id", videoID)
			return c.Redirect(302, "/admin/asset-health?err=Failed to clear errors")
		}

		return c.Redirect(302, fmt.Sprintf("/admin/asset-health?msg=Cleared errors for %s - will retry on next catchup cycle", videoID))
	}
}

func HandleAdminAssetHealthRetryAll(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()
		q := dbc.Queries(ctx)

		if err := q.ClearAllVideoAssetErrors(ctx); err != nil {
			slog.Error("failed to clear all video asset errors", "error", err)
			return c.Redirect(302, "/admin/asset-health?err=Failed to clear errors")
		}

		return c.Redirect(302, "/admin/asset-health?msg=Cleared all asset errors - videos will retry on next catchup cycle")
	}
}

// Helper functions to extract typed values from AssetMap (map[string]any).

func extractInt(m db.AssetMap, key string) int {
	if m == nil {
		return 0
	}
	v, ok := m[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return 0
	}
}

func formatErrorTime(m db.AssetMap, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}
	return t.Format("2006-01-02 15:04")
}

func extractErrorMap(m db.AssetMap, key string) map[string]string {
	if m == nil {
		return nil
	}
	v, ok := m[key]
	if !ok {
		return nil
	}
	raw, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	result := make(map[string]string, len(raw))
	for k, val := range raw {
		if s, ok := val.(string); ok {
			result[k] = s
		} else {
			result[k] = fmt.Sprintf("%v", val)
		}
	}
	return result
}

func extractBoolMap(m db.AssetMap) map[string]bool {
	if m == nil {
		return nil
	}
	result := make(map[string]bool)
	for k, v := range m {
		// Skip internal error-tracking keys.
		if k == "_error_count" || k == "_last_error_at" || k == "_errors" {
			continue
		}
		if b, ok := v.(bool); ok {
			result[k] = b
		}
	}
	return result
}
