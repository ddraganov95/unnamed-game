package server

import (
	"fmt"
	"log"

	"unnamed-game/internal/game"

	"github.com/gorilla/websocket"
)

func (server *Server) BroadcastGlobalChat(playerid string, message string) {
	msg := fmt.Sprintf("[%s]: %s", playerid, message)
	log.Println(msg)

	server.lobbyMu.Lock()
	server.chatHistory = append(server.chatHistory, msg)
	if len(server.chatHistory) > game.MaxChatHistory {
		server.chatHistory = server.chatHistory[1:]
	}

	for conn := range server.lobbyConns {
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
