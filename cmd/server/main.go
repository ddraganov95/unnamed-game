package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"unnamed-game/internal/server"

	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()

	//Root signal context for the whole application lifetime
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	//Initialize container
	srv, err := server.NewServer(ctx)
	if err != nil {
		log.Fatalf("Failed to initialize server: %v", err)
	}

	//Register routes
	srv.Mux.HandleFunc("POST /api/users", srv.HandleCreateUser)
	srv.Mux.HandleFunc("GET /api/users/me", srv.HandleGetSelf)
	srv.Mux.HandleFunc("GET /api/users/me/achievements", srv.HandleGetSelfAchievements)
	srv.Mux.HandleFunc("GET /api/users/me/leaderboard", srv.HandleGetSelfLeaderboard)
	srv.Mux.HandleFunc("GET /api/users/me/config", srv.HandleGetSelfConfig)
	srv.Mux.HandleFunc("PUT /api/users/me/config", srv.HandleUpdateSelfConfig)
	srv.Mux.HandleFunc("POST /api/games", srv.HandleCreateGame)
	srv.Mux.HandleFunc("POST /api/games/{id}/join", srv.HandleJoinGame)
	srv.Mux.HandleFunc("GET /api/leaderboard", srv.HandleLeaderboard)
	srv.Mux.HandleFunc("/ws", srv.HandleWebSocket)
	srv.Mux.HandleFunc("/ws/global-chat", srv.HandleLobbyChatWS)

	fileServer := http.FileServer(http.Dir("./web"))
	srv.Mux.Handle("/", fileServer)

	fmt.Println("Hello To Unnamed RPG Game Server")

	//Run hands over control and blocks until Ctrl+C triggers graceful exit
	if err := srv.Run(ctx, ":8080"); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
