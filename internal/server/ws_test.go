package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"unnamed-game/internal/game"
	"unnamed-game/internal/testutil"
)

const defaultWait = 5 * time.Second

func playerPos(g *game.Game, id string) game.Position {
	g.Mu.Lock()
	defer g.Mu.Unlock()
	p, _ := g.GetPlayerByID(id)
	return p.GetPosition()
}

// ---------------------------------------------------------------------------
// Game websocket
// ---------------------------------------------------------------------------

func TestWebSocketWithoutGameIsRejected(t *testing.T) {
	env := newTestEnv(t)
	c := env.newClient("alice") // never created/joined a game
	if _, err := c.dial("/ws"); err == nil {
		t.Fatal("websocket upgrade succeeded for a player with no game")
	}
}

func TestWebSocketStreamsFramesAndAppliesInput(t *testing.T) {
	env := newTestEnv(t)
	c := env.newClient("alice")
	id := c.createGame()
	ws := c.mustDial("/ws")
	frame := ws.waitFor(t, defaultWait, "first frame", isFrame)
	if !strings.Contains(frame, id.String()) {
		t.Errorf("frame footer should show the game id")
	}

	g, _ := env.game(id)
	start := playerPos(g, "alice")
	// Pick a direction that is free on this random level.
	for _, k := range []struct {
		key string
		dx  int
		dy  int
	}{{"d", 1, 0}, {"a", -1, 0}, {"s", 0, 1}, {"w", 0, -1}} {
		g.Mu.Lock()
		_, blocked := g.Level.GetBlockerAt(game.Position{X: start.X + k.dx, Y: start.Y + k.dy})
		g.Mu.Unlock()
		if blocked {
			continue
		}
		ws.send(t, k.key)
		want := game.Position{X: start.X + k.dx, Y: start.Y + k.dy}
		if !waitUntil(defaultWait, func() bool { return playerPos(g, "alice") == want }) {
			t.Fatalf("key %q: player at %v, want %v", k.key, playerPos(g, "alice"), want)
		}
		return
	}
	t.Skip("spawn point boxed in on all four sides")
}

func TestCopyGameIdMessage(t *testing.T) {
	env := newTestEnv(t)
	c := env.newClient("alice")
	id := c.createGame()
	ws := c.mustDial("/ws")
	ws.waitFor(t, defaultWait, "first frame", isFrame)
	ws.send(t, "c")
	msg := ws.waitFor(t, defaultWait, "copy_clipboard", isJSONType("copy_clipboard"))
	var out OutboundWSMessage
	json.Unmarshal([]byte(msg), &out)
	if out.Payload != id.String() {
		t.Fatalf("payload %q want %q", out.Payload, id)
	}
}

func TestQuitSavesStatsAndEndsGame(t *testing.T) {
	env := newTestEnv(t)
	c := env.newClient("alice")
	id := c.createGame()
	ws := c.mustDial("/ws")
	ws.waitFor(t, defaultWait, "first frame", isFrame)

	ws.send(t, "q")
	summary := ws.waitFor(t, defaultWait, "session_summary", isJSONType("session_summary"))
	if !strings.Contains(summary, `"player_id":"alice"`) || !strings.Contains(summary, `"user_stats"`) {
		t.Errorf("summary %s", summary)
	}
	if !ws.closed(defaultWait) {
		t.Errorf("server did not close the socket after quit")
	}
	if !waitUntil(defaultWait, func() bool { _, ok := env.game(id); return !ok }) {
		t.Fatalf("game still active after the last player quit")
	}

	var status, reason string
	env.db.Pool().QueryRow(context.Background(), "SELECT status, coalesce(termination_reason,'') FROM games WHERE game_id=$1", id).Scan(&status, &reason)
	if status != "inactive" || reason != game.DestroyReasonPlayerQuit.String() {
		t.Errorf("game row status=%q reason=%q", status, reason)
	}
	var gameTime int64
	env.db.Pool().QueryRow(context.Background(), "SELECT total_game_time FROM user_stats WHERE user_id=$1", c.userID).Scan(&gameTime)
	if gameTime < 0 {
		t.Errorf("total_game_time=%d", gameTime)
	}
}

func TestClosingSocketDisconnectsPlayerButKeepsGame(t *testing.T) {
	env := newTestEnv(t)
	a := env.newClient("alice")
	b := env.newClient("bob")
	id := a.createGame()
	b.join(id)
	wa := a.mustDial("/ws")
	wa.waitFor(t, defaultWait, "frame", isFrame)
	wb := b.mustDial("/ws")
	wb.waitFor(t, defaultWait, "frame", isFrame)

	wb.conn.Close()
	g, _ := env.game(id)
	if !waitUntil(defaultWait, func() bool { return len(activePlayers(g)) == 1 }) {
		t.Fatalf("active players %v, want only alice", activePlayers(g))
	}
	if _, ok := env.game(id); !ok {
		t.Fatal("game ended although alice is still connected")
	}
	wa.waitFor(t, defaultWait, "frames keep flowing to alice", isFrame)
}

// Reconnecting (e.g. page refresh) closes the old socket. The old handler's
// deferred DisconnectPlayer then still sends EventTypeDisconnect for the
// player id, which disconnects the NEW session as well.
func TestReconnectKeepsNewSessionActive(t *testing.T) {
	env := newTestEnv(t)
	c := env.newClient("alice")
	id := c.createGame()
	old := c.mustDial("/ws")
	old.waitFor(t, defaultWait, "frame on old socket", isFrame)

	fresh := c.mustDial("/ws")
	fresh.waitFor(t, defaultWait, "frame on new socket", isFrame)
	time.Sleep(time.Second) // let the old handler finish its teardown

	g, ok := env.game(id)
	if !ok {
		t.Fatal("game ended on reconnect")
	}
	if ids := activePlayers(g); len(ids) != 1 {
		t.Fatalf("after reconnect active players = %v, want [alice]", ids)
	}
	if _, ok := fresh.tryWaitFor(2*time.Second, isFrame); !ok {
		t.Fatal("new socket stopped receiving frames after reconnect")
	}
}

// When the last player quits while their client is still sending input, the
// game loop stops draining EventChan. The server listener then blocks forever
// in DisconnectPlayer sending to that full channel, so the game is never
// finalized in the DB nor removed from activeGames.
func TestQuitWhileSpammingInputFinalizesGame(t *testing.T) {
	env := newTestEnv(t)
	c := env.newClient("alice")
	id := c.createGame()
	ws := c.mustDial("/ws")
	ws.waitFor(t, defaultWait, "first frame", isFrame)

	ws.send(t, "q")
	for i := 0; i < 200; i++ {
		if err := ws.conn.WriteMessage(1, []byte("w")); err != nil {
			break
		}
	}
	if !waitUntil(10*time.Second, func() bool { _, ok := env.game(id); return !ok }) {
		t.Fatal("game still in activeGames 10s after the last player quit")
	}
	var status string
	env.db.Pool().QueryRow(context.Background(), "SELECT status FROM games WHERE game_id=$1", id).Scan(&status)
	if status != "inactive" {
		t.Fatalf("game status in DB = %q, want inactive", status)
	}
}

// A game that has been destroyed (but whose cleanup has not finished, or is
// stuck) must refuse joins and websocket registration instead of hanging on
// its EventChan, which nothing reads anymore.
func TestDestroyedGameRejectsJoinAndConnect(t *testing.T) {
	env := newTestEnv(t)
	alice := env.newClient("alice")
	id := alice.createGame()
	g, ok := env.game(id)
	if !ok {
		t.Fatal("game not active")
	}
	// Close DestroyChan without running the server-side cleanup, so the game
	// is still in activeGames (the window in which the bug used to hang).
	g.Mu.Lock()
	close(g.DestroyChan)
	g.Mu.Unlock()
	for len(g.EventChan) < cap(g.EventChan) {
		g.EventChan <- game.GameEvent{Type: game.EventTypeKey, PlayerID: "x", Key: 'w'}
	}

	bob := env.newClient("bob")
	if resp := bob.join(id); resp.StatusCode != 503 {
		t.Errorf("join destroyed game: status %d want 503", resp.StatusCode)
	}

	conn := &GameWSConnection{Ctx: context.Background(), UserID: bob.userID.String(), PlayerID: "bob"}
	errc := make(chan error, 1)
	go func() { errc <- env.srv.RegisterPlayer(g, "bob", conn) }()
	select {
	case err := <-errc:
		if err == nil {
			t.Error("RegisterPlayer on destroyed game returned nil")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("RegisterPlayer hung on a destroyed game")
	}
}

// listenToGameEvents writes copy_clipboard / session_summary JSON on the same
// websocket that streamFrames writes frames to, from a different goroutine.
// gorilla/websocket supports one concurrent writer and panics otherwise,
// crashing the whole server process. Runs in a subprocess so the crash only
// fails this test.
func TestConcurrentWebSocketWritesDoNotCrash(t *testing.T) {
	if os.Getenv("SUBPROCESS_CONCURRENT_WRITE") == "1" {
		concurrentWriteScenario(t)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestConcurrentWebSocketWritesDoNotCrash$", "-test.count=1")
	cmd.Env = append(os.Environ(), "SUBPROCESS_CONCURRENT_WRITE=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		lines := strings.Split(string(out), "\n")
		for _, l := range lines {
			if strings.Contains(l, "panic") || strings.Contains(l, "concurrent write") {
				t.Fatalf("server crashed: %s", l)
			}
		}
		t.Fatalf("subprocess failed: %v\n%s", err, out)
	}
}

func concurrentWriteScenario(t *testing.T) {
	env := newTestEnv(t)
	c := env.newClient("alice")
	c.createGame()
	ws := c.mustDial("/ws")
	ws.waitFor(t, defaultWait, "first frame", isFrame)
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		ws.send(t, "c")
		time.Sleep(2 * time.Millisecond)
	}
}

// ---------------------------------------------------------------------------
// Lobby / global chat
// ---------------------------------------------------------------------------

func TestLobbyChatHistoryAndBroadcast(t *testing.T) {
	env := newTestEnv(t)
	env.srv.BroadcastGlobalMessage("old message")

	a := env.newClient("alice")
	b := env.newClient("bob")
	wa := a.mustDial("/ws/global-chat")
	wb := b.mustDial("/ws/global-chat")
	wa.waitFor(t, defaultWait, "history replay", func(m string) bool { return m == "old message" })
	wb.waitFor(t, defaultWait, "history replay", func(m string) bool { return m == "old message" })

	// Full path: ws -> DB function -> NOTIFY -> listener -> broadcast.
	time.Sleep(300 * time.Millisecond) // listener connects asynchronously at boot
	wa.send(t, "hello lobby")
	want := "[alice]: hello lobby"
	wb.waitFor(t, defaultWait, "broadcast to bob", func(m string) bool { return m == want })
	wa.waitFor(t, defaultWait, "broadcast to alice", func(m string) bool { return m == want })

	var n int
	env.db.Pool().QueryRow(context.Background(), "SELECT count(*) FROM global_chat_messages WHERE user_id=$1", a.userID).Scan(&n)
	if n != 1 {
		t.Errorf("chat rows=%d want 1", n)
	}
}

func TestLobbyChatSlowClientDoesNotBlockBroadcast(t *testing.T) {
	env := newTestEnv(t)
	slow := env.newClient("slow")
	ws := slow.mustDial("/ws/global-chat")
	_ = ws // never read beyond the buffered reader; server-side Write chan fills up
	done := make(chan struct{})
	go func() {
		for i := 0; i < 500; i++ {
			env.srv.BroadcastGlobalMessage(fmt.Sprintf("msg %d", i))
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(defaultWait):
		t.Fatal("BroadcastGlobalMessage blocked on a slow client")
	}
}

// lobbyConns is keyed by playerID. A second lobby socket for the same player
// (two tabs) replaces the first; when the first closes, its cleanup deletes
// the map entry that now belongs to the second, which stops receiving chat.
func TestLobbyChatTwoTabsSamePlayer(t *testing.T) {
	testutil.KnownBug(t, "closing one of two lobby tabs for the same player removes the other tab from lobbyConns")
	env := newTestEnv(t)
	a := env.newClient("alice")
	tab1 := a.mustDial("/ws/global-chat")
	tab2 := a.mustDial("/ws/global-chat")
	time.Sleep(200 * time.Millisecond)
	tab1.conn.Close()
	time.Sleep(300 * time.Millisecond)

	env.srv.BroadcastGlobalMessage("are you there?")
	if _, ok := tab2.tryWaitFor(2*time.Second, func(m string) bool { return m == "are you there?" }); !ok {
		t.Fatal("remaining tab no longer receives global chat")
	}
}

// Every game reads global chat from the single shared server.globalChat
// channel, so each message is consumed by exactly one game's PollChat; all
// other games never see it.
func TestGlobalChatReachesEveryGame(t *testing.T) {
	testutil.KnownBug(t, "global chat uses one shared channel; each message reaches only one game")
	env := newTestEnv(t)
	var games []*game.Game
	for i := 0; i < 3; i++ {
		c := env.newClient(fmt.Sprintf("p%d", i))
		id := c.createGame()
		c.mustDial("/ws").waitFor(t, defaultWait, "frame", isFrame)
		g, _ := env.game(id)
		games = append(games, g)
	}
	env.srv.BroadcastGlobalMessage("hello everyone")
	for i, g := range games {
		ok := waitUntil(2*time.Second, func() bool {
			g.Mu.Lock()
			defer g.Mu.Unlock()
			for _, m := range g.ChatHistory {
				if strings.Contains(m, "hello everyone") {
					return true
				}
			}
			return false
		})
		if !ok {
			t.Errorf("game %d never showed the global message", i)
		}
	}
}
