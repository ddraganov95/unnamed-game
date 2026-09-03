package main

import (
	"fmt"
	"log"
	"net/http"
	"unnamed-game/internal/server"

	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()
	//Initialize the server container
	srv, err := server.NewServer()
	if err != nil {
		log.Fatalf("Failed to initialize server: %v", err)
	}

	//Register REST API Endpoints
	srv.Mux.HandleFunc("POST /api/users", srv.HandleCreateUser)

	srv.Mux.HandleFunc("GET /api/users/{id}", srv.HandleGetUser)

	srv.Mux.HandleFunc("GET /api/users/me", srv.HandleGetSelf)

	srv.Mux.HandleFunc("POST /api/games", srv.HandleCreateGame)
	srv.Mux.HandleFunc("POST /api/games/{id}/join", srv.HandleJoinGame)

	//Register Game WebSocket route
	srv.Mux.HandleFunc("/ws", srv.HandleWebSocket)
	//Register Global Chat WebSocket route
	srv.Mux.HandleFunc("/ws/global-chat", srv.HandleLobbyChatWS)

	//Serve static files
	fileServer := http.FileServer(http.Dir("./web"))
	srv.Mux.Handle("/", fileServer)

	fmt.Println("Hello To Unnamed RPG Game Server")

	port := ":8080"
	fmt.Printf("Server running at http://localhost%s\n", port)
	if err := http.ListenAndServe(port, srv.Mux); err != nil {
		log.Fatal("Server error: ", err)
	}
}
