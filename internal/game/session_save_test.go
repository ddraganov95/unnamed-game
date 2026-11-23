package game

import (
	"testing"
	"time"
)

// user_stats adds what we send to its totals, so each save must contain only
// what happened since the previous save.
func TestTakeUnsavedSummaryReturnsDeltas(t *testing.T) {
	p := NewPlayer("alice", defaultKeybinds)
	p.EnemiesKilled, p.XPGained, p.DamageDealt, p.DamageTaken, p.LevelsCompleted = 3, 50, 40, 10, 1
	p.Score = 120
	p.playedTime = 2 * time.Second

	first := p.TakeUnsavedSummary()
	if first.EnemiesKilled != 3 || first.XPGained != 50 || first.DamageDealt != 40 ||
		first.DamageTaken != 10 || first.LevelsCompleted != 1 || first.SessionDuration != 2*time.Second {
		t.Fatalf("first save should contain everything: %+v", first)
	}

	again := p.TakeUnsavedSummary()
	if again.EnemiesKilled != 0 || again.XPGained != 0 || again.DamageDealt != 0 ||
		again.DamageTaken != 0 || again.LevelsCompleted != 0 || again.SessionDuration != 0 || again.Deaths != 0 {
		t.Fatalf("nothing happened since the last save, got %+v", again)
	}
	if again.Score != 120 || again.PlayerLevel != p.Level {
		t.Errorf("score/level are absolute (DB keeps the max): %+v", again)
	}

	p.EnemiesKilled += 2
	p.playedTime += time.Second
	p.CurrentHealth = 0 // died
	third := p.TakeUnsavedSummary()
	if third.EnemiesKilled != 2 || third.SessionDuration != time.Second || third.Deaths != 1 {
		t.Fatalf("third save should contain only the new kills/time/death: %+v", third)
	}
	if fourth := p.TakeUnsavedSummary(); fourth.Deaths != 0 {
		t.Errorf("a death must be counted once, got %d again", fourth.Deaths)
	}
}

// Play time counts only connected stretches, not the time a player spent
// away (closed tab) before coming back.
func TestPlayTimeExcludesDisconnectedTime(t *testing.T) {
	g := newTestGame(t)
	if err := g.HandlePlayerConnect(GameEvent{PlayerID: "alice", Object: defaultKeybinds, ConnID: 1}); err != nil {
		t.Fatal(err)
	}
	p, _ := g.GetPlayerByID("alice")
	p.playStart = time.Now().Add(-3 * time.Second) // connected 3s ago

	g.HandlePlayerDisconnect("alice")
	ev := drainServerEvents(g)[0]
	got := ev.Object.(PlayerDisconnectPayload).Unsaved.SessionDuration
	if got < 3*time.Second || got > 4*time.Second {
		t.Fatalf("first stretch play time %v, want ~3s", got)
	}

	time.Sleep(200 * time.Millisecond) // away
	if err := g.HandlePlayerConnect(GameEvent{PlayerID: "alice", Object: defaultKeybinds, ConnID: 2}); err != nil {
		t.Fatal(err)
	}
	g.HandlePlayerDisconnect("alice")
	ev = drainServerEvents(g)[0]
	if got := ev.Object.(PlayerDisconnectPayload).Unsaved.SessionDuration; got > 100*time.Millisecond {
		t.Errorf("second save %v includes time spent disconnected", got)
	}
}

// The disconnect event must carry the achievement progress: the session is
// removed from the engine right after, so the server can't export it later.
func TestDisconnectPayloadCarriesAchievementProgress(t *testing.T) {
	c := testCatalog(t)
	g := newTestGameWithCatalog(t, c)
	addPlayerAt(t, g, "alice", Position{X: 5, Y: 5})
	g.AchievementEngine.LoadPlayerAchievements("alice", freshStates(c))
	g.AchievementEngine.evaluateEvent(AchievementEvent{PlayerIDs: []string{"alice"}, Key: "kill:goblin", Amount: 1})

	g.HandlePlayerDisconnect("alice")
	evs := drainServerEvents(g)
	if len(evs) != 1 || evs[0].Type != EventTypeDisconnect {
		t.Fatalf("events %+v", evs)
	}
	payload, ok := evs[0].Object.(PlayerDisconnectPayload)
	if !ok {
		t.Fatalf("disconnect event Object is %T", evs[0].Object)
	}
	found := false
	for _, dto := range payload.AchievementProgress {
		for _, v := range dto.Progress {
			if v == 1 {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("kill:goblin progress missing from payload: %+v", payload.AchievementProgress)
	}
	if len(payload.Achievements) == 0 {
		t.Errorf("client achievement view missing from payload")
	}
	if g.AchievementEngine.ExportPlayerProgress("alice") != nil {
		t.Errorf("session should be removed from the engine after disconnect")
	}
}

// Destroying a game with players still connected disconnects them first, so
// the server saves their sessions before finalizing the game.
func TestDestroyDisconnectsActivePlayersBeforeGameDestroy(t *testing.T) {
	g := newTestGame(t)
	seen := fakeServer(t, g)
	addPlayerAt(t, g, "alice", Position{X: 5, Y: 5})
	addPlayerAt(t, g, "bob", Position{X: 15, Y: 5})

	withTimeout(t, 3*time.Second, "Destroy", func() { g.Destroy("idle_check") })

	var types []EventType
	disconnected := map[string]bool{}
	deadline := time.After(2 * time.Second)
	for len(types) < 4 {
		select {
		case ev := <-seen:
			types = append(types, ev.Type)
			if ev.Type == EventTypeDisconnect {
				if _, ok := ev.Object.(PlayerDisconnectPayload); !ok {
					t.Errorf("disconnect of %s without payload", ev.PlayerID)
				}
				disconnected[ev.PlayerID] = true
			}
		case <-deadline:
			t.Fatalf("server events so far: %v", types)
		}
	}
	want := []EventType{EventTypeDisconnect, EventTypeDisconnect, EventTypeGameDestroy, EventTypeDeactivateGame}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("server events %v, want %v", types, want)
		}
	}
	if !disconnected["alice"] || !disconnected["bob"] {
		t.Errorf("disconnected %v, want alice and bob", disconnected)
	}
}
