package game

import (
	"testing"

	"unnamed-game/internal/testutil"
)

func TestDealDamageIgnoresSameTeam(t *testing.T) {
	g := newTestGame(t)
	a := addPlayerAt(t, g, "a", Position{X: 5, Y: 5})
	b := addPlayerAt(t, g, "b", Position{X: 6, Y: 5})
	if g.DealDamage(a, GlobalAttacks[AttackBasic], b) {
		t.Fatalf("friendly fire returned true")
	}
	if b.CurrentHealth != b.MaxHealth {
		t.Fatalf("friendly fire dealt damage")
	}
}

func TestEnemyMeleeDamagesPlayerWithReduction(t *testing.T) {
	g := newTestGame(t)
	p := addPlayerAt(t, g, "alice", Position{X: 5, Y: 5})
	e := addEnemyAt(g, "g", EnemyGoblin, Position{X: 6, Y: 5})
	e.CurrentAttackSpeed = 1 // ready to attack on this update

	UpdateEnemy(g, e)

	raw := BasicAttackBaseDamage * (100 + e.GetDamageMultiplierPercent()) / 100
	want := raw * (100 - PlayerDamageReductionPercent) / 100
	if got := p.MaxHealth - p.CurrentHealth; got != want {
		t.Fatalf("player took %d damage, want %d", got, want)
	}
	if p.DamageTaken != want {
		t.Errorf("DamageTaken=%d want %d", p.DamageTaken, want)
	}
	if e.CurrentAttackSpeed != e.MaxAttackSpeed {
		t.Errorf("attack cooldown not reset")
	}
}

func TestPlayerDeath(t *testing.T) {
	g := newTestGame(t)
	p := addPlayerAt(t, g, "alice", Position{X: 5, Y: 5})
	p.CurrentHealth = 1
	p.TakeDamage(Damage{EntityID: "Goblin 1", Value: 100}, g)

	if p.IsAlive() {
		t.Fatalf("player should be dead")
	}
	if p.CurrentHealth != 0 {
		t.Errorf("health should clamp to 0, got %d", p.CurrentHealth)
	}
	if p.KilledBy != "Goblin 1" {
		t.Errorf("KilledBy=%q", p.KilledBy)
	}
	if g.PlayerDeaths != 1 {
		t.Errorf("PlayerDeaths=%d want 1", g.PlayerDeaths)
	}
	if _, ok := g.Level.Entities["alice"]; ok {
		t.Errorf("dead player still on the level")
	}
}

func TestEnemyKillCreditsPlayerAndRemovesEnemy(t *testing.T) {
	g := newTestGame(t)
	p := addPlayerAt(t, g, "alice", Position{X: 5, Y: 5})
	e := addEnemyAt(g, "g", EnemyGoblin, Position{X: 6, Y: 5})
	e.CurrentHealth = 1

	e.TakeDamage(Damage{EntityID: "alice", Value: 50}, g)

	if g.EnemiesKilled != 1 || p.EnemiesKilled != 1 {
		t.Fatalf("kills: game=%d player=%d, want 1/1", g.EnemiesKilled, p.EnemiesKilled)
	}
	if p.Score != e.Score {
		t.Errorf("score=%d want %d", p.Score, e.Score)
	}
	if p.XPGained != e.ExperienceVal {
		t.Errorf("xp=%d want %d", p.XPGained, e.ExperienceVal)
	}
	if len(g.Level.Enemies) != 0 {
		t.Errorf("enemy still in Level.Enemies")
	}
	if _, ok := g.Level.Entities["g"]; ok {
		t.Errorf("enemy still in Entities")
	}
	entityConsistent(t, g.Level)
}

func TestXpIsSharedWithAllActivePlayers(t *testing.T) {
	g := newTestGame(t)
	a := addPlayerAt(t, g, "a", Position{X: 5, Y: 5})
	b := addPlayerAt(t, g, "b", Position{X: 15, Y: 15})
	e := addEnemyAt(g, "g", EnemyGoblin, Position{X: 6, Y: 5})
	e.CurrentHealth = 1
	e.TakeDamage(Damage{EntityID: "a", Value: 50}, g)
	if a.XPGained != e.ExperienceVal || b.XPGained != e.ExperienceVal {
		t.Fatalf("xp not shared: a=%d b=%d", a.XPGained, b.XPGained)
	}
	if b.EnemiesKilled != 0 {
		t.Errorf("kill credited to non-killer")
	}
}

func newArrow(p *Player, dir Direction, rng int) *Projectile {
	atk := GlobalAttacks[AttackArrow]
	atk.Direction = dir
	atk.Range = rng
	atk.Damage = CreateDamage(p, atk.Damage.Type, atk.Damage.Value)
	return CreateProjectile(p, atk, ProjectileArrow)
}

func TestProjectileTravelsAndExpires(t *testing.T) {
	g := newTestGame(t)
	p := addPlayerAt(t, g, "alice", Position{X: 5, Y: 5})
	proj := newArrow(p, Direction{X: 1, Y: 0}, 3)
	g.Level.AddProjectile(proj)

	proj.Update(g) // moves immediately (speed counter starts at 0)
	if proj.GetPosition() != (Position{X: 6, Y: 5}) {
		t.Fatalf("first update: %v", proj.GetPosition())
	}
	// Waits MaxMovementSpeed ticks before the next move.
	for i := 0; i < proj.MaxMovementSpeed; i++ {
		proj.Update(g)
	}
	if proj.GetPosition() != (Position{X: 6, Y: 5}) {
		t.Fatalf("moved during cooldown: %v", proj.GetPosition())
	}
	proj.Update(g)
	if proj.GetPosition() != (Position{X: 7, Y: 5}) {
		t.Fatalf("second move: %v", proj.GetPosition())
	}
	for i := 0; i <= proj.MaxMovementSpeed; i++ {
		proj.Update(g)
	}
	if len(g.Level.Projectiles) != 0 {
		t.Fatalf("projectile should expire after its range")
	}
	if _, ok := g.Level.Entities[proj.GetID()]; ok {
		t.Fatalf("expired projectile still in Entities")
	}
	entityConsistent(t, g.Level)
}

func TestProjectileHitsEnemyAndWall(t *testing.T) {
	g := newTestGame(t)
	p := addPlayerAt(t, g, "alice", Position{X: 5, Y: 5})
	e := addEnemyAt(g, "g", EnemyGoblin, Position{X: 6, Y: 5})
	hit := newArrow(p, Direction{X: 1, Y: 0}, 10)
	g.Level.AddProjectile(hit)
	hit.Update(g)
	if e.CurrentHealth == e.MaxHealth {
		t.Fatalf("arrow did not damage enemy")
	}
	if len(g.Level.Projectiles) != 0 {
		t.Fatalf("arrow not removed after hit")
	}

	wallArrow := newArrow(p, Direction{X: -1, Y: 0}, 10)
	addWallAt(g, Position{X: 4, Y: 5})
	g.Level.AddProjectile(wallArrow)
	wallArrow.Update(g)
	if len(g.Level.Projectiles) != 0 {
		t.Fatalf("arrow not removed after hitting a wall")
	}
	entityConsistent(t, g.Level)
}

// UpdateGame ranges over Level.Projectiles while Projectile.Update removes
// entries from that same slice. Removing element i shifts the backing array,
// so the next projectile is skipped and the last one is updated twice.
func TestUpdateGameUpdatesEveryProjectileOnce(t *testing.T) {
	testutil.KnownBug(t, "removing a projectile while iterating Level.Projectiles skips the next one and double-updates the last")
	g := newTestGame(t)
	p := addPlayerAt(t, g, "alice", Position{X: 5, Y: 20})
	a := newArrow(p, Direction{X: 1, Y: 0}, 1) // expires on its first update
	a.SetPosition(Position{X: 10, Y: 10})
	b := newArrow(p, Direction{X: 1, Y: 0}, 20)
	b.SetPosition(Position{X: 10, Y: 12})
	c := newArrow(p, Direction{X: 1, Y: 0}, 20)
	c.SetPosition(Position{X: 10, Y: 14})
	g.Level.AddProjectile(a)
	g.Level.AddProjectile(b)
	g.Level.AddProjectile(c)

	g.UpdateGame()

	if b.GetPosition().X != 11 {
		t.Errorf("projectile b was skipped: at %v", b.GetPosition())
	}
	if c.GetPosition().X != 11 || c.CurrentMovementSpeed != c.MaxMovementSpeed {
		t.Errorf("projectile c updated more than once: at %v speed %d", c.GetPosition(), c.CurrentMovementSpeed)
	}
}

func TestGetNextStepDirectionAroundWall(t *testing.T) {
	g := newTestGame(t)
	// Wall directly between start and target, open above and below.
	addWallAt(g, Position{X: 6, Y: 5})
	dir := GetNextStepDirection(Position{X: 5, Y: 5}, Position{X: 8, Y: 5}, g)
	if dir == (Direction{}) {
		t.Fatalf("no path found around a single wall")
	}
	if dir == (Direction{X: 1, Y: 0}) {
		t.Fatalf("path goes straight into the wall")
	}
	if dir.X != 1 {
		t.Errorf("expected a diagonal step towards the target, got %v", dir)
	}

	if d := GetNextStepDirection(Position{X: 5, Y: 5}, Position{X: 5, Y: 5}, g); d != (Direction{}) {
		t.Errorf("same start/target should return zero direction, got %v", d)
	}

	// Fully boxed in: no path.
	for _, pos := range []Position{{19, 19}, {20, 19}, {21, 19}, {19, 20}, {21, 20}, {19, 21}, {20, 21}, {21, 21}} {
		addWallAt(g, pos)
	}
	if d := GetNextStepDirection(Position{X: 20, Y: 20}, Position{X: 30, Y: 30}, g); d != (Direction{}) {
		t.Errorf("boxed in should return zero direction, got %v", d)
	}
}

func TestEnemyChasesPlayerInAggroRange(t *testing.T) {
	g := newTestGame(t)
	addPlayerAt(t, g, "alice", Position{X: 5, Y: 5})
	e := addEnemyAt(g, "g", EnemyGoblin, Position{X: 15, Y: 5})
	e.CurrentMovementSpeed = 1
	UpdateEnemy(g, e)
	if e.GetPosition() != (Position{X: 14, Y: 5}) {
		t.Fatalf("enemy should step towards the player, at %v", e.GetPosition())
	}

	far := addEnemyAt(g, "far", EnemyGoblin, Position{X: 5, Y: 5 + EnemyDefaultAggroRange + 5})
	far.CurrentMovementSpeed = 1
	start := far.GetPosition()
	UpdateEnemy(g, far)
	if far.GetPosition() != start {
		t.Errorf("enemy outside aggro range moved")
	}
}

func TestHitEffectExpires(t *testing.T) {
	g := newTestGame(t)
	eff := CreateHitEffect("fx", Position{X: 3, Y: 3}, 2)
	g.Level.AddEffect(eff)
	eff.Update(g)
	if _, ok := g.Level.Effects["fx"]; !ok {
		t.Fatalf("effect removed too early")
	}
	eff.Update(g)
	if _, ok := g.Level.Effects["fx"]; ok {
		t.Fatalf("effect not removed after its timer")
	}
}
