package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/internal/web"
	"thirdcoast.systems/rewind/internal/application"
	"thirdcoast.systems/rewind/internal/config"
	"thirdcoast.systems/rewind/internal/db"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	slog.Info("Starting web service")

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

	// Initialize encryption manager
	encMgr, err := application.InitEncryptionManager()
	if err != nil {
		slog.Error("failed to initialize encryption manager", "error", err)
		os.Exit(1)
	}

	// Initialize session manager
	sessionMgr := auth.NewSessionManager(os.Getenv("SESSION_SECRET"))

	e, err := web.NewWebserver(ctx, dbc, encMgr, sessionMgr)
	if err != nil {
		slog.Error("failed to create webserver", "error", err)
		os.Exit(1)
	}

	addr := ":" + strconv.Itoa(conf.WebServerPort)

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = e.Shutdown(shutdownCtx)
	}()

	slog.Info("Listening", "addr", addr)
	if err := e.Start(addr); err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		// Echo returns an error on Shutdown; treat it as normal if context is done.
		if ctx.Err() != nil {
			return
		}
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}
