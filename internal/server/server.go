package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
	"uuid"

	"unnamed-game/internal/db"
	"unnamed-game/internal/game"

	"github.com/gorilla/websocket"
)

const (
	DBWriteQueueSize = 256
	DBWriteTimeout   = 5 * time.Second
	// ShutdownGameTimeout bounds how long shutdown waits for active games to finalize.
	ShutdownGameTimeout = 10 * time.Second
	// CloseCodeJoinRejected is the websocket close code sent when the game
	// refuses the player after the upgrade (e.g. full). Mirrored in web/game.js.
	CloseCodeJoinRejected = 4001
)

type dbWriteTask struct {
	name string
	run  func(context.Context) error
}

type Server struct {
	mu             sync.RWMutex
	lobbyMu        sync.Mutex
	Upgrader       websocket.Upgrader
	serverConfig   ServerConfig
	chatHistory    []string
	activeConns    map[string]*GameWSConnection
	activeGames    map[uuid.UUID]*game.Game
	playerSessions map[string]uuid.UUID
	playerUsers    map[string]uuid.UUID
	lobbyConns     map[*ChatWSConnection]struct{}
	achievementCat *game.AchievementCatalog
	db             *db.Database
	Mux            *http.ServeMux
	httpServer     *http.Server
	dbWriteChan    chan dbWriteTask
	nextConnID     atomic.Uint64
}
type ServerConfig struct {
	Host string `json:"host"`
	Port string `json:"port"`
}
type GameWSConnection struct {
	Ctx      context.Context
	Cancel   context.CancelFunc
	Conn     *websocket.Conn
	UserID   string
	PlayerID string
	// ConnID is unique per websocket; passed to the game on connect and
	// disconnect so a replaced socket can't disconnect the newer one.
	ConnID uint64
	// writeMu serializes writes: gorilla/websocket allows only one
	// concurrent writer and panics otherwise. Always write through
	// WriteText/WriteJSON, never Conn.Write* directly.
	writeMu sync.Mutex
}

func (c *GameWSConnection) WriteText(s string) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.Conn.WriteMessage(websocket.TextMessage, []byte(s))
}

func (c *GameWSConnection) WriteJSON(v any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.Conn.WriteJSON(v)
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

type CreateUserRequest struct {
	PlayerID string `json:"player_id"`
}

type GameResponse struct {
	GameID      string `json:"game_id"`
	RedirectURL string `json:"redirect_url"`
}

func NewConfig() ServerConfig {
	return ServerConfig{
		Host: db.GetEnv("HOST", "localhost"),
		Port: db.GetEnv("PORT", "8080"),
	}
}
func NewServer(ctx context.Context) (*Server, error) {
	database, err := db.NewDatabase()
	if err != nil {
		return nil, err
	}
	return NewServerWithDB(ctx, database, NewConfig())
}

// NewServerWithDB builds a server on an already-connected database. NewServer
// uses it for production; tests use it to point the server at a test database.
func NewServerWithDB(ctx context.Context, database *db.Database, config ServerConfig) (*Server, error) {
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
		lobbyConns:     make(map[*ChatWSConnection]struct{}),
		playerUsers:    make(map[string]uuid.UUID),
		dbWriteChan:    make(chan dbWriteTask, DBWriteQueueSize),
		Mux:            http.NewServeMux(),
		db:             database,
		achievementCat: catalog,
		serverConfig:   config,
	}
	go srv.dbWriteWorker(ctx)
	game.InitGameRegistries()
	events := []string{"achievement_unlocked", "global_chat"}
	go database.ListenToDBEvents(ctx, events, srv.handleDBEvent)
	return srv, nil
}
// RegisterRoutes mounts every API, websocket and static-file route on Mux.
func (srv *Server) RegisterRoutes(webDir string) {
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
	srv.Mux.Handle("/", http.FileServer(http.Dir(webDir)))
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
func (s *Server) Run(ctx context.Context) error {
	port := fmt.Sprintf(":%s", s.serverConfig.Port)
	s.httpServer = &http.Server{
		Addr:    port,
		Handler: s.Mux,
	}

	shutdownDone := make(chan struct{})

	// Watch for Ctrl+C signal cancelation in the background
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		slog.Info("shutdown signal received, closing HTTP server")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
			slog.Error("http shutdown forced", "error", err)
		}
		s.endActiveGamesOnShutdown()
	}()

	slog.Info("server listening", "addr", port)

	err := s.httpServer.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	// ListenAndServe returns as soon as shutdown starts; wait for the games'
	// DB writes to finish before the process exits.
	<-shutdownDone
	slog.Info("server stopped")
	return nil
}
func (server *Server) buildServerHost() string {
	return fmt.Sprintf("%s:%s", server.serverConfig.Host, server.serverConfig.Port)
}
func (server *Server) BuildGamePageURL(r *http.Request, host string, gameID uuid.UUID) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s/game.html?gameId=%s", scheme, host, gameID.String())
}

func (server *Server) dbWriteWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case task := <-server.dbWriteChan:
			writeCtx, cancel := context.WithTimeout(context.Background(), DBWriteTimeout)
			err := task.run(writeCtx)
			cancel()

			if err != nil {
				slog.Error("db write failed", "task", task.name, "error", err)
			}
		}
	}
}

func (server *Server) enqueueDBWrite(name string, run func(context.Context) error) {
	select {
	case server.dbWriteChan <- dbWriteTask{name: name, run: run}:
	default:
		slog.Error("db write queue full, write dropped", "task", name)
	}
}

// endActiveGamesOnShutdown ends every active game with the normal destroy
// path, so each one's DB row is finalized and its players' sessions saved.
// It waits (bounded by ShutdownGameTimeout) for the games to deactivate.
func (s *Server) endActiveGamesOnShutdown() {
	s.mu.RLock()
	games := make([]*game.Game, 0, len(s.activeGames))
	for _, g := range s.activeGames {
		games = append(games, g)
	}
	s.mu.RUnlock()

	for _, g := range games {
		g.Destroy("server_shutdown")
	}

	deadline := time.Now().Add(ShutdownGameTimeout)
	for {
		s.mu.RLock()
		remaining := len(s.activeGames)
		s.mu.RUnlock()
		if remaining == 0 {
			return
		}
		if time.Now().After(deadline) {
			slog.Warn("games still active after shutdown timeout", "count", remaining)
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}
