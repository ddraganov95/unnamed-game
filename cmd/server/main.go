package main

import (
	"context"
	"fmt"
	"log"
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
	srv.RegisterRoutes("./web")

	fmt.Println("Hello To Unnamed RPG Game Server")

	//Run hands over control and blocks until Ctrl+C triggers graceful exit
	if err := srv.Run(ctx); err != nil {
		log.Fatalf("Server error: %v", err)
	}

}
