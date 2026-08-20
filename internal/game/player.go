package game

import "math"

type Player struct {
	Entity
	Health
	Experience
	Direction
	KeyBindings        map[rune]func(g *Game)
	TypingKeyBindings  map[rune]func(g *Game)
	KeyQueue           []rune
	UnlockedAttacks    []Attack
	LastDamageRecieved Damage
	PlayerState        PlayerState
	MessageBuffer      string
	EquippedAttack     int
}
type PlayerState int

const (
	StatePlaying PlayerState = iota
	StateTyping
)

func (s PlayerState) String() string {
	switch s {
	case StatePlaying:
		return "Playing"
	case StateTyping:
		return "Typing"
	default:
		return "Unknown"
	}
}
func (player *Player) MoveUp(game *Game) {
	player.Direction = Direction{X: 0, Y: -1}
	nextPosition := Position{X: player.Position.X, Y: player.Position.Y - 1}
	if nextPosition.Y < 0 {
		return
	}
	if _, ok := game.Level.GetBlockerAt(nextPosition); ok {
		return
	}
	game.Level.MoveEntity(player, nextPosition)
}
func (player *Player) MoveDown(game *Game) {
	player.Direction = Direction{X: 0, Y: 1}
	nextPosition := Position{X: player.Position.X, Y: player.Position.Y + 1}
	if nextPosition.Y >= game.Level.sizeY {
		return
	}
	if _, ok := game.Level.GetBlockerAt(nextPosition); ok {
		return
	}
	game.Level.MoveEntity(player, nextPosition)
}
func (player *Player) MoveLeft(game *Game) {
	player.Direction = Direction{X: -1, Y: 0}
	nextPosition := Position{X: player.Position.X - 1, Y: player.Position.Y}
	if nextPosition.X < 0 {
		return
	}
	if _, ok := game.Level.GetBlockerAt(nextPosition); ok {
		return
	}
	game.Level.MoveEntity(player, nextPosition)
}
func (player *Player) MoveRight(game *Game) {
	player.Direction = Direction{X: 1, Y: 0}
	nextPosition := Position{X: player.Position.X + 1, Y: player.Position.Y}
	if nextPosition.X >= game.Level.sizeX {
		return
	}
	if _, ok := game.Level.GetBlockerAt(nextPosition); ok {
		return
	}
	game.Level.MoveEntity(player, nextPosition)
}
func (player *Player) Update(game *Game) {
	keyPresses := player.KeyQueue
	if player.PlayerState == StatePlaying {
		for _, key := range keyPresses {
			if function, exists := player.KeyBindings[key]; exists {
				function(game)
			}
		}
		player.KeyQueue = nil
		return
	}
	if player.PlayerState == StateTyping {
		for _, key := range keyPresses {
			if function, exists := player.TypingKeyBindings[key]; exists {
				function(game)
			} else {
				player.MessageBuffer += string(key)
			}
		}
	}
	player.KeyQueue = nil
}
func (player *Player) GetSymbol() rune {
	return SymbolPlayer
}
func (p *Player) IsAlive() bool {
	return p.CurrentHealth > 0
}
func NewPlayer(id string) *Player {
	player := &Player{
		Entity:         Entity{ID: id},
		Health:         Health{CurrentHealth: 100, MaxHealth: 100},
		Direction:      Direction{X: 0, Y: 0},
		EquippedAttack: AttackBasic,
		PlayerState:    StatePlaying,
	}
	player.Experience = Experience{Level: 1, ExperienceVal: 0}
	player.UnlockAttacks()
	player.InitKeybindings()
	player.InitTypingKeybindings()
	return player
}
func (player *Player) Attack(game *Game) {
	player.GetAttacksSlice()[player.EquippedAttack].Execute(game, player)
}
func (player *Player) GetEquippedAttack() Attack {
	return player.UnlockedAttacks[player.EquippedAttack]
}
func (player *Player) ChangeEquippedAttack(game *Game) {
	if player.EquippedAttack == len(player.GetAttacksSlice())-1 {
		player.EquippedAttack = 0
		return
	}
	player.EquippedAttack++
}
func (player *Player) GetAttacksSlice() []Attack {
	return player.UnlockedAttacks
}
func (player *Player) GetDirection() Direction {
	return player.Direction
}
func (player *Player) IsBlocking() bool {
	return true
}
func (player *Player) TakeDamage(damage Damage, game *Game) {
	damageToTake := (damage.Value * (100 - PlayerDamageReductionPercent)) / 100
	player.CurrentHealth -= int(damageToTake)
	player.LastDamageRecieved = damage
	game.CreateLog("%s %s hits %s for %d damage", LogInfo, damage.EntityID, player.GetID(), damageToTake)
}
func (player *Player) GetDamageMultiplierPercent() int {
	return PlayerDefaultDamageMultipier * 2 * player.Level
}
func (player *Player) IsEnemy() bool {
	return false
}
func (player *Player) GetProjectileSpeed() int {
	return 0
}
func (player *Player) GainXp(xp int) {
	player.ExperienceVal += xp
	if player.LevelUp() {
		player.UnlockAttacks()
	}
}
func (player *Player) LevelUp() bool {
	leveledUp := false
	for player.ExperienceVal >= GetXpRequiredForNextLevel(player.Level) {
		player.ExperienceVal -= GetXpRequiredForNextLevel(player.Level)
		player.Level++
		leveledUp = true
	}
	return leveledUp
}
func (player *Player) UnlockAttacks() {
	var available []Attack
	for _, attack := range GlobalAttacks {
		if player.Level >= attack.RequiredLevel {
			available = append(available, attack)
		}
	}
	if len(available) > len(player.UnlockedAttacks) {
		player.EquippedAttack = len(available) - 1
	}
	player.UnlockedAttacks = available
}
func GetXpRequiredForNextLevel(currentLevel int) int {
	return int(math.Round(PlayerLevelOneExperience * math.Pow(PlayerLevelXpRequirementMultiplier, float64(currentLevel-1))))
}
