package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
	"uuid"

	"unnamed-game/internal/db"
	"unnamed-game/internal/game"

	"github.com/gorilla/websocket"
)

type Server struct {
	mu             sync.RWMutex
	lobbyMu        sync.Mutex
	Upgrader       websocket.Upgrader
	chatHistory    []string
	activeConns    map[string]*GameWSConnection
	activeGames    map[uuid.UUID]*game.Game
	playerSessions map[string]uuid.UUID
	playerUsers    map[string]uuid.UUID
	lobbyConns     map[string]*ChatWSConnection
	achievementCat *game.AchievementCatalog
	db             *db.Database
	Mux            *http.ServeMux
	httpServer     *http.Server
	globalChat     chan string
	PlayerCounter  uint64
}
type GameWSConnection struct {
	Ctx      context.Context
	Cancel   context.CancelFunc
	Conn     *websocket.Conn
	UserID   string
	PlayerID string
}
type ChatWSConnection struct {
	Ctx      context.Context
	Cancel   context.CancelFunc
	Conn     *websocket.Conn
	UserID   string
	PlayerID string
	Write    chan string
}
type OutboundWSMessage struct {
	Type    string `json:"type"`
	Payload string `json:"payload"`
}

type GameRequest struct {
	PlayerID string `json:"player_id"`
}

type GameResponse struct {
	GameID string `json:"game_id"`
}

func NewServer(ctx context.Context) (*Server, error) {
	database, err := db.NewDatabase()
	if err != nil {
		return nil, err
	}

	// Set a strict 5-second deadline for server startup I/O
	bootCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	//Fetch raw definitions (DTOs) from database
	defs, err := database.FetchAchievementCatalog(bootCtx)
	if err != nil {
		return nil, fmt.Errorf("failed to load achievement catalog defs: %w", err)
	}

	//Build in-memory rulebook + inverted index
	catalog, err := game.BuildAchievementCatalog(defs)
	if err != nil {
		return nil, fmt.Errorf("failed to build global achievement catalog: %w", err)
	}

	srv := &Server{
		Upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
		activeConns:    make(map[string]*GameWSConnection),
		activeGames:    make(map[uuid.UUID]*game.Game),
		playerSessions: make(map[string]uuid.UUID),
		lobbyConns:     make(map[string]*ChatWSConnection),
		playerUsers:    make(map[string]uuid.UUID),
		globalChat:     make(chan string, game.MaxChatHistory),
		Mux:            http.NewServeMux(),
		db:             database,
		achievementCat: catalog,
	}
	game.InitGameRegistries()
	events := []string{"achievement_unlocked", "global_chat"}
	go database.ListenToDBEvents(ctx, events, srv.handleDBEvent)
	return srv, nil
}

func (server *Server) FindGameById(gameId uuid.UUID) (*game.Game, bool) {
	g, exists := server.activeGames[gameId]
	return g, exists
}

func (server *Server) FindGameByPlayerId(playerId string) (*game.Game, bool) {
	if gameId, exists := server.playerSessions[playerId]; exists {
		return server.FindGameById(gameId)
	}
	return nil, false
}

func (server *Server) AddGame(g *game.Game) {
	server.activeGames[g.GameId] = g
}

func (server *Server) AddPlayerIdToGame(playerId string, g *game.Game) {
	server.playerSessions[playerId] = g.GameId
}
func (s *Server) Run(ctx context.Context, addr string) error {
	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: s.Mux,
	}

	// Watch for Ctrl+C signal cancelation in the background
	go func() {
		<-ctx.Done()
		log.Println("[SERVER] Shutdown signal received, closing HTTP server...")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("[SERVER] HTTP shutdown forced: %v", err)
		}
	}()

	log.Printf("[SERVER] Running at http://localhost%s\n", addr)

	err := s.httpServer.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	log.Println("[SERVER] Server stopped")
	return nil
}
