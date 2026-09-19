// Command sfu is Rewind's Selective Forwarding Unit: a standalone WebRTC media
// router for producer sessions. It shares the project's config and database
// (for director election) but handles only media; all UI state lives in the web
// service's SSE hubs.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"thirdcoast.systems/rewind/internal/sfu"
	"thirdcoast.systems/rewind/internal/application"
	"thirdcoast.systems/rewind/internal/config"
	"thirdcoast.systems/rewind/internal/db"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	slog.Info("Starting SFU service")

	conf, err := config.LoadConfig(ctx)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	if conf.DatabaseRetries <= 0 {
		conf.DatabaseRetries = 10
	}

	pool, err := application.OpenDBPoolWithRetry(ctx, *conf)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	dbc, err := db.NewDatabaseConnection(ctx, pool)
	if err != nil {
		slog.Error("failed to create database connection", "error", err)
		os.Exit(1)
	}
	defer dbc.Close()

	if err := sfu.Start(ctx, dbc, conf); err != nil {
		slog.Error("SFU server failed", "error", err)
		os.Exit(1)
	}
}
