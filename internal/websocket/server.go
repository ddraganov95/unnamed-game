package websocket

import (
	"fmt"
	"log"
	"net/http"
	"sync"
	"sync/atomic"

	"unnamed-game/internal/game"

	"github.com/gorilla/websocket"
)

type Server struct {
	Game          *game.Game
	PlayerCounter uint64
	Upgrader      websocket.Upgrader
	mu            sync.Mutex // Protects game creation
}

func NewServer() *Server {
	return &Server{
		Upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow local connections for development
			},
		},
	}
}

func (server *Server) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Lazy initialization: Create and start the game only when players arrive
	server.mu.Lock()
	if server.Game == nil {
		globalChat := make(chan string, game.MaxChatHistory)
		server.Game = game.NewGame(globalChat)

		// Start the game loop in the background since we now have a player joining
		go game.StartGame(server.Game)
		fmt.Println("-> First player connected: Game instance created and loop started!")
	}
	server.mu.Unlock()

	conn, err := server.Upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Failed to upgrade:", err)
		return
	}
	defer conn.Close()

	// Generate a unique ID safely
	id := atomic.AddUint64(&server.PlayerCounter, 1)
	playerID := fmt.Sprintf("player_%d", id)

	// Create the player object
	player := game.NewPlayer(playerID)
	player.OnQuit = func() {
		player.GenerateSummary().Print()
		conn.Close()
	}

	server.Game.Mu.Lock()
	server.Game.SpawnPlayer(player)
	server.Game.Mu.Unlock()

	defer server.Game.RemovePlayer(playerID)

	// Frame Writer Goroutine
	go func() {
		for frame := range player.DisplayChan {
			err := conn.WriteMessage(websocket.TextMessage, []byte(frame))
			if err != nil {
				fmt.Printf("Player %s disconnected because: %v\n", playerID, err)
				break // Connection dropped
			}
		}
	}()

	fmt.Printf("Player %s connected and spawned!\n", playerID)

	// Read Player Input Loop
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			fmt.Printf("Player %s disconnected\n", playerID)
			break
		}

		fmt.Printf("Player %s sent message: %s\n", playerID, msg)

		if len(msg) > 0 {
			server.Game.InputChan <- game.PlayerInput{
				PlayerID: playerID,
				Key:      rune(msg[0]),
			}
		}
	}
}
