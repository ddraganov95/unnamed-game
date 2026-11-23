// Package pgtest gives tests a connection to a real, dedicated Postgres test
// database.
//
// Connection settings come from the same DB_* variables the server uses
// (environment first, then the repo's .env). The database NAME comes from
// TEST_DB_NAME, which must be set explicitly and must differ from DB_NAME so
// tests can never wipe the dev database. When TEST_DB_NAME is unset, tests
// that need the database are skipped. The database is created if missing.
//
// Every test that opens the DB holds a Postgres advisory lock for its whole
// duration, so test packages running in parallel (go test ./...) take turns
// instead of truncating each other's data.
package pgtest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"unnamed-game/internal/db"

	"github.com/jackc/pgx/v5"
	"github.com/joho/godotenv"
)

const advisoryLockKey = 7_000_001

var loadEnvOnce sync.Once

func loadDotEnv() {
	loadEnvOnce.Do(func() {
		dir, err := os.Getwd()
		if err != nil {
			return
		}
		for {
			if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
				_ = godotenv.Load(filepath.Join(dir, ".env")) // does not override real env vars
				return
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				return
			}
			dir = parent
		}
	})
}

// Config returns the DB config pointed at the test database, or skips the test.
func Config(t testing.TB) db.DBConfig {
	t.Helper()
	loadDotEnv()
	name := os.Getenv("TEST_DB_NAME")
	if name == "" {
		t.Skip("TEST_DB_NAME not set; skipping test that needs Postgres")
	}
	cfg := db.LoadConfig()
	if name == cfg.DBName {
		t.Fatalf("TEST_DB_NAME (%q) must differ from DB_NAME: tests truncate every table", name)
	}
	cfg.DBName = name
	cfg.MaxOpenConn = 20
	cfg.MinIdleConn = 0
	return cfg
}

func ensureDatabase(ctx context.Context, cfg db.DBConfig) error {
	admin := db.LoadConfig() // connect to the dev DB just to issue CREATE DATABASE
	conn, err := pgx.Connect(ctx, admin.ConnString())
	if err != nil {
		return fmt.Errorf("connect to %s to create test db: %w", admin.DBName, err)
	}
	defer conn.Close(ctx)
	var exists bool
	if err := conn.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname=$1)", cfg.DBName).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	_, err = conn.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{cfg.DBName}.Sanitize())
	if err != nil {
		// Another package may have created it concurrently.
		if err2 := conn.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname=$1)", cfg.DBName).Scan(&exists); err2 == nil && exists {
			return nil
		}
		return fmt.Errorf("create test database %s: %w", cfg.DBName, err)
	}
	return nil
}

// lockDB takes the cross-process advisory lock and releases it at cleanup.
func lockDB(t testing.TB, cfg db.DBConfig) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, cfg.ConnString())
	if err != nil {
		t.Fatalf("pgtest: connect for lock: %v", err)
	}
	lockCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if _, err := conn.Exec(lockCtx, "SELECT pg_advisory_lock($1)", advisoryLockKey); err != nil {
		conn.Close(ctx)
		t.Fatalf("pgtest: advisory lock: %v", err)
	}
	t.Cleanup(func() { conn.Close(context.Background()) }) // closing the session releases the lock
}

// Open returns a *db.Database on a freshly truncated test database with the
// schema applied. The database is closed when the test ends.
func Open(t testing.TB) *db.Database {
	t.Helper()
	cfg := Config(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := ensureDatabase(ctx, cfg); err != nil {
		t.Fatalf("pgtest: %v", err)
	}
	lockDB(t, cfg)
	database, err := db.NewDatabaseFromConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("pgtest: open test db: %v", err)
	}
	t.Cleanup(database.Close)
	Reset(t, database)
	return database
}

// Reset removes all users, games and chat (and everything cascading from
// them). The seeded achievements catalog is kept.
func Reset(t testing.TB, database *db.Database) {
	t.Helper()
	_, err := database.Pool().Exec(context.Background(),
		"TRUNCATE users, games, global_chat_messages RESTART IDENTITY CASCADE")
	if err != nil {
		t.Fatalf("pgtest: truncate: %v", err)
	}
}
