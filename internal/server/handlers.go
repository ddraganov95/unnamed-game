package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"unnamed-game/internal/db"
	"unnamed-game/internal/game"
	"uuid"

	"github.com/gorilla/websocket"
)

func (server *Server) handleDBEvent(channel, payload string) {
	switch channel {
	case "achievement_unlocked":
		server.handleAchievementUnlockedEvent(payload)
	case "global_chat":
		server.BroadcastGlobalMessage(payload)
	default:
		slog.Warn("unhandled db event channel", "channel", channel)
	}
}
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

	targetGameID := uuid.NewV7()
	// Not started yet: if anything below fails the game is just dropped,
	// with no goroutines left running.
	targetGame := game.CreateGame(targetGameID, server.achievementCat)

	if err := targetGame.Validate(playerID); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	if err := server.db.InsertNewGame(r.Context(), targetGameID, server.buildServerHost()); err != nil {
		requestLogger(r.Context()).Error("failed to create game in db", "game_id", targetGameID, "player_id", playerID, "error", err)
		http.Error(w, "Failed to create game", http.StatusInternalServerError)
		return
	}
	requestLogger(r.Context()).Info("game created", "game_id", targetGameID, "player_id", playerID)
	// Add joining player's achievement progress in this room
	progress, err := server.db.FetchPlayerProgress(r.Context(), userID)
	if err == nil {
		targetGame.AchievementEngine.LoadPlayerAchievements(playerID, progress)
	}

	// Setup succeeded: start the server listener and the game, then make it
	// visible to joins and websocket connects.
	go server.listenToGameEvents(targetGame)
	targetGame.Start()

	server.mu.Lock()
	server.activeGames[targetGame.GameId] = targetGame
	server.playerSessions[playerID] = targetGame.GameId
	server.mu.Unlock()

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(GameResponse{
		GameID:      targetGameID.String(),
		RedirectURL: server.BuildGamePageURL(r, server.buildServerHost(), targetGameID),
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
	host, err := server.db.FetchGameHostByGameId(r.Context(), parsedID)
	if err != nil {
		requestLogger(r.Context()).Error("failed to look up game host", "game_id", parsedID, "error", err)
		http.Error(w, "Game not found", http.StatusNotFound)
		return
	}
	localHost := server.buildServerHost()
	if localHost != host {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(GameResponse{
			GameID:      parsedID.String(),
			RedirectURL: server.BuildGamePageURL(r, host, parsedID),
		})
		return
	}

	server.mu.RLock()
	targetGame, exists := server.activeGames[parsedID]
	server.mu.RUnlock()

	if !exists {
		http.Error(w, "Game session not found", http.StatusNotFound)
		return
	}

	if err := targetGame.Validate(playerID); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	// Add joining player's achievement progress in this room
	progress, err := server.db.FetchPlayerProgress(r.Context(), userID)
	if err == nil {
		targetGame.AchievementEngine.LoadPlayerAchievements(playerID, progress)
	}

	server.mu.Lock()
	server.playerSessions[playerID] = targetGame.GameId
	server.mu.Unlock()

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(GameResponse{
		GameID:      parsedID.String(),
		RedirectURL: server.BuildGamePageURL(r, host, targetGame.GameId),
	})
}

func (server *Server) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	userID, playerID, err := ExtractSessionIDs(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	parsedUserID, err := uuid.Parse(userID)
	if err != nil {
		http.Error(w, "Invalid user ID format in session", http.StatusUnauthorized)
		return
	}
	server.mu.Lock()
	server.playerUsers[playerID] = parsedUserID
	server.mu.Unlock()
	g, ok := server.FindGameByPlayerId(playerID)
	if !ok {
		requestLogger(r.Context()).Warn("no game found for player on websocket connect", "player_id", playerID)
		return
	}

	if err := server.InitializeConnection(g, playerID); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	conn, err := server.Upgrader.Upgrade(w, r, nil)
	if err != nil {
		requestLogger(r.Context()).Info("websocket upgrade failed", "player_id", playerID, "error", err)
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	newConnection := &GameWSConnection{
		Conn:     conn,
		Ctx:      ctx,
		Cancel:   cancel,
		UserID:   userID,
		PlayerID: playerID,
		ConnID:   server.nextConnID.Add(1),
	}
	if err := server.RegisterPlayer(g, playerID, newConnection); err != nil {
		// The upgrade already succeeded, so tell the client why with a close
		// frame (web/game.js shows the reason in the lobby).
		reason := err.Error()
		if len(reason) > 120 { // control frame payload limit is 125 bytes
			reason = reason[:120]
		}
		if err := conn.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(CloseCodeJoinRejected, reason),
			time.Now().Add(time.Second)); err != nil {
			requestLogger(r.Context()).Debug("join rejection close frame not sent", "player_id", playerID, "error", err)
		}
		if err := conn.Close(); err != nil {
			requestLogger(r.Context()).Debug("rejected websocket close failed", "player_id", playerID, "error", err)
		}
		requestLogger(r.Context()).Info("websocket join rejected", "game_id", g.GameId, "player_id", playerID, "close_code", CloseCodeJoinRejected, "reason", reason)
		return
	}
	defer server.DisconnectPlayer(g, playerID, newConnection)

	go server.streamFrames(g, playerID, newConnection)
	server.readPlayerInputs(g, playerID, conn)
}

func (server *Server) HandleLobbyChatWS(w http.ResponseWriter, r *http.Request) {
	userID, playerID, err := ExtractSessionIDs(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := server.Upgrader.Upgrade(w, r, nil)
	if err != nil {
		requestLogger(r.Context()).Info("lobby chat websocket upgrade failed", "player_id", playerID, "error", err)
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	server.lobbyMu.Lock()
	chatConn := &ChatWSConnection{
		Conn:     conn,
		Ctx:      ctx,
		Cancel:   cancel,
		UserID:   userID,
		PlayerID: playerID,
		Write:    make(chan string, 50),
	}
	server.lobbyConns[chatConn] = struct{}{}
	go func() {
		for msg := range chatConn.Write {
			conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
			if err := conn.WriteMessage(websocket.TextMessage, []byte(msg)); err != nil {
				requestLogger(r.Context()).Debug("lobby chat write failed", "player_id", playerID, "error", err)
				break
			}
		}
	}()
	for _, historyMsg := range server.chatHistory {
		if err := conn.WriteMessage(websocket.TextMessage, []byte(historyMsg)); err != nil {
			requestLogger(r.Context()).Debug("lobby chat history write failed", "player_id", playerID, "error", err)
			break
		}
	}
	server.lobbyMu.Unlock()

	defer func() {
		server.lobbyMu.Lock()
		delete(server.lobbyConns, chatConn)
		server.lobbyMu.Unlock()
		close(chatConn.Write)
		conn.Close()
	}()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			requestLogger(r.Context()).Debug("lobby chat connection closed", "player_id", playerID, "error", err)
			break
		}

		if len(msg) > 0 {
			server.SendGlobalChatToDB(playerID, string(msg))
		}
	}
}

func (server *Server) HandleCreateUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	var req CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.PlayerID == "" {
		http.Error(w, "Invalid request payload or missing player_id", http.StatusBadRequest)
		return
	}

	user, err := server.db.UpsertUser(r.Context(), req.PlayerID)
	if err != nil {
		requestLogger(r.Context()).Error("failed to upsert user", "player_id", req.PlayerID, "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	cookieValue := fmt.Sprintf("%s:%s", user.UserID, user.PlayerID)
	server.mu.Lock()
	server.playerUsers[req.PlayerID] = user.UserID
	server.mu.Unlock()
	requestLogger(r.Context()).Debug("user upserted", "player_id", req.PlayerID, "user_id", user.UserID)
	http.SetCookie(w, &http.Cookie{
		Name:     "player_session",
		Value:    cookieValue, // "user-uuid:player-id"
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400,
	})

	json.NewEncoder(w).Encode(map[string]string{
		"status":   "ok",
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

	userSummary, err := server.db.FetchUserSummary(r.Context(), userID)
	if err != nil {
		requestLogger(r.Context()).Error("failed to fetch user summary", "user_id", userID, "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := json.NewEncoder(w).Encode(userSummary); err != nil {
		requestLogger(r.Context()).Error("failed to encode user summary", "user_id", userID, "error", err)
		http.Error(w, "Failed to encode user data", http.StatusInternalServerError)
		return
	}
	requestLogger(r.Context()).Debug("fetched user summary", "player_id", playerID, "user_id", userSummary.UserID)
}

func (server *Server) HandleGetSelfAchievements(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	userIDStr, _, err := ExtractSessionIDs(r)
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
		requestLogger(r.Context()).Error("failed to fetch achievements", "user_id", userID, "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := json.NewEncoder(w).Encode(achievements); err != nil {
		requestLogger(r.Context()).Error("failed to encode achievements", "user_id", userID, "error", err)
		http.Error(w, "Failed to encode achievements data", http.StatusInternalServerError)
		return
	}

	requestLogger(r.Context()).Debug("fetched achievements", "user_id", userID)
}

const LeaderBoardPageSize = 10 //NUMBER OF ROWS SHOWN ON THE LEADERBOARD
type SelfLeaderboardReponse struct {
	LeaderboardReponse
	CurrentUser db.UserLeaderboardView `json:"CurrentUser"`
}

func (server *Server) HandleGetSelfLeaderboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	userIDStr, _, err := ExtractSessionIDs(r)
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
		requestLogger(r.Context()).Error("failed to fetch leaderboard", "user_id", userID, "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	currentuser, err := getCurrentUserFromPage(userID, usersOnLeaderboard)
	if err != nil {
		requestLogger(r.Context()).Warn("user not found on leaderboard page", "user_id", userID, "error", err)
	}

	//page calculation math
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
		requestLogger(r.Context()).Error("failed to encode leaderboard", "user_id", userID, "error", err)
		http.Error(w, "Failed to encode leaderboard data", http.StatusInternalServerError)
		return
	}
	requestLogger(r.Context()).Debug("fetched leaderboard", "user_id", userID)
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
	usersOnLeaderboard, err := server.db.FetchLeaderboardPage(r.Context(), page, LeaderBoardPageSize)
	if err != nil {
		requestLogger(r.Context()).Error("failed to fetch leaderboard page", "error", err)
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
		requestLogger(r.Context()).Error("failed to encode leaderboard page", "error", err)
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
	userID, playerID, err = parseSessionCookie(r)
	if err != nil {
		reason := "malformed"
		if errors.Is(err, http.ErrNoCookie) {
			reason = "missing"
		}
		requestLogger(r.Context()).Debug("session cookie rejected", "reason", reason)
	}
	return userID, playerID, err
}

// parseSessionCookie reads the session cookie without logging, so the request
// logger can use it too.
func parseSessionCookie(r *http.Request) (userID, playerID string, err error) {
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

	requestLogger(r.Context()).Debug("fetching user configuration", "user_id", userID, "player_id", playerID)

	userConfig, err := server.db.FetchUserConfiguration(r.Context(), userID)
	if err != nil {
		requestLogger(r.Context()).Error("failed to fetch user configuration", "user_id", userID, "error", err)
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(userConfig)
}
// Keys hard-coded in game.Player.InitKeybindings (copy game ID, next level,
// enter to type). A user keybind on one of these would be silently overridden.
var reservedKeybindKeys = map[string]struct{}{
	"c":  {},
	" ":  {},
	"\r": {},
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

		if _, reserved := reservedKeybindKeys[key]; reserved {
			http.Error(w, fmt.Sprintf("Key '%s' is reserved and cannot be assigned to '%s'",
				key,
				db.GetActionLabel(action),
			), http.StatusBadRequest)
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

	requestLogger(r.Context()).Debug("updating user configuration", "user_id", userID, "player_id", playerID)

	err = server.db.UpsertUserConfiguration(r.Context(), userID, userConfig)
	if err != nil {
		requestLogger(r.Context()).Error("failed to save user configuration", "user_id", userID, "error", err)
		http.Error(w, "failed to save configuration", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(userConfig)
}
