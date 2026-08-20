package game

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"golang.org/x/term"
)

type Game struct {
	Level       Level
	Events      []Event
	Frame       [][]rune
	GlobalChat  chan string
	InputChan   chan PlayerInput
	ChatHistory []string
	Players     []*Player
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
func StartGame(game *Game, inputChan chan PlayerInput) {
	//Game should be started after initializing with PrepareGame function. This function will start the game loop and handle player input.
	fmt.Println("Game Starting...")
	// Set terminal to raw mode to capture input without echoing (should be done in PrepareGame function)
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		panic(err)
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState) // Restore terminal state on exit

	defer close(inputChan)

	fmt.Print("Spawning Player...\r\n")
	player := NewPlayer("Drago") // Create a new player with ID "Drago"
	game.Players = append(game.Players, player)
	go HandlePlayerInputChannel(game, player, inputChan) // Start the input handler in a separate goroutine
	NewLevel(game)
	fmt.Print("Game Started!\r\n")
	//Main game loop should tick at a certain interval, for now we will just use a ticker to tick every 30 milliseconds.
	ticker := time.NewTicker(30 * time.Millisecond)
	defer ticker.Stop()
	fmt.Print("Expecting Input...\r\n")
	//main game loop

	for game.Running {
		<-ticker.C
		UpdateGame(game, inputChan)                 // Update game state based on player input
		game.DrawLevelForPlayer(game.Level, player) // Render level
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

func (game *Game) DrawLevelForPlayer(level Level, player *Player) {
	//Reset the canvas
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
	//Flush the final composed frame to the terminal
	game.FlushFrame()
}
func (game *Game) SpawnPlayer(player *Player) {
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
func (game *Game) FlushFrame() {
	var sb strings.Builder
	//sb.WriteString("\033[2J\033[H")
	sb.WriteString("\033[H")
	for _, row := range game.Frame {
		sb.WriteString(string(row))
		sb.WriteString("\r\n")
	}
	fmt.Print(sb.String())
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
	input := InitInputChan(game)
	game.GlobalChat = globalChat
	StartGame(game, input) // Start the game loop
	return game
}
func InitInputChan(game *Game) chan PlayerInput {
	return make(chan PlayerInput, InputBufferPerPlayer*MaxPlayerCount)
}
