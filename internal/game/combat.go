package game

import (
	"fmt"
	"sync/atomic"
)

type Direction struct {
	X, Y int
}
type Attack struct {
	Execute func(game *Game, attacker Attacker)
	Damage  Damage
	Direction
	Name          string
	RequiredLevel int
	Range         int
}
type Damage struct {
	EntityID string
	Type     int
	Value    int
}

const (
	ProjectileArrow = iota
	ProjectileSpell
	ProjectileOrb
)

type Projectile struct {
	OnUpdate func()
	OnRemove func()
	Entity
	Direction
	Speed
	Attack   Attack
	Attacker Attacker
	SenderID string
	Kind     int
}

const (
	AttackBasic = iota
	AttackArrow
	AttackSpell
	AttackOrb
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
			Execute:       NewMeleeExecutor(),
			RequiredLevel: 1,
			Damage:        Damage{Value: BasicAttackBaseDamage, Type: BasicDamage},
		},
		AttackArrow: {
			Name:          "Arrow",
			Range:         ArrowAttackBaseRange,
			Execute:       NewRangedExecutor(AttackArrow, ProjectileArrow),
			RequiredLevel: 3,
			Damage:        Damage{Value: ArrowAttackBaseDamage, Type: ArrowDamage},
		},
		AttackSpell: {
			Name:          "Spell",
			Range:         SpellAttackBaseRange,
			Execute:       NewRangedExecutor(AttackArrow, ProjectileSpell),
			RequiredLevel: 15,
			Damage:        Damage{Value: SpellAttackBaseDamage, Type: SpellDamage},
		},
		AttackOrb: {
			Name:          "Orb",
			Range:         SpellAttackBaseRange,
			Execute:       NewOrbExecutor(AttackOrb, ProjectileOrb),
			RequiredLevel: 35,
			Damage:        Damage{Value: SpellAttackBaseDamage, Type: SpellDamage},
		},
	}
}
func NewMeleeExecutor() func(game *Game, attacker Attacker) {
	return func(game *Game, attacker Attacker) {
		attack := GlobalAttacks[AttackBasic]
		attack.Direction = attacker.GetDirection()
		attack.Damage = CreateDamage(attacker, attack.Damage.Type, attack.Damage.Value)
		attackPosition := Position{
			X: attacker.GetPosition().X + attacker.GetDirection().X,
			Y: attacker.GetPosition().Y + attacker.GetDirection().Y,
		}
		if entity, exists := game.Level.GetAttackableAt(attackPosition); exists {
			game.DealDamage(attacker, attack, entity)
		}
	}
}
func NewRangedExecutor(attackIndex int, style int) func(game *Game, attacker Attacker) {
	return func(game *Game, attacker Attacker) {
		attack := GlobalAttacks[attackIndex]
		attack.Direction = attacker.GetDirection()
		attack.Damage = CreateDamage(attacker, attack.Damage.Type, attack.Damage.Value)
		proj := CreateProjectile(attacker, attack, style)
		game.Level.AddProjectile(proj)
	}
}
func NewOrbExecutor(attackIndex int, style int) func(game *Game, attacker Attacker) {
	return func(game *Game, attacker Attacker) {
		attack := GlobalAttacks[attackIndex]
		attack.Direction = attacker.GetDirection()
		forward := attacker.GetDirection()
		backward := Direction{X: -forward.X, Y: -forward.Y} // Directly opposite direction
		attack.Direction = forward
		attack.Damage = CreateDamage(attacker, attack.Damage.Type, attack.Damage.Value)
		proj := CreateProjectile(attacker, attack, style)
		all8Directions := []Direction{
			{X: 0, Y: -1},  // N
			{X: 1, Y: -1},  // NE
			{X: 1, Y: 0},   // E
			{X: 1, Y: 1},   // SE
			{X: 0, Y: 1},   // S
			{X: -1, Y: 1},  // SW
			{X: -1, Y: 0},  // W
			{X: -1, Y: -1}, // NW
		}

		// Filter out straight-ahead and straight-behind once during setup
		validDirections := make([]Direction, 0, 6)
		for _, d := range all8Directions {
			if d == forward || d == backward {
				continue
			}
			validDirections = append(validDirections, d)
		}

		dirIndex := 0
		ticks := 0
		proj.OnUpdate = func() {
			ticks++
			if ticks%10 != 0 { // Fire only on every so frames
				return
			}
			projAttack := GlobalAttacks[AttackArrow]
			shootDir := validDirections[dirIndex]
			dirIndex = (dirIndex + 1) % len(validDirections)
			projAttack.Direction = shootDir
			projAttack.Damage = CreateDamage(attacker, attack.Damage.Type, attack.Damage.Value)
			subProj := CreateProjectile(attacker, projAttack, ProjectileArrow)
			subProj.SetPosition(proj.GetPosition())
			game.Level.AddProjectile(subProj)
		}
		proj.OnRemove = func() {

			for _, direction := range all8Directions {
				projAttack := GlobalAttacks[AttackArrow]
				projAttack.Direction = direction
				projAttack.Damage = CreateDamage(attacker, attack.Damage.Type, attack.Damage.Value)
				subProj := CreateProjectile(attacker, projAttack, ProjectileArrow)
				subProj.SetPosition(proj.GetPosition())
				game.Level.AddProjectile(subProj)
			}
		}

		game.Level.AddProjectile(proj)
	}
}
func (projectile *Projectile) GetSenderID() string {
	return projectile.SenderID
}

var projectileCounter uint64

func CreateProjectile(attacker Attacker, attack Attack, kind int) *Projectile {
	pos := attacker.GetPosition()
	//dir := attacker.GetDirection()

	// Generate a unique projectile ID using the attacker's ID and counter
	idNum := atomic.AddUint64(&projectileCounter, 1)
	projID := fmt.Sprintf("%s_proj_%d", attacker.GetID(), idNum)

	return &Projectile{
		Entity:               CreateEntity(projID, pos, attacker.GetTeam()),
		Direction:            attack.Direction,
		SenderID:             attacker.GetID(),
		Attack:               attack,
		Attacker:             attacker,
		CurrentMovementSpeed: 0,
		MaxMovementSpeed:     attacker.GetProjectileSpeed(),
		Kind:                 kind,
	}
}
func (p *Projectile) GetSymbol() rune {
	switch p.Kind {
	case ProjectileArrow:
		switch {
		case p.Direction.Y == 0 && p.Direction.X == -1:
			return SymbolArrowLeft
		case p.Direction.Y == 0 && p.Direction.X == 1:
			return SymbolArrowRight
		case p.Direction.Y == 1 && p.Direction.X == 0:
			return SymbolArrovDown
		case p.Direction.Y == -1 && p.Direction.X == 0:
			return SymbolArrowUp
		case p.Direction.Y == -1 && p.Direction.X == -1:
			return SymbolArrowUpLeft
		case p.Direction.Y == -1 && p.Direction.X == 1:
			return SymbolArrowUpRight
		case p.Direction.Y == 1 && p.Direction.X == -1:
			return SymbolArrowDownLeft
		case p.Direction.Y == 1 && p.Direction.X == 1:
			return SymbolArrowDownRight
		}
		return SymbolArrowLeft

	case ProjectileSpell:
		switch {
		case p.Direction.Y == 0 && p.Direction.X == -1:
			return SymbolSpellLeft
		case p.Direction.Y == 0 && p.Direction.X == 1:
			return SymbolSpellRight
		case p.Direction.Y == 1 && p.Direction.X == 0:
			return SymbolSpellDown
		case p.Direction.Y == -1 && p.Direction.X == 0:
			return SymbolSpellUp
		case p.Direction.Y == -1 && p.Direction.X == -1:
			return SymbolSpellUpLeft
		case p.Direction.Y == -1 && p.Direction.X == 1:
			return SymbolSpellUpRight
		case p.Direction.Y == 1 && p.Direction.X == -1:
			return SymbolSpellDownLeft
		case p.Direction.Y == 1 && p.Direction.X == 1:
			return SymbolSpellDownRight
		}
		return SymbolSpellUp
	case ProjectileOrb:
		return SymbolOrb
	}
	return ' '
}

func (projectile *Projectile) ResetMovementSpeed() {
	projectile.CurrentMovementSpeed = projectile.MaxMovementSpeed
}
func CreateDamage(attacker Attacker, ttype int, value int) Damage {
	return Damage{
		Type:     ttype,
		Value:    (value * (100 + attacker.GetDamageMultiplierPercent())) / 100,
		EntityID: attacker.GetID(),
	}
}
func (projectile *Projectile) Update(game *Game) {
	//fmt.Printf("Projectile %s tried to move with %v speed\n", projectile.GetEntityID(), projectile.CurrentMovementSpeed)
	if projectile.OnUpdate != nil {
		projectile.OnUpdate()
	}
	if projectile.CurrentMovementSpeed > 0 {
		projectile.CurrentMovementSpeed--
		return
	}
	//fmt.Printf("Projectile %s actually moved!\n", projectile.GetEntityID())
	projectile.ResetMovementSpeed()
	projectile.Attack.Range--
	//Check Range
	if projectile.Attack.Range <= 0 {
		game.Level.RemoveProjectile(projectile)
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
	if newPos.X < 0 || newPos.X >= LevelSizeX ||
		newPos.Y < 0 || newPos.Y >= LevelSizeY {
		game.Level.RemoveProjectile(projectile)
		return
	}

	// Check attackable entities
	if attackable, exists := game.Level.GetAttackableAt(newPos); exists {
		if ok := game.DealDamage(projectile.Attacker, projectile.Attack, attackable); ok {
			game.Level.RemoveProjectile(projectile)
			return
		}
		game.Level.MoveEntity(projectile, newPos)
		return
	}

	// Check blockers
	if _, blocked := game.Level.GetBlockerAt(newPos); blocked {
		//log.Println("projectile blocked")
		game.Level.RemoveProjectile(projectile)
		return
	}

	game.Level.MoveEntity(projectile, newPos)
}
func (game *Game) DealDamage(attacker Attacker, attack Attack, target Attackable) bool {
	if attacker.GetTeam() == target.GetTeam() {
		return false
	}
	target.TakeDamage(attack.Damage, game)
	id := fmt.Sprintf("%s_effect", attack.Damage.EntityID)
	game.Level.AddEffect(CreateHitEffect(id, target.GetPosition(), 3))
	return true

}
func (attack Attack) String() string {
	return attack.Name
}
func (game *Game) Attack(attacker *Enemy) bool {
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
