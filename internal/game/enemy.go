package game

import (
	"fmt"
	"math"
)

type Enemy struct {
	Entity
	Health
	Speed
	Direction
	LastDamageRecieved Damage
	VoiceLine          string
	DamageMultiplier   float64
	ExperienceVal      int
	Score              int
	ProjectileSpeed    int
	EquippedAttack     int
	AggroRange         int
	EnemyType          EnemyType
	Symbol             rune
}
type EnemyType uint8

const (
	EnemyUnknown EnemyType = iota
	EnemyGoblin
	EnemyArcher
	EnemyImp
)

var EnemyBlueprints map[EnemyType]Enemy

func InitEnemyBlueprints() {
	EnemyBlueprints = map[EnemyType]Enemy{
		EnemyGoblin: {
			MaxHealth:        200,
			Symbol:           SymbolGoblin,
			Score:            5,
			ExperienceVal:    GoblinDefaultExperience,
			EquippedAttack:   AttackBasic,
			MaxMovementSpeed: EnemyDefaultMovementSpeed,
			MaxAttackSpeed:   EnemyDefaultAttackSpeed,
			ProjectileSpeed:  ProjectileDefaultTravelSpeed,
			DamageMultiplier: float64(EnemyDefaultDamageMultipier-70) / 100.0,
		},
		EnemyArcher: {
			MaxHealth:        50,
			Symbol:           SymbolArcher,
			Score:            7,
			ExperienceVal:    ArcherDefaultExperience,
			EquippedAttack:   AttackArrow,
			MaxMovementSpeed: EnemyDefaultMovementSpeed + 40,
			MaxAttackSpeed:   EnemyDefaultAttackSpeedRanged,
			ProjectileSpeed:  ProjectileDefaultTravelSpeed * 15,
			DamageMultiplier: float64(EnemyDefaultDamageMultipier-30) / 100.0,
			VoiceLine:        "STOP HITTING ME!!!!",
		},
		EnemyImp: {
			MaxHealth:        700,
			Symbol:           SymbolImp,
			Score:            15,
			ExperienceVal:    ImpDefaultExperience,
			EquippedAttack:   AttackSpell,
			MaxMovementSpeed: EnemyDefaultMovementSpeed + 20,
			MaxAttackSpeed:   EnemyDefaultAttackSpeedRanged,
			ProjectileSpeed:  ProjectileDefaultTravelSpeed * 10,
			DamageMultiplier: EnemyDefaultDamageMultipier,
		},
	}
}

type PathNode struct {
	pos       Position
	firstStep Direction
}
type SpawnRule struct {
	CalcCount func(playerLevel int) int
	Count     int
	MinLevel  int
	MaxLevel  int
	EnemyType EnemyType
}

var GlobalSpawnRules []SpawnRule

func InitSpawnRules() {
	GlobalSpawnRules = []SpawnRule{
		{
			EnemyType: EnemyGoblin,
			CalcCount: CalculateGoblinsPerLevel,
			MinLevel:  1,
			MaxLevel:  50,
		},
		{
			EnemyType: EnemyArcher,
			CalcCount: CalculateArchersPerLevel,
			MinLevel:  4,
			MaxLevel:  100,
		},
		{
			EnemyType: EnemyImp,
			CalcCount: CalculateImpsPerLevel,
			MinLevel:  8,
			MaxLevel:  100,
		},
	}
}
func (enemy *Enemy) IsAlive() bool {
	return enemy.CurrentHealth > 0
}
func (enemy *Enemy) GetLastDamageTakenFrom() string {
	return enemy.LastDamageRecieved.EntityID
}
func CreateEnemy(id string, pos Position, kind EnemyType) *Enemy {
	bp := EnemyBlueprints[kind]
	e := &Enemy{
		ID:               id,
		Position:         pos,
		Blocker:          true,
		Team:             TeamEnemy,
		Health:           CreateHealth(bp.MaxHealth),
		AggroRange:       EnemyDefaultAggroRange,
		EnemyType:        kind,
		Symbol:           bp.Symbol,
		Score:            bp.Score,
		EquippedAttack:   bp.EquippedAttack,
		DamageMultiplier: bp.DamageMultiplier,
		ProjectileSpeed:  bp.ProjectileSpeed,
		VoiceLine:        bp.VoiceLine,
	}
	e.ExperienceVal = bp.ExperienceVal
	e.SetSpeed(bp.MaxMovementSpeed, bp.MaxAttackSpeed)
	return e
}
func (enemy *Enemy) Update(game *Game) {
	UpdateEnemy(game, enemy)
}
func (t EnemyType) String() string {
	switch t {
	case EnemyGoblin:
		return "Goblin"
	case EnemyArcher:
		return "Archer"
	case EnemyImp:
		return "Imp"
	default:
		return "unknown"
	}
}
func (enemy *Enemy) GetSymbol() rune {
	return enemy.Symbol
}

func (enemy *Enemy) GetScore() int {
	return enemy.Score
}
func (enemy *Enemy) Move(game *Game) bool {
	return MoveEnemyGeneric(game, enemy)
}
func (enemy *Enemy) TakeDamage(damage Damage, game *Game) {
	enemy.CurrentHealth -= damage.Value
	enemy.LastDamageRecieved = damage
	if player, ok := game.GetPlayerByID(damage.EntityID); ok {
		player.DamageDealt += damage.Value
	}
	if enemy.VoiceLine != "" {
		game.CreateLog("%s %s say: %s", LogInfo, enemy.GetID(), enemy.VoiceLine)
	}
	enemy.CheckDeath(game)
}
func (enemy *Enemy) DistributeXp(game *Game) {
	game.DistributeXp(enemy.ExperienceVal)
}
func (enemy *Enemy) GetDamageMultiplierPercent() int {
	return int(enemy.DamageMultiplier)
}
func (enemy *Enemy) GetProjectileSpeed() int {
	return enemy.ProjectileSpeed
}
func (enemy *Enemy) SetSpeed(moveSpeed int, attackSpeed int) {
	enemy.Speed = Speed{
		MaxMovementSpeed:     moveSpeed,
		CurrentMovementSpeed: moveSpeed,
		MaxAttackSpeed:       attackSpeed,
		CurrentAttackSpeed:   attackSpeed}
}
func (enemy *Enemy) GetDirection() Direction {
	return enemy.Direction
}
func CalculateDirection(object1 GameObject, object2 GameObject) Direction {
	X1 := object1.GetPosition().X
	Y1 := object1.GetPosition().Y
	X2 := object2.GetPosition().X
	Y2 := object2.GetPosition().Y
	return Direction{
		X: maxOne(X2 - X1),
		Y: maxOne(Y2 - Y1),
	}
}
func maxOne(x int) int {
	if x == 0 {
		return 0
	}
	if x < 0 {
		return -1
	}
	return 1
}
func (enemy *Enemy) LowerMovementSpeed(speed int) {
	enemy.CurrentMovementSpeed = enemy.CurrentMovementSpeed - speed
}
func (enemy *Enemy) ResetMovementSpeed() {
	enemy.CurrentMovementSpeed = enemy.MaxMovementSpeed
}
func (enemy *Enemy) GetMovementAvailable() bool {
	return enemy.CurrentMovementSpeed <= 0
}
func (enemy *Enemy) LowerAttackSpeed(speed int) {
	enemy.CurrentAttackSpeed = enemy.CurrentAttackSpeed - speed
}
func (enemy *Enemy) ResetAttackSpeed() {
	enemy.CurrentAttackSpeed = enemy.MaxAttackSpeed
}
func (enemy *Enemy) GetAttackAvailable() bool {
	return enemy.CurrentAttackSpeed <= 0
}
func (enemy *Enemy) IsEnemy() bool {
	return true
}
func UpdateEnemy(game *Game, enemy *Enemy) {
	enemy.CheckDeath(game)

	player, playerExist := GetPlayerInRange(enemy.AggroRange, enemy, game)
	if !playerExist {
		return
	}
	dir := CalculateDirection(enemy, player)
	enemy.SetDirection(dir)

	enemy.LowerAttackSpeed(1)
	if game.Attack(enemy) {
		return
	}

	enemy.LowerMovementSpeed(1)
	if !enemy.GetMovementAvailable() {
		return
	}
	//If we are in range no need to try to move. Half the range to account for diagonal + don't actually need to be full max range all the time. Add 1 to account for basic attack
	if _, playerInAttackRange := GetPlayerInRange(enemy.GetEquippedAttack().Range/2+1, enemy, game); playerInAttackRange {
		return
	}
	//Try to move normally. If it fails, try to side-step!
	if !enemy.Move(game) {
		enemy.SetDirection(GetNextStepDirection(enemy.GetPosition(), player.GetPosition(), game))
		enemy.Move(game)
	}
	//We moved check into what
	CheckTileImpact(game, enemy)

	enemy.ResetMovementSpeed()
}
func MoveEnemyGeneric(game *Game, enemy *Enemy) bool {
	currentPos := enemy.GetPosition()
	dir := enemy.GetDirection()
	nextPosition := Position{
		X: currentPos.X + dir.X,
		Y: currentPos.Y + dir.Y,
	}

	if !IsPositionInBounds(nextPosition) {
		return false
	}
	if _, ok := game.Level.GetBlockerAt(nextPosition); ok {
		return false
	}

	game.Level.MoveEntity(enemy, nextPosition)
	return true
}
func CheckTileImpact(game *Game, enemy *Enemy) {
	pos := enemy.GetPosition()
	for _, entity := range game.Level.posEntities[pos] {
		proj, ok := entity.(*Projectile)
		if !ok {
			continue
		}
		if game.DealDamage(proj.Attacker, proj.Attack, enemy) {
			game.Level.RemoveProjectile(proj)
			return
		}
	}
}
func GetPlayerInRange(scanRange int, enemy *Enemy, game *Game) (*Player, bool) {
	enemyPos := enemy.GetPosition()

	var closestPlayer *Player
	minDist := math.MaxInt

	for _, player := range game.GetActivePlayers() {
		if player == nil || !player.IsAlive() {
			continue
		}

		// Calculate absolute distance on both axes
		dx := abs(player.Position.X - enemyPos.X)
		dy := abs(player.Position.Y - enemyPos.Y)

		// Check if player is within the bounding scan range box
		if dx <= scanRange && dy <= scanRange {
			// Use squared Euclidean distance to compare true distance without floating-point math
			dist := dx*dx + dy*dy

			if dist < minDist {
				minDist = dist
				closestPlayer = player
			}
		}
	}

	if closestPlayer != nil {
		return closestPlayer, true
	}

	return nil, false
}
func (enemy *Enemy) GetEquippedAttack() Attack {
	return GlobalAttacks[enemy.EquippedAttack]
}
func (enemy *Enemy) SetDirection(dir Direction) {
	enemy.Direction = dir
}
func GetNextStepDirection(start Position, target Position, game *Game) Direction {
	if start == target {
		return Direction{X: 0, Y: 0}
	}

	// 8-way movement (Cardinals + Diagonals)
	directions := []Direction{
		{X: 0, Y: -1}, {X: 0, Y: 1}, {X: -1, Y: 0}, {X: 1, Y: 0},
		{X: -1, Y: -1}, {X: 1, Y: -1}, {X: -1, Y: 1}, {X: 1, Y: 1},
	}

	queue := []PathNode{}
	visited := make(map[Position]bool)
	visited[start] = true

	// Initialize queue with valid first steps from start
	for _, dir := range directions {
		nextPos := Position{X: start.X + dir.X, Y: start.Y + dir.Y}
		if !IsPositionInBounds(nextPos) {
			continue
		}

		isBlocker := false
		if nextPos != target {
			if _, blocked := game.Level.GetBlockerAt(nextPos); blocked {
				isBlocker = true
			}
		}

		if !isBlocker {
			queue = append(queue, PathNode{pos: nextPos, firstStep: dir})
			visited[nextPos] = true
		}
	}

	// Process BFS queue
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		if curr.pos == target {
			return curr.firstStep
		}

		for _, dir := range directions {
			nextPos := Position{X: curr.pos.X + dir.X, Y: curr.pos.Y + dir.Y}
			if !IsPositionInBounds(nextPos) || visited[nextPos] {
				continue
			}

			isBlocker := false
			if nextPos != target {
				if _, blocked := game.Level.GetBlockerAt(nextPos); blocked {
					isBlocker = true
				}
			}

			if !isBlocker {
				visited[nextPos] = true
				queue = append(queue, PathNode{pos: nextPos, firstStep: curr.firstStep})
			}
		}
	}

	// Fallback if completely blocked / no path
	return Direction{X: 0, Y: 0}
}
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
func CalculateGoblinsPerLevel(playerLevel int) int {
	return 1 + playerLevel/2
}
func CalculateArchersPerLevel(playerLevel int) int {
	return 1 + playerLevel/4
}
func CalculateImpsPerLevel(playerLevel int) int {
	return 1 + playerLevel/10
}
func (enemy *Enemy) CheckDeath(game *Game) {
	if !enemy.IsAlive() {
		killer := enemy.GetLastDamageTakenFrom()
		if player, ok := game.GetPlayerByID(killer); ok {
			player.EnemiesKilled++
			player.AddScore(enemy.GetScore())
		}
		game.AchievementEngine.PublishEvent(AchievementEvent{
			PlayerIDs: game.GetActiveAndAlivePlayerID(),
			Key:       fmt.Sprintf("kill:%s", enemy.EnemyType), // Produces "KILL:goblin", "KILL:archer", etc.
			Amount:    1,
		})

		enemy.DistributeXp(game)
		game.CreateLog("Trying to remove enemy %s", enemy.GetID())
		game.Level.RemoveEnemy(enemy)
	}
}
