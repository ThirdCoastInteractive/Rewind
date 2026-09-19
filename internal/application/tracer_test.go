package application

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestConfigurePool_WiresTracerAndTsvectorCodec(t *testing.T) {
	cfg, err := pgxpool.ParseConfig("postgres://u:p@localhost:5432/db")
	require.NoError(t, err)

	configurePool(cfg)

	require.NotNil(t, cfg.ConnConfig.Tracer)
	tr, ok := cfg.ConnConfig.Tracer.(*SlowQueryTracer)
	require.True(t, ok)
	require.Equal(t, slowQueryThreshold, tr.Threshold)
	require.NotNil(t, cfg.AfterConnect)
}

func TestSlowQueryTracer_WarnsWhenSlow(t *testing.T) {
	out := captureSlog(t)

	tr := &SlowQueryTracer{Threshold: 50 * time.Millisecond}
	ctx := tr.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{
		SQL:  "SELECT * FROM users WHERE token = $1",
		Args: []any{"super-secret-cookie"},
	})
	start := ctx.Value(slowQueryStartKey{}).(slowQueryStart)
	start.started = time.Now().Add(-80 * time.Millisecond)
	ctx = context.WithValue(ctx, slowQueryStartKey{}, start)
	tr.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})

	logged := out.String()
	require.Contains(t, logged, "slow query")
	require.Contains(t, logged, "SELECT * FROM users WHERE token = $1")
	require.NotContains(t, logged, "super-secret-cookie")
	require.Contains(t, logged, `"duration"`)
}

func TestSlowQueryTracer_SkipsFastQueries(t *testing.T) {
	out := captureSlog(t)

	tr := &SlowQueryTracer{Threshold: time.Hour}
	ctx := tr.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{
		SQL:  "SELECT 1",
		Args: []any{"nope"},
	})
	tr.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})

	require.Empty(t, out.String())
}

func TestSlowQueryTracer_TruncatesSQLAndOmitsArgs(t *testing.T) {
	out := captureSlog(t)

	longSQL := "SELECT " + strings.Repeat("x", slowQuerySQLLimit+64)
	tr := &SlowQueryTracer{Threshold: 50 * time.Millisecond}
	ctx := tr.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{
		SQL:  longSQL,
		Args: []any{"cookie-value", map[string]string{"secret": "hunter2"}},
	})
	start := ctx.Value(slowQueryStartKey{}).(slowQueryStart)
	start.started = time.Now().Add(-80 * time.Millisecond)
	ctx = context.WithValue(ctx, slowQueryStartKey{}, start)
	tr.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})

	var rec map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &rec))
	sql, _ := rec["sql"].(string)
	require.True(t, strings.HasPrefix(sql, "SELECT "))
	require.True(t, strings.HasSuffix(sql, "..."))
	require.LessOrEqual(t, len(sql), slowQuerySQLLimit+3)
	require.NotContains(t, out.String(), "cookie-value")
	require.NotContains(t, out.String(), "hunter2")
	_, hasArgs := rec["args"]
	require.False(t, hasArgs)
}

func TestTruncateSQL(t *testing.T) {
	got := truncateSQL("SELECT\n\t*\nFROM videos", 64)
	require.Equal(t, "SELECT * FROM videos", got)

	got = truncateSQL(strings.Repeat("a", 10), 4)
	require.Equal(t, "aaaa...", got)
}

func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	prev := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}
