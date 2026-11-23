package db_test

import (
	"context"
	"io"
	"log"
	"os"
	"testing"
	"time"
	"uuid"

	"unnamed-game/internal/db"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestMain(m *testing.M) {
	if os.Getenv("GAME_TEST_LOGS") != "1" {
		log.SetOutput(io.Discard)
	}
	os.Exit(m.Run())
}

func ctxT(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func mustUser(t *testing.T, d *db.Database, playerID string) uuid.UUID {
	t.Helper()
	ref, err := d.UpsertUser(ctxT(t), playerID)
	if err != nil {
		t.Fatalf("UpsertUser(%s): %v", playerID, err)
	}
	return ref.UserID
}

// achievementID returns the id of a seeded achievement by code.
func achievementID(t *testing.T, d *db.Database, code string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := d.Pool().QueryRow(ctxT(t), "SELECT achievement_id FROM achievements WHERE code=$1", code).Scan(&id); err != nil {
		t.Fatalf("achievement %s: %v", code, err)
	}
	return id
}

// listen opens a dedicated connection LISTENing on channel.
func listen(t *testing.T, d *db.Database, channel string) *pgx.Conn {
	t.Helper()
	conn, err := pgx.ConnectConfig(ctxT(t), d.Pool().Config().ConnConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close(context.Background()) })
	if _, err := conn.Exec(ctxT(t), "LISTEN "+pgx.Identifier{channel}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	return conn
}

func waitNotification(t *testing.T, conn *pgx.Conn, d time.Duration) (*pgconn.Notification, bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	n, err := conn.WaitForNotification(ctx)
	if err != nil {
		return nil, false
	}
	return n, true
}
