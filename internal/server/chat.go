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
	for _, connection := range server.lobbyConns {
		select {
		case connection.Write <- msg:
		default:
		}
	}
	server.lobbyMu.Unlock()

	select {
	case server.globalChat <- msg:
	default:
	}
}
func (server *Server) SendGlobalChatToDB(ctx context.Context, playerId string, message string) {
	server.mu.RLock()
	userId, exists := server.playerUsers[playerId]
	server.mu.RUnlock()
	if !exists {
		log.Printf("[DEBUG] PlayerID: %s could not be matched to UserID. Message: %s not sent", userId, message)
		return
	}
	msgBroadcasted := fmt.Sprintf("[%s]: %s", playerId, message)
	log.Println(msgBroadcasted)

	err := server.db.CallFunction(ctx, "global_chat_message_send", userId, message, msgBroadcasted)
	if err != nil {
		log.Printf("Mesage not sent. Reason: %s", err)
	}
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
