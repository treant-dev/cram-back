package db

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// These need a real Postgres, so they skip when DATABASE_URL is unset — which is the case in CI.
// Run them against the dev stack:
//
//	DATABASE_URL='postgres://cram:changeme@postgres:5432/cram?sslmode=disable' go test ./internal/db/
func requireDB(t *testing.T) {
	t.Helper()
	if _, ok := lookupDatabaseURL(); !ok {
		t.Skip("DATABASE_URL not set")
	}
}

func lookupDatabaseURL() (string, bool) {
	v, ok := os.LookupEnv("DATABASE_URL")
	return v, ok && v != ""
}

func TestStatementTimeoutIsApplied(t *testing.T) {
	requireDB(t)
	t.Setenv("DB_STATEMENT_TIMEOUT", "300ms")

	pool, err := NewPool(context.Background())
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer pool.Close()

	// The context is deliberately generous: if the query is cut short, it is Postgres doing it,
	// not Go cancelling the request. That distinction is the whole point — a client-side deadline
	// abandons the query while it keeps running on the server and keeps holding a connection.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	start := time.Now()
	_, err = pool.Exec(ctx, "select pg_sleep(5)")
	if err == nil {
		t.Fatal("a 5s query completed; statement_timeout is not in effect")
	}
	if !strings.Contains(err.Error(), "canceling statement due to statement timeout") {
		t.Fatalf("query failed for the wrong reason: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("timeout took %v, expected it to fire near 300ms", elapsed)
	}
}

func TestStatementTimeoutCanBeDisabled(t *testing.T) {
	requireDB(t)
	t.Setenv("DB_STATEMENT_TIMEOUT", "0")

	pool, err := NewPool(context.Background())
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer pool.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := pool.Exec(ctx, "select pg_sleep(1)"); err != nil {
		t.Errorf("a 1s query was refused with the timeout disabled: %v", err)
	}
}

func TestURLValueWinsOverDefault(t *testing.T) {
	requireDB(t)
	url, _ := lookupDatabaseURL()
	sep := "?"
	if strings.Contains(url, "?") {
		sep = "&"
	}
	t.Setenv("DATABASE_URL", url+sep+"statement_timeout=250ms")
	t.Setenv("DB_STATEMENT_TIMEOUT", "60s") // must lose to the URL

	pool, err := NewPool(context.Background())
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer pool.Close()

	var setting string
	if err := pool.QueryRow(context.Background(), "show statement_timeout").Scan(&setting); err != nil {
		t.Fatalf("show statement_timeout: %v", err)
	}
	if setting != "250ms" {
		t.Errorf("statement_timeout = %q, want the value from the URL (250ms)", setting)
	}
}
