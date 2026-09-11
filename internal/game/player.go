package game

import (
	"log"
	"strings"
	"time"
	"unicode"
)

type Player struct {
	Entity
	Health
	Experience
	Direction
	PlayerSessionSummary
	KeyBindings         map[rune]func(g *Game)
	TypingKeyBindings   map[rune]func(g *Game)
	DisplayChan         chan string
	KeyQueue            []rune
	UnlockedAttacks     []Attack
	LastDamageRecieved  Damage
	MessageBuffer       string
	PlayerState         PlayerState
	PlayerTypingChannel PlayerTypingChannel
	AFKMinutes          int
	EquippedAttack      int
}
type PlayerState int

const (
	StatePlaying PlayerState = iota
	StateTyping
	StateDisconnected
)

type PlayerTypingChannel int

const (
	StateTypingGameChat PlayerTypingChannel = iota
	StateTypingGlobalChat
)

func (s PlayerState) String() string {
	switch s {
	case StatePlaying:
		return "Playing"
	case StateTyping:
		return "Typing"
	case StateDisconnected:
		return "Disconnected"
	default:
		return "Unknown"
	}
}
func (player *Player) MoveUp(game *Game) {
	if !player.IsAlive() {
		return
	}
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
	if !player.IsAlive() {
		return
	}
	player.Direction = Direction{X: 0, Y: 1}
	nextPosition := Position{X: player.Position.X, Y: player.Position.Y + 1}
	if nextPosition.Y >= LevelSizeY {
		return
	}
	if _, ok := game.Level.GetBlockerAt(nextPosition); ok {
		return
	}
	game.Level.MoveEntity(player, nextPosition)
}
func (player *Player) MoveLeft(game *Game) {
	if !player.IsAlive() {
		return
	}
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
	if !player.IsAlive() {
		return
	}
	player.Direction = Direction{X: 1, Y: 0}
	nextPosition := Position{X: player.Position.X + 1, Y: player.Position.Y}
	if nextPosition.X >= LevelSizeX {
		return
	}
	if _, ok := game.Level.GetBlockerAt(nextPosition); ok {
		return
	}
	game.Level.MoveEntity(player, nextPosition)
}
func (player *Player) UpdatePlayer(game *Game) {
	//Naming is intentional so it doesn't get Updatable interface and doesnt get updated with all the entities
	//This is made so that we can always process player inputs regardless of game state
	if player.PlayerState == StateDisconnected {
		return
	}
	keyPresses := player.KeyQueue
	if player.PlayerState == StatePlaying {
		for _, key := range keyPresses {
			if function, exists := player.KeyBindings[unicode.ToLower(key)]; exists {
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
				player.AddToMessageBuffer(key)
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
func NewPlayer(id string, keybinds map[string]string) *Player {
	player := &Player{
		ID:            id,
		CurrentHealth: 100, MaxHealth: 100,
		X: 0, Y: 0,
		Blocker:             true,
		EquippedAttack:      AttackBasic,
		PlayerState:         StatePlaying,
		PlayerTypingChannel: StateTypingGameChat,
		DisplayChan:         make(chan string, 100),
		PlayerID:            id,
		SessionStart:        time.Now(),
	}
	player.Experience = Experience{Level: 1, ExperienceVal: 0, NextLevelXP: XpRequirements[1]}
	player.UnlockAttacks()
	player.InitKeybindings(keybinds)
	player.InitTypingKeybindings()

	return player
}
func (player *Player) Attack(game *Game) {
	if !player.IsAlive() {
		return
	}
	player.UnlockedAttacks[player.EquippedAttack].Execute(game, player)
}
func (player *Player) GetEquippedAttack() Attack {
	return player.UnlockedAttacks[player.EquippedAttack]
}
func (player *Player) ChangeEquippedAttack(game *Game) {
	if player.EquippedAttack == len(player.UnlockedAttacks)-1 {
		player.EquippedAttack = 0
		return
	}
	player.EquippedAttack++
}
func (player *Player) GetDirection() Direction {
	return player.Direction
}
func (player *Player) TakeDamage(damage Damage, game *Game) {
	damageToTake := int((damage.Value * (100 - PlayerDamageReductionPercent)) / 100)
	player.CurrentHealth -= damageToTake
	player.LastDamageRecieved = damage
	log.Printf("[PLAYER]: Took damage %d from %s", player.LastDamageRecieved.Value, damage.EntityID)
	player.DamageTaken += damageToTake
	player.CheckDeath(game)
	game.CreateLog("%s %s hits %s for %d damage", LogInfo, damage.EntityID, player.GetID(), damageToTake)
}
func (player *Player) CheckDeath(game *Game) {
	if !player.IsAlive() {
		killer := player.GetLastDamageTakenFrom()
		player.KilledBy = killer
		log.Printf("[PLAYER]: Killed by %s, last damage taken from: %s", killer, player.LastDamageRecieved.EntityID)
		player.CurrentHealth = 0
		game.CreateLog("%s %s Died to %s", LogSuccess, player.GetID(), killer)
		game.Level.RemoveEntity(player)
	}
}
func (player *Player) GetDamageMultiplierPercent() int {
	return PlayerDefaultDamageMultipier * 2 * player.Level
}
func (player *Player) IsEnemy() bool {
	return false
}
func (player *Player) GetProjectileSpeed() int {
	return 2
}
func (player *Player) GainXp(xp int) {
	if !player.IsAlive() {
		return
	}
	player.ExperienceVal += xp
	player.XPGained += xp
	if player.LevelUp() {
		player.UnlockAttacks()
	}
}
func (player *Player) LevelUp() bool {
	leveledUp := false
	for player.ExperienceVal >= player.NextLevelXP {
		player.ExperienceVal -= player.NextLevelXP
		player.Level++
		leveledUp = true
		player.NextLevelXP = XpRequirements[player.Level]
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
func (player *Player) GetLastDamageTakenFrom() string {
	return player.LastDamageRecieved.EntityID
}
func (player *Player) AddScore(score int) {
	scoreToAdd := int(float64(score) * (1.0 + float64(player.LevelsCompleted)*0.1))
	player.Score += scoreToAdd
}

// ============================================================================
// Keybindings & Input Management
// ============================================================================
func (player *Player) EnqueueKey(r rune) {
	player.KeyQueue = append(player.KeyQueue, r)
}
func (player *Player) InitKeybindings(playerKeybinds map[string]string) {
	// Map JSON config keys to player action methods
	nameToMethod := map[string]func(g *Game){
		"move_up":      player.MoveUp,
		"move_down":    player.MoveDown,
		"move_left":    player.MoveLeft,
		"move_right":   player.MoveRight,
		"attack_enemy": player.Attack,
		"quit_game":    player.QuitGame,
		"swap_attack":  player.ChangeEquippedAttack,
	}

	finalKeybinds := make(map[rune]func(g *Game))

	// Dynamically assign configured keybindings
	for configKey, method := range nameToMethod {
		if str, ok := playerKeybinds[configKey]; ok && len(str) > 0 {
			char := []rune(str)[0] // Convert 1-char string to rune
			finalKeybinds[char] = method
		}
	}

	finalKeybinds['\r'] = player.ChangeTypeState
	finalKeybinds[' '] = player.GetNextLevel
	finalKeybinds['c'] = player.CopyGameId

	player.KeyBindings = finalKeybinds
}
func (player *Player) InitTypingKeybindings() {
	player.TypingKeyBindings = map[rune]func(g *Game){
		'\r': player.ChangeTypeState,
		'\b': player.RemoveLastByteMessage,
		127:  player.RemoveLastByteMessage,
		' ':  player.ChangePlayerTypingChannel,
	}
}

func (player *Player) QuitGame(game *Game) {
	game.AchievementEngine.PublishEvent(AchievementEvent{
		PlayerIDs: []string{player.GetID()},
		Key:       "GAME:default",
		Amount:    0,
	})
	game.EventChan <- GameEvent{
		Type:     EventTypeDisconnect,
		PlayerID: player.GetID(),
	}
	game.CreateLog("Quitting Game...")
}
func (player *Player) GetNextLevel(game *Game) {
	log.Printf("[DEBUG] %s ,Pressed Space!", player.GetID())
	if !player.IsAlive() && game.State != StateGameIntermission {
		return
	}
	game.EventChan <- GameEvent{
		Type:     EventTypeGetNextLevel,
		PlayerID: player.GetID(),
	}
}
func (player *Player) CopyGameId(game *Game) {
	game.ServerEventChan <- ServerEvent{
		Type:     EventTypeCopyGame,
		PlayerID: player.GetID(),
		Value:    player.GameID,
	}
	game.CreateLog("Game ID copied!")
}

// ============================================================================
// Chat & Typing Subsystem
// ============================================================================
func (player *Player) ChangeTypeState(game *Game) {
	if player.PlayerState == StateTyping {
		player.SendMessage(game)
		player.PlayerState = StatePlaying
		return
	}
	player.PlayerState = StateTyping
}
func (player *Player) ChangePlayerTypingChannel(game *Game) {
	if strings.HasPrefix(player.MessageBuffer, "/1") {
		player.PlayerTypingChannel = StateTypingGameChat
		player.MessageBuffer = strings.TrimPrefix(player.MessageBuffer, "/1")
		return
	}
	if strings.HasPrefix(player.MessageBuffer, "/2") {
		player.PlayerTypingChannel = StateTypingGlobalChat
		player.MessageBuffer = strings.TrimPrefix(player.MessageBuffer, "/2")
		return
	}
	player.AddToMessageBuffer(' ')
}
func (player *Player) GetCursor() string {
	switch player.PlayerTypingChannel {
	case StateTypingGameChat:
		return ChatCursorGameChat
	case StateTypingGlobalChat:
		return ChatCursorGlobalChat
	default:
		return ChatCursorDefault
	}
}
func (player *Player) RemoveLastByteMessage(game *Game) {
	if len(player.MessageBuffer) > 0 {
		player.MessageBuffer = player.MessageBuffer[:len(player.MessageBuffer)-1]
	}
}
func (player *Player) SendMessage(game *Game) {
	if len(player.MessageBuffer) > 0 {
		if player.PlayerTypingChannel == StateTypingGameChat {
			game.ServerEventChan <- ServerEvent{
				Type:     EventTypeGameChatMsg,
				PlayerID: player.GetID(),
				Value:    player.MessageBuffer}
		}
		if player.PlayerTypingChannel == StateTypingGlobalChat {
			game.ServerEventChan <- ServerEvent{
				Type:     EventTypeGlobalChatMsg,
				PlayerID: player.GetID(),
				Value:    player.MessageBuffer}
		}
		player.MessageBuffer = ""
	}
}
func (player *Player) AddToMessageBuffer(key rune) {
	maxAllowedLength := MaxChatMessageLength - 5
	if len(player.MessageBuffer) < maxAllowedLength {
		player.MessageBuffer += string(key)
	}
}
