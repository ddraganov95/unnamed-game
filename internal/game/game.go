package game

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
	"uuid"
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
	LevelNumber     int
	GameId          uuid.UUID
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
				UpdateGame(game)
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
func UpdateGame(game *Game) {
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
	//Draw the player-specific UI on top
	game.DrawPlayerHUD(player)
	//Draw the lower part of the screen
	game.DrawFooter()
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

	game.DrawPlayerHUD(player)
	game.DrawFooter()

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
	if player == nil {
		return
	}
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
func (game *Game) DrawFooter() {
	row1Idx := MaxScreenHeight - 2
	row2Idx := MaxScreenHeight - 1

	if row1Idx >= 0 && row2Idx < len(game.Frame) {
		// Clear both footer rows first
		for col := range game.Frame[row1Idx] {
			game.Frame[row1Idx][col] = ' '
			game.Frame[row2Idx][col] = ' '
		}

		text1 := "Press [C] to copy Game ID"
		text2 := fmt.Sprintf("Game ID: %s", game.GameId.String())

		// Center and draw Row 1 (Instruction)
		startCol1 := (MaxScreenWidth - len(text1)) / 2
		for i, ch := range text1 {
			target := startCol1 + i
			if target >= 0 && target < MaxScreenWidth {
				game.Frame[row1Idx][target] = ch
			}
		}

		// Center and draw Row 2 (The ID itself)
		startCol2 := (MaxScreenWidth - len(text2)) / 2
		for i, ch := range text2 {
			target := startCol2 + i
			if target >= 0 && target < MaxScreenWidth {
				game.Frame[row2Idx][target] = ch
			}
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
func NewGame(gameId uuid.UUID, globalChat chan string) *Game {
	game := InitializeGame()
	game.GlobalChat = globalChat
	game.GameId = gameId
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
		//Same level reconnecting try same position
		if game.LevelNumber-1 == player.LevelsCompleted && !game.Level.PutEntityAtPosition(player, player.GetPosition()) {
			return errors.New("Failed to place player: Spawn point was blocked.")
		} else {
			//Reconnecting after players continued
			game.SpawnPlayer(player)
		}
		player.PlayerState = StatePlaying
	} else {
		log.Println("[DEBUG] New Player Connecting")
		player := NewPlayer(playerID)
		player.GameID = game.GameId.String()
		log.Printf("[DEBUG] -------------------New Player Game id: %s", player.GameID)
		game.SpawnPlayer(player)
	}
	game.EmptyMinutes = 0
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
		close(game.DestroyChan) //Safe to close
	}
	dcChan := make(chan error)
	game.ServerEventChan <- ServerEvent{
		Type:     EventTypeMassDisconnect,
		RespChan: dcChan,
	}
	<-dcChan

	// Signal the server that the game is dead
	game.ServerEventChan <- ServerEvent{Type: EventTypeIdleCheck}
	log.Println("[DEBUG] Game instance successfully destroyed and idle check sent.")
}
func HandleEmptyGame(game *Game) {
	ticker := time.NewTicker(1 * time.Minute)
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
