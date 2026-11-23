package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

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
			log.Printf("[CHAT] Game %s chat inbox full; global message dropped for it", g.GameId)
		}
	}
}
func (server *Server) SendGlobalChatToDB(playerId string, message string) {
	server.mu.RLock()
	userId, exists := server.playerUsers[playerId]
	server.mu.RUnlock()
	if !exists {
		log.Printf("[DEBUG] PlayerID: %s could not be matched to UserID. Message: %s not sent", playerId, message)
		return
	}
	msgBroadcasted := fmt.Sprintf("[%s]: %s", playerId, message)
	log.Println(msgBroadcasted)

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
	log.Printf("[DB EVENT] Handling achievement unlocked event: %s", payload)
	var payloadJson AchievementNotificationPayload
	err := json.Unmarshal([]byte(payload), &payloadJson)
	if err != nil {
		log.Printf("[DB EVENT] Failed to unmarshal achievement unlocked payload: %v", err)
		return
	}
	if !payloadJson.IsGlobalAnnouncement {
		log.Printf("[DB EVENT] Achievement unlocked for player, not globally announced %s: %s", payloadJson.PlayerID, payloadJson.Title)
		return
	}
	message := fmt.Sprintf("[Achievement]: %s unlocked %s!", payloadJson.PlayerID, payloadJson.Title)
	server.BroadcastGlobalMessage(message)
}
