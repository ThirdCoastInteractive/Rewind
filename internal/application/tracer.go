package application

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	slowQueryThreshold = 50 * time.Millisecond
	slowQuerySQLLimit  = 256
)

type slowQueryStartKey struct{}

type slowQueryStart struct {
	started time.Time
	sql     string
}

// SlowQueryTracer is a pgx QueryTracer that slog-warns queries slower than Threshold.
// SQL is truncated; query parameters are never logged.
type SlowQueryTracer struct {
	Threshold time.Duration
}

func (t *SlowQueryTracer) threshold() time.Duration {
	if t == nil || t.Threshold <= 0 {
		return slowQueryThreshold
	}
	return t.Threshold
}

func (t *SlowQueryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, slowQueryStartKey{}, slowQueryStart{
		started: time.Now(),
		sql:     data.SQL,
	})
}

func (t *SlowQueryTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	start, ok := ctx.Value(slowQueryStartKey{}).(slowQueryStart)
	if !ok {
		return
	}
	dur := time.Since(start.started)
	if dur < t.threshold() {
		return
	}
	slog.Warn("slow query", "duration", dur, "sql", truncateSQL(start.sql, slowQuerySQLLimit))
}

func truncateSQL(sql string, limit int) string {
	sql = strings.Join(strings.Fields(sql), " ")
	if limit <= 0 || len(sql) <= limit {
		return sql
	}
	return sql[:limit] + "..."
}
