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
	"unnamed-game/internal/safe"

	"github.com/gorilla/websocket"
)

const (
	DBWriteQueueSize = 256
	DBWriteTimeout   = 5 * time.Second
	// ShutdownTimeout is the total budget for graceful shutdown, shared by
	// every stage. It must fit inside the process stop timeout (30s on ECS).
	ShutdownTimeout = 25 * time.Second
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
	dbWriteDone    chan struct{} // closed once dbWriteWorker has drained and exited
	// writeMu guards writesClosed. enqueueDBWrite and runAsync check the flag
	// under RLock and register work; shutdown sets it under Lock, so no new
	// write can start once shutdown is draining.
	writeMu      sync.RWMutex
	writesClosed bool
	asyncWG      sync.WaitGroup // in-flight runAsync saves
	nextConnID   atomic.Uint64
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
		dbWriteDone:    make(chan struct{}),
		Mux:            http.NewServeMux(),
		db:             database,
		achievementCat: catalog,
		serverConfig:   config,
	}
	go srv.dbWriteWorker()
	game.InitGameRegistries()
	events := []string{"achievement_unlocked", "global_chat"}
	go database.ListenToDBEvents(ctx, events, func(channel, payload string) {
		safe.Call("db event", func() {
			srv.handleDBEvent(channel, payload)
		}, nil)
	})
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
		Handler: s.Handler(),
	}

	shutdownDone := make(chan struct{})

	// Watch for Ctrl+C signal cancelation in the background
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		slog.Info("shutdown signal received, shutting down")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
		defer cancel()
		s.shutdown(shutdownCtx)
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

// shutdown runs the stages in dependency order under one deadline: stop
// accepting HTTP, close lobby sockets, end games (their rows and sessions are
// saved), wait for in-flight saves, then drain the DB write queue.
func (s *Server) shutdown(ctx context.Context) {
	if err := s.httpServer.Shutdown(ctx); err != nil {
		slog.Error("http shutdown forced", "error", err)
	}
	s.closeLobbyConnections()
	s.endActiveGamesOnShutdown(ctx)
	s.stopDBWrites(ctx)
}

// dbWriteWorker drains dbWriteChan until stopDBWrites closes it. It does not
// watch the signal context, so writes queued during shutdown still run.
func (server *Server) dbWriteWorker() {
	defer close(server.dbWriteDone)
	for task := range server.dbWriteChan {
		writeCtx, cancel := context.WithTimeout(context.Background(), DBWriteTimeout)
		var err error
		safe.Call("db write "+task.name, func() {
			err = task.run(writeCtx)
		}, nil)
		cancel()

		if err != nil {
			slog.Error("db write failed", "task", task.name, "error", err)
		}
	}
}

func (server *Server) enqueueDBWrite(name string, run func(context.Context) error) {
	server.writeMu.RLock()
	defer server.writeMu.RUnlock()
	if server.writesClosed {
		slog.Error("shutdown in progress, db write dropped", "task", name)
		return
	}
	select {
	case server.dbWriteChan <- dbWriteTask{name: name, run: run}:
	default:
		slog.Error("db write queue full, write dropped", "task", name)
	}
}

// stopDBWrites refuses new writes, waits for in-flight game saves, then closes
// the queue and waits for the worker to finish what is already queued.
func (s *Server) stopDBWrites(ctx context.Context) {
	s.writeMu.Lock()
	s.writesClosed = true
	s.writeMu.Unlock()

	// No new runAsync can register now, so waiting on the WaitGroup is safe.
	asyncDone := make(chan struct{})
	go func() {
		s.asyncWG.Wait()
		close(asyncDone)
	}()
	select {
	case <-asyncDone:
	case <-ctx.Done():
		slog.Warn("game saves still running at shutdown deadline")
	}

	close(s.dbWriteChan)
	select {
	case <-s.dbWriteDone:
	case <-ctx.Done():
		slog.Warn("db write queue not drained at shutdown deadline", "queued", len(s.dbWriteChan))
	}
}

// closeLobbyConnections sends a "going away" close frame to every lobby chat
// socket and closes it. Lobby sockets are not tied to a game, so nothing else
// closes them during shutdown.
func (s *Server) closeLobbyConnections() {
	s.lobbyMu.Lock()
	conns := make([]*ChatWSConnection, 0, len(s.lobbyConns))
	for c := range s.lobbyConns {
		conns = append(conns, c)
	}
	s.lobbyMu.Unlock()

	for _, c := range conns {
		// WriteControl may run concurrently with the lobby writer goroutine.
		_ = c.Conn.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseGoingAway, "server shutting down"),
			time.Now().Add(time.Second))
		c.Conn.Close()
	}
}

// endActiveGamesOnShutdown ends every active game with the normal destroy
// path, so each one's DB row is finalized and its players' sessions saved.
// It waits until ctx is done or the games have deactivated.
func (s *Server) endActiveGamesOnShutdown(ctx context.Context) {
	s.mu.RLock()
	games := make([]*game.Game, 0, len(s.activeGames))
	for _, g := range s.activeGames {
		games = append(games, g)
	}
	s.mu.RUnlock()

	// Destroy games in parallel: each game only locks its own state, and the
	// DB pool limits how many saves actually run at once. Not waited on
	// directly; the deadline loop below bounds the wait, so one stuck game
	// can't hang shutdown.
	for _, g := range games {
		safe.Go("shutdown destroy", func() {
			g.Destroy(game.DestroyReasonServerShutdown)
		}, nil)
	}

	for {
		s.mu.RLock()
		remaining := len(s.activeGames)
		s.mu.RUnlock()
		if remaining == 0 {
			return
		}
		select {
		case <-ctx.Done():
			slog.Warn("games still active at shutdown deadline", "count", remaining)
			return
		case <-time.After(50 * time.Millisecond):
		}
	}
}
