package main

import (
	"fmt"
	"log"
	"net/http"
	"sync/atomic"
	"unnamed-game/internal/game"

	"github.com/gorilla/websocket"
)

type Server struct {
	Game          *game.Game
	PlayerCounter uint64
	Upgrader      websocket.Upgrader
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow local connections for development
	},
}

func (server *Server) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := server.Upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Failed to upgrade:", err)
		return
	}
	defer conn.Close()

	//Generate a unique ID safely
	id := atomic.AddUint64(&server.PlayerCounter, 1)
	playerID := fmt.Sprintf("player_%d", id)

	//Create the player object
	player := game.NewPlayer(playerID)
	player.OnQuit = func() {
		conn.Close()
	}

	//Spawn/Register the player into the game engine
	server.Game.SpawnPlayer(player)

	//Ensure the player is automatically removed from the world when this tab closes
	defer server.Game.RemovePlayer(playerID)

	go func() {
		for frame := range player.DisplayChan {
			err := conn.WriteMessage(websocket.TextMessage, []byte(frame))
			if err != nil {
				break // Connection dropped
			}
		}
	}()

	fmt.Printf("Player %s connected and spawned!\n", playerID)

	//Read Player Input
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			fmt.Printf("Player %s disconnected\n", playerID)
			break
		}
		fmt.Printf("Player %s did %s\n", playerID, msg)
		if len(msg) > 0 {
			server.Game.InputChan <- game.PlayerInput{
				PlayerID: playerID,
				Key:      rune(msg[0]),
			}
		}
	}
}
