package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"thirdcoast.systems/rewind/internal/application"
	"thirdcoast.systems/rewind/internal/config"
	"thirdcoast.systems/rewind/internal/db"
)

func main() {
	slog.Info("Starting database migrator service")

	startupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	conf, err := config.LoadConfig(startupCtx)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Connect to database with retry logic
	pool, err := application.OpenDBPoolWithRetry(startupCtx, *conf)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	slog.Info("Database pool connection established")

	// Create database connection
	databaseConnection, err := db.NewDatabaseConnection(startupCtx, pool)
	if err != nil {
		slog.Error("failed to create database connection", "error", err)
		os.Exit(1)
	}
	defer databaseConnection.Close()
	slog.Info("Database connection established")

	// Run migrations
	err = databaseConnection.Migrate(startupCtx)
	if err != nil {
		slog.Error("failed to run PostgreSQL migrations", "error", err)
		os.Exit(1)
	}

	slog.Info("Database migrations completed successfully")
}
