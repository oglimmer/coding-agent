// Package db opens the Postgres connection pool (pgx via the database/sql
// adapter) and runs embedded SQL migrations at startup.
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Exec is satisfied by both *sql.DB and *sql.Tx so query helpers work inside
// or outside a transaction.
type Exec interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Open connects to Postgres and pings in a retry loop. Tuned for a PgBouncer
// transaction-pooling front end (prepared-statement caches disabled).
func Open(ctx context.Context, url string) (*sql.DB, error) {
	cfg, err := pgx.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.DefaultQueryExecMode = pgx.QueryExecModeExec
	cfg.StatementCacheCapacity = 0
	cfg.DescriptionCacheCapacity = 0

	pool := stdlib.OpenDB(*cfg)
	pool.SetMaxOpenConns(20)
	pool.SetMaxIdleConns(5)
	pool.SetConnMaxIdleTime(30 * time.Second)
	pool.SetConnMaxLifetime(5 * time.Minute)

	var pingErr error
	for i := 0; i < 30; i++ {
		if pingErr = pool.PingContext(ctx); pingErr == nil {
			return pool, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	_ = pool.Close()
	return nil, fmt.Errorf("database not reachable after retries: %w", pingErr)
}

// Migrate applies every embedded migration in filename order. Each script must
// be idempotent; this runs them all on every startup.
func Migrate(ctx context.Context, pool *sql.DB) error {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		sqlBytes, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		if _, err := pool.ExecContext(ctx, string(sqlBytes)); err != nil {
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		log.Printf("INFO db: applied migration %s", name)
	}
	return nil
}
