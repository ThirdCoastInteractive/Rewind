package application

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"thirdcoast.systems/rewind/internal/config"
)

var (
	dbOpenBackoffBase  = 1 * time.Second
	dbOpenBackoffScale = 1.618
)

// OpenDBPoolWithRetry initializes a new PostgreSQL connection pool with retry logic.
func OpenDBPoolWithRetry(ctx context.Context, conf config.Config) (*pgxpool.Pool, error) {
	var pool *pgxpool.Pool
	var lastErr error

	cfg, err := pgxpool.ParseConfig(conf.DatabaseDSN)
	if err != nil {
		return nil, fmt.Errorf("failed to parse DSN: %w", err)
	}

	fmt.Printf("Connecting to database at %s\n", cfg.ConnConfig.Host)
	for i := 0; i < conf.DatabaseRetries; i++ {
		if pool, err = pgxpool.NewWithConfig(ctx, cfg); err == nil {
			break
		}
		lastErr = err

		backoff := time.Duration(float64(dbOpenBackoffBase) * math.Pow(dbOpenBackoffScale, float64(i)))
		fmt.Printf("Retrying in %v...\n", backoff)
		time.Sleep(backoff)
	}

	if pool == nil {
		if lastErr != nil {
			return nil, fmt.Errorf("failed to connect to database after multiple attempts: %w", lastErr)
		}
		return nil, fmt.Errorf("failed to connect to database after multiple attempts")
	}

	fmt.Printf("\nConnected to database at %s\n", cfg.ConnConfig.Host)
	fmt.Printf("Testing connection to database...\n")
	for i := 0; i < conf.DatabaseRetries; i++ {
		pingCtx, cancel := context.WithTimeout(context.Background(), 1*time.Second)

		if err = pool.Ping(pingCtx); err == nil {
			cancel()
			fmt.Printf("Pinged to database at %s\n", cfg.ConnConfig.Host)
			return pool, nil
		}
		cancel()
		lastErr = err

		backoff := time.Duration(float64(dbOpenBackoffBase) * math.Pow(dbOpenBackoffScale, float64(i)))
		fmt.Printf("Retrying in %v...\n", backoff)
		time.Sleep(backoff)
	}
	if lastErr != nil {
		return nil, fmt.Errorf("failed to ping database after multiple attempts: %w", lastErr)
	}
	return nil, fmt.Errorf("failed to ping database after multiple attempts")
}
