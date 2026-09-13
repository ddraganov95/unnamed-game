package game

type Identifiable interface {
	GetID() string
}
type Positionable interface {
	GetPosition() Position
	SetPosition(position Position)
}
type Blocker interface {
	IsBlocking() bool
}
type Teamer interface {
	GetTeam() Team
}
type GameObject interface {
	Identifiable
	Positionable
	Blocker
	Teamer
}
type Attackable interface {
	Identifiable
	Positionable
	Teamer
	TakeDamage(Damage, *Game)
}
type Attacker interface {
	Identifiable
	Positionable
	Teamer
	GetDirection() Direction
	GetEquippedAttack() Attack
	GetDamageMultiplierPercent() int
	GetProjectileSpeed() int
}

type Drawable interface {
	Positionable
	GetSymbol() rune
}
type Effect interface {
	Drawable
	Identifiable
	Update(game *Game)
}
type Living interface {
	IsAlive() bool
}
