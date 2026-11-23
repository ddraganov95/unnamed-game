package server

import (
	"context"
	"testing"
	"time"
	"uuid"

	"unnamed-game/internal/game"
)

func gameTime(t *testing.T, env *testEnv, userID uuid.UUID) int64 {
	t.Helper()
	var v int64
	if err := env.db.Pool().QueryRow(context.Background(),
		"SELECT total_game_time FROM user_stats WHERE user_id=$1", userID).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

// twoPlayerGame starts a game with alice and bob connected; bob keeps the
// game alive so alice can leave and come back.
func twoPlayerGame(t *testing.T, env *testEnv) (alice *client, aliceWS *wsConn, id uuid.UUID) {
	t.Helper()
	alice = env.newClient("alice")
	bob := env.newClient("bob")
	id = alice.createGame()
	if resp := bob.join(id); resp.StatusCode != 200 {
		t.Fatalf("bob join: %d", resp.StatusCode)
	}
	bob.mustDial("/ws").waitFor(t, defaultWait, "bob frame", isFrame)
	aliceWS = alice.mustDial("/ws")
	aliceWS.waitFor(t, defaultWait, "alice frame", isFrame)
	return alice, aliceWS, id
}

// Closing the tab (no 'q') must still save the session.
func TestClosingTabSavesStats(t *testing.T) {
	env := newTestEnv(t)
	alice, ws, _ := twoPlayerGame(t, env)
	time.Sleep(1200 * time.Millisecond)
	ws.conn.Close()
	if !waitUntil(defaultWait, func() bool { return gameTime(t, env, alice.userID) >= 1 }) {
		t.Fatalf("total_game_time=%d after closing the tab, want >= 1", gameTime(t, env, alice.userID))
	}
}

// Quit, rejoin the same game, quit again: the first part must not be counted
// a second time.
func TestQuitRejoinQuitDoesNotDoubleCount(t *testing.T) {
	env := newTestEnv(t)
	alice, ws, id := twoPlayerGame(t, env)
	time.Sleep(2100 * time.Millisecond)
	ws.send(t, "q")
	ws.waitFor(t, defaultWait, "first summary", isJSONType("session_summary"))
	first := gameTime(t, env, alice.userID)
	if first < 2 {
		t.Fatalf("first quit saved total_game_time=%d, want >= 2", first)
	}

	if resp := alice.join(id); resp.StatusCode != 200 {
		t.Fatalf("rejoin: %d", resp.StatusCode)
	}
	ws = alice.mustDial("/ws")
	ws.waitFor(t, defaultWait, "frame after rejoin", isFrame)
	ws.send(t, "q")
	ws.waitFor(t, defaultWait, "second summary", isJSONType("session_summary"))
	if second := gameTime(t, env, alice.userID); second > first+1 {
		t.Fatalf("total_game_time went %d -> %d after an immediate quit (double counted)", first, second)
	}
}

// The AFK kick (idle ticker) saves the session and tells the client.
func TestAFKKickSavesStatsAndNotifiesClient(t *testing.T) {
	env := newTestEnv(t)
	alice, ws, id := twoPlayerGame(t, env)
	time.Sleep(1200 * time.Millisecond)
	g, _ := env.game(id)
	g.Mu.Lock()
	p, _ := g.GetPlayerByID("alice")
	p.AFKMinutes = game.PlayerAllowedAFKMins // bob stays below the limit
	if bob, ok := g.GetPlayerByID("bob"); ok {
		bob.AFKMinutes = 0
	}
	g.Mu.Unlock()

	if g.IdleTick() {
		t.Fatal("game should not be destroyed: bob is still active")
	}
	ws.waitFor(t, defaultWait, "session summary after AFK kick", isJSONType("session_summary"))
	if got := gameTime(t, env, alice.userID); got < 1 {
		t.Fatalf("total_game_time=%d after AFK kick, want >= 1", got)
	}
}

// Achievement progress was exported after the session had already been
// removed from the engine, so it was never saved on disconnect.
func TestAchievementProgressSavedOnQuit(t *testing.T) {
	env := newTestEnv(t)
	alice, ws, id := twoPlayerGame(t, env)
	g, _ := env.game(id)
	g.AchievementEngine.PublishEvent(game.AchievementEvent{PlayerIDs: []string{"alice"}, Key: "kill:goblin", Amount: 3})
	time.Sleep(200 * time.Millisecond) // processed asynchronously

	ws.send(t, "q")
	ws.waitFor(t, defaultWait, "summary", isJSONType("session_summary"))
	var rows int
	env.db.Pool().QueryRow(context.Background(),
		"SELECT count(*) FROM player_achievements_progress WHERE user_id=$1", alice.userID).Scan(&rows)
	if rows == 0 {
		t.Fatal("no achievement progress saved after quit")
	}
}
