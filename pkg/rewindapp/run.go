// Package rewindapp is the importable Rewind process.
// cmd/web and the private RewindLive binary both call Run.
package rewindapp

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/boot"
	"thirdcoast.systems/rewind/internal/application"
	"thirdcoast.systems/rewind/internal/config"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/download"
	"thirdcoast.systems/rewind/internal/encode"
	"thirdcoast.systems/rewind/internal/ingest"
	"thirdcoast.systems/rewind/internal/sfu"
	"thirdcoast.systems/rewind/pkg/archive"
	"thirdcoast.systems/rewind/pkg/plugin/builtin"
)

const defaultRoles = "web,download,ingest,encode,sfu,migrate"

// Run is the Rewind process (serve / migrate / version). args is typically os.Args[1:].
func Run(args []string) {
	cmd := "serve"
	if len(args) > 0 {
		cmd = args[0]
	}
	switch cmd {
	case "serve":
		serve()
	case "migrate":
		migrate()
	case "version":
		fmt.Println(versionString())
	case "-h", "--help", "help":
		usage(os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		usage(os.Stderr)
		os.Exit(2)
	}
}

func usage(w io.Writer) {
	fmt.Fprintf(w, `Usage: rewind <command>

Commands:
  serve     Run selected roles (default)
  migrate   Apply database migrations and exit
  version   Print version

Roles are selected with REWIND_ROLES (comma-separated).
Default: %s
`, defaultRoles)
}

func versionString() string {
	if bi, ok := debug.ReadBuildInfo(); ok {
		if v := strings.TrimSpace(bi.Main.Version); v != "" && v != "(devel)" {
			return v
		}
		var rev, dirty string
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				rev = s.Value
			case "vcs.modified":
				if s.Value == "true" {
					dirty = "-dirty"
				}
			}
		}
		if rev != "" {
			if len(rev) > 7 {
				rev = rev[:7]
			}
			return rev + dirty
		}
	}
	return "dev"
}

func migrate() {
	slog.Info("Starting database migrator")
	startupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	_, dbc := openDB(startupCtx)
	defer dbc.Close()
	if err := dbc.Migrate(startupCtx); err != nil {
		slog.Error("failed to run PostgreSQL migrations", "error", err)
		os.Exit(1)
	}
	slog.Info("Database migrations completed successfully")
}

func serve() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	roles := parseRoles()
	slog.Info("Starting rewind", "roles", strings.Join(rolesList(roles), ","))

	conf, dbc := openDB(ctx)
	defer dbc.Close()

	builtin.Defaults(auth.NewSessionManager(os.Getenv("SESSION_SECRET")), dbc)
	archive.Bind(dbc)

	if roles["sfu"] && os.Getenv("SFU_SIGNAL_URL") == "" {
		conf.SFUSignalURL = "ws://127.0.0.1:8081/signal"
	}

	if roles["migrate"] {
		if err := dbc.Migrate(ctx); err != nil {
			slog.Error("failed to run PostgreSQL migrations", "error", err)
			os.Exit(1)
		}
		slog.Info("Database migrations completed successfully")
	}

	started := false
	if roles["download"] {
		started = true
		go runRole(ctx, "download", func() error { return download.Start(ctx, dbc, conf) })
	}
	if roles["ingest"] {
		started = true
		go runRole(ctx, "ingest", func() error { return ingest.Start(ctx, dbc, conf) })
	}
	if roles["encode"] {
		started = true
		go runRole(ctx, "encode", func() error { return encode.Start(ctx, dbc, conf) })
	}
	if roles["sfu"] {
		started = true
		go runRole(ctx, "sfu", func() error { return sfu.Start(ctx, dbc, conf) })
	}
	if roles["web"] {
		started = true
		go runRole(ctx, "web", func() error { return boot.StartWeb(ctx, dbc, conf) })
	}
	if !started {
		return
	}
	<-ctx.Done()
	slog.Info("shutting down")
}

func runRole(ctx context.Context, name string, fn func() error) {
	slog.Info("role started", "role", name)
	if err := fn(); err != nil && ctx.Err() == nil {
		slog.Error("role failed", "role", name, "error", err)
		os.Exit(1)
	}
	slog.Info("role stopped", "role", name)
}

func openDB(ctx context.Context) (*config.Config, *db.DatabaseConnection) {
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
	dbc, err := db.NewDatabaseConnection(ctx, pool)
	if err != nil {
		pool.Close()
		slog.Error("failed to create database connection", "error", err)
		os.Exit(1)
	}
	return conf, dbc
}

func parseRoles() map[string]bool {
	raw := strings.TrimSpace(os.Getenv("REWIND_ROLES"))
	if raw == "" {
		raw = defaultRoles
	}
	out := make(map[string]bool)
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(strings.ToLower(part))
		switch part {
		case "downloader":
			part = "download"
		case "encoder":
			part = "encode"
		}
		if part == "" {
			continue
		}
		switch part {
		case "web", "download", "ingest", "encode", "sfu", "migrate":
			out[part] = true
		default:
			slog.Warn("unknown REWIND_ROLES entry", "role", part)
		}
	}
	return out
}

func rolesList(roles map[string]bool) []string {
	order := []string{"migrate", "web", "download", "ingest", "encode", "sfu"}
	var list []string
	for _, name := range order {
		if roles[name] {
			list = append(list, name)
		}
	}
	return list
}
