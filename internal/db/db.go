package db

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

// defaultStatementTimeout bounds any single query. Without it one stuck statement holds a pool
// connection forever, and the pool is small (pgx defaults to max(4, NumCPU) connections), so a
// handful of them starve the whole API — web UI included. Override with DB_STATEMENT_TIMEOUT
// using Postgres syntax ("30s", "500ms", "0" to disable).
//
// Applied to the pool only. Migrations run through golang-migrate on their own connection from
// the same URL, so a long DDL statement is not at risk of being cut off here.
const defaultStatementTimeout = "30s"

func NewPool(ctx context.Context) (*pgxpool.Pool, error) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return nil, fmt.Errorf("DATABASE_URL is not set")
	}

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}

	// A statement_timeout already present in the URL wins, so deployments can override without
	// touching the binary.
	if _, set := cfg.ConnConfig.RuntimeParams["statement_timeout"]; !set {
		timeout := os.Getenv("DB_STATEMENT_TIMEOUT")
		if timeout == "" {
			timeout = defaultStatementTimeout
		}
		cfg.ConnConfig.RuntimeParams["statement_timeout"] = timeout
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}

	return pool, nil
}
