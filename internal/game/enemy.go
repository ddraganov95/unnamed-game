package game

import (
	"fmt"
)

type Enemy struct {
	Entity
	Health
	Speed
	Experience
	Direction
	LastDamageRecieved Damage
	DamageMultiplier   float64
	EquippedAttack     int
	AggroRange         int
}
type Goblin struct {
	Enemy
}
type Archer struct {
	Enemy
}
type PathNode struct {
	pos       Position
	firstStep Direction
}
type SpawnRule struct {
	Create    func(countEnemy int, pos Position) GameObject
	CalcCount func(playerLevel int) int
	Count     int
	MinLevel  int
	MaxLevel  int
}

var GlobalSpawnRules []SpawnRule

func InitSpawnRules() {
	GlobalSpawnRules = []SpawnRule{
		{
			Create:    CreateGoblin,
			CalcCount: CalculateGoblinsPerLevel,
			MinLevel:  1,
			MaxLevel:  50,
		},
		{
			Create:    CreateArcher,
			CalcCount: CalculateArchersPerLevel,
			MinLevel:  4,
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
func CreateEnemy(id string, pos Position, health Health) Enemy {
	return Enemy{
		Entity:     CreateEntity(id, pos),
		Health:     health,
		AggroRange: EnemyDefaultAggroRange,
	}
}
func (enemy *Enemy) Update(game *Game) {
	UpdateEnemy(game, enemy)
}

func (goblin *Goblin) Update(game *Game) {
	UpdateEnemy(game, goblin)
}
func (archer *Archer) Update(game *Game) {
	UpdateEnemy(game, archer)
}
func (goblin *Goblin) GetSymbol() rune {
	return SymbolGoblin
}
func (archer *Archer) GetSymbol() rune {
	return SymbolArcher
}
func CreateArcher(countArcher int, pos Position) GameObject {
	id := fmt.Sprintf("Archer %d", countArcher)
	archer := &Archer{
		Enemy: CreateEnemy(id, pos, CreateHealth(50)),
	}
	archer.ExperienceVal = ArcherDefaultExperience
	archer.EquippedAttack = AttackArrow
	archer.SetSpeed(EnemyDefaultMovementSpeed+10, EnemyDefaultAttackSpeedRanged)
	return archer
}
func CreateGoblin(countGoblin int, pos Position) GameObject {
	id := fmt.Sprintf("Goblin %d", countGoblin)
	goblin := &Goblin{
		Enemy: CreateEnemy(id, pos, CreateHealth(200)),
	}
	goblin.ExperienceVal = GoblinDefaultExperience
	goblin.EquippedAttack = AttackBasic
	goblin.SetSpeed(EnemyDefaultMovementSpeed, EnemyDefaultAttackSpeed)
	return goblin
}
func (goblin *Goblin) IsBlocking() bool {
	return true
}
func (archer *Archer) IsBlocking() bool {
	return true
}
func (goblin *Goblin) TakeDamage(damage Damage, game *Game) {
	goblin.CurrentHealth -= damage.Value
	goblin.LastDamageRecieved = damage
	//game.CreateLog("%s %s hits %s for %d damage", LogInfo, damage.EntityID, goblin.GetID(), damage.Value)
	goblin.CheckDeath(game)
}
func (archer *Archer) TakeDamage(damage Damage, game *Game) {
	archer.CurrentHealth -= damage.Value
	archer.LastDamageRecieved = damage
	game.CreateLog("%s %s say: STOP HITTING ME!!!!", LogInfo, archer.GetID())
	archer.CheckDeath(game)
}
func (enemy *Enemy) DistributeXp(game *Game) {
	game.DistributeXp(enemy.ExperienceVal)
}
func (goblin *Goblin) DistributeXp(game *Game) {
	game.DistributeXp(goblin.ExperienceVal)
}
func (archer *Archer) DistributeXp(game *Game) {
	game.DistributeXp(archer.ExperienceVal)
}
func (enemy *Enemy) GetDamageMultiplierPercent() int {
	return EnemyDefaultDamageMultipier
}
func (goblin *Goblin) GetDamageMultiplierPercent() int {
	return EnemyDefaultDamageMultipier - 70
}
func (archer *Archer) GetDamageMultiplierPercent() int {
	return EnemyDefaultDamageMultipier - 30
}
func (archer *Archer) GetProjectileSpeed() int {
	return ProjectileDefaultTravelSpeed
}
func (enemy *Enemy) SetSpeed(moveSpeed int, attackSpeed int) {
	enemy.Speed = Speed{
		MaxMovementSpeed:     moveSpeed,
		CurrentMovementSpeed: moveSpeed,
		MaxAttackSpeed:       attackSpeed,
		CurrentAttackSpeed:   attackSpeed}
}
func (archer *Archer) Move(game *Game) bool {
	return MoveEnemyGeneric(game, archer)
}
func (goblin *Goblin) Move(game *Game) bool {
	return MoveEnemyGeneric(game, goblin)
}
func (enemy *Enemy) GetDirection() Direction {
	return enemy.Direction
}
func (enemy *Enemy) GetAggroRange() int {
	return enemy.AggroRange
}
func (enemy *Enemy) Move(game *Game) bool {
	return MoveEnemyGeneric(game, enemy)
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
func MoveEnemyGeneric(game *Game, enemy GenericEnemy) bool {
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
func (enemy *Enemy) GetProjectileSpeed() int {
	return ProjectileDefaultTravelSpeed
}
func UpdateEnemy(game *Game, enemy GenericEnemy) {
	enemy.CheckDeath(game)

	player, playerExist := GetPlayerInRange(enemy.GetAggroRange(), enemy, game)
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
	enemy.ResetMovementSpeed()
}
func GetPlayerInRange(scanRange int, enemy GenericEnemy, game *Game) (*Player, bool) {
	enemyPos := enemy.GetPosition()

	for _, player := range game.Players {
		if player == nil {
			continue
		}

		// Calculate absolute distance on both axes
		dx := abs(player.Position.X - enemyPos.X)
		dy := abs(player.Position.Y - enemyPos.Y)

		// Scans the full square radius in all directions (including diagonals)
		if dx <= scanRange && dy <= scanRange {
			return player, true
		}
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
func (enemy *Enemy) CheckDeath(game *Game) {
	if !enemy.IsAlive() {
		game.CreateLog("%s %s killed %s", LogSuccess, enemy.GetLastDamageTakenFrom(), enemy.GetID())
		enemy.DistributeXp(game)
		game.Level.RemoveEntity(enemy)
	}
}
