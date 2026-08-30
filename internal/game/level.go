package game

import (
	"fmt"
	"math/rand"
)

type Size struct {
	sizeX, sizeY int
}
type Level struct {
	Size
	Entities          map[string]GameObject
	posEntities       map[Position][]GameObject
	Effects           map[string]Effect
	AllAttacks        map[int]Attack
	PlayerSpawnPoints []Position
	EnemySpawnPoints  []Position
	Floor             [][]rune
}
type Zone struct {
	Entity
	Size
}
type LevelConfig struct {
	Width  int
	Height int
	Rules  []SpawnRule
}

func (level *Level) GetEntityAt(pos Position) ([]GameObject, bool) {
	entities, exists := level.posEntities[pos]
	var activeEntities []GameObject
	for _, entity := range entities {
		activeEntities = append(activeEntities, entity)
	}
	if len(activeEntities) == 0 {
		return nil, false
	}
	return activeEntities, exists
}
func (level *Level) GetBlockerAt(pos Position) (Blocker, bool) {
	if entities, found := level.GetEntityAt(pos); found {
		for _, entity := range entities {
			if blocker, ok := entity.(Blocker); ok {
				// If you are using the IsBlocking() method check:
				if blocker.IsBlocking() {
					return blocker, true
				}
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
func (level *Level) GetSize() (int, int) {
	return LevelSizeX, LevelSizeY
}
func PrepareLevel() *Level {
	fmt.Print("Preparing Level...\r\n")
	//creates an empty level
	return &Level{
		Entities:    make(map[string]GameObject),
		posEntities: make(map[Position][]GameObject),
		Effects:     make(map[string]Effect)}
}
func (level *Level) InitializeField() {
	X, Y := level.GetSize()
	level.sizeX = X
	level.sizeY = Y
	tile := SymbolDefaultLevelTile
	level.Floor = make([][]rune, Y)
	for row := 0; row < Y; row++ {
		level.Floor[row] = make([]rune, X)
		for column := 0; column < X; column++ {
			level.Floor[row][column] = tile
		}
	}
}
func (level *Level) InitializeWalls() {
	// Top wall: start at (0,0), step right (dx=1, dy=0), for 'x' length
	level.GenerateLine(0, 0, 1, 0, level.sizeX, CreateTopWall, "TopWall")

	// Bottom wall: start at bottom-left (0, y-1), step right (dx=1, dy=0), for 'x' length
	level.GenerateLine(0, level.sizeY-1, 1, 0, level.sizeX, CreateTopWall, "BotWall")

	// Left wall: start at (0,0), step down (dx=0, dy=1), for 'y' length
	level.GenerateLine(0, 0, 0, 1, level.sizeY, CreateSideWall, "LeftWall")

	// Right wall: start at top-right (x-1, 0), step down (dx=0, dy=1), for 'y' length
	level.GenerateLine(level.sizeX-1, 0, 0, 1, level.sizeY, CreateSideWall, "RightWall")
}
func (level *Level) InitializeSpawnPoints() {
	// Reuses the shared unblocked map scan
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
func NewLevel(game *Game) {
	fmt.Println("Entering level gen...")
	level := PrepareLevel()
	level.InitializeField()
	level.InitializeWalls()
	level.InitializeZoneObjects()
	level.InitializeSpawnPoints()
	game.Events = nil
	game.Level = *level
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
func (level *Level) PutEntityAtPosition(entity Positionable, pos Position) bool {
	if _, taken := level.GetEntityAt(pos); !taken {
		entity.SetPosition(pos)
		level.AddEntity(entity)
		return true
	}
	return false
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
				enemy := rule.Create(spawned, pos)
				level.AddEntity(enemy)
				spawned++
			}
		}
	}
}
func (level *Level) CreateRandomPosition() Position {
	return Position{
		X: rand.Intn(level.sizeX-2) + 1,
		Y: rand.Intn(level.sizeY-2) + 1,
	}
}
func (level *Level) InitRandomZone() GameObject {
	width := rand.Intn(MaxRegularZoneSizeX) + MinRegularZoneSizeX
	height := rand.Intn(MaxRegularZoneSizeY) + MinRegularZoneSizeY

	// Ensure zones stay strictly within the inner area (away from outer walls 0 and size-1)
	maxRangeX := level.sizeX - width - 2
	maxRangeY := level.sizeY - height - 2

	// Force x and y to start at least at 1 and end before the outer border
	x := rand.Intn(maxRangeX) + 1
	y := rand.Intn(maxRangeY) + 1

	return &Zone{
		Size: Size{
			sizeX: width,
			sizeY: height,
		},
		Entity: Entity{
			Position: Position{
				X: x,
				Y: y,
			},
			ID: fmt.Sprintf("Zone_%d_%d", x, y),
		},
	}
}
func (level *Level) InitializeZoneObjects() {
	for i := 0; i < MaxRegularZonesPerLevel; i++ {
		zone := level.InitRandomZone()

		if builder, ok := zone.(*Zone); ok {
			level.InitializeZone(builder)
		}
	}
}
func (level *Level) InitializeZone(zone *Zone) {
	startX, startY := zone.Position.X, zone.Position.Y
	w, h := zone.GetSize()
	wallCreator := zone.GetWallCreator()

	level.GenerateLine(startX, startY, 1, 0, w, wallCreator, zone.GetID()+"Top")
	level.GenerateLine(startX, startY+h-1, 1, 0, w, wallCreator, zone.GetID()+"Bot")
	level.GenerateLine(startX, startY, 0, 1, h, wallCreator, zone.GetID()+"Left")
	level.GenerateLine(startX+w-1, startY, 0, 1, h, wallCreator, zone.GetID()+"Right")

	if zone.GetInteriorFilled() {
		for y := 1; y < h-1; y++ {
			for x := 1; x < w-1; x++ {
				posX := startX + x
				posY := startY + y
				id := fmt.Sprintf("%s_tile_%d+%d", zone.GetID(), posX, posY)
				pos := Position{X: posX, Y: posY}

				tile := wallCreator(id, pos)
				level.AddEntity(tile)
			}
		}
	}
}
func (level *Level) Update(game *Game) {
	if game.State != StateGamePlaying {
		return
	}
	activePlayers := game.GetActivePlayers()
	if len(activePlayers) == 0 {
		return //If everyone refreshed or disconnected, don't trigger a fake "Game Over"
	}
	//Check if all players died
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

	//Check if all enemies are dead -> Enter Intermission instead of instantly loading next level
	aliveEnemies := false
	for _, enemy := range game.Level.Entities {
		if genericEnemy, ok := enemy.(GenericEnemy); ok {
			if genericEnemy.IsAlive() {
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
		game.State = StateGameIntermission
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
func (zone *Zone) GetSize() (int, int) {
	return zone.sizeX, zone.sizeY
}
func (zone *Zone) GetSymbol() rune {
	return SymbolTopWall
}
func (zone *Zone) IsBlocking() bool {
	return true
}
func (zone *Zone) GetInteriorFilled() bool {
	return false
}
func (zone *Zone) GetWallCreator() func(string, Position) GameObject {
	return CreateTopWall
}
func (level *Level) HasClearExitPath(pos Position) bool {
	// Check Up
	upClear := true
	for i := 1; i <= MaxRegularZoneSizeY; i++ {
		checkPos := Position{X: pos.X, Y: pos.Y - i}
		if checkPos.Y <= 0 {
			upClear = false
			break
		}
		if _, blocked := level.GetBlockerAt(checkPos); blocked {
			upClear = false
			break
		}
	}
	if upClear {
		return true
	}

	// Check Down
	downClear := true
	for i := 1; i <= MaxRegularZoneSizeY; i++ {
		checkPos := Position{X: pos.X, Y: pos.Y + i}
		if checkPos.Y >= level.sizeY-1 {
			downClear = false
			break
		}
		if _, blocked := level.GetBlockerAt(checkPos); blocked {
			downClear = false
			break
		}
	}
	if downClear {
		return true
	}

	// Check Left
	leftClear := true
	for i := 1; i <= MaxRegularZoneSizeX; i++ {
		checkPos := Position{X: pos.X - i, Y: pos.Y}
		if checkPos.X <= 0 {
			leftClear = false
			break
		}
		if _, blocked := level.GetBlockerAt(checkPos); blocked {
			leftClear = false
			break
		}
	}
	if leftClear {
		return true
	}

	// Check Right
	rightClear := true
	for i := 1; i <= MaxRegularZoneSizeX; i++ {
		checkPos := Position{X: pos.X + i, Y: pos.Y}
		if checkPos.X >= level.sizeX-1 {
			rightClear = false
			break
		}
		if _, blocked := level.GetBlockerAt(checkPos); blocked {
			rightClear = false
			break
		}
	}
	if rightClear {
		return true
	}

	return false
}
func (level *Level) GetUnblockedInnerPositions() []Position {
	var availableSpots []Position
	for y := 1; y < level.sizeY-1; y++ {
		for x := 1; x < level.sizeX-1; x++ {
			pos := Position{X: x, Y: y}
			if _, blocked := level.GetBlockerAt(pos); !blocked {
				availableSpots = append(availableSpots, pos)
			}
		}
	}
	return availableSpots
}
func GridDistance(pos1, pos2 Position) int {
	dx := GetAbsoluteValue(pos1.X - pos2.X)
	dy := GetAbsoluteValue(pos1.Y - pos2.Y)

	if dx > dy {
		return dx
	}
	return dy
}
func GetAbsoluteValue(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
func IsPositionInBounds(pos Position) bool {
	return pos.X >= 0 &&
		pos.X < LevelSizeX &&
		pos.Y >= 0 &&
		pos.Y < LevelSizeY
}
func (game *Game) DistributeXp(xp int) {
	for _, player := range game.GetActivePlayers() {
		player.GainXp(xp)
	}
}
func (game *Game) GetAveragePlayerLevel() int {
	if len(game.GetActivePlayers()) == 0 {
		return 1
	}
	totalLevel := 0
	for _, player := range game.GetActivePlayers() {
		totalLevel += player.Level
	}
	return totalLevel / len(game.GetActivePlayers())
}
func GetSpawnRulesForLevel(playerLevel int) []SpawnRule {
	var validRules []SpawnRule
	for _, rule := range GlobalSpawnRules {
		if playerLevel >= rule.MinLevel && playerLevel <= rule.MaxLevel {
			rule.Count = rule.CalcCount(playerLevel)
			validRules = append(validRules, rule)
		}
	}
	return validRules
}
