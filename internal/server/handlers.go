package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"unnamed-game/internal/db"
	"unnamed-game/internal/game"
	"uuid"

	"github.com/gorilla/websocket"
)

func (server *Server) HandleCreateGame(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	userIDStr, playerID, err := ExtractSessionIDs(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		http.Error(w, "Invalid user ID format in session", http.StatusUnauthorized)
		return
	}

	server.mu.Lock()
	defer server.mu.Unlock()

	targetGameID := uuid.NewV7()
	targetGame := game.NewGame(targetGameID, server.globalChat, server.achievementCat)

	if err := targetGame.Validate(playerID); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	server.AddGame(targetGame)
	go server.listenToGameEvents(targetGame)
	log.Println("NEW GAME CREATED")

	server.AddPlayerIdToGame(playerID, targetGame)
	log.Printf("added %s to game: %s", playerID, targetGame.GameId)

	// Add joining player's achievement progress in this room
	progress, err := server.db.FetchPlayerProgress(r.Context(), userID)
	if err == nil {
		targetGame.AchievementEngine.LoadPlayerAchievements(playerID, progress)
	}

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

	userIDStr, playerID, err := ExtractSessionIDs(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		http.Error(w, "Invalid user ID format in session", http.StatusUnauthorized)
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

	if err := targetGame.Validate(playerID); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	server.AddPlayerIdToGame(playerID, targetGame)

	// Add joining player's achievement progress in this room
	progress, err := server.db.FetchPlayerProgress(r.Context(), userID)
	if err == nil {
		targetGame.AchievementEngine.LoadPlayerAchievements(playerID, progress)
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(GameResponse{
		GameID: parsedID.String(),
	})
}

func (server *Server) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	userID, playerID, err := ExtractSessionIDs(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	server.lobbyMu.Lock()
	server.playerUsers[playerID], err = uuid.Parse(userID)
	server.lobbyMu.Unlock()
	if err != nil {
		http.Error(w, "Invalid user ID format in session", http.StatusUnauthorized)
		return
	}
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
	ctx, cancel := context.WithCancel(r.Context())
	newConnection := &WSConnection{
		Conn:     conn,
		Ctx:      ctx,
		Cancel:   cancel,
		UserID:   userID,
		PlayerID: playerID,
	}
	if err := server.RegisterPlayer(g, playerID, newConnection); err != nil {
		conn.Close()
		return
	}
	defer server.DisconnectPlayer(g, playerID, newConnection)

	go server.streamFrames(g, playerID, conn)
	fmt.Printf("Player %s connected and spawned!\n", playerID)
	server.readPlayerInputs(g, playerID, conn)
}

func (server *Server) HandleLobbyChatWS(w http.ResponseWriter, r *http.Request) {
	_, playerID, err := ExtractSessionIDs(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

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
			server.SendGlobalChatToDB(r.Context(), playerID, string(msg))
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

	user, err := server.db.UpsertUser(r.Context(), req.PlayerID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	cookieValue := fmt.Sprintf("%s:%s", user.UserID, user.PlayerID)
	server.mu.Lock()
	server.playerUsers[req.PlayerID] = user.UserID
	server.mu.Unlock()
	log.Printf("[DB] Successfully saved %s player with UUID %s", req.PlayerID, user.UserID)
	http.SetCookie(w, &http.Cookie{
		Name:     "player_session",
		Value:    cookieValue, // "user-uuid:player-id"
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400,
	})

	json.NewEncoder(w).Encode(map[string]string{
		"status":   "ok", // <------
		"redirect": "/lobby.html",
	})
}

func (server *Server) HandleGetSelf(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	userID, playerID, err := ExtractSessionIDs(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	user, err := server.db.FetchUserSummary(r.Context(), userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := json.NewEncoder(w).Encode(user); err != nil {
		http.Error(w, "Failed to encode user data", http.StatusInternalServerError)
		return
	}
	server.mu.Lock()
	server.playerUsers[playerID] = user.UserID
	server.mu.Unlock()
	log.Printf("[DB] Successfully saved %s player with UUID %s", playerID, user.UserID)
}

func (server *Server) HandleGetSelfAchievements(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	userIDStr, playerID, err := ExtractSessionIDs(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		http.Error(w, "Invalid user ID format", http.StatusBadRequest)
		return
	}

	achievements, err := server.db.FetchPlayerProgress(r.Context(), userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := json.NewEncoder(w).Encode(achievements); err != nil {
		http.Error(w, "Failed to encode achievements data", http.StatusInternalServerError)
		return
	}

	server.mu.Lock()
	server.playerUsers[playerID] = userID
	server.mu.Unlock()
	log.Printf("[DB] Successfully fetched achievements for player with UUID %s", userID.String())
}

const LeaderBoardPageSize = 10 //NUMBER OF ROWS SHOWN ON THE LEADERBOARD
type SelfLeaderboardReponse struct {
	CurrentUser           db.UserLeaderboardView   `json:"CurrentUser"`
	UsersOnPage           []db.UserLeaderboardView `json:"UsersOnPage"`
	CurrentPage           int                      `json:"CurrentPage"`
	NextPageAvailable     bool                     `json:"NextPageAvailable"`
	PreviousPageAvailable bool                     `json:"PreviousPageAvailable"`
}

func (server *Server) HandleGetSelfLeaderboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	userIDStr, playerID, err := ExtractSessionIDs(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		http.Error(w, "Invalid user ID format", http.StatusBadRequest)
		return
	}

	// Fetch 10 + 1 rows aligned to user's page
	usersOnLeaderboard, err := server.db.FetchUserLeaderboard(r.Context(), userID, LeaderBoardPageSize)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	currentuser, err := getCurrentUserFromPage(userID, usersOnLeaderboard)
	if err != nil {
		log.Printf("[DB] %s,%s", userID.String(), err)
	}

	// Correct page calculation math
	currentPage := 1
	if currentuser.Rank > 0 {
		currentPage = ((currentuser.Rank - 1) / LeaderBoardPageSize) + 1
	}

	// Check N+1 for pagination
	hasNext := len(usersOnLeaderboard) > LeaderBoardPageSize
	hasPrev := currentPage > 1

	if hasNext {
		usersOnLeaderboard = usersOnLeaderboard[:LeaderBoardPageSize]
	}

	response := SelfLeaderboardReponse{
		CurrentUser:           currentuser,
		CurrentPage:           currentPage,
		UsersOnPage:           usersOnLeaderboard,
		NextPageAvailable:     hasNext,
		PreviousPageAvailable: hasPrev,
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "Failed to encode leaderboard data", http.StatusInternalServerError)
		return
	}

	server.mu.Lock()
	server.playerUsers[playerID] = userID
	server.mu.Unlock()
	log.Printf("[DB] Successfully fetched leaderboard for player with UUID %s", userID.String())
}
func getCurrentUserFromPage(userID uuid.UUID, usersOnLeaderboard []db.UserLeaderboardView) (db.UserLeaderboardView, error) {
	for i := range usersOnLeaderboard {
		if usersOnLeaderboard[i].UserID == userID {
			return usersOnLeaderboard[i], nil
		}
	}
	return db.UserLeaderboardView{}, errors.New("Couldn't find user on his leaderboard page")
}

type LeaderboardReponse struct {
	UsersOnPage           []db.UserLeaderboardView `json:"UsersOnPage"`
	CurrentPage           int                      `json:"CurrentPage"`
	NextPageAvailable     bool                     `json:"NextPageAvailable"`
	PreviousPageAvailable bool                     `json:"PreviousPageAvailable"`
}

func (server *Server) HandleLeaderboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	//?page=1 or path value /1)
	pageStr := r.URL.Query().Get("page")
	if pageStr == "" {
		pageStr = r.PathValue("page")
	}
	page, err := strconv.Atoi(pageStr)
	if err != nil || page < 1 {
		page = 1
	}
	usersOnLeaderboard, err := server.db.FetchLeaderboardPage(r.Context(), page, LeaderBoardPageSize+1)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	hasNext, hasPrev := getPrevNext(usersOnLeaderboard, page, LeaderBoardPageSize)
	if hasNext {
		usersOnLeaderboard = usersOnLeaderboard[:LeaderBoardPageSize]
	}
	response := LeaderboardReponse{
		UsersOnPage:           usersOnLeaderboard,
		CurrentPage:           page,
		NextPageAvailable:     hasNext,
		PreviousPageAvailable: hasPrev,
	}

	//Send JSON
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "Failed to encode leaderboard data", http.StatusInternalServerError)
		return
	}
}
func getPrevNext[T any](paginatedList []T, page int, maxSize int) (bool, bool) {
	//This method assumes that the list will be generated with page size + 1
	hasNext := false
	if len(paginatedList) > maxSize {
		hasNext = true
	}

	hasPrev := false
	if page > 1 {
		hasPrev = true
	}
	return hasNext, hasPrev
}
func ExtractSessionIDs(r *http.Request) (userID, playerID string, err error) {
	cookie, err := r.Cookie("player_session")
	if err != nil {
		return "", "", err
	}

	parts := strings.Split(cookie.Value, ":")
	if len(parts) != 2 {
		return "", "", errors.New("invalid session cookie format")
	}

	return parts[0], parts[1], nil
}
func (server *Server) HandleGetSelfConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	userIDStr, playerID, err := ExtractSessionIDs(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		http.Error(w, "Invalid user ID format in session", http.StatusUnauthorized)
		return
	}

	log.Printf("[DEBUG] FETCHING USER Configuration FOR PLAYER: uid: %s, pid: %s", userID, playerID)

	userConfig, err := server.db.FetchUserConfiguration(r.Context(), userID)
	if err != nil {
		log.Printf("Error fetching config: %s", err)
	}

	server.mu.Lock()
	server.playerUsers[playerID] = userID
	server.mu.Unlock()
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(userConfig)
}
func (server *Server) HandleUpdateSelfConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	userIDStr, playerID, err := ExtractSessionIDs(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		http.Error(w, "Invalid user ID format in session", http.StatusUnauthorized)
		return
	}
	var userConfig db.UserConfiguration

	err = json.NewDecoder(r.Body).Decode(&userConfig)
	if err != nil {
		http.Error(w, "Sent config is wrong format", http.StatusBadRequest)
		return
	}
	seenKeys := make(map[string]string)

	for action, key := range userConfig.Keybinds {
		runes := []rune(key)
		if len(runes) != 1 {
			http.Error(w, fmt.Sprintf("Keybinding for '%s' must be exactly 1 character", action), http.StatusBadRequest)
			return
		}

		if existingAction, exists := seenKeys[key]; exists {
			http.Error(w, fmt.Sprintf("Key '%s' cannot be assigned to '%s' (already bound to '%s')",
				key,
				db.GetActionLabel(action),
				db.GetActionLabel(existingAction)),
				http.StatusBadRequest,
			)
			return
		}

		seenKeys[key] = action
	}

	log.Printf("[DEBUG] Updating USER Configuration FOR PLAYER: uid: %s, pid: %s", userID, playerID)

	err = server.db.UpsertUserConfiguration(r.Context(), userID, userConfig)
	if err != nil {
		log.Printf("Error fetching config: %s", err)
	}

	server.mu.Lock()
	server.playerUsers[playerID] = userID
	server.mu.Unlock()
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(userConfig)
}
