package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestStatementsUnavailable(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errors.New("connection refused"), false},
		{errors.New("pg_stat_statements must be loaded via shared_preload_libraries"), true},
		{errors.New(`relation "pg_stat_statements" does not exist`), true},
		{sqlStateError{msg: `relation "foo" does not exist`, state: "42P01"}, true},
		{sqlStateError{msg: "unique violation", state: "23505"}, false},
	}
	for _, tc := range cases {
		if got := statementsUnavailable(tc.err); got != tc.want {
			t.Errorf("statementsUnavailable(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

func TestLoadDatabasePage_NilPool(t *testing.T) {
	page := loadDatabasePage(context.Background(), nil, "total", false)
	if !page.StatementsMissing {
		t.Fatal("expected statements missing when pool is nil")
	}
	if page.TotalConns != 0 || page.IdleConns != 0 || page.AcquiredConns != 0 {
		t.Fatalf("expected zero pool stats, got %+v", page)
	}
}

func TestStatementsOrder(t *testing.T) {
	if got := statementsOrder("mean"); !strings.HasPrefix(got, "mean_exec_time") {
		t.Fatal(got)
	}
	if got := statementsOrder("calls"); !strings.HasPrefix(got, "calls") {
		t.Fatal(got)
	}
	if got := statementsOrder("drop table"); !strings.HasPrefix(got, "total_exec_time") {
		t.Fatal(got)
	}
}

func TestIsUtilityStatement(t *testing.T) {
	if !isUtilityStatement("rollback") || !isUtilityStatement("VACUUM (ANALYZE) video_comments") {
		t.Fatal("expected utilities")
	}
	if isUtilityStatement("-- name: GetDashboardOverview :one SELECT") {
		t.Fatal("application query treated as utility")
	}
}

func TestHandleAdminDatabaseResetStatements_NilDB(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/admin/database/reset-statements", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	if err := HandleAdminDatabaseResetStatements(nil, nil)(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusFound {
		t.Fatalf("status %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "err=") {
		t.Fatalf("location %s", loc)
	}
}

func TestHandleAdminDatabasePage_NilDB(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/admin/database", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("currentUsername", "admin")

	if err := HandleAdminDatabasePage(nil, nil)(c); err != nil {
		t.Fatal(err)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "DATABASE") {
		t.Fatalf("missing heading: %s", body)
	}
	if !strings.Contains(body, "restart postgres") {
		t.Fatalf("missing extension note: %s", body)
	}
	if !strings.Contains(body, "POOL TOTAL") {
		t.Fatalf("missing pool stats: %s", body)
	}
}

type sqlStateError struct {
	msg   string
	state string
}

func (e sqlStateError) Error() string    { return e.msg }
func (e sqlStateError) SQLState() string { return e.state }
