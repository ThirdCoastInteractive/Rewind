//go:build releaseaudit

package integration

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"thirdcoast.systems/rewind/internal/db"
)

// This release gate uses only the disposable integration database. A separate
// schema installs the actual migrations shipped by v0.0.3, which omitted 37–39.
func TestV003MigrationLedgerUpgrade(t *testing.T) {
	ctx := context.Background()
	const dsn = "postgres://rewind_test:disposable-test-only@127.0.0.1:15439/rewind_test?sslmode=disable"
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := "release_audit_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	// Shadow the integration database's public ledger before adding public to
	// the search path for extension types and operators.
	if _, err := pool.Exec(ctx, `CREATE TABLE goose_db_version (id bigserial PRIMARY KEY, version_id bigint NOT NULL, is_applied boolean NOT NULL, tstamp timestamp NOT NULL DEFAULT now()); INSERT INTO goose_db_version(version_id,is_applied) VALUES(0,true)`); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) []byte {
		cmd := exec.Command("git", append([]string{"-c", "safe.directory=" + filepath.ToSlash(root)}, args...)...)
		cmd.Dir = root
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("read released migrations: %v", err)
		}
		return out
	}
	dir := t.TempDir()
	for _, file := range strings.Fields(string(git("ls-tree", "-r", "--name-only", "v0.0.3", "internal/db/sql/migrations"))) {
		if err := os.WriteFile(filepath.Join(dir, filepath.Base(file)), git("show", "v0.0.3:"+file), 0600); err != nil {
			t.Fatal(err)
		}
	}
	goose.SetBaseFS(os.DirFS(dir))
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()
	if err := goose.UpContext(ctx, sqlDB, "."); err != nil {
		t.Fatal(err)
	}
	// This saved producer state must survive retiring the old feature.
	if _, err := pool.Exec(ctx, `INSERT INTO player_sessions(session_code,producer_id,state,expires_at) VALUES('123456',gen_random_uuid(),'{"saved":"scene"}',now()+interval '1 day')`); err != nil {
		t.Fatal(err)
	}
	err = (&db.DatabaseConnection{Pool: pool}).Migrate(ctx)
	if err != nil {
		t.Fatal(fmt.Errorf("v0.0.3 upgrade rejected before applying new schema: %w", err))
	}
	var saved string
	if err := pool.QueryRow(ctx, "SELECT state->>'saved' FROM player_sessions WHERE session_code='123456'").Scan(&saved); err != nil || saved != "scene" {
		t.Fatalf("legacy scene lost: %q %v", saved, err)
	}
	if err := (&db.DatabaseConnection{Pool: pool}).Migrate(ctx); err != nil {
		t.Fatalf("repeat upgrade: %v", err)
	}
}
