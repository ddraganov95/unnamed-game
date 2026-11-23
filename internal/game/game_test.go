package game

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"unnamed-game/internal/testutil"
)

func TestConnectNewAndExistingPlayer(t *testing.T) {
	g := newTestGame(t)
	if err := g.HandlePlayerConnect(GameEvent{PlayerID: "alice", Object: defaultKeybinds}); err != nil {
		t.Fatal(err)
	}
	p, ok := g.GetActivePlayerById("alice")
	if !ok {
		t.Fatal("player not active after connect")
	}
	if p.GameID != g.GameId.String() {
		t.Errorf("player GameID=%q want %q", p.GameID, g.GameId)
	}
	if _, onLevel := g.Level.Entities["alice"]; !onLevel {
		t.Fatal("player not placed on the level")
	}

	// Disconnect then reconnect on the same level: keep the old position.
	press(g, p, 'd')
	pos := p.GetPosition()
	g.HandlePlayerDisconnect("alice")
	if _, ok := g.GetActivePlayerById("alice"); ok {
		t.Fatal("player still active after disconnect")
	}
	if _, onLevel := g.Level.Entities["alice"]; onLevel {
		t.Fatal("disconnected player still on the level")
	}
	evs := drainServerEvents(g)
	if len(evs) != 1 || evs[0].Type != EventTypeDisconnect {
		t.Fatalf("expected one disconnect server event, got %+v", evs)
	}

	if err := g.HandlePlayerConnect(GameEvent{PlayerID: "alice"}); err != nil {
		t.Fatal(err)
	}
	if len(g.Players) != 1 {
		t.Fatalf("reconnect duplicated the player: %d players", len(g.Players))
	}
	if p.PlayerState != StatePlaying || p.GetPosition() != pos {
		t.Errorf("reconnect: state=%v pos=%v want playing at %v", p.PlayerState, p.GetPosition(), pos)
	}
}

func TestReconnectAfterLevelChangeRespawns(t *testing.T) {
	g := newTestGame(t)
	p := addPlayerAt(t, g, "alice", Position{X: 50, Y: 40})
	g.HandlePlayerDisconnect("alice")
	drainServerEvents(g)
	g.LevelsCompleted++ // the others moved on to a new level

	if err := g.HandlePlayerConnect(GameEvent{PlayerID: "alice"}); err != nil {
		t.Fatal(err)
	}
	onSpawn := false
	for _, sp := range g.Level.PlayerSpawnPoints {
		if p.GetPosition() == sp {
			onSpawn = true
		}
	}
	if !onSpawn {
		t.Errorf("player should respawn at a spawn point, at %v", p.GetPosition())
	}
}

func TestReconnectWhenOldTileTakenFallsBackToSpawn(t *testing.T) {
	g := newTestGame(t)
	p := addPlayerAt(t, g, "alice", Position{X: 50, Y: 40})
	g.HandlePlayerDisconnect("alice")
	drainServerEvents(g)
	addEnemyAt(g, "g", EnemyGoblin, Position{X: 50, Y: 40})

	if err := g.HandlePlayerConnect(GameEvent{PlayerID: "alice"}); err != nil {
		t.Fatal(err)
	}
	if p.GetPosition() == (Position{X: 50, Y: 40}) {
		t.Fatalf("player placed on an occupied tile")
	}
	entityConsistent(t, g.Level)
}

func TestValidate(t *testing.T) {
	g := newTestGame(t)
	if err := g.Validate("alice"); err != nil {
		t.Fatalf("empty game should accept: %v", err)
	}
	addPlayerAt(t, g, "alice", Position{X: 5, Y: 5})
	for i := 1; i < MaxPlayerCount; i++ {
		addPlayerAt(t, g, fmt.Sprintf("p%d", i), Position{X: 5 + i*2, Y: 5})
	}
	if err := g.Validate("newcomer"); err == nil {
		t.Errorf("full game should reject")
	}

	g2 := newTestGame(t)
	dead := addPlayerAt(t, g2, "dead", Position{X: 5, Y: 5})
	dead.CurrentHealth = 0
	if err := g2.Validate("dead"); err == nil {
		t.Errorf("dead player should not be able to rejoin")
	}

	g3 := newTestGame(t)
	g3.State = StateGameOver
	if err := g3.Validate("x"); err == nil {
		t.Errorf("game over should reject")
	}
}

// Capacity is enforced when the player actually connects (inside the game
// loop), not only on the HTTP join pre-check, which reserves nothing.
func TestConnectEnforcesCapacity(t *testing.T) {
	g := newTestGame(t)
	for i := 0; i < MaxPlayerCount; i++ {
		if err := g.HandlePlayerConnect(GameEvent{PlayerID: fmt.Sprintf("p%d", i), Object: defaultKeybinds}); err != nil {
			t.Fatalf("connect p%d: %v", i, err)
		}
	}
	if err := g.HandlePlayerConnect(GameEvent{PlayerID: "extra", Object: defaultKeybinds}); err == nil {
		t.Fatal("connect beyond MaxPlayerCount succeeded")
	}
	if _, ok := g.GetPlayerByID("extra"); ok {
		t.Error("rejected player was added to the game")
	}
	// An active player reconnecting keeps their slot.
	if err := g.HandlePlayerConnect(GameEvent{PlayerID: "p0", Object: defaultKeybinds}); err != nil {
		t.Errorf("reconnect of active player rejected: %v", err)
	}
	// Once someone leaves, a new player fits again.
	g.HandlePlayerDisconnect("p1")
	drainServerEvents(g)
	if err := g.HandlePlayerConnect(GameEvent{PlayerID: "extra", Object: defaultKeybinds}); err != nil {
		t.Errorf("connect after a player left: %v", err)
	}
}

// Page refresh: the new socket's Connect and the old socket's Disconnect can
// reach the game loop in either order. Both orders must end with the player
// active on the new connection, in one place on the map.
func TestReconnectEventOrdering(t *testing.T) {
	connect := func(g *Game, conn uint64) {
		if err := g.HandlePlayerConnect(GameEvent{PlayerID: "alice", Object: defaultKeybinds, ConnID: conn}); err != nil {
			t.Fatalf("connect conn %d: %v", conn, err)
		}
	}
	disconnect := func(g *Game, conn uint64) {
		g.EventChan <- GameEvent{Type: EventTypeDisconnect, PlayerID: "alice", ConnID: conn}
		g.ProcessInputs()
	}
	check := func(t *testing.T, g *Game, wantConn uint64, wantPos Position) {
		t.Helper()
		p, ok := g.GetActivePlayerById("alice")
		if !ok {
			t.Fatal("alice not active after reconnect")
		}
		if p.ConnID != wantConn {
			t.Errorf("ConnID=%d want %d", p.ConnID, wantConn)
		}
		if p.GetPosition() != wantPos {
			t.Errorf("position %v want %v", p.GetPosition(), wantPos)
		}
		n := 0
		for _, objs := range g.Level.posEntities {
			for _, o := range objs {
				if o.GetID() == "alice" {
					n++
				}
			}
		}
		if n != 1 {
			t.Errorf("alice appears %d times on the map, want 1", n)
		}
		entityConsistent(t, g.Level)
	}

	t.Run("new connect first, stale disconnect after", func(t *testing.T) {
		g := newTestGame(t)
		connect(g, 1)
		p, _ := g.GetPlayerByID("alice")
		pos := p.GetPosition()
		connect(g, 2)    // takeover while still active
		disconnect(g, 1) // late disconnect of the replaced socket: ignored
		check(t, g, 2, pos)
		if evs := drainServerEvents(g); len(evs) != 0 {
			t.Errorf("stale disconnect produced server events %+v", evs)
		}
	})

	t.Run("old disconnect first, new connect after", func(t *testing.T) {
		g := newTestGame(t)
		connect(g, 1)
		p, _ := g.GetPlayerByID("alice")
		pos := p.GetPosition()
		disconnect(g, 1)
		drainServerEvents(g)
		connect(g, 2)
		check(t, g, 2, pos)
	})

	t.Run("disconnect of the current connection still works", func(t *testing.T) {
		g := newTestGame(t)
		connect(g, 1)
		addPlayerAt(t, g, "bob", Position{X: 30, Y: 30}) // keep the game alive
		disconnect(g, 1)
		if _, ok := g.GetActivePlayerById("alice"); ok {
			t.Error("alice still active after her own connection disconnected")
		}
	})
}

func TestLevelCompleteMovesToIntermission(t *testing.T) {
	g := newTestGame(t)
	p := addPlayerAt(t, g, "alice", Position{X: 5, Y: 5})
	g.CreateLog("something happened")

	g.Level.Update(g) // no enemies on the empty level

	if g.State != StateGameIntermission {
		t.Fatalf("state=%v want intermission", g.State)
	}
	if g.LevelsCompleted != 1 || p.LevelsCompleted != 1 {
		t.Errorf("levels completed: game=%d player=%d", g.LevelsCompleted, p.LevelsCompleted)
	}
	evs := drainServerEvents(g)
	if len(evs) != 1 || evs[0].Type != EventTypeLevelCompleted {
		t.Fatalf("expected a LevelCompleted event, got %+v", evs)
	}
	logs, ok := evs[0].Object.([]Event)
	if !ok || len(logs) == 0 {
		t.Fatalf("LevelCompleted should carry the extracted logs, got %T len=%d", evs[0].Object, len(logs))
	}
	if len(g.Events) != 0 {
		t.Errorf("logs should be drained after extraction")
	}
}

func TestLevelUpdateWithEnemiesAliveDoesNothing(t *testing.T) {
	g := newTestGame(t)
	addPlayerAt(t, g, "alice", Position{X: 5, Y: 5})
	addEnemyAt(g, "g", EnemyGoblin, Position{X: 40, Y: 40})
	g.Level.Update(g)
	if g.State != StateGamePlaying {
		t.Fatalf("state changed with enemies alive: %v", g.State)
	}
}

func TestAllPlayersDeadIsGameOver(t *testing.T) {
	g := newTestGame(t)
	p := addPlayerAt(t, g, "alice", Position{X: 5, Y: 5})
	addEnemyAt(g, "g", EnemyGoblin, Position{X: 40, Y: 40})
	p.CurrentHealth = 0
	g.Level.Update(g)
	if g.State != StateGameOver {
		t.Fatalf("state=%v want game over", g.State)
	}
}

func TestNextLevelFromIntermission(t *testing.T) {
	g := newTestGame(t)
	p := addPlayerAt(t, g, "alice", Position{X: 5, Y: 5})
	p.CurrentHealth = 10
	g.State = StateGameIntermission
	totalBefore := g.TotalLevels

	press(g, p, ' ')
	g.ProcessInputs()

	if g.State != StateGamePlaying {
		t.Fatalf("state=%v want playing", g.State)
	}
	if g.TotalLevels != totalBefore+1 {
		t.Errorf("TotalLevels=%d want %d", g.TotalLevels, totalBefore+1)
	}
	if p.CurrentHealth != p.MaxHealth {
		t.Errorf("player not healed on new level: %d", p.CurrentHealth)
	}
	if _, ok := g.Level.Entities["alice"]; !ok {
		t.Errorf("player not spawned on the new level")
	}
	if len(g.Level.Enemies) == 0 {
		t.Errorf("new level has no enemies")
	}
}

// GetNextLevel's guard is `!alive && state != intermission`, so an ALIVE
// player pressing space in the middle of a level generates a brand new level
// (skipping the current one and resetting enemies).
func TestSpaceDuringPlayDoesNotSkipLevel(t *testing.T) {
	testutil.KnownBug(t, "pressing space while playing skips to a new level (GetNextLevel guard uses && instead of ||)")
	g := newTestGame(t)
	p := addPlayerAt(t, g, "alice", Position{X: 5, Y: 5})
	addEnemyAt(g, "g", EnemyGoblin, Position{X: 40, Y: 40})
	level := g.Level

	press(g, p, ' ')
	g.ProcessInputs()

	if g.Level != level {
		t.Fatalf("space during play replaced the level")
	}
}

func TestQuitLastPlayerDestroysGame(t *testing.T) {
	g := newTestGame(t)
	seen := fakeServer(t, g)
	p := addPlayerAt(t, g, "alice", Position{X: 5, Y: 5})

	press(g, p, 'q')
	withTimeout(t, 3*time.Second, "ProcessInputs(quit)", g.ProcessInputs)

	select {
	case <-g.DestroyChan:
	default:
		t.Fatal("DestroyChan not closed after last player quit")
	}
	var types []EventType
	deadline := time.After(2 * time.Second)
	for len(types) < 3 {
		select {
		case ev := <-seen:
			types = append(types, ev.Type)
		case <-deadline:
			t.Fatalf("server events so far: %v", types)
		}
	}
	want := []EventType{EventTypeDisconnect, EventTypeGameDestroy, EventTypeDeactivateGame}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("server events %v, want %v", types, want)
		}
	}
}

func TestQuitWithOtherPlayersKeepsGame(t *testing.T) {
	g := newTestGame(t)
	a := addPlayerAt(t, g, "a", Position{X: 5, Y: 5})
	addPlayerAt(t, g, "b", Position{X: 15, Y: 5})
	press(g, a, 'q')
	g.ProcessInputs()
	select {
	case <-g.DestroyChan:
		t.Fatal("game destroyed while a player remains")
	default:
	}
	if _, ok := g.GetActivePlayerById("a"); ok {
		t.Errorf("quitting player still active")
	}
}

func TestDestroyIsIdempotent(t *testing.T) {
	g := newTestGame(t)
	fakeServer(t, g)
	withTimeout(t, 3*time.Second, "first Destroy", func() { g.Destroy("test") })
	withTimeout(t, 3*time.Second, "second Destroy", func() { g.Destroy("test") })
}

func TestTickRendersFrameForActivePlayers(t *testing.T) {
	g := newTestGame(t)
	p := addPlayerAt(t, g, "alice", Position{X: 5, Y: 5})
	addEnemyAt(g, "g", EnemyGoblin, Position{X: 40, Y: 40})

	p.EnqueueKey('d')
	g.Tick()

	if p.GetPosition() != (Position{X: 6, Y: 5}) {
		t.Errorf("queued key not applied during tick, at %v", p.GetPosition())
	}
	select {
	case frame := <-p.DisplayChan:
		if !strings.HasPrefix(frame, "\033[H") {
			t.Errorf("frame should start with cursor-home escape")
		}
		rows := strings.Split(strings.TrimSuffix(strings.TrimPrefix(frame, "\033[H"), "\r\n"), "\r\n")
		if len(rows) != MaxScreenHeight {
			t.Fatalf("frame has %d rows, want %d", len(rows), MaxScreenHeight)
		}
		for i, r := range rows {
			if n := len([]rune(r)); n != MaxScreenWidth {
				t.Fatalf("row %d has %d runes, want %d", i, n, MaxScreenWidth)
			}
		}
		row := []rune(rows[VerticalPadding+p.GetPosition().Y])
		if row[MaxMessageLength+3+p.GetPosition().X] != SymbolPlayer {
			t.Errorf("player symbol not drawn at its position")
		}
		if !strings.Contains(frame, g.GameId.String()) {
			t.Errorf("footer should contain the game id")
		}
	default:
		t.Fatal("no frame pushed to the player")
	}
}

func TestKeyEventResetsAFKAndIgnoresDisconnected(t *testing.T) {
	g := newTestGame(t)
	p := addPlayerAt(t, g, "alice", Position{X: 5, Y: 5})
	p.AFKMinutes = 1
	g.EventChan <- GameEvent{Type: EventTypeKey, PlayerID: "alice", Key: 'd'}
	g.ProcessInputs()
	if p.AFKMinutes != 0 || len(p.KeyQueue) != 1 {
		t.Fatalf("key event: afk=%d queue=%v", p.AFKMinutes, p.KeyQueue)
	}

	g.HandlePlayerDisconnect("alice")
	p.KeyQueue = nil
	g.EventChan <- GameEvent{Type: EventTypeKey, PlayerID: "alice", Key: 'd'}
	g.ProcessInputs()
	if len(p.KeyQueue) != 0 {
		t.Fatalf("disconnected player received input")
	}
}

func TestIdleTickKicksAFKPlayer(t *testing.T) {
	g := newTestGame(t)
	p := addPlayerAt(t, g, "alice", Position{X: 5, Y: 5})
	for i := 0; i < PlayerAllowedAFKMins-1; i++ {
		if g.IdleTick() {
			t.Fatal("game destroyed too early")
		}
	}
	if p.PlayerState == StateDisconnected {
		t.Fatal("player kicked before the AFK limit")
	}
	g.IdleTick()
	if p.PlayerState != StateDisconnected {
		t.Fatalf("player not kicked after %d AFK minutes", PlayerAllowedAFKMins)
	}
	found := false
	for _, ev := range drainServerEvents(g) {
		if ev.Type == EventTypeDisconnect && ev.PlayerID == "alice" {
			found = true
		}
	}
	if !found {
		t.Errorf("no disconnect server event for the AFK player")
	}
}

func TestIdleTickDestroysEmptyGameAfterThreshold(t *testing.T) {
	g := newTestGame(t)
	for i := 1; i < StopGameAfterIdleMinutes; i++ {
		if g.IdleTick() {
			t.Fatalf("destroy requested after %d idle minutes", i)
		}
	}
	if !g.IdleTick() {
		t.Fatalf("destroy not requested after %d idle minutes", StopGameAfterIdleMinutes)
	}
}

func TestIdleTickResetsWhenPlayersActive(t *testing.T) {
	g := newTestGame(t)
	g.IdleTick()
	g.IdleTick()
	p := addPlayerAt(t, g, "alice", Position{X: 5, Y: 5})
	if g.EmptyMinutes != 0 {
		t.Fatalf("connect should reset EmptyMinutes, got %d", g.EmptyMinutes)
	}
	p.AFKMinutes = -100 // keep them from being kicked
	for i := 0; i < 10; i++ {
		if g.IdleTick() {
			t.Fatal("game with an active player was destroyed")
		}
	}
}

func TestLogsWindowAndExtract(t *testing.T) {
	g := newTestGame(t)
	for i := 0; i < MaxEventsLength+10; i++ {
		g.CreateLog("log %03d", i)
	}
	// Row 0 of the panel shows the oldest log in the visible window.
	first := string([]rune{g.GetEventRuneAt(0, 4), g.GetEventRuneAt(0, 5), g.GetEventRuneAt(0, 6)})
	if first != "010" {
		t.Errorf("first visible log = %q, want 010", first)
	}
	if r := g.GetEventRuneAt(0, MaxMessageLength+5); r != SymbolDefault {
		t.Errorf("past message length should be blank")
	}

	long := strings.Repeat("x", MaxMessageLength*2)
	g.CreateLog("%s", long)
	if r := g.GetEventRuneAt(MaxEventsLength-1, MaxMessageLength); r != SymbolDefault {
		t.Errorf("long log not truncated in the panel")
	}

	logs := g.ExtractLogsToSave()
	if len(logs) != MaxEventsLength+11 {
		t.Errorf("extracted %d logs", len(logs))
	}
	if g.ExtractLogsToSave() != nil {
		t.Errorf("second extract should return nil")
	}
}

func TestChatHistoryCapAndPoll(t *testing.T) {
	g := newTestGame(t)
	for i := 0; i < MaxChatHistory+5; i++ {
		g.appendChat(fmt.Sprintf("m%d", i))
	}
	if len(g.ChatHistory) != MaxChatHistory || g.ChatHistory[0] != "m5" {
		t.Fatalf("history len=%d first=%q", len(g.ChatHistory), g.ChatHistory[0])
	}

	g.ChatHistory = nil
	g.GlobalChat <- "hello"
	g.BroadcastGameChat("alice", "hi")
	g.PollChat()
	if len(g.ChatHistory) != 2 {
		t.Fatalf("PollChat should drain both channels, got %v", g.ChatHistory)
	}
	joined := strings.Join(g.ChatHistory, "|")
	if !strings.Contains(joined, ChatChannelDisplayGlobalChat+"hello") || !strings.Contains(joined, ChatChannelDisplayGameChat+"[alice]: hi") {
		t.Errorf("unexpected chat history %v", g.ChatHistory)
	}
}

func TestBroadcastGameChatDropsWhenFull(t *testing.T) {
	g := newTestGame(t)
	for i := 0; i < cap(g.GameChat); i++ {
		g.GameChat <- "x"
	}
	withTimeout(t, time.Second, "BroadcastGameChat on full channel", func() {
		g.BroadcastGameChat("alice", "overflow")
	})
}

func TestGameSummary(t *testing.T) {
	g := newTestGame(t)
	addPlayerAt(t, g, "a", Position{X: 5, Y: 5})
	addPlayerAt(t, g, "b", Position{X: 15, Y: 5})
	g.EnemiesKilled = 3
	s := g.GenerateSummary()
	if s.PlayersConnected != 2 || s.EnemiesKilled != 3 || s.TotalLevels != g.TotalLevels {
		t.Fatalf("summary %+v", s)
	}
}
