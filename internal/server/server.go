package server

import (
	"net/http"
	"sync"
	"uuid"

	"unnamed-game/internal/game"

	"github.com/gorilla/websocket"
)

type Server struct {
	PlayerCounter  uint64
	Upgrader       websocket.Upgrader
	mu             sync.Mutex
	activeConns    map[string]*websocket.Conn // Tracks the active GameWebSocket per player ID
	activeGames    map[uuid.UUID]*game.Game   // Map gameid -> game
	playerSessions map[string]uuid.UUID       // Map String playerid -> gameid
	lobbyConns     map[*websocket.Conn]bool   // Tracks active LobbyWebSockets
	chatHistory    []string
	globalChat     chan string
	lobbyMu        sync.Mutex
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

func NewServer() *Server {
	return &Server{
		Upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
		activeConns:    make(map[string]*websocket.Conn),
		activeGames:    make(map[uuid.UUID]*game.Game),
		playerSessions: make(map[string]uuid.UUID),
		globalChat:     make(chan string, game.MaxChatHistory),
	}
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
