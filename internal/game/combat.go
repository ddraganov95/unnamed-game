package game

import (
	"fmt"
	"time"
)

type Direction struct {
	X, Y int
}
type Attack struct {
	Direction
	Damage        *Damage
	Name          string
	RequiredLevel int
	Range         int
	Execute       func(game *Game, attacker Attacker)
}
type Damage struct {
	EntityID string
	Type     int
	Value    int
}
type Projectile struct {
	Entity
	Direction
	Speed
	Attack   *Attack
	SenderID string
	IsEnemy  bool
}
type Arrow struct {
	Projectile
}
type Spell struct {
	Projectile
}

const (
	AttackBasic = iota
	AttackArrow
	AttackSpell
)
const (
	BasicDamage = iota
	ArrowDamage
	SpellDamage
)

var GlobalAttacks []Attack

func InitAttacks() {
	GlobalAttacks = []Attack{
		AttackBasic: {
			Name:          "Basic",
			Range:         BasicAttackBaseRange,
			Execute:       BasicAttack,
			RequiredLevel: 1,
			Damage:        &Damage{Value: BasicAttackBaseDamage, Type: BasicDamage},
		},
		AttackArrow: {
			Name:          "Arrow",
			Range:         ArrowAttackBaseRange,
			Execute:       ArrowAttack,
			RequiredLevel: 3,
			Damage:        &Damage{Value: ArrowAttackBaseDamage, Type: ArrowDamage},
		},
		AttackSpell: {
			Name:          "Spell",
			Range:         SpellAttackBaseRange,
			Execute:       SpellAttack,
			RequiredLevel: 6,
			Damage:        &Damage{Value: SpellAttackBaseDamage, Type: SpellDamage},
		},
	}
}
func (projectile *Projectile) GetSenderID() string {
	return projectile.SenderID
}
func CreateProjectile(attacker Attacker, attack Attack) Projectile {
	pos := attacker.GetPosition()
	dir := attacker.GetDirection()

	// Generate a unique projectile ID using the attacker's ID and timestamp
	projID := fmt.Sprintf("%s_proj_%d", attacker.GetID(), time.Now().UnixNano())

	return Projectile{
		Entity:    CreateEntity(projID, pos),
		Direction: dir,
		SenderID:  attacker.GetID(),
		Attack:    &attack,
		IsEnemy:   attacker.IsEnemy(),
		Speed: Speed{MaxMovementSpeed: attacker.GetProjectileSpeed(),
			CurrentMovementSpeed: attacker.GetProjectileSpeed()},
	}
}
func (arrow *Arrow) GetSymbol() rune {
	switch {
	case arrow.Direction.Y == 0 && arrow.Direction.X == -1:
		return SymbolArrowLeft
	case arrow.Direction.Y == 0 && arrow.Direction.X == 1:
		return SymbolArrowRight
	case arrow.Direction.Y == 1 && arrow.Direction.X == 0:
		return SymbolArrovDown
	case arrow.Direction.Y == -1 && arrow.Direction.X == 0:
		return SymbolArrowUp
	case arrow.Direction.Y == -1 && arrow.Direction.X == -1:
		return SymbolArrowUpLeft
	case arrow.Direction.Y == -1 && arrow.Direction.X == 1:
		return SymbolArrowUpRight
	case arrow.Direction.Y == 1 && arrow.Direction.X == -1:
		return SymbolArrowDownLeft
	case arrow.Direction.Y == 1 && arrow.Direction.X == 1:
		return SymbolArrowDownRight
	}
	return SymbolArrowLeft
}
func (spell *Spell) GetSymbol() rune {
	switch {
	case spell.Direction.Y == 0 && spell.Direction.X == -1:
		return SymbolSpellLeft
	case spell.Direction.Y == 0 && spell.Direction.X == 1:
		return SymbolSpellRight
	case spell.Direction.Y == 1 && spell.Direction.X == 0:
		return SymbolSpellDown
	case spell.Direction.Y == -1 && spell.Direction.X == 0:
		return SymbolSpellUp
	case spell.Direction.Y == -1 && spell.Direction.X == -1:
		return SymbolSpellUpLeft
	case spell.Direction.Y == -1 && spell.Direction.X == 1:
		return SymbolSpellUpRight
	case spell.Direction.Y == 1 && spell.Direction.X == -1:
		return SymbolSpellDownLeft
	case spell.Direction.Y == 1 && spell.Direction.X == 1:
		return SymbolSpellDownRight
	}
	return SymbolSpellUp
}
func (projectile *Projectile) GetEntityID() string {
	if projectile.Attack.Damage != nil {
		return projectile.Attack.Damage.EntityID
	}
	return projectile.SenderID
}
func (projectile *Projectile) ResetMovementSpeed() {
	projectile.CurrentMovementSpeed = projectile.MaxMovementSpeed
}
func (attack *Attack) GetDamage() *Damage {
	return attack.Damage
}
func CreateDamage(attacker Attacker, ttype int, value int) *Damage {
	return &Damage{
		Type:     ttype,
		Value:    (value * (100 + attacker.GetDamageMultiplierPercent())) / 100,
		EntityID: attacker.GetID(),
	}
}
func CreateAttack(direction Direction, damage *Damage, name string) *Attack {
	return &Attack{
		Direction: direction,
		Damage:    damage,
		Name:      name,
	}
}
func CreateArrow(attacker Attacker, attack *Attack) GameObject {
	return &Arrow{
		Projectile: CreateProjectile(attacker, *attack),
	}
}

func CreateSpell(attacker Attacker, attack *Attack) GameObject {
	return &Spell{
		Projectile: CreateProjectile(attacker, *attack),
	}
}
func (projectile *Projectile) Update(game *Game) {
	projectile.CurrentMovementSpeed--
	if projectile.CurrentMovementSpeed > 0 {
		return
	}

	projectile.Attack.Range--
	//Check Range
	if projectile.Attack.Range <= 0 {
		game.Level.RemoveEntity(projectile)
		return
	}
	newPos := Position{
		X: projectile.Position.X + projectile.Direction.X,
		Y: projectile.Position.Y + projectile.Direction.Y,
	}
	if projectile.Direction.X != 0 && projectile.Direction.Y != 0 {
		projectile.Attack.Range--
	}
	// Check bounds
	if newPos.X < 0 || newPos.X >= game.Level.sizeX ||
		newPos.Y < 0 || newPos.Y >= game.Level.sizeY {
		game.Level.RemoveEntity(projectile)
		return
	}

	// Check attackable entities
	if attackable, exists := game.Level.GetAttackableAt(newPos); exists && !(projectile.IsEnemy && attackable.IsEnemy()) {
		game.DealDamage(*projectile.Attack, attackable)
		game.Level.RemoveEntity(projectile)
		return
	}

	// Check blockers
	if blocker, blocked := game.Level.GetBlockerAt(newPos); blocked && !(projectile.IsEnemy && blocker.IsEnemy()) {
		game.Level.RemoveEntity(projectile)
		return
	}

	game.Level.MoveEntity(projectile, newPos)
}
func (game *Game) DealDamage(attack Attack, target GameObject) {
	damage := attack.GetDamage()
	if damage == nil {
		return
	}
	if attackable, ok := target.(Attackable); ok {
		attackable.TakeDamage(*attack.Damage, game)
		id := CreateID("%s_effect", attack.GetDamage().EntityID)
		game.Level.AddEffect(CreateHitEffect(id, target.GetPosition(), 3))
	}
}
func (attack Attack) String() string {
	return attack.Name
}
func ArrowAttack(game *Game, attacker Attacker) {
	blueprint := GlobalAttacks[AttackArrow]

	attack := Attack{
		Name:      blueprint.Name,
		Range:     blueprint.Range,
		Direction: attacker.GetDirection(),
		Damage: CreateDamage(
			attacker,
			blueprint.Damage.Type,
			blueprint.Damage.Value,
		),
	}
	game.Level.AddEntity(CreateArrow(attacker, &attack))
}

func SpellAttack(game *Game, attacker Attacker) {
	blueprint := GlobalAttacks[AttackSpell]

	attack := Attack{
		Name:      blueprint.Name,
		Range:     blueprint.Range,
		Direction: attacker.GetDirection(),
		Damage: CreateDamage(
			attacker,
			blueprint.Damage.Type,
			blueprint.Damage.Value,
		),
	}
	game.Level.AddEntity(CreateSpell(attacker, &attack))
}
func BasicAttack(game *Game, attacker Attacker) {
	blueprint := GlobalAttacks[AttackBasic]

	attack := Attack{
		Name:      blueprint.Name,
		Range:     blueprint.Range,
		Direction: attacker.GetDirection(),
		Damage: CreateDamage(
			attacker,
			blueprint.Damage.Type,
			blueprint.Damage.Value,
		),
	}

	attackPosition := Position{
		X: attacker.GetPosition().X + attacker.GetDirection().X,
		Y: attacker.GetPosition().Y + attacker.GetDirection().Y,
	}
	if entity, exists := game.Level.GetAttackableAt(attackPosition); exists {
		game.DealDamage(attack, entity)
	}
}
func (game *Game) Attack(attacker GenericEnemy) bool {
	if !attacker.GetAttackAvailable() {
		return false
	}
	attack := attacker.GetEquippedAttack()
	attackRange := attack.Range

	_, playerExist := GetPlayerInRange(attackRange, attacker, game)
	if !playerExist {
		return false
	}
	attack.Execute(game, attacker)
	attacker.ResetAttackSpeed()
	return true
}
