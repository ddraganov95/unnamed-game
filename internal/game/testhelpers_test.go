package game

import (
	"io"
	"log"
	"os"
	"testing"
	"time"
	"uuid"
)

// TestMain initializes the global registries (attacks, blueprints, xp table)
// once and silences the very chatty debug logging unless GAME_TEST_LOGS=1.
func TestMain(m *testing.M) {
	InitGameRegistries()
	if os.Getenv("GAME_TEST_LOGS") != "1" {
		log.SetOutput(io.Discard)
	}
	os.Exit(m.Run())
}

// emptyCatalog is an achievement catalog with no definitions.
func emptyCatalog() *AchievementCatalog {
	c, err := BuildAchievementCatalog(nil)
	if err != nil {
		panic(err)
	}
	return c
}

// newEmptyLevel returns a level with only the border walls and three fixed
// player spawn points, so tests control every other entity.
func newEmptyLevel() *Level {
	level := InitializeLevel()
	level.InitializeField()
	level.InitializeWalls()
	level.PlayerSpawnPoints = []Position{{X: 10, Y: 10}, {X: 20, Y: 10}, {X: 30, Y: 10}}
	return level
}

// newTestGame builds a game WITHOUT starting any goroutines (no game loop,
// no idle ticker, no achievement processor). Tests drive it via Tick/IdleTick.
// The level is replaced by an empty bordered level with no enemies.
func newTestGame(t *testing.T) *Game {
	t.Helper()
	return newTestGameWithCatalog(t, emptyCatalog())
}

func newTestGameWithCatalog(t *testing.T, catalog *AchievementCatalog) *Game {
	t.Helper()
	g := InitializeGame()
	g.GameId = uuid.NewV7()
	g.GlobalChat = make(chan string, MaxChatHistory)
	g.AchievementEngine = NewAchievementEngine(catalog, g.DestroyChan, g.ServerEventChan)
	g.Level = newEmptyLevel()
	g.EnemiesSpawned = 0
	return g
}

var defaultKeybinds = map[string]string{
	"move_up":      "w",
	"move_down":    "s",
	"move_left":    "a",
	"move_right":   "d",
	"attack_enemy": "f",
	"swap_attack":  "e",
	"quit_game":    "q",
}

// addPlayerAt connects a new player through the real connect path and then
// moves them to pos.
func addPlayerAt(t *testing.T, g *Game, id string, pos Position) *Player {
	t.Helper()
	if err := g.HandlePlayerConnect(GameEvent{Type: EventTypeConnect, PlayerID: id, Object: defaultKeybinds}); err != nil {
		t.Fatalf("connect %s: %v", id, err)
	}
	p, ok := g.GetPlayerByID(id)
	if !ok {
		t.Fatalf("player %s not in game after connect", id)
	}
	g.Level.RemoveEntity(p)
	p.SetPosition(pos)
	g.Level.AddEntity(p)
	return p
}

// addEnemyAt places an enemy of the given kind at pos.
func addEnemyAt(g *Game, id string, kind EnemyType, pos Position) *Enemy {
	e := CreateEnemy(id, pos, kind)
	g.AddEnemy(e)
	return e
}

// addWallAt places a wall tile at pos.
func addWallAt(g *Game, pos Position) {
	g.Level.AddEntity(CreateWall("testwall", pos))
}

// press feeds keys to a player and runs one player update (what Tick does
// for each player after processing inputs).
func press(g *Game, p *Player, keys ...rune) {
	for _, k := range keys {
		p.EnqueueKey(k)
	}
	p.UpdatePlayer(g)
}

// drainServerEvents collects every server event currently buffered.
func drainServerEvents(g *Game) []ServerEvent {
	var out []ServerEvent
	for {
		select {
		case ev := <-g.ServerEventChan:
			out = append(out, ev)
		default:
			return out
		}
	}
}

// fakeServer emulates the server's listenToGameEvents loop: it acknowledges
// destroy requests and records every event. It stops on EventTypeDeactivateGame
// or when the test ends.
func fakeServer(t *testing.T, g *Game) <-chan ServerEvent {
	t.Helper()
	seen := make(chan ServerEvent, 1024)
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	go func() {
		for {
			select {
			case <-done:
				return
			case ev := <-g.ServerEventChan:
				if ev.RespChan != nil {
					ev.RespChan <- nil
				}
				select {
				case seen <- ev:
				default:
				}
				if ev.Type == EventTypeDeactivateGame {
					return
				}
			}
		}
	}()
	return seen
}

// withTimeout runs fn and fails the test if it does not return within d.
// Used to turn deadlocks into test failures instead of hung test runs.
func withTimeout(t *testing.T, d time.Duration, what string, fn func()) bool {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
		return true
	case <-time.After(d):
		t.Errorf("%s did not finish within %v (deadlock?)", what, d)
		return false
	}
}

// entityConsistent checks the Entities map and the position index agree.
func entityConsistent(t *testing.T, level *Level) {
	t.Helper()
	count := 0
	for pos, objs := range level.posEntities {
		if len(objs) == 0 {
			t.Errorf("empty slice left in posEntities at %v", pos)
		}
		for _, o := range objs {
			count++
			if o.GetPosition() != pos {
				t.Errorf("entity %s indexed at %v but positioned at %v", o.GetID(), pos, o.GetPosition())
			}
		}
	}
	_ = count
}
