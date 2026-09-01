package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"uuid"

	"unnamed-game/internal/game"

	"github.com/gorilla/websocket"
)

func (server *Server) HandleCreateGame(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	var req GameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.PlayerID == "" {
		http.Error(w, "Invalid request payload or missing player_id", http.StatusBadRequest)
		return
	}

	server.mu.Lock()
	defer server.mu.Unlock()

	targetGameID := uuid.NewV7()
	targetGame := game.NewGame(targetGameID, server.globalChat)

	server.AddGame(targetGame)
	go server.listenToGameEvents(targetGame)
	log.Println("NEW GAME CREATED")

	if err := targetGame.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	server.AddPlayerIdToGame(req.PlayerID, targetGame)
	log.Printf("added %s to game: %s", req.PlayerID, targetGame.GameId)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(GameResponse{
		GameID: targetGameID.String(),
	})
}

func (server *Server) HandleJoinGame(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	var req GameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.PlayerID == "" {
		http.Error(w, "Invalid request payload or missing parameters", http.StatusBadRequest)
		return
	}

	parsedID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Error(w, "Invalid game ID format", http.StatusBadRequest)
		return
	}

	server.mu.Lock()
	defer server.mu.Unlock()

	targetGame, exists := server.activeGames[parsedID]
	if !exists {
		http.Error(w, "Game session not found", http.StatusNotFound)
		return
	}

	if err := targetGame.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	server.AddPlayerIdToGame(req.PlayerID, targetGame)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(GameResponse{
		GameID: parsedID.String(),
	})
}

func (server *Server) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	username := r.URL.Query().Get("playerId")
	if username == "" {
		http.Error(w, "No Player ID.", http.StatusBadRequest)
		return
	}

	g, ok := server.FindGameByPlayerId(username)
	if !ok {
		log.Printf("[ERROR] Cannot find game for %s", username)
		return
	}

	if err := server.InitializeConnection(g, username); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	conn, err := server.Upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	if err := server.RegisterPlayer(g, username, conn); err != nil {
		conn.Close()
		return
	}
	defer server.DisconnectPlayer(g, username, conn)

	go server.streamFrames(g, username, conn)
	fmt.Printf("Player %s connected and spawned!\n", username)
	server.readPlayerInputs(g, username, conn)
}

func (server *Server) HandleLobbyChatWS(w http.ResponseWriter, r *http.Request) {
	conn, err := server.Upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Error connecting to global chat")
		return
	}

	server.lobbyMu.Lock()
	if server.lobbyConns == nil {
		server.lobbyConns = make(map[*websocket.Conn]bool)
	}
	server.lobbyConns[conn] = true

	for _, historyMsg := range server.chatHistory {
		if err := conn.WriteMessage(websocket.TextMessage, []byte(historyMsg)); err != nil {
			break
		}
	}
	server.lobbyMu.Unlock()

	defer func() {
		server.lobbyMu.Lock()
		delete(server.lobbyConns, conn)
		server.lobbyMu.Unlock()
		conn.Close()
	}()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}

		if len(msg) > 0 {
			server.BroadcastGlobalChat(r.URL.Query().Get("playerId"), string(msg))
		}
	}
}
