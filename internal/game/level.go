package game

import (
	"fmt"
	"log"
	"math/rand"
	"time"
)

type Level struct {
	CreatedAt         time.Time
	Entities          map[string]GameObject
	posEntities       map[Position][]GameObject
	Effects           map[string]Effect
	AllAttacks        map[int]Attack
	PlayerSpawnPoints []Position
	EnemySpawnPoints  []Position
	Floor             [][]rune
	Enemies           []*Enemy
	Projectiles       []*Projectile
}
type Zone struct {
	Entity
	WallCreator func(id string, pos Position) GameObject
	sizeX       int
	sizeY       int
}

func InitializeLevel() *Level {
	fmt.Print("Preparing Level...\r\n")
	return &Level{
		Entities:    make(map[string]GameObject),
		posEntities: make(map[Position][]GameObject),
		Effects:     make(map[string]Effect),
		CreatedAt:   time.Now(),
	}
}

func NewLevel(game *Game) {
	fmt.Println("Entering level gen...")
	level := InitializeLevel()
	level.InitializeField()
	level.InitializeWalls()
	level.InitializeZoneObjects()
	level.InitializeSpawnPoints()

	game.Events = nil
	game.Level = level
	game.LevelNumber++

	for _, player := range game.GetActivePlayers() {
		if !player.IsAlive() {
			continue
		}
		game.SpawnPlayer(player)
		player.HealToFull()
		player.LevelsCompleted++
	}

	fmt.Println("Players Spawned...")
	rules := GetSpawnRulesForLevel(game.GetAveragePlayerLevel())
	level.SpawnEnemies(rules)
	fmt.Println("Finished level gen...")
}

func (level *Level) Update(game *Game) {
	if game.State != StateGamePlaying {
		return
	}
	activePlayers := game.GetActivePlayers()
	if len(activePlayers) == 0 {
		return
	}

	alivePlayers := false
	for _, player := range activePlayers {
		if player.IsAlive() {
			alivePlayers = true
			break
		}
	}
	if !alivePlayers {
		game.CreateLog("%s Everyone Died", LogError)
		game.CreateLog("%s Game Over!", LogError)
		game.State = StateGameOver
		return
	}

	aliveEnemies := false
	for _, entity := range game.Level.Entities {
		if entity.GetTeam() == TeamEnemy {
			if living, ok := entity.(Living); ok && living.IsAlive() {
				aliveEnemies = true
				break
			}
		}
	}
	if !aliveEnemies {
		game.CreateLog("%s Enemies Are All dead", LogSuccess)
		for _, player := range game.GetActivePlayers() {
			player.LevelsCompleted++
		}
		elapsedSeconds := int64(time.Since(level.CreatedAt).Seconds())
		game.AchievementEngine.PublishEvent(AchievementEvent{
			PlayerIDs: game.GetActiveAndAlivePlayerID(),
			Key:       "TIME:level_speedrun",
			Amount:    elapsedSeconds,
		})
		game.State = StateGameIntermission
	}
}

func (level *Level) InitializeField() {
	tile := SymbolDefaultLevelTile
	level.Floor = make([][]rune, LevelSizeY)
	for row := 0; row < LevelSizeY; row++ {
		level.Floor[row] = make([]rune, LevelSizeX)
		for column := 0; column < LevelSizeX; column++ {
			level.Floor[row][column] = tile
		}
	}
}

func (level *Level) InitializeWalls() {
	level.GenerateLine(0, 0, 1, 0, LevelSizeX, CreateWall, "TopWall")
	level.GenerateLine(0, LevelSizeY-1, 1, 0, LevelSizeX, CreateWall, "BotWall")
	level.GenerateLine(0, 0, 0, 1, LevelSizeY, CreateWall, "LeftWall")
	level.GenerateLine(LevelSizeX-1, 0, 0, 1, LevelSizeY, CreateWall, "RightWall")
}

func (level *Level) InitializeSpawnPoints() {
	allPositions := level.GetUnblockedInnerPositions()
	availablePositions := level.GetExitablePositions(allPositions)

	rand.Shuffle(len(availablePositions), func(i, j int) {
		availablePositions[i], availablePositions[j] = availablePositions[j], availablePositions[i]
	})

	desiredCount := PlayerSpawnPointsPerLevel
	if len(availablePositions) < desiredCount {
		desiredCount = len(availablePositions)
	}

	level.PlayerSpawnPoints = availablePositions[:desiredCount]
	level.EnemySpawnPoints = availablePositions[desiredCount:]
}

func (level *Level) InitializeZoneObjects() {
	for i := 0; i < MaxRegularZonesPerLevel; i++ {
		level.AddZone(level.NewRandomZone())
	}
}

func (level *Level) NewRandomZone() *Zone {
	width := rand.Intn(MaxRegularZoneSizeX) + MinRegularZoneSizeX
	height := rand.Intn(MaxRegularZoneSizeY) + MinRegularZoneSizeY

	maxRangeX := LevelSizeX - width - 2
	maxRangeY := LevelSizeY - height - 2

	x := rand.Intn(maxRangeX) + 1
	y := rand.Intn(maxRangeY) + 1

	return &Zone{
		sizeX:       width,
		sizeY:       height,
		ID:          fmt.Sprintf("Zone_%d_%d", x, y),
		Position:    Position{X: x, Y: y},
		Blocker:     true,
		WallCreator: CreateWall,
	}
}

func (level *Level) AddZone(zone *Zone) {
	startX, startY := zone.Position.X, zone.Position.Y
	w, h := zone.GetSize()

	level.GenerateLine(startX, startY, 1, 0, w, zone.WallCreator, zone.GetID()+"Top")
	level.GenerateLine(startX, startY+h-1, 1, 0, w, zone.WallCreator, zone.GetID()+"Bot")
	level.GenerateLine(startX, startY, 0, 1, h, zone.WallCreator, zone.GetID()+"Left")
	level.GenerateLine(startX+w-1, startY, 0, 1, h, zone.WallCreator, zone.GetID()+"Right")

	if zone.GetInteriorFilled() {
		for y := 1; y < h-1; y++ {
			for x := 1; x < w-1; x++ {
				posX := startX + x
				posY := startY + y
				id := fmt.Sprintf("%s_tile_%d+%d", zone.GetID(), posX, posY)
				pos := Position{X: posX, Y: posY}

				tile := zone.WallCreator(id, pos)
				level.AddEntity(tile)
			}
		}
	}
}

func (level *Level) GenerateLine(startX, startY, dx, dy, length int, create func(string, Position) GameObject, prefix string) {
	for i := 0; i < length; i++ {
		x := startX + (i * dx)
		y := startY + (i * dy)
		id := fmt.Sprintf("%s_%d+%d", prefix, x, y)
		pos := Position{X: x, Y: y}
		linePiece := create(id, pos)
		level.AddEntity(linePiece)
	}
}

func (level *Level) SpawnEnemies(rules []SpawnRule) {
	availablePositions := level.EnemySpawnPoints

	rand.Shuffle(len(availablePositions), func(i, j int) {
		availablePositions[i], availablePositions[j] = availablePositions[j], availablePositions[i]
	})

	posIndex := 0
	for _, rule := range rules {
		spawned := 0
		for posIndex < len(availablePositions) && spawned < rule.Count {
			pos := availablePositions[posIndex]
			posIndex++
			if _, taken := level.GetEntityAt(pos); !taken {
				id := fmt.Sprintf("%s %d", rule.EnemyType.String(), spawned+1)
				enemy := CreateEnemy(id, pos, rule.EnemyType)
				log.Printf("add enemy")
				level.AddEnemy(enemy)
				spawned++
			}
		}
	}
}

func (level *Level) GetEntityAt(pos Position) ([]GameObject, bool) {
	entities, exists := level.posEntities[pos]
	if len(entities) == 0 {
		return nil, false
	}
	return entities, exists
}

func (level *Level) GetBlockerAt(pos Position) (GameObject, bool) {
	if entities, found := level.GetEntityAt(pos); found {
		for _, entity := range entities {
			if entity.IsBlocking() {
				return entity, true
			}

		}
	}
	return nil, false
}

func (level *Level) GetAttackableAt(pos Position) (Attackable, bool) {
	if entities, found := level.GetEntityAt(pos); found {
		for _, entity := range entities {
			if attackable, ok := entity.(Attackable); ok {
				return attackable, true
			}
		}
	}
	return nil, false
}

func (level *Level) GetSpawnPoint() (Position, bool) {
	index := rand.Intn(len(level.PlayerSpawnPoints))
	pos := level.PlayerSpawnPoints[index]
	if _, blocked := level.GetBlockerAt(pos); !blocked {
		return pos, true
	} else {
		for _, spawnPos := range level.PlayerSpawnPoints {
			if _, blocked := level.GetBlockerAt(spawnPos); !blocked {
				return spawnPos, true
			}
		}
	}
	return Position{}, false
}

func (level *Level) PutEntityAtPosition(entity GameObject, pos Position) bool {
	if _, taken := level.GetEntityAt(pos); !taken {
		entity.SetPosition(pos)
		level.AddEntity(entity)
		return true
	}
	return false
}

func (level *Level) CreateRandomPosition() Position {
	return Position{
		X: rand.Intn(LevelSizeX-2) + 1,
		Y: rand.Intn(LevelSizeY-2) + 1,
	}
}

func (level *Level) GetExitablePositions(allPositions []Position) []Position {
	var availablePositions []Position
	for _, pos := range allPositions {
		if level.HasClearExitPath(pos) {
			availablePositions = append(availablePositions, pos)
		}
	}
	return availablePositions
}

func (level *Level) HasClearExitPath(pos Position) bool {
	dirs := []Position{{0, -1}, {0, 1}, {-1, 0}, {1, 0}}
	limits := []int{MaxRegularZoneSizeY, MaxRegularZoneSizeY, MaxRegularZoneSizeX, MaxRegularZoneSizeX}

	for i, d := range dirs {
		clear := true
		for step := 1; step <= limits[i]; step++ {
			checkPos := Position{X: pos.X + d.X*step, Y: pos.Y + d.Y*step}
			if checkPos.X <= 0 || checkPos.X >= LevelSizeX-1 || checkPos.Y <= 0 || checkPos.Y >= LevelSizeY-1 {
				clear = false
				break
			}
			if _, blocked := level.GetBlockerAt(checkPos); blocked {
				clear = false
				break
			}
		}
		if clear {
			return true
		}
	}
	return false
}

func (level *Level) GetUnblockedInnerPositions() []Position {
	var availableSpots []Position
	for y := 1; y < LevelSizeY-1; y++ {
		for x := 1; x < LevelSizeX-1; x++ {
			pos := Position{X: x, Y: y}
			if _, blocked := level.GetBlockerAt(pos); !blocked {
				availableSpots = append(availableSpots, pos)
			}
		}
	}
	return availableSpots
}

func (zone *Zone) GetSize() (int, int) {
	return zone.sizeX, zone.sizeY
}

func (zone *Zone) GetInteriorFilled() bool {
	return false
}

func GridDistance(pos1, pos2 Position) int {
	dx := abs(pos1.X - pos2.X)
	dy := abs(pos1.Y - pos2.Y)

	if dx > dy {
		return dx
	}
	return dy
}

func IsPositionInBounds(pos Position) bool {
	return pos.X >= 0 &&
		pos.X < LevelSizeX &&
		pos.Y >= 0 &&
		pos.Y < LevelSizeY
}

func GetSpawnRulesForLevel(playerLevel int) []SpawnRule {
	var validRules []SpawnRule
	for _, rule := range GlobalSpawnRules {
		if playerLevel >= rule.MinLevel && playerLevel <= rule.MaxLevel {
			ruleCopy := rule
			ruleCopy.Count = rule.CalcCount(playerLevel)
			validRules = append(validRules, ruleCopy)
		}
	}
	return validRules
}
