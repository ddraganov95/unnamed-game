package game

import "testing"

func TestAddMoveRemoveEntityKeepsIndexConsistent(t *testing.T) {
	level := newEmptyLevel()
	e := CreateEnemy("g1", Position{X: 5, Y: 5}, EnemyGoblin)
	level.AddEntity(e)

	if got, ok := level.GetEntityAt(Position{X: 5, Y: 5}); !ok || got[0] != e {
		t.Fatalf("entity not indexed at its position")
	}

	level.MoveEntity(e, Position{X: 6, Y: 5})
	if _, ok := level.GetEntityAt(Position{X: 5, Y: 5}); ok {
		t.Errorf("old position still holds an entity after move")
	}
	if _, present := level.posEntities[Position{X: 5, Y: 5}]; present {
		t.Errorf("empty slice not cleaned up at old position")
	}
	if e.GetPosition() != (Position{X: 6, Y: 5}) {
		t.Errorf("entity position not updated, got %v", e.GetPosition())
	}
	entityConsistent(t, level)

	level.RemoveEntity(e)
	if _, ok := level.Entities["g1"]; ok {
		t.Errorf("entity still in Entities after remove")
	}
	if _, ok := level.GetEntityAt(Position{X: 6, Y: 5}); ok {
		t.Errorf("entity still indexed after remove")
	}
	entityConsistent(t, level)
}

func TestRemoveEntityOnlyRemovesTarget(t *testing.T) {
	level := newEmptyLevel()
	pos := Position{X: 5, Y: 5}
	a := CreateEnemy("a", pos, EnemyGoblin)
	b := CreateEnemy("b", pos, EnemyGoblin)
	level.AddEntity(a)
	level.AddEntity(b)

	level.RemoveEntity(a)
	got, ok := level.GetEntityAt(pos)
	if !ok || len(got) != 1 || got[0].GetID() != "b" {
		t.Fatalf("expected only b at %v, got %v", pos, got)
	}
}

func TestGetBlockerAndAttackableAt(t *testing.T) {
	level := newEmptyLevel()
	if _, ok := level.GetBlockerAt(Position{X: 0, Y: 0}); !ok {
		t.Errorf("border wall should block (0,0)")
	}
	if _, ok := level.GetBlockerAt(Position{X: 5, Y: 5}); ok {
		t.Errorf("empty tile should not block")
	}
	if _, ok := level.GetAttackableAt(Position{X: 0, Y: 0}); ok {
		t.Errorf("walls must not be attackable")
	}
	e := CreateEnemy("g", Position{X: 5, Y: 5}, EnemyGoblin)
	level.AddEntity(e)
	if got, ok := level.GetAttackableAt(Position{X: 5, Y: 5}); !ok || got.GetID() != "g" {
		t.Errorf("enemy should be attackable")
	}
}

func TestPutEntityAtPositionRejectsOccupiedTile(t *testing.T) {
	level := newEmptyLevel()
	a := CreateEnemy("a", Position{}, EnemyGoblin)
	b := CreateEnemy("b", Position{}, EnemyGoblin)
	if !level.PutEntityAtPosition(a, Position{X: 3, Y: 3}) {
		t.Fatalf("first put should succeed")
	}
	if level.PutEntityAtPosition(b, Position{X: 3, Y: 3}) {
		t.Fatalf("second put on occupied tile should fail")
	}
	if _, ok := level.Entities["b"]; ok {
		t.Errorf("rejected entity must not be added")
	}
}

func TestGetSpawnPoint(t *testing.T) {
	level := newEmptyLevel()
	pos, ok := level.GetSpawnPoint()
	if !ok {
		t.Fatalf("expected a free spawn point")
	}
	found := false
	for _, sp := range level.PlayerSpawnPoints {
		if sp == pos {
			found = true
		}
	}
	if !found {
		t.Errorf("returned %v which is not a spawn point", pos)
	}

	// Block two of three: must return the remaining one.
	level.AddEntity(CreateWall("w1", level.PlayerSpawnPoints[0]))
	level.AddEntity(CreateWall("w2", level.PlayerSpawnPoints[1]))
	for i := 0; i < 20; i++ {
		pos, ok = level.GetSpawnPoint()
		if !ok || pos != level.PlayerSpawnPoints[2] {
			t.Fatalf("expected the only free spawn %v, got %v ok=%v", level.PlayerSpawnPoints[2], pos, ok)
		}
	}

	level.AddEntity(CreateWall("w3", level.PlayerSpawnPoints[2]))
	if _, ok := level.GetSpawnPoint(); ok {
		t.Errorf("all spawn points blocked: expected ok=false")
	}
}

func TestGridDistanceAndBounds(t *testing.T) {
	cases := []struct {
		a, b Position
		want int
	}{
		{Position{0, 0}, Position{0, 0}, 0},
		{Position{0, 0}, Position{3, 1}, 3},
		{Position{5, 5}, Position{2, 9}, 4},
	}
	for _, c := range cases {
		if got := GridDistance(c.a, c.b); got != c.want {
			t.Errorf("GridDistance(%v,%v)=%d want %d", c.a, c.b, got, c.want)
		}
	}
	if !IsPositionInBounds(Position{0, 0}) || !IsPositionInBounds(Position{LevelSizeX - 1, LevelSizeY - 1}) {
		t.Errorf("corners should be in bounds")
	}
	if IsPositionInBounds(Position{-1, 0}) || IsPositionInBounds(Position{LevelSizeX, 0}) || IsPositionInBounds(Position{0, LevelSizeY}) {
		t.Errorf("outside positions reported in bounds")
	}
}

// Level generation is random (math/rand global source, not seedable since
// Go 1.24); check the invariants hold across many generated levels.
func TestNewLevelInvariants(t *testing.T) {
	for seed := 1; seed <= 50; seed++ {
		g := newTestGame(t)
		NewLevel(g)
		level := g.Level

		for x := 0; x < LevelSizeX; x++ {
			for _, y := range []int{0, LevelSizeY - 1} {
				if _, ok := level.GetBlockerAt(Position{x, y}); !ok {
					t.Fatalf("seed %d: border (%d,%d) not walled", seed, x, y)
				}
			}
		}
		for y := 0; y < LevelSizeY; y++ {
			for _, x := range []int{0, LevelSizeX - 1} {
				if _, ok := level.GetBlockerAt(Position{x, y}); !ok {
					t.Fatalf("seed %d: border (%d,%d) not walled", seed, x, y)
				}
			}
		}
		if len(level.PlayerSpawnPoints) != PlayerSpawnPointsPerLevel {
			t.Fatalf("seed %d: %d player spawn points, want %d", seed, len(level.PlayerSpawnPoints), PlayerSpawnPointsPerLevel)
		}
		for _, sp := range level.PlayerSpawnPoints {
			if _, blocked := level.GetBlockerAt(sp); blocked {
				t.Fatalf("seed %d: spawn point %v blocked", seed, sp)
			}
			if !level.HasClearExitPath(sp) {
				t.Fatalf("seed %d: spawn point %v has no clear exit path", seed, sp)
			}
		}
		if len(level.Enemies) == 0 {
			t.Fatalf("seed %d: no enemies spawned", seed)
		}
		for _, e := range level.Enemies {
			if !IsPositionInBounds(e.GetPosition()) {
				t.Fatalf("seed %d: enemy %s out of bounds", seed, e.GetID())
			}
		}
		entityConsistent(t, level)
	}
}

func TestSpawnRulesPerLevel(t *testing.T) {
	types := func(rules []SpawnRule) map[EnemyType]int {
		m := map[EnemyType]int{}
		for _, r := range rules {
			m[r.EnemyType] = r.Count
		}
		return m
	}
	l1 := types(GetSpawnRulesForLevel(1))
	if len(l1) != 1 || l1[EnemyGoblin] != CalculateGoblinsPerLevel(1) {
		t.Errorf("level 1 should spawn only goblins, got %v", l1)
	}
	l8 := types(GetSpawnRulesForLevel(8))
	if l8[EnemyGoblin] == 0 || l8[EnemyArcher] == 0 || l8[EnemyImp] == 0 {
		t.Errorf("level 8 should spawn all enemy types, got %v", l8)
	}
	l60 := types(GetSpawnRulesForLevel(60))
	if _, ok := l60[EnemyGoblin]; ok {
		t.Errorf("goblins should stop spawning after level 50, got %v", l60)
	}
}

func TestSpawnEnemiesUsesUniqueFreeTiles(t *testing.T) {
	g := newTestGame(t)
	g.Level.EnemySpawnPoints = []Position{{5, 5}, {6, 5}, {7, 5}}
	g.SpawnEnemies([]SpawnRule{{EnemyType: EnemyGoblin, Count: 10}})
	if len(g.Level.Enemies) != 3 {
		t.Fatalf("expected spawning capped at 3 free tiles, got %d", len(g.Level.Enemies))
	}
	if g.EnemiesSpawned != 3 {
		t.Errorf("EnemiesSpawned=%d want 3", g.EnemiesSpawned)
	}
	entityConsistent(t, g.Level)
}
