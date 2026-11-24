//go:build integration && faultinject

package integration

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// Two players play one game (the game is capped at MaxPlayerCount = 2). One
// of them sends "!panic" in game chat, which panics the game loop (see
// internal/game/faultinject_on.go). The game must end through the normal
// path: every player gets a session summary and a closed socket, the DB row
// records the panic, and the server keeps serving new games.
//
//	TEST_DB_NAME=terminal_dungeon_test go test -tags "integration faultinject" -run GamePanic ./test/integration/
func TestGamePanicEndsOnlyThatGame(t *testing.T) {
	d := openDB(t)
	n := startNode(t, d)

	alice := n.login("alice")
	bob := n.login("bob")

	gr := alice.createGame()
	if code, _ := bob.join(gr.GameID); code != http.StatusOK {
		t.Fatalf("bob join: %d", code)
	}
	wa := alice.dial("/ws")
	wb := bob.dial("/ws")
	wa.waitFor(t, "alice frame", isFrame)
	wb.waitFor(t, "bob frame", isFrame)

	// Some ordinary play first, so the game has state to save.
	for _, k := range []string{"d", "d", "s", "f"} {
		wa.key(t, k)
		wb.key(t, k)
	}

	// Bob triggers the panic from game chat.
	wb.key(t, "\r")
	wb.typeText(t, "!panic")
	wb.key(t, "\r")

	// Every player is disconnected with a session summary, then the socket closes.
	for _, ws := range []*socket{wa, wb} {
		ws.waitFor(t, "session summary after panic", jsonType("session_summary"))
		ws.waitClosed(t, "socket closed after panic")
	}

	// The DB row is finalized with the panic reason.
	ctx := context.Background()
	deadline := time.Now().Add(wait)
	var status, reason string
	for time.Now().Before(deadline) {
		d.Pool().QueryRow(ctx, "SELECT status, COALESCE(termination_reason,'') FROM games WHERE game_id=$1", gr.GameID).Scan(&status, &reason)
		if status == "inactive" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if status != "inactive" || reason != "panic" {
		t.Fatalf("game row status=%q termination_reason=%q, want inactive/panic", status, reason)
	}

	// The server is still up: a new game starts and streams frames.
	dave := n.login("dave")
	dave.createGame()
	wd := dave.dial("/ws")
	wd.waitFor(t, "frame in a new game after the panic", isFrame)

	resp, err := http.Get(n.ts.URL + "/api/leaderboard")
	if err != nil {
		t.Fatalf("server not responding after panic: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("leaderboard after panic: %d", resp.StatusCode)
	}
}
