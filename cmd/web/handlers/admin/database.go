package admin

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/utils/format"
)

const (
	activitySQL = `
SELECT COALESCE(state, '(none)') AS state, count(*)::bigint
FROM pg_stat_activity
GROUP BY state
ORDER BY count(*) DESC`

	extensionSQL = `SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname = 'pg_stat_statements')`

	statsResetSQL = `SELECT stats_reset FROM pg_stat_statements_info`

	resetStatementsSQL = `SELECT pg_stat_statements_reset()`
)

// HandleAdminDatabasePage serves GET /admin/database with pool, activity, and statement stats.
func HandleAdminDatabasePage(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		username, _ := c.Get("currentUsername").(string)
		ctx := c.Request().Context()
		page := loadDatabasePage(ctx, dbc, c.QueryParam("sort"), c.QueryParam("noise") == "1")
		if errMsg := c.QueryParam("err"); errMsg != "" {
			page.AlertType = "error"
			page.AlertMsg = errMsg
		} else if msg := c.QueryParam("msg"); msg != "" {
			page.AlertType = "success"
			page.AlertMsg = msg
		}
		return templates.AdminDatabase(username, page).Render(ctx, c.Response().Writer)
	}
}

// HandleAdminDatabaseResetStatements serves POST /admin/database/reset-statements.
func HandleAdminDatabaseResetStatements(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		if dbc == nil || dbc.Pool == nil {
			return c.Redirect(302, "/admin/database?err="+url.QueryEscape("Database is unavailable"))
		}
		if _, err := dbc.Exec(c.Request().Context(), resetStatementsSQL); err != nil {
			slog.Error("pg_stat_statements_reset failed", "error", err)
			return c.Redirect(302, "/admin/database?err="+url.QueryEscape("Could not clear statement stats"))
		}
		return c.Redirect(302, "/admin/database?msg="+url.QueryEscape("Statement stats cleared"))
	}
}

func statementsOrder(sort string) string {
	switch strings.ToLower(strings.TrimSpace(sort)) {
	case "mean":
		return "mean_exec_time DESC, calls DESC"
	case "calls":
		return "calls DESC, total_exec_time DESC"
	default:
		return "total_exec_time DESC, calls DESC"
	}
}

func isUtilityStatement(query string) bool {
	q := strings.TrimSpace(strings.ToLower(query))
	switch {
	case strings.HasPrefix(q, "rollback"),
		strings.HasPrefix(q, "begin"),
		strings.HasPrefix(q, "commit"),
		strings.HasPrefix(q, "vacuum"),
		strings.HasPrefix(q, "explain"),
		strings.HasPrefix(q, "alter "),
		strings.HasPrefix(q, "create "),
		strings.Contains(q, "pg_stat_statements"):
		return true
	default:
		return false
	}
}

func loadDatabasePage(ctx context.Context, dbc *db.DatabaseConnection, sort string, showNoise bool) templates.AdminDatabasePage {
	page := templates.AdminDatabasePage{Sort: sort, ShowNoise: showNoise}
	if page.Sort != "mean" && page.Sort != "calls" {
		page.Sort = "total"
	}
	if dbc == nil || dbc.Pool == nil {
		page.StatementsMissing = true
		return page
	}

	stat := dbc.Stat()
	page.TotalConns = stat.TotalConns()
	page.IdleConns = stat.IdleConns()
	page.AcquiredConns = stat.AcquiredConns()

	rows, err := dbc.Query(ctx, activitySQL)
	if err != nil {
		slog.Error("pg_stat_activity query failed", "error", err)
	} else {
		defer rows.Close()
		for rows.Next() {
			var row templates.AdminActivityCount
			if scanErr := rows.Scan(&row.State, &row.Count); scanErr != nil {
				slog.Error("pg_stat_activity scan failed", "error", scanErr)
				continue
			}
			page.Activity = append(page.Activity, row)
		}
		if err = rows.Err(); err != nil {
			slog.Error("pg_stat_activity rows failed", "error", err)
		}
	}

	var installed bool
	if err = dbc.QueryRow(ctx, extensionSQL).Scan(&installed); err != nil {
		slog.Error("pg_stat_statements extension check failed", "error", err)
		page.StatementsMissing = true
		return page
	}
	if !installed {
		page.StatementsMissing = true
		return page
	}

	var resetAt time.Time
	if err = dbc.QueryRow(ctx, statsResetSQL).Scan(&resetAt); err == nil && !resetAt.IsZero() {
		page.StatsReset = resetAt.UTC().Format("2006-01-02 15:04 UTC")
	}

	fetch := 80
	if showNoise {
		fetch = 40
	}
	stmtSQL := `SELECT query, calls, total_exec_time, mean_exec_time, rows
FROM pg_stat_statements
ORDER BY ` + statementsOrder(page.Sort) + `
LIMIT $1`
	stmtRows, err := dbc.Query(ctx, stmtSQL, fetch)
	if err != nil {
		if statementsUnavailable(err) {
			page.StatementsMissing = true
			return page
		}
		slog.Error("pg_stat_statements query failed", "error", err)
		page.StatementsMissing = true
		return page
	}
	defer stmtRows.Close()
	for stmtRows.Next() {
		var row templates.AdminStatementRow
		if scanErr := stmtRows.Scan(&row.Query, &row.Calls, &row.TotalTime, &row.MeanTime, &row.Rows); scanErr != nil {
			slog.Error("pg_stat_statements scan failed", "error", scanErr)
			continue
		}
		row.Query = format.Truncate(strings.Join(strings.Fields(row.Query), " "), 200)
		if !showNoise && isUtilityStatement(row.Query) {
			continue
		}
		page.Statements = append(page.Statements, row)
		if len(page.Statements) >= 40 {
			break
		}
	}
	if err = stmtRows.Err(); err != nil {
		if statementsUnavailable(err) {
			page.StatementsMissing = true
			return page
		}
		slog.Error("pg_stat_statements rows failed", "error", err)
	}
	return page
}

func statementsUnavailable(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "shared_preload_libraries") {
		return true
	}
	if strings.Contains(msg, "pg_stat_statements") {
		return true
	}
	var pe interface{ SQLState() string }
	if errors.As(err, &pe) && pe.SQLState() == "42P01" {
		return true
	}
	return false
}
