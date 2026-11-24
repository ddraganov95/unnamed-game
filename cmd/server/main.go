package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"unnamed-game/internal/logging"
	"unnamed-game/internal/server"

	"github.com/joho/godotenv"
)

func main() {
	// Load .env first so LOG_LEVEL from the file is seen by logging.Setup.
	_ = godotenv.Load()
	logging.Setup()

	//Root signal context for the whole application lifetime
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	//Initialize container
	srv, err := server.NewServer(ctx)
	if err != nil {
		slog.Error("failed to initialize server", "error", err)
		os.Exit(1)
	}

	//Register routes
	srv.RegisterRoutes("./web")

	slog.Info("unnamed rpg game server starting")

	//Run hands over control and blocks until Ctrl+C triggers graceful exit
	if err := srv.Run(ctx); err != nil {
		slog.Error("server stopped with error", "error", err)
		os.Exit(1)
	}
}
