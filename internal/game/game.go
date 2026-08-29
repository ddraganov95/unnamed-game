package game

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

type Game struct {
	Level           Level
	Events          []Event
	Frame           [][]rune
	GlobalChat      chan string
	EventChan       chan GameEvent   //Channel so server sends info to the game
	ServerEventChan chan ServerEvent //Channel so that game can send info to the server
	DestroyChan     chan struct{}
	ChatHistory     []string
	Players         []*Player
	Mu              sync.RWMutex
	State           GameState
	EmptyMinutes    int
}
type GameState int

const (
	StateGamePlaying GameState = iota
	StateGameOver
	StateGameIntermission
)

func InitializeGame() *Game {
	//creates an empty game state and initializes the game. This function should be called before StartGame.
	fmt.Print("Preparing Game...\r\n")
	game := &Game{
		State:       StateGamePlaying,
		DestroyChan: make(chan struct{}),
	}
	game.InitFrame()
	InitAttacks()
	InitSpawnRules()
	InitEventChan(game)
	NewLevel(game)
	return game
}
func StartGame(game *Game) {
	ticker := time.NewTicker(30 * time.Millisecond)
	defer ticker.Stop()
	log.Println("[DEBUG] Start Game")
	for {
		select {
		case <-game.DestroyChan:
			log.Println("[DEBUG] StartGame loop terminated.")
			return
		case <-ticker.C:
			//log.Println("[DEBUG] game loop start")
			game.Mu.Lock()
			game.ProcessInputs()
			game.PollGlobalChat()
			for _, player := range game.GetActivePlayers() {
				player.UpdatePlayer(game)
			}
			if game.State == StateGamePlaying {
				UpdateGame(game, game.EventChan)
			}
			game.Mu.Unlock()
			//log.Println("[DEBUG] game updated")
			for _, player := range game.GetActivePlayers() {
				var renderedFrame string
				switch game.State {
				case StateGamePlaying:
					renderedFrame = game.DrawLevelForPlayer(game.Level, player)
				case StateGameIntermission:
					renderedFrame = game.DrawLevelIntermissionForPlayer(game.Level, player)
				case StateGameOver:
					renderedFrame = game.DrawGameOverForPlayer(game.Level, player)
				}
				select {
				case player.DisplayChan <- renderedFrame:
				default:
				}
			}
		}
	}
}
func UpdateGame(game *Game, eventChan chan GameEvent) {
	UpdateMap(game.Level.Entities, game)
	UpdateMap(game.Level.Effects, game)
	game.Level.Update(game) //Check win/loss conditions
}
func (game *Game) DrawLevelForPlayer(level Level, player *Player) string {
	//Reset the frame
	game.ClearFrame()
	//Draw Logs
	game.DrawLogsPanel()
	//Draw the shared world
	game.DrawEntities(game.Level)
	game.DrawEffects(game.Level)
	//Draw global chat
	game.DrawGlobalChatPanel(level, player)
	//Layer the player-specific UI on top
	if player != nil {
		game.DrawPlayerHUD(player)
	}
	//Return the composed frame string to the game loop
	return game.FlushFrame()
}
func (game *Game) DrawSummaryScreenForPlayer(level Level, player *Player, summaryLines []string) string {
	game.ClearFrame()
	game.DrawLogsPanel()
	game.DrawGlobalChatPanel(level, player)

	startRow := 4
	startCol := MaxMessageLength + 4

	for rIdx, line := range summaryLines {
		targetRow := startRow + rIdx
		if targetRow >= MaxScreenHeight {
			break
		}
		for cIdx, ch := range line {
			targetCol := startCol + cIdx
			if targetCol < MaxScreenWidth {
				game.Frame[targetRow][targetCol] = ch
			}
		}
	}

	if player != nil {
		game.DrawPlayerHUD(player)
	}

	return game.FlushFrame()
}
func (game *Game) DrawLevelIntermissionForPlayer(level Level, player *Player) string {
	var summary PlayerSessionSummary
	if player != nil {
		summary = player.GenerateSummary()
	}
	return game.DrawSummaryScreenForPlayer(level, player, GetSummaryLines(summary))
}

func (game *Game) DrawGameOverForPlayer(level Level, player *Player) string {
	var summary PlayerSessionSummary
	if player != nil {
		summary = player.GenerateSummary()
	}
	return game.DrawSummaryScreenForPlayer(level, player, GetGameOverSummaryLines(summary))
}
func (game *Game) SpawnPlayer(player *Player) {
	//Check if the player is already in the slice so level transitions don't duplicate them
	alreadyExists := false
	for _, p := range game.Players {
		if p.GetID() == player.GetID() {
			alreadyExists = true
			break
		}
	}
	if !alreadyExists {
		game.Players = append(game.Players, player)
		log.Printf("Successfully spawned player %s", player.GetID())
	}
	pos, available := game.Level.GetSpawnPoint()
	if !available {
		log.Println("No available spawn points found!")
		return
	}
	if !game.Level.PutEntityAtPosition(player, pos) {
		log.Println("Failed to place player: Spawn point was blocked.")
		return
	}
	log.Printf("Successfully spawned player at %d, %d", pos.X, pos.Y)
}
func (game *Game) InitFrame() {
	game.Frame = make([][]rune, MaxScreenHeight)
	for row := 0; row < MaxScreenHeight; row++ {
		game.Frame[row] = make([]rune, MaxScreenWidth)
	}
}
func (game *Game) PollGlobalChat() {
	for {
		select {
		case msg := <-game.GlobalChat:
			game.ChatHistory = append(game.ChatHistory, msg)
			if len(game.ChatHistory) > MaxChatHistory {
				game.ChatHistory = game.ChatHistory[1:]
			}
		default:
			return
		}
	}
}
func (game *Game) ClearFrame() {
	for row := 0; row < MaxScreenHeight; row++ {
		for col := 0; col < MaxScreenWidth; col++ {
			game.Frame[row][col] = SymbolDefault
		}
	}
}
func (game *Game) DrawEntities(level Level) {
	for _, entity := range level.Entities {
		game.DrawObject(level, entity)
	}
}
func (game *Game) DrawEffects(level Level) {
	for _, effect := range level.Effects {
		game.DrawObject(level, effect)
	}
}
func (game *Game) DrawObject(level Level, obj GameObject) {
	drawable, ok := obj.(Drawable)
	if !ok {
		return
	}

	X, Y := level.GetSize()
	pos := obj.GetPosition()

	if pos.X < 0 || pos.X >= X ||
		pos.Y < 0 || pos.Y >= Y {
		return
	}

	targetRow := VerticalPadding + pos.Y
	targetCol := MaxMessageLength + 3 + pos.X

	if targetRow < MaxScreenHeight &&
		targetCol < MaxScreenWidth {
		game.Frame[targetRow][targetCol] = drawable.GetSymbol()
	}
}
func (game *Game) DrawGlobalChatPanel(level Level, player *Player) {
	gameStartCol := MaxMessageLength + 3
	chatStartCol := gameStartCol + LevelSizeX + 3

	//Draw chat history
	for i, msg := range game.ChatHistory {
		row := VerticalPadding + i
		if row >= MaxScreenHeight {
			break
		}
		for colIdx, ch := range msg {
			targetCol := chatStartCol + colIdx
			if targetCol < MaxScreenWidth {
				game.Frame[row][targetCol] = ch
			}
		}
	}

	//Draw the cursor and message if player is typing
	if player != nil && player.PlayerState == StateTyping {
		typingRow := VerticalPadding + len(game.ChatHistory) + 1
		if typingRow < MaxScreenHeight {
			prompt := fmt.Sprintf("%s: %s_", ChatCursor, player.MessageBuffer)
			for colIdx, ch := range prompt {
				targetCol := chatStartCol + colIdx
				if targetCol < MaxScreenWidth {
					game.Frame[typingRow][targetCol] = ch
				}
			}
		}
	}
}
func (game *Game) FlushFrame() string {
	var sb strings.Builder
	sb.WriteString("\033[H")
	for _, row := range game.Frame {
		sb.WriteString(string(row))
		sb.WriteString("\r\n")
	}
	return sb.String()
}
func (game *Game) DrawPlayerHUD(player *Player) {
	// Draw personal player stats at the top of the middle section
	hudText := fmt.Sprintf(" %c%d | %c%v | %c%d/%d | %c%d",
		SymbolHitPoints, player.CurrentHealth, SymbolCurrentAttack, player.GetEquippedAttack().String(), SymbolCurrentExperience, player.ExperienceVal, GetXpRequiredForNextLevel(player.Level), SymbolCurrentLevel, player.Level)
	gameStartCol := MaxMessageLength + 3
	for col, ch := range hudText {
		targetCol := gameStartCol + col
		if targetCol < MaxScreenWidth {
			game.Frame[0][targetCol] = ch
		}
	}
}
func (game *Game) DrawLogsPanel() {
	for row := 0; row < MaxScreenHeight; row++ {
		if row >= MaxEventsLength {
			break
		}
		for col := 0; col < MaxMessageLength; col++ {
			game.Frame[row][col] = game.GetEventRuneAt(row, col)
		}
	}
}
func UpdateMap[T any](m map[string]T, game *Game) {
	for _, item := range m {
		if updateable, ok := any(item).(Updateable); ok {
			updateable.Update(game)
		}
	}
}
func NewGame(globalChat chan string) *Game {
	game := InitializeGame()
	game.GlobalChat = globalChat
	go StartGame(game)       // Start the game loop
	go HandleEmptyGame(game) //Ticks ever 1 min to see if theres activity.
	return game
}
func InitEventChan(game *Game) {
	evntChan := make(chan GameEvent, InputBufferPerPlayer*MaxPlayerCount)
	game.EventChan = evntChan

	serverEvntChan := make(chan ServerEvent, MaxPlayerCount)
	game.ServerEventChan = serverEvntChan
}
func (game *Game) GetPlayerByID(playerID string) (*Player, bool) {
	for _, p := range game.Players {
		if p.GetID() == playerID {
			return p, true
		}
	}
	return nil, false
}
func (game *Game) ValidateSpace() error {
	if len(game.GetActivePlayers()) >= MaxPlayerCount {
		return fmt.Errorf("game is full")
	}
	return nil
}
func (g *Game) GetActivePlayers() []*Player {
	var active []*Player
	for _, p := range g.Players {
		if p.PlayerState != StateDisconnected {
			active = append(active, p)
		}
	}
	return active
}
func (g *Game) GetActivePlayerById(playerId string) (*Player, bool) {
	for _, p := range g.Players {
		if p.PlayerState != StateDisconnected && p.GetID() == playerId {
			return p, true
		}
	}
	return nil, false
}
func (game *Game) HandlePlayerConnect(playerID string) error {
	log.Println("[DEBUG] Player Connecting")
	if player, exists := game.GetPlayerByID(playerID); exists {
		log.Println("[DEBUG] Existing Player Connecting")
		player.PlayerState = StatePlaying
		if !game.Level.PutEntityAtPosition(player, player.GetPosition()) {
			log.Println("Failed to place player: Spawn point was blocked.")
		}
	} else {
		log.Println("[DEBUG] New Player Connecting")
		player := NewPlayer(playerID)
		game.SpawnPlayer(player)
	}
	return nil
}
func (game *Game) HandlePlayerDisconnect(playerID string) {
	log.Println("[DEBUG] Player Disconnecting")
	if player, exists := game.GetActivePlayerById(playerID); exists {
		player.PlayerState = StateDisconnected
		game.Level.RemoveEntity(player)
		game.ServerEventChan <- ServerEvent{PlayerID: playerID, Type: EventTypeDisconnect}
		log.Printf("[DEBUG] Player %s Disconnected", playerID)
	}
}
func (game *Game) Destroy() {
	select {
	case <-game.DestroyChan:
		return // Already destroyed, exit safely
	default:
		close(game.DestroyChan) // Safe to close once
	}

	players := game.Players
	if game.EventChan != nil {
		close(game.EventChan)
	}

	// Clean up player channels safely outside the lock
	for _, p := range players {
		p.PlayerState = StateDisconnected
		if p.DisplayChan != nil {
			select {
			case <-p.DisplayChan:
			default:
				close(p.DisplayChan)
			}
		}
	}

	// Signal the server that the game is dead
	game.ServerEventChan <- ServerEvent{Type: EventTypeIdleCheck}
	log.Println("[DEBUG] Game instance successfully destroyed and idle check sent.")
}
func HandleEmptyGame(game *Game) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-game.DestroyChan:
			log.Println("[DEBUG] HandleEmptyGame loop terminated.")
			return
		case <-ticker.C:
			game.Mu.Lock()
			if len(game.GetActivePlayers()) == 0 || game.State != StateGamePlaying {
				game.EmptyMinutes++
				log.Printf("[DEBUG] Game empty for %d minute(s)", game.EmptyMinutes)
				game.CreateLog("[SERVER] Game ending in %d minute(s)", StopGameAfterIdleMinutes-game.EmptyMinutes)
				if game.EmptyMinutes >= StopGameAfterIdleMinutes {
					log.Println("[DEBUG] Idle threshold reached via ticker. Shutting down game.")
					game.Mu.Unlock()
					game.Destroy()
					return
				}
			} else {
				game.EmptyMinutes = 0
			}
			game.Mu.Unlock()
		}
	}
}
