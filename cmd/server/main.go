package main

import (
	"fmt"
	"log"
	"net/http"

	"unnamed-game/internal/websocket"
)

func main() {
	//Create Router
	mux := http.NewServeMux()

	//Initialize the server container
	srv := websocket.NewServer()

	//Register REST API Endpoints
	mux.HandleFunc("POST /api/games", srv.HandleCreateGame)
	mux.HandleFunc("POST /api/games/{id}/join", srv.HandleJoinGame)

	// Register WebSocket route
	mux.HandleFunc("/ws", srv.HandleWebSocket)

	// Serve static files
	fileServer := http.FileServer(http.Dir("./web"))
	mux.Handle("/", fileServer)

	fmt.Println("Hello To Unnamed RPG Game Server")

	port := ":8080"
	fmt.Printf("Server running at http://localhost%s\n", port)
	if err := http.ListenAndServe(port, mux); err != nil {
		log.Fatal("Server error: ", err)
	}
}
