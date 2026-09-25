package sfu

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"thirdcoast.systems/rewind/internal/config"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/turn"
)

const defaultSFUPort = 8081

// Start constructs the SFU and serves HTTP until ctx is cancelled or the
// listener exits. Signaling binds 127.0.0.1 by default (web proxies to
// loopback). Set SFU_LISTEN_HOST=0.0.0.0 only for a dedicated SFU container
// that other Compose services must reach. ICE/media still use UDP 50000-50100.
func Start(ctx context.Context, dbc *db.DatabaseConnection, conf *config.Config, providers ...*turn.Provider) error {
	port := conf.SFUPort
	if port == 0 {
		port = defaultSFUPort
	}

	srv, err := NewServer(dbc, conf, providers...)
	if err != nil {
		return err
	}

	host := strings.TrimSpace(os.Getenv("SFU_LISTEN_HOST"))
	if host == "" {
		host = "127.0.0.1"
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdownCtx)
	}()

	slog.Info("SFU listening", "addr", addr)
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
