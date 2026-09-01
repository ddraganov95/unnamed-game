package main

import (
	"fmt"
	"log"
	"net/http"

	"unnamed-game/internal/server"
)

func main() {
	//Create Router
	mux := http.NewServeMux()

	//Initialize the server container
	srv := server.NewServer()

	//Register REST API Endpoints
	mux.HandleFunc("POST /api/games", srv.HandleCreateGame)
	mux.HandleFunc("POST /api/games/{id}/join", srv.HandleJoinGame)

	// Register Game WebSocket route
	mux.HandleFunc("/ws", srv.HandleWebSocket)
	// Register Global Chat WebSocket route
	mux.HandleFunc("/ws/global-chat", srv.HandleLobbyChatWS)

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
