package websocket

import (
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"unnamed-game/internal/game"

	"github.com/gorilla/websocket"
)

type Server struct {
	Game          *game.Game
	PlayerCounter uint64
	Upgrader      websocket.Upgrader
	mu            sync.Mutex
	activeConns   map[string]*websocket.Conn // Tracks the active WebSocket per player ID
}

func NewServer() *Server {
	return &Server{
		Upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
		activeConns: make(map[string]*websocket.Conn),
	}
}

func (server *Server) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	username := r.URL.Query().Get("name")
	if username == "" {
		username = fmt.Sprintf("Adventurer_%d", time.Now().Unix()%1000)
	}
	log.Printf("[DEBUG] HandleWebSocket: Started for user %s\n", username)

	g := server.GetOrCreateGame()

	//Handle connection validation / takeover rules
	log.Printf("[DEBUG] HandleWebSocket: Calling InitializeConnection for %s\n", username)
	if err := server.InitializeConnection(g, username); err != nil {
		status := http.StatusConflict
		if err.Error() == "game is full" {
			status := http.StatusServiceUnavailable
			log.Printf("[DEBUG] HandleWebSocket: Connection rejected for %s: %v (Status: %d)\n", username, err, status)
		}
		log.Printf("[DEBUG] HandleWebSocket: Connection rejected for %s: %v (Status: %d)\n", username, err, status)
		http.Error(w, err.Error(), status)
		return
	}
	log.Printf("[DEBUG] HandleWebSocket: InitializeConnection passed for %s\n", username)

	//Upgrade the HTTP connection to a WebSocket
	log.Printf("[DEBUG] HandleWebSocket: Upgrading connection for %s\n", username)
	conn, err := server.Upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[DEBUG] HandleWebSocket: Failed to upgrade WebSocket for %s: %v\n", username, err)
		return
	}
	log.Printf("[DEBUG] HandleWebSocket: Successfully upgraded WebSocket for %s\n", username)
	//Register or reattach the player entity to this connection
	log.Printf("[DEBUG] HandleWebSocket: Calling RegisterPlayer for %s\n", username)
	if err := server.RegisterPlayer(g, username, conn); err != nil {
		conn.Close()
		return
	}
	defer server.DisconnectPlayer(g, username, conn)
	log.Printf("[DEBUG] HandleWebSocket: RegisterPlayer completed for %s\n", username)

	//Start streaming frames and reading inputs
	log.Printf("[DEBUG] HandleWebSocket: Starting streamFrames and readPlayerInputs for %s\n", username)
	go server.streamFrames(g, username, conn)
	fmt.Printf("Player %s connected and spawned!\n", username)
	server.readPlayerInputs(g, username, conn)
	log.Printf("[DEBUG] HandleWebSocket: Exiting handler for %s\n", username)
}

func (server *Server) GetOrCreateGame() *game.Game {
	log.Println("[DEBUG] GetOrCreateGame: about to lock server.mu")
	server.mu.Lock()
	defer server.mu.Unlock()
	log.Println("[DEBUG] GetOrCreateGame: acquired server.mu")

	if server.Game == nil {
		globalChat := make(chan string, game.MaxChatHistory)
		server.Game = game.NewGame(globalChat)
		go server.listenToGameEvents(server.Game)
		fmt.Println("-> First player connected: Game instance created and loop started!")
	}
	return server.Game
}

func (server *Server) InitializeConnection(g *game.Game, username string) error {
	log.Println("[DEBUG] Init: about to lock server.mu")
	server.mu.Lock()
	log.Println("[DEBUG] Init: acquired server.mu")

	oldConn, exists := server.activeConns[username]
	if exists {
		delete(server.activeConns, username)
	}

	server.mu.Unlock()
	log.Println("[DEBUG] Init: released server.mu")

	if exists && oldConn != nil {
		fmt.Printf("Player %s reconnecting/taking over. Closing old connection.\n", username)
		log.Println("[DEBUG] Init: about to call oldConn.Close()")
		oldConn.Close()
		log.Println("[DEBUG] Init: old connection closed successfully")
	}

	log.Println("[DEBUG] Init: about to lock g.Mu for check/validation")
	g.Mu.RLock()
	log.Println("[DEBUG] Init: acquired g.Mu")

	playerExists := false
	for _, p := range g.Players {
		if p.GetID() == username {
			playerExists = true
			break
		}
	}

	if playerExists {
		g.Mu.RUnlock()
		log.Println("[DEBUG] Init: released g.Mu (player exists, reconnecting)")
		fmt.Printf("Player %s entity exists in game. Reconnecting.\n", username)
		return nil
	}

	// If they don't exist, validate space
	err := g.ValidateSpace()
	g.Mu.RUnlock()
	log.Println("[DEBUG] Init: released g.Mu (validated space)")

	return err
}

func (server *Server) RegisterPlayer(g *game.Game, playerID string, conn *websocket.Conn) error {
	respChan := make(chan error, 1)
	log.Printf("Response channel created")
	// Notify the game loop that a player has connected/reconnected
	g.EventChan <- game.GameEvent{
		Type:     game.EventTypeConnect,
		PlayerID: playerID,
		RespChan: respChan,
	}
	err := <-respChan
	log.Printf("Response error %v", err)
	if err != nil {
		log.Printf("Couldnt connect player: %s", playerID)
		return err
	}
	server.mu.Lock()
	server.activeConns[playerID] = conn
	server.mu.Unlock()
	return nil
}

func (server *Server) streamFrames(g *game.Game, playerID string, conn *websocket.Conn) {
	log.Printf("[DEBUG] streamFrames: starting for %s\n", playerID)
	g.Mu.RLock()
	var playerChan chan string
	for _, p := range g.Players {
		if p.GetID() == playerID {
			playerChan = p.DisplayChan
			break
		}
	}
	g.Mu.RUnlock()

	if playerChan == nil {
		log.Printf("[DEBUG] streamFrames: error - channel not found for %s\n", playerID)
		return
	}

	log.Printf("[DEBUG] streamFrames: entering frame loop for %s\n", playerID)
	for frame := range playerChan {
		if err := conn.WriteMessage(websocket.TextMessage, []byte(frame)); err != nil {
			log.Printf("[DEBUG] streamFrames: write error for %s: %v\n", playerID, err)
			break
		}
	}
	log.Printf("[DEBUG] streamFrames: exiting for %s\n", playerID)
}

func (server *Server) readPlayerInputs(g *game.Game, playerID string, conn *websocket.Conn) {
	log.Printf("[DEBUG] readPlayerInputs: starting for %s\n", playerID)

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			log.Printf("[DEBUG] readPlayerInputs: read error/closed for %s: %v\n", playerID, err)
			break
		}

		if len(msg) > 0 {
			log.Printf("[DEBUG] Sending Input %s %v", playerID, rune(msg[0]))
			g.EventChan <- game.GameEvent{
				PlayerID: playerID,
				Type:     game.EventTypeKey,
				Key:      rune(msg[0]),
			}
		}
	}
}
func (server *Server) listenToGameEvents(g *game.Game) {
	for event := range g.ServerEventChan {
		switch event.Type {
		case game.EventTypeDisconnect:
			server.mu.Lock()
			conn, exists := server.activeConns[event.PlayerID]
			server.mu.Unlock()
			if exists {
				server.DisconnectPlayer(g, event.PlayerID, conn)
			}
		case game.EventTypeIdleCheck:
			//TODO REMOVE FROM GAME LIST
		}
	}
}
func (server *Server) DisconnectPlayer(g *game.Game, playerID string, targetConn *websocket.Conn) {
	server.mu.Lock()
	// Only disconnect if this is still the active connection for the player
	// (prevents stale closures from killing a newly reconnected socket)
	if activeConn, exists := server.activeConns[playerID]; exists && activeConn == targetConn {
		delete(server.activeConns, playerID)
	}
	server.mu.Unlock()

	// Safely close the underlying socket (this immediately kills readPlayerInputs and streamFrames)
	targetConn.Close()

	// Notify the game loop once
	g.EventChan <- game.GameEvent{
		Type:     game.EventTypeDisconnect,
		PlayerID: playerID,
	}
	log.Printf("[DEBUG] Player %s fully disconnected and cleaned up.", playerID)
}
