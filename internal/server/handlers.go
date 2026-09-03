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

	cookie, err := r.Cookie("player_session")
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	playerID := cookie.Value

	server.mu.Lock()
	defer server.mu.Unlock()

	targetGameID := uuid.NewV7()
	targetGame := game.NewGame(targetGameID, server.globalChat)

	if err := targetGame.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	server.AddGame(targetGame)
	go server.listenToGameEvents(targetGame)
	log.Println("NEW GAME CREATED")

	server.AddPlayerIdToGame(playerID, targetGame)
	log.Printf("added %s to game: %s", playerID, targetGame.GameId)

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

	//Retrieve session identity directly from cookie
	cookie, err := r.Cookie("player_session")
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	playerID := cookie.Value

	//Extract game ID from URL path parameters
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

	//Attach validated cookie identity to target game room
	server.AddPlayerIdToGame(playerID, targetGame)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(GameResponse{
		GameID: parsedID.String(),
	})
}

func (server *Server) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("player_session")
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	playerID := cookie.Value

	g, ok := server.FindGameByPlayerId(playerID)
	if !ok {
		log.Printf("[ERROR] Cannot find game for %s", playerID)
		return
	}

	if err := server.InitializeConnection(g, playerID); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	conn, err := server.Upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	if err := server.RegisterPlayer(g, playerID, conn); err != nil {
		conn.Close()
		return
	}
	defer server.DisconnectPlayer(g, playerID, conn)

	go server.streamFrames(g, playerID, conn)
	fmt.Printf("Player %s connected and spawned!\n", playerID)
	server.readPlayerInputs(g, playerID, conn)

}

func (server *Server) HandleLobbyChatWS(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("player_session")
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	playerID := cookie.Value

	conn, err := server.Upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Error connecting to global chat")
		return
	}

	server.lobbyMu.Lock()
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
			server.BroadcastGlobalChat(playerID, string(msg))
		}
	}
}
func (server *Server) HandleCreateUser(w http.ResponseWriter, r *http.Request) {
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

	user, err := server.db.GetOrCreateUser(r.Context(), req.PlayerID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "player_session",
		Value:    user.PlayerID, // Store the PLAYER ID
		Path:     "/",
		HttpOnly: true,                 // Blocks JavaScript access (XSS defense)
		SameSite: http.SameSiteLaxMode, // Prevents CSRF on cross-site requests
		MaxAge:   86400,                // 24 hours in seconds
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":   "ok",
		"redirect": "/lobby.html"})
}
func (server *Server) HandleGetUser(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	targetPlayerID := r.PathValue("id")
	if targetPlayerID == "" {
		http.Error(w, "Missing player ID", http.StatusBadRequest)
		return
	}

	_, err := r.Cookie("player_session")
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	user, err := server.db.GetUserSummary(r.Context(), targetPlayerID)
	if err != nil {
		http.Error(w, "Player not found", http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(user)
}
func (server *Server) HandleGetSelf(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	cookie, err := r.Cookie("player_session")
	if err != nil || cookie.Value == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	playerID := cookie.Value

	user, err := server.db.GetUserSummary(r.Context(), playerID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := json.NewEncoder(w).Encode(user); err != nil {
		http.Error(w, "Failed to encode user data", http.StatusInternalServerError)
		return
	}
}
