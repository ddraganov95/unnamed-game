package server

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"unnamed-game/internal/game"

	"github.com/gorilla/websocket"
)

func (server *Server) BroadcastGlobalMessage(msg string) {
	server.lobbyMu.Lock()
	server.chatHistory = append(server.chatHistory, msg)
	if len(server.chatHistory) > game.MaxChatHistory {
		server.chatHistory = server.chatHistory[1:]
	}

	for conn := range server.lobbyConns {
		conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
		if err := conn.WriteMessage(websocket.TextMessage, []byte(msg)); err != nil {
			conn.Close()
			delete(server.lobbyConns, conn)
		}
	}
	server.lobbyMu.Unlock()

	select {
	case server.globalChat <- msg:
	default:
	}
}
func (server *Server) BroadcastGlobalChat(playerid string, message string) {
	msg := fmt.Sprintf("[%s]: %s", playerid, message)
	log.Println(msg)
	server.BroadcastGlobalMessage(msg)
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
