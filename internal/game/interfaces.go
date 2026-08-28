package game

type Updateable interface {
	GameObject
	Update(game *Game)
}
type Attackable interface {
	GameObject
	TakeDamage(Damage, *Game)
	IsEnemy() bool
}
type Attacker interface {
	GameObject
	GetDirection() Direction
	GetEquippedAttack() Attack
	GetDamageMultiplierPercent() int
	IsEnemy() bool
	GetProjectileSpeed() int
}
type GameObject interface {
	GetID() string
	GetPosition() Position
}
type Blocker interface {
	IsBlocking() bool
	IsEnemy() bool
}
type Drawable interface {
	GetSymbol() rune
}
type Effect interface {
	Drawable
	Positionable
	Updateable
}
type Positionable interface {
	GameObject
	SetPosition(position Position)
}
type GenericEnemy interface {
	Positionable
	Attacker
	IsAlive() bool
	GetLastDamageTakenFrom() string
	GetAggroRange() int
	Move(game *Game) bool
	LowerMovementSpeed(speed int)
	GetMovementAvailable() bool
	ResetMovementSpeed()
	LowerAttackSpeed(speed int)
	GetAttackAvailable() bool
	ResetAttackSpeed()
	SetDirection(Direction)
	DistributeXp(game *Game)
	CheckDeath(game *Game)
}
