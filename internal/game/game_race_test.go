package game

import (
	"fmt"
	"sync"
	"testing"
	"time"
	"uuid"

	"go.uber.org/goleak"
)

// startRealGame starts a game with all its goroutines (30ms loop, idle
// ticker, achievement processor) plus a stand-in for the server listener.
func startRealGame(t *testing.T) *Game {
	t.Helper()
	g := NewGame(uuid.NewV7(), make(chan string, MaxChatHistory), testCatalog(t))
	fakeServer(t, g)
	return g
}

func connect(t *testing.T, g *Game, id string) {
	t.Helper()
	resp := make(chan error, 1)
	g.EventChan <- GameEvent{Type: EventTypeConnect, PlayerID: id, Object: defaultKeybinds, RespChan: resp}
	select {
	case err := <-resp:
		if err != nil {
			t.Fatalf("connect %s: %v", id, err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("connect %s timed out", id)
	}
}

// Destroying a game must stop every goroutine it started.
func TestDestroyStopsAllGameGoroutines(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())
	g := startRealGame(t)
	connect(t, g, "alice")
	withTimeout(t, 5*time.Second, "Destroy", func() { g.Destroy("test") })
	// Give the loops a moment to observe DestroyChan.
	time.Sleep(100 * time.Millisecond)
}

// Inputs, connects, disconnects and chat from many goroutines while the real
// loop runs. Catches deadlocks and panics; under -race also data races.
func TestGameLoopUnderConcurrentEvents(t *testing.T) {
	g := startRealGame(t)
	players := []string{"p0", "p1"}
	for _, p := range players {
		connect(t, g, p)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	for _, id := range players {
		wg.Add(2)
		go func(id string) { // input sender (like readPlayerInputs)
			defer wg.Done()
			keys := []rune{'w', 'a', 's', 'd', 'f', 'e'}
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				case g.EventChan <- GameEvent{Type: EventTypeKey, PlayerID: id, Key: keys[i%len(keys)]}:
				}
				time.Sleep(time.Millisecond)
			}
		}(id)
		go func(id string) { // frame reader (like streamFrames)
			defer wg.Done()
			p, _ := g.GetPlayerByID(id)
			for {
				select {
				case <-stop:
					return
				case <-p.DisplayChan:
				}
			}
		}(id)
	}
	wg.Add(1)
	go func() { // reconnect churn
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			case g.EventChan <- GameEvent{Type: EventTypeDisconnect, PlayerID: "p1"}:
			}
			resp := make(chan error, 1)
			select {
			case <-stop:
				return
			case g.EventChan <- GameEvent{Type: EventTypeConnect, PlayerID: "p1", RespChan: resp}:
			}
			select {
			case <-resp:
			case <-stop:
				return
			}
			g.BroadcastGameChat("p1", fmt.Sprintf("msg %d", i))
			time.Sleep(5 * time.Millisecond)
		}
	}()

	time.Sleep(1500 * time.Millisecond)
	close(stop)
	withTimeout(t, 5*time.Second, "event goroutines", wg.Wait)

	// The loop must still be alive and responsive. Free p1's slot first (the
	// churn may have left p1 connected and the game is capped at
	// MaxPlayerCount); events are processed in order.
	g.EventChan <- GameEvent{Type: EventTypeDisconnect, PlayerID: "p1"}
	connect(t, g, "late")
	withTimeout(t, 5*time.Second, "Destroy", func() { g.Destroy("test") })
}

// IdleTick (1-minute ticker) mutates players under the lock while Tick
// renders frames AFTER releasing it. Only the race detector can see this.
func TestIdleTickConcurrentWithGameLoop(t *testing.T) {
	g := startRealGame(t)
	connect(t, g, "alice")
	p, _ := g.GetPlayerByID("alice")
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-p.DisplayChan:
			}
		}
	}()
	for i := 0; i < 20; i++ {
		g.IdleTick()
		time.Sleep(10 * time.Millisecond)
	}
	close(stop)
	withTimeout(t, 5*time.Second, "Destroy", func() { g.Destroy("test") })
}

// QuitGame and GetNextLevel send on g.EventChan from inside UpdatePlayer,
// i.e. from the game loop while it holds g.Mu. The game loop is the only
// reader of EventChan, so if the channel is full (a client sending input
// faster than one tick drains it) the loop blocks on itself forever and the
// whole game freezes for every player.
func TestQuitWithFullEventChanDoesNotDeadlockLoop(t *testing.T) {
	g := newTestGame(t)
	fakeServer(t, g) // last player quitting destroys the game
	p := addPlayerAt(t, g, "alice", Position{X: 5, Y: 5})
	for len(g.EventChan) < cap(g.EventChan) {
		g.EventChan <- GameEvent{Type: EventTypeKey, PlayerID: "alice", Key: 'w'}
	}
	ok := withTimeout(t, 2*time.Second, "UpdatePlayer('q') with full EventChan", func() { press(g, p, 'q') })
	if !ok {
		<-g.EventChan // unblock the stuck goroutine
	}
}

// Same self-send problem for space (next level) during intermission.
func TestNextLevelWithFullEventChanDoesNotDeadlockLoop(t *testing.T) {
	g := newTestGame(t)
	p := addPlayerAt(t, g, "alice", Position{X: 5, Y: 5})
	g.State = StateGameIntermission
	for len(g.EventChan) < cap(g.EventChan) {
		g.EventChan <- GameEvent{Type: EventTypeKey, PlayerID: "alice", Key: 'w'}
	}
	ok := withTimeout(t, 2*time.Second, "UpdatePlayer(' ') with full EventChan", func() { press(g, p, ' ') })
	if !ok {
		<-g.EventChan
		return
	}
	if g.State != StateGamePlaying {
		t.Errorf("state=%v want playing after next level", g.State)
	}
}

func TestSendEventAfterDestroyDoesNotBlock(t *testing.T) {
	g := newTestGame(t)
	fakeServer(t, g)
	for len(g.EventChan) < cap(g.EventChan) {
		g.EventChan <- GameEvent{Type: EventTypeKey, PlayerID: "x", Key: 'w'}
	}
	withTimeout(t, 3*time.Second, "Destroy", func() { g.Destroy("test") })
	var sent bool
	withTimeout(t, 2*time.Second, "SendEvent on destroyed game", func() {
		sent = g.SendEvent(GameEvent{Type: EventTypeDisconnect, PlayerID: "x"})
	})
	if sent {
		t.Error("SendEvent reported success on a destroyed game")
	}
	if err := g.Validate("bob"); err == nil {
		t.Error("Validate accepted a player into a destroyed game")
	}
}

// Keys queued after 'q' in the same tick must not act on the player who
// just quit.
func TestKeysAfterQuitInSameTickAreIgnored(t *testing.T) {
	g := newTestGame(t)
	fakeServer(t, g)
	addPlayerAt(t, g, "bob", Position{X: 20, Y: 5}) // keeps the game alive
	p := addPlayerAt(t, g, "alice", Position{X: 5, Y: 5})
	press(g, p, 'q', 'd')
	if p.GetPosition() != (Position{X: 5, Y: 5}) {
		t.Errorf("player moved after quitting: %v", p.GetPosition())
	}
	if _, ok := g.GetActivePlayerById("alice"); ok {
		t.Errorf("alice still active after quit")
	}
}
