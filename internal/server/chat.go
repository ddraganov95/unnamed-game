package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"unnamed-game/internal/game"
)

func (server *Server) BroadcastGlobalMessage(msg string) {
	server.lobbyMu.Lock()
	server.chatHistory = append(server.chatHistory, msg)
	if len(server.chatHistory) > game.MaxChatHistory {
		server.chatHistory = server.chatHistory[1:]
	}
	for connection := range server.lobbyConns {
		select {
		case connection.Write <- msg:
		default:
		}
	}
	server.lobbyMu.Unlock()

	// Fan out to every game: a channel delivers each value to only one
	// reader, so games can't share one global chat channel.
	server.mu.RLock()
	games := make([]*game.Game, 0, len(server.activeGames))
	for _, g := range server.activeGames {
		games = append(games, g)
	}
	server.mu.RUnlock()
	for _, g := range games {
		if !g.DeliverGlobalChat(msg) {
			slog.Warn("global chat inbox full, message dropped", "game_id", g.GameId)
		}
	}
}
func (server *Server) SendGlobalChatToDB(playerId string, message string) {
	server.mu.RLock()
	userId, exists := server.playerUsers[playerId]
	server.mu.RUnlock()
	if !exists {
		slog.Warn("global chat dropped, player not matched to user", "player_id", playerId)
		return
	}
	msgBroadcasted := fmt.Sprintf("[%s]: %s", playerId, message)
	slog.Info("global chat", "player_id", playerId, "length", len(message))
	slog.Debug("global chat text", "player_id", playerId, "text", message)

	server.enqueueDBWrite("send global chat", func(ctx context.Context) error {
		return server.db.CallFunction(ctx, "global_chat_message_send", userId, message, msgBroadcasted)
	})
}

type AchievementNotificationPayload struct {
	PlayerID             string `json:"player_id"`
	Title                string `json:"title"`
	IsGlobalAnnouncement bool   `json:"is_global_announcement"`
}

func (server *Server) handleAchievementUnlockedEvent(payload string) {
	var payloadJson AchievementNotificationPayload
	err := json.Unmarshal([]byte(payload), &payloadJson)
	if err != nil {
		slog.Error("failed to unmarshal achievement unlocked payload", "error", err)
		return
	}
	if !payloadJson.IsGlobalAnnouncement {
		slog.Debug("achievement unlocked, not announced globally", "player_id", payloadJson.PlayerID, "title", payloadJson.Title)
		return
	}
	message := fmt.Sprintf("[Achievement]: %s unlocked %s!", payloadJson.PlayerID, payloadJson.Title)
	server.BroadcastGlobalMessage(message)
}
