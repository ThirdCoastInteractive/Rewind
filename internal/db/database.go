package db

import (
	"context"
	"embed"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

type DatabaseConnection struct {
	*pgxpool.Pool
}

const DBRetryCount = 15

// NewDatabaseConnection creates a new database connection
func NewDatabaseConnection(ctx context.Context, pool *pgxpool.Pool) (*DatabaseConnection, error) {
	for i := range DBRetryCount {
		err := pool.Ping(ctx)
		if err == nil {
			return &DatabaseConnection{pool}, nil
		}

		fmt.Printf("could not ping the database: %v\n", err)

		// Golden ratio backoff
		fib := 1.61803398875
		sleep := time.Duration((float64(i) * fib)) * time.Second
		fmt.Printf("could not connect to database, retrying in %s\n", sleep)
		time.Sleep(sleep)
	}

	return nil, fmt.Errorf("could not connect to database after %d retries", DBRetryCount)
}

// Close closes the database connection
func (db *DatabaseConnection) Close() {
	db.Pool.Close()
}

func (db *DatabaseConnection) Queries(ctx context.Context) *Queries {
	return New(db)
}

func (db *DatabaseConnection) NewWithTX(ctx context.Context) (*Queries, pgx.Tx, error) {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to begin transaction: %w", err)
	}

	return New(tx), tx, nil
}

//go:embed sql/migrations/*.sql
var embedMigrations embed.FS

// Migrate runs the goose migrations
func (db *DatabaseConnection) Migrate(ctx context.Context) error {
	goose.SetBaseFS(embedMigrations)

	err := goose.SetDialect("postgres")
	if err != nil {
		return err
	}

	stdDb := stdlib.OpenDBFromPool(db.Pool)
	defer stdDb.Close()

	currentVersion, err := goose.GetDBVersionContext(ctx, stdDb)
	if err != nil {
		return err
	}

	migrations, err := goose.CollectMigrations("sql/migrations", 0, goose.MaxVersion)
	if err != nil {
		return err
	}

	fmt.Println("Migrations embedded:")
	for _, m := range migrations {
		switch m.Version {
		case currentVersion:
			fmt.Printf(" *  %s: %02d\n", m.Source, m.Version)
		case goose.MaxVersion:
			fmt.Printf(" ^  %s: %02d\n", m.Source, m.Version)
		default:
			fmt.Printf("    %s: %02d\n", m.Source, m.Version)
		}
	}

	if currentVersion == goose.MaxVersion {
		// No migrations to run. We're up to date
		return nil
	}

	var targetVersion int64
	if down, ok := os.LookupEnv("GOOSE_DOWN_TO"); ok {
		targetVersion, err = strconv.ParseInt(down, 10, 64)
		if err != nil {
			return fmt.Errorf("failed to parse GOOSE_DOWN_TO version: %w", err)
		}
		err = goose.DownToContext(ctx, stdDb, "sql/migrations", targetVersion)
	} else {
		// Handle up migrations
		if up, ok := os.LookupEnv("GOOSE_UP_TO"); ok {
			targetVersion, err = strconv.ParseInt(up, 10, 64)
			if err != nil {
				return fmt.Errorf("failed to parse GOOSE_UP_TO version: %w", err)
			}
		} else {
			// Default: migrate to latest version
			targetVersion = goose.MaxVersion
		}
		err = goose.UpToContext(ctx, stdDb, "sql/migrations", targetVersion)
	}

	if err != nil {
		return err
	}

	return nil
}
