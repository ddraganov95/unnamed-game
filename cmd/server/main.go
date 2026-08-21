package main

import (
	"fmt"
	"log"
	"net/http"

	"unnamed-game/internal/websocket"
)

func main() {
	// Serve static files
	fileServer := http.FileServer(http.Dir("./web"))
	http.Handle("/", fileServer)

	fmt.Println("Hello To Unnamed RPG Game Server")

	// Initialize the server container
	srv := websocket.NewServer()

	// Register WebSocket route
	http.HandleFunc("/ws", srv.HandleWebSocket)

	port := ":8080"
	fmt.Printf("Server running at http://localhost%s\n", port)
	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatal("Server error: ", err)
	}
}
