package game

import (
	"testing"

)

func TestPlayerMovement(t *testing.T) {
	g := newTestGame(t)
	p := addPlayerAt(t, g, "alice", Position{X: 5, Y: 5})

	press(g, p, 'd')
	if p.GetPosition() != (Position{X: 6, Y: 5}) {
		t.Fatalf("move right: got %v", p.GetPosition())
	}
	press(g, p, 's')
	press(g, p, 'a')
	press(g, p, 'w')
	if p.GetPosition() != (Position{X: 5, Y: 5}) {
		t.Fatalf("down/left/up should return to start, got %v", p.GetPosition())
	}
	// Upper-case input maps to the lower-case binding.
	press(g, p, 'D')
	if p.GetPosition() != (Position{X: 6, Y: 5}) {
		t.Fatalf("upper-case D should move right, got %v", p.GetPosition())
	}
	entityConsistent(t, g.Level)
}

func TestPlayerMovementBlocked(t *testing.T) {
	g := newTestGame(t)
	p := addPlayerAt(t, g, "alice", Position{X: 1, Y: 1})

	press(g, p, 'a') // border wall at x=0
	press(g, p, 'w') // border wall at y=0
	if p.GetPosition() != (Position{X: 1, Y: 1}) {
		t.Fatalf("player walked into the border wall: %v", p.GetPosition())
	}

	addWallAt(g, Position{X: 2, Y: 1})
	press(g, p, 'd')
	if p.GetPosition() != (Position{X: 1, Y: 1}) {
		t.Fatalf("player walked into a wall: %v", p.GetPosition())
	}
	// Direction still updates even when blocked (used for aiming).
	if p.GetDirection() != (Direction{X: 1, Y: 0}) {
		t.Errorf("direction should face right after blocked move, got %v", p.GetDirection())
	}

	addEnemyAt(g, "g", EnemyGoblin, Position{X: 1, Y: 2})
	press(g, p, 's')
	if p.GetPosition() != (Position{X: 1, Y: 1}) {
		t.Fatalf("player walked into an enemy: %v", p.GetPosition())
	}
}

func TestDeadPlayerCannotActOrGainXp(t *testing.T) {
	g := newTestGame(t)
	p := addPlayerAt(t, g, "alice", Position{X: 5, Y: 5})
	e := addEnemyAt(g, "g", EnemyGoblin, Position{X: 6, Y: 5})
	p.CurrentHealth = 0

	press(g, p, 'd', 'f')
	if p.GetPosition() != (Position{X: 5, Y: 5}) {
		t.Errorf("dead player moved")
	}
	if e.CurrentHealth != e.MaxHealth {
		t.Errorf("dead player dealt damage")
	}
	p.GainXp(1000)
	if p.XPGained != 0 || p.Level != 1 {
		t.Errorf("dead player gained xp/levels")
	}
}

func TestPlayerMeleeAttackAndCooldown(t *testing.T) {
	g := newTestGame(t)
	p := addPlayerAt(t, g, "alice", Position{X: 5, Y: 5})
	e := addEnemyAt(g, "g", EnemyGoblin, Position{X: 6, Y: 5})
	p.Direction = Direction{X: 1, Y: 0}

	want := BasicAttackBaseDamage * (100 + p.GetDamageMultiplierPercent()) / 100
	press(g, p, 'f', 'f') // second press is within the cooldown
	if got := e.MaxHealth - e.CurrentHealth; got != want {
		t.Fatalf("damage after double press = %d, want %d (one hit)", got, want)
	}
	if p.DamageDealt != want {
		t.Errorf("DamageDealt=%d want %d", p.DamageDealt, want)
	}
	if len(g.Level.Effects) != 1 {
		t.Errorf("expected a hit effect, got %d", len(g.Level.Effects))
	}

	// Cooldown elapses over MaxAttackSpeed player updates.
	for i := 0; i < p.MaxAttackSpeed; i++ {
		p.UpdatePlayer(g)
	}
	press(g, p, 'f')
	if got := e.MaxHealth - e.CurrentHealth; got != 2*want {
		t.Fatalf("second hit after cooldown: total damage %d, want %d", got, 2*want)
	}
}

func TestGainXpLevelsUpAndUnlocksAttacks(t *testing.T) {
	p := NewPlayer("alice", defaultKeybinds)
	if p.Level != 1 || p.NextLevelXP != XpRequirements[1] {
		t.Fatalf("new player level=%d next=%d", p.Level, p.NextLevelXP)
	}
	if len(p.UnlockedAttacks) != 1 {
		t.Fatalf("level 1 should have only the basic attack, got %d", len(p.UnlockedAttacks))
	}

	p.GainXp(XpRequirements[1])
	if p.Level != 2 || p.ExperienceVal != 0 {
		t.Fatalf("after exact xp: level=%d xp=%d", p.Level, p.ExperienceVal)
	}

	// Enough for several levels at once; leftover is kept.
	p.GainXp(XpRequirements[2] + XpRequirements[3] + 1)
	if p.Level != 4 || p.ExperienceVal != 1 {
		t.Fatalf("multi-level: level=%d xp=%d", p.Level, p.ExperienceVal)
	}
	if len(p.UnlockedAttacks) != 2 || p.GetEquippedAttack().Name != "Arrow" {
		t.Fatalf("level 4 should unlock and auto-equip Arrow, got %d attacks, equipped %s", len(p.UnlockedAttacks), p.GetEquippedAttack().Name)
	}

	// Swap cycles through unlocked attacks.
	p.ChangeEquippedAttack(nil)
	if p.GetEquippedAttack().Name != "Basic" {
		t.Errorf("swap should wrap to Basic, got %s", p.GetEquippedAttack().Name)
	}
	p.ChangeEquippedAttack(nil)
	if p.GetEquippedAttack().Name != "Arrow" {
		t.Errorf("swap should go to Arrow, got %s", p.GetEquippedAttack().Name)
	}
}

func TestCustomKeybindsAndQuitKey(t *testing.T) {
	g := newTestGame(t)
	binds := map[string]string{
		"move_up": "i", "move_down": "k", "move_left": "j", "move_right": "l",
		"attack_enemy": "h", "swap_attack": "u", "quit_game": "x",
	}
	if err := g.HandlePlayerConnect(GameEvent{PlayerID: "bob", Object: binds}); err != nil {
		t.Fatal(err)
	}
	p, _ := g.GetPlayerByID("bob")
	g.Level.RemoveEntity(p)
	p.SetPosition(Position{X: 5, Y: 5})
	g.Level.AddEntity(p)

	press(g, p, 'd') // default binding must NOT work
	if p.GetPosition() != (Position{X: 5, Y: 5}) {
		t.Errorf("default key 'd' moved a player with custom binds")
	}
	press(g, p, 'l')
	if p.GetPosition() != (Position{X: 6, Y: 5}) {
		t.Errorf("custom key 'l' did not move right, at %v", p.GetPosition())
	}
	if got := p.GetQuitKey(); got != "X" {
		t.Errorf("GetQuitKey=%q want X", got)
	}
}

func TestConnectWithoutKeybindsDoesNotPanic(t *testing.T) {
	g := newTestGame(t)
	fakeServer(t, g) // 'q' quits the only player, which destroys the game
	if err := g.HandlePlayerConnect(GameEvent{PlayerID: "nokeys", Object: nil}); err != nil {
		t.Fatal(err)
	}
	p, ok := g.GetPlayerByID("nokeys")
	if !ok {
		t.Fatal("player not added")
	}
	press(g, p, 'd', 'q', 'f')
}

func TestTypingModeGameChat(t *testing.T) {
	g := newTestGame(t)
	p := addPlayerAt(t, g, "alice", Position{X: 5, Y: 5})

	press(g, p, '\r')
	if p.PlayerState != StateTyping {
		t.Fatalf("enter should switch to typing")
	}
	press(g, p, 'h', 'i', 'x', '\b', 'd') // 'd' must type, not move
	if p.MessageBuffer != "hid" {
		t.Fatalf("buffer=%q want %q", p.MessageBuffer, "hid")
	}
	if p.GetPosition() != (Position{X: 5, Y: 5}) {
		t.Errorf("player moved while typing")
	}

	press(g, p, '\r')
	if p.PlayerState != StatePlaying || p.MessageBuffer != "" {
		t.Fatalf("enter should send and return to playing; state=%v buf=%q", p.PlayerState, p.MessageBuffer)
	}
	evs := drainServerEvents(g)
	if len(evs) != 1 || evs[0].Type != EventTypeGameChatMsg || evs[0].Value != "hid" || evs[0].PlayerID != "alice" {
		t.Fatalf("expected one game chat event, got %+v", evs)
	}
}

func TestTypingModeSwitchToGlobalChat(t *testing.T) {
	g := newTestGame(t)
	p := addPlayerAt(t, g, "alice", Position{X: 5, Y: 5})

	for _, k := range []rune{'\r', '/', '2', ' ', 'y', 'o', '\r'} {
		press(g, p, k) // one key per tick
	}
	evs := drainServerEvents(g)
	if len(evs) != 1 || evs[0].Type != EventTypeGlobalChatMsg || evs[0].Value != "yo" {
		t.Fatalf("expected one global chat event 'yo', got %+v", evs)
	}
	if p.GetCursor() != ChatCursorGlobalChat {
		t.Errorf("cursor=%q want global", p.GetCursor())
	}

	// Empty message sends nothing.
	press(g, p, '\r')
	press(g, p, '\r')
	if evs := drainServerEvents(g); len(evs) != 0 {
		t.Errorf("empty message produced events: %+v", evs)
	}
}

// UpdatePlayer picks the key map (playing vs typing) once per tick, so keys
// that arrive in the same 30ms tick right after Enter are still run as game
// commands. Pasting or fast typing "\rhello q" can move, attack or quit.
func TestKeysAfterEnterInSameTickAreTyped(t *testing.T) {
	g := newTestGame(t)
	fakeServer(t, g)
	p := addPlayerAt(t, g, "alice", Position{X: 5, Y: 5})
	press(g, p, '\r', 'd', 'q')
	if p.MessageBuffer != "dq" {
		t.Errorf("buffer=%q want %q", p.MessageBuffer, "dq")
	}
	if p.GetPosition() != (Position{X: 5, Y: 5}) {
		t.Errorf("typed 'd' moved the player")
	}
	if _, ok := g.GetActivePlayerById("alice"); !ok {
		t.Errorf("typed 'q' quit the game")
	}
}

func TestMessageBufferMaxLength(t *testing.T) {
	p := NewPlayer("alice", defaultKeybinds)
	for i := 0; i < 200; i++ {
		p.AddToMessageBuffer('a')
	}
	if len(p.MessageBuffer) != MaxChatMessageLength-5 {
		t.Fatalf("buffer length %d, want cap %d", len(p.MessageBuffer), MaxChatMessageLength-5)
	}
}

// Backspace removes the last BYTE, so deleting a multi-byte character leaves
// invalid UTF-8 in the buffer (which is then sent to chat and the DB).
func TestBackspaceRemovesWholeRune(t *testing.T) {
	p := NewPlayer("alice", defaultKeybinds)
	p.AddToMessageBuffer('a')
	p.AddToMessageBuffer('é')
	p.RemoveLastByteMessage(nil)
	if p.MessageBuffer != "a" {
		t.Fatalf("buffer=%q want %q", p.MessageBuffer, "a")
	}
}

func TestAddScoreScalesWithLevelsCompleted(t *testing.T) {
	p := NewPlayer("alice", defaultKeybinds)
	p.AddScore(10)
	if p.Score != 10 {
		t.Fatalf("score=%d want 10", p.Score)
	}
	p.LevelsCompleted = 5
	p.AddScore(10)
	if p.Score != 25 {
		t.Fatalf("score=%d want 25 (10 + 10*1.5)", p.Score)
	}
}

func TestPlayerSummary(t *testing.T) {
	p := NewPlayer("alice", defaultKeybinds)
	p.Score = 42
	s := p.GenerateSummary()
	if s.Score != 42 || s.Deaths != 0 || s.PlayerID != "alice" {
		t.Fatalf("alive summary wrong: %+v", s)
	}
	p.CurrentHealth = 0
	s = p.GenerateSummary()
	if s.Score != 0 || s.Deaths != 1 {
		t.Fatalf("dead player summary should zero score and count a death: %+v", s)
	}
}
