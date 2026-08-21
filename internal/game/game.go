package game

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

type Game struct {
	Level       Level
	Events      []Event
	Frame       [][]rune
	GlobalChat  chan string
	InputChan   chan PlayerInput
	ChatHistory []string
	Players     []*Player
	mu          sync.Mutex
	Running     bool
}

func InitializeGame() *Game {
	//creates an empty game state and initializes the game. This function should be called before StartGame.
	fmt.Print("Preparing Game...\r\n")
	game := &Game{
		Running: true,
	}
	game.InitFrame()
	InitAttacks()
	InitSpawnRules()
	InitInputChan(game)
	return game
}
func StartGame(game *Game) {

	NewLevel(game)

	ticker := time.NewTicker(30 * time.Millisecond)
	defer ticker.Stop()

	for game.Running {
		<-ticker.C

		//Update positions, logic, inputs
		UpdateGame(game, game.InputChan)

		//Send rendered frames to every connected player
		game.mu.Lock()
		for _, player := range game.Players {
			// Make sure DrawLevelForPlayer returns a string representation of the screen
			renderedFrame := game.DrawLevelForPlayer(game.Level, player)

			// Non-blocking send so a lagging browser tab doesn't freeze the entire game loop
			select {
			case player.DisplayChan <- renderedFrame:
			default:
			}
		}
		game.mu.Unlock()
	}
}
func UpdateGame(game *Game, inputChan chan PlayerInput) {
	//Drain input channel and update player key queues
	//Only players can have inputs and their inputs are stored in the key queue. The game loop will process the key queue and call the appropriate functions.
	//PlayerIds are unique identifiers for the players. No other entity can have the same id as a player, even other players.
inputLoop:
	for {
		select {
		case input := <-inputChan:
			if reciever, ok := game.Level.Entities[input.PlayerID].(InputReceiver); ok {
				reciever.EnqueueKey(input.Key)
			}
		default:
			break inputLoop
		}
	}
	UpdateMap(game.Level.Entities, game)
	UpdateMap(game.Level.Effects, game)
	game.PollGlobalChat()   //Get Chat Log from global channel.
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
func (game *Game) SpawnPlayer(player *Player) {
	game.mu.Lock()
	defer game.mu.Unlock()
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
		log.Printf("Successfully registered player %s", player.GetID())
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
	sb.WriteString("\033[H") // Reset cursor to top-left for xterm.js
	for _, row := range game.Frame {
		sb.WriteString(string(row))
		sb.WriteString("\r\n")
	}
	return sb.String()
}
func (game *Game) DrawPlayerHUD(player *Player) {
	// Draw personal player stats at the top of the middle section
	hudText := fmt.Sprintf(" %c%d | %c%v | %c%d//%d | %c%d",
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
	InitInputChan(game)
	game.GlobalChat = globalChat
	go StartGame(game) // Start the game loop
	return game
}
func InitInputChan(game *Game) {
	playerInput := make(chan PlayerInput, InputBufferPerPlayer*MaxPlayerCount)
	game.InputChan = playerInput

}
func (game *Game) RemovePlayer(playerID string) {
	game.mu.Lock()
	defer game.mu.Unlock()

	// Remove entity from the level safely
	if player, exists := game.Level.Entities[playerID]; exists {
		game.Level.RemoveEntity(player)
		delete(game.Level.Entities, playerID) // If it's a map
	}

	// Find and remove the player from the Players slice
	for i, p := range game.Players {
		if p.GetID() == playerID {
			// Go slice removal pattern: combine elements before `i` with elements after `i`
			game.Players = append(game.Players[:i], game.Players[i+1:]...)
			break
		}
	}

	fmt.Printf("Cleaned up player %s from game state.\n", playerID)
}
