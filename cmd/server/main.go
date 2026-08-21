package main

import (
	"fmt"
	"log"
	"net/http"
	"unnamed-game/internal/game"

	"github.com/gorilla/websocket"
)

func main() {
	// Serve static files
	fileServer := http.FileServer(http.Dir("./web"))
	http.Handle("/", fileServer)

	fmt.Println("Hello To Unnamed RPG Game")

	// Initialize the chat channel and catch the game instance pointer
	globalChat := make(chan string, game.MaxChatHistory)
	defer close(globalChat)
	gameInstance := game.NewGame(globalChat)
	// Create your Server instance with the game and upgrader config
	srv := &Server{
		Game: gameInstance,
		Upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow local connections for development
			},
		},
	}

	//Hook up the method using your server instance (srv)
	http.HandleFunc("/ws", srv.HandleWebSocket)

	port := ":8080"
	fmt.Printf("Server running at http://localhost%s\n", port)
	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatal("Server error: ", err)
	}
}
