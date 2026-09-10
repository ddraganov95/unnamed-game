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
	PlayerCounter  uint64
	Upgrader       websocket.Upgrader
	mu             sync.RWMutex
	activeConns    map[string]*WSConnection // Tracks the active GameWebSocket per player ID
	activeGames    map[uuid.UUID]*game.Game // Map gameid -> game
	playerSessions map[string]uuid.UUID     // Map String playerid -> gameid
	playerUsers    map[string]uuid.UUID     // Map String playerid -> userid
	lobbyConns     map[*websocket.Conn]bool // Tracks active LobbyWebSockets
	chatHistory    []string
	achievementCat *game.AchievementCatalog
	globalChat     chan string
	lobbyMu        sync.Mutex
	db             *db.Database
	Mux            *http.ServeMux
	httpServer     *http.Server
}
type WSConnection struct {
	Conn     *websocket.Conn
	Ctx      context.Context
	Cancel   context.CancelFunc
	UserID   string
	PlayerID string
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
		activeConns:    make(map[string]*WSConnection),
		activeGames:    make(map[uuid.UUID]*game.Game),
		playerSessions: make(map[string]uuid.UUID),
		lobbyConns:     make(map[*websocket.Conn]bool),
		playerUsers:    make(map[string]uuid.UUID),
		globalChat:     make(chan string, game.MaxChatHistory),
		Mux:            http.NewServeMux(),
		db:             database,
		achievementCat: catalog,
	}
	go srv.ListenToDBEvents(ctx)
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
