package db

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// DedicatedListen takes a connection out of the shared pool and LISTENs on it.
// Callers must Close the returned conn; it is no longer owned by the pool.
func DedicatedListen(ctx context.Context, dbc *DatabaseConnection, channels ...string) (*pgx.Conn, error) {
	acquired, err := dbc.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	conn := acquired.Hijack()
	for _, ch := range channels {
		if _, err := conn.Exec(ctx, "LISTEN "+pgx.Identifier{ch}.Sanitize()); err != nil {
			_ = conn.Close(ctx)
			return nil, err
		}
	}
	return conn, nil
}

// RunListenLoop reconnects and LISTENs until ctx is done. Notifications do not
// occupy pool slots, so query traffic cannot starve behind a stuck listener.
// Optional onReady callbacks run after each successful LISTEN, including reconnects.
func RunListenLoop(ctx context.Context, dbc *DatabaseConnection, channels []string, onNotify func(*pgconn.Notification), onReady ...func()) {
	for ctx.Err() == nil {
		conn, err := DedicatedListen(ctx, dbc, channels...)
		if err != nil {
			if !waitListenRetry(ctx, time.Second) {
				return
			}
			continue
		}
		// A fresh snapshot after LISTEN closes both startup and reconnect gaps.
		for _, ready := range onReady {
			ready()
		}
		for ctx.Err() == nil {
			n, err := conn.WaitForNotification(ctx)
			if err != nil {
				break
			}
			if onNotify != nil {
				onNotify(n)
			}
		}
		_ = conn.Close(context.Background())
		if !waitListenRetry(ctx, time.Second) {
			return
		}
	}
}

func waitListenRetry(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
