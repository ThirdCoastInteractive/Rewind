//go:build integration

package integration

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"sync"
	"testing"
	"thirdcoast.systems/rewind/internal/agent"
	"thirdcoast.systems/rewind/internal/application"
	"thirdcoast.systems/rewind/internal/config"
	"thirdcoast.systems/rewind/internal/db"
	"time"
)

func TestDelegatedMessageDeduplicatesConcurrentSubmissions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := application.OpenDBPoolWithRetry(ctx, config.Config{DatabaseDSN: "postgres://rewind_test:disposable-test-only@127.0.0.1:15439/rewind_test?sslmode=disable", DatabaseRetries: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	dbc := &db.DatabaseConnection{Pool: pool}
	if err = dbc.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	user := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	if _, err = pool.Exec(ctx, "INSERT INTO users(id,user_name,email,password,enabled) VALUES($1,$2,$2,'fixture',true)", user, user.String()); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	ids := make(chan string, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			run, err := agent.SubmitDelegated(ctx, dbc, user, "same-message", "Find archive evidence")
			if err != nil {
				t.Error(err)
				return
			}
			ids <- run.ID.String()
		}()
	}
	wg.Wait()
	close(ids)
	first := ""
	count := 0
	for id := range ids {
		count++
		if first != "" && first != id {
			t.Fatal("duplicate run created")
		}
		first = id
	}
	if count != 6 {
		t.Fatal("submission failed")
	}
	if _, err = agent.SubmitDelegated(ctx, dbc, user, "same-message", "Different mutation"); err == nil {
		t.Fatal("changed request reused identity")
	}
	if _, err = pool.Exec(ctx, "UPDATE users SET enabled=false WHERE id=$1", user); err != nil {
		t.Fatal(err)
	}
	if _, err = agent.SubmitDelegated(ctx, dbc, user, "second-message", "Find archive evidence"); err == nil {
		t.Fatal("disabled account submitted task")
	}
}
