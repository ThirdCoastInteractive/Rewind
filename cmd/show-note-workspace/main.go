package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"thirdcoast.systems/rewind/internal/application"
	"thirdcoast.systems/rewind/internal/config"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/shownote"
)

func main() {
	preflightOnly := flag.Bool("preflight", false, "verify migration coverage without changing show notes")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	conf, err := config.LoadConfig(ctx)
	if err != nil {
		exitError("load config", err)
	}
	pool, err := application.OpenDBPoolWithRetry(ctx, *conf)
	if err != nil {
		exitError("connect to database", err)
	}
	defer pool.Close()
	dbc, err := db.NewDatabaseConnection(ctx, pool)
	if err != nil {
		exitError("initialize database", err)
	}
	defer dbc.Close()
	if err := dbc.Migrate(ctx); err != nil {
		exitError("run database migrations", err)
	}

	report := shownote.WorkspacePreflight(ctx, dbc)
	if !*preflightOnly {
		report = shownote.MigrateAllWorkspaces(ctx, dbc)
		if report.Failed == 0 {
			report = shownote.WorkspacePreflight(ctx, dbc)
		}
	}
	fmt.Printf("show workspace: total=%d migrated=%d pending=%d failed=%d ready=%t\n", report.Total, report.Migrated, report.Pending, report.Failed, report.CanEnable)
	for _, failure := range report.Failures {
		fmt.Printf("  - %s\n", failure)
	}
	if !report.CanEnable {
		os.Exit(1)
	}
}

func exitError(action string, err error) {
	slog.Error(action, "error", err)
	os.Exit(1)
}
