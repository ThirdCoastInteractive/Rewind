// Package boot starts the HTTP server. It lives under cmd/web so it can import
// cmd/web/internal/web, and is importable from pkg/rewindapp.
package boot

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"log/slog"

	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/internal/web"
	"thirdcoast.systems/rewind/internal/application"
	"thirdcoast.systems/rewind/internal/config"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/plugin"
	"thirdcoast.systems/rewind/pkg/plugin/builtin"
)

// StartWeb serves the Rewind UI. Plugins must already be Use'd, or Defaults fill gaps.
func StartWeb(ctx context.Context, dbc *db.DatabaseConnection, conf *config.Config) error {
	encMgr, err := application.InitEncryptionManager()
	if err != nil {
		return fmt.Errorf("initialize encryption manager: %w", err)
	}

	sessionMgr := auth.NewSessionManager(os.Getenv("SESSION_SECRET"))
	builtin.Defaults(sessionMgr, dbc)
	if plugin.Auth() == nil || plugin.Blobs() == nil {
		return fmt.Errorf("plugin auth and blob are required")
	}

	e, err := web.NewWebserver(ctx, dbc, encMgr, sessionMgr)
	if err != nil {
		return fmt.Errorf("create webserver: %w", err)
	}
	if live := plugin.LiveIngest(); live != nil {
		live.Mount(e.Echo)
	}
	if a := plugin.Auth(); a != nil {
		a.Mount(e.Echo)
	}

	port := conf.WebServerPort
	if port == 0 {
		port = 8080
	}
	addr := ":" + strconv.Itoa(port)

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = e.Shutdown(shutdownCtx)
	}()

	slog.Info("Listening", "addr", addr)
	if err := e.Start(addr); err != nil {
		if errors.Is(err, http.ErrServerClosed) || errors.Is(err, context.Canceled) || ctx.Err() != nil {
			return nil
		}
		return err
	}
	return nil
}
