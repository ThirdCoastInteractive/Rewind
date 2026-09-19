package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"thirdcoast.systems/rewind/internal/application"
	"thirdcoast.systems/rewind/internal/config"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/encode"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	slog.Info("Starting encoder service")

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

	if err := encode.Start(ctx, dbc, conf); err != nil {
		slog.Error("encoder service failed", "error", err)
		os.Exit(1)
	}
}
