package server

import (
	"fmt"

	"unnamed-game/internal/game"

	"github.com/gorilla/websocket"
)

func (server *Server) InitializeConnection(g *game.Game, username string) error {
	server.mu.Lock()
	oldConn, exists := server.activeConns[username]
	if exists {
		delete(server.activeConns, username)
	}
	server.mu.Unlock()

	if exists && oldConn != nil {
		fmt.Printf("Player %s reconnecting. Closing old connection.\n", username)
		oldConn.Close()
	}

	g.Mu.RLock()
	defer g.Mu.RUnlock()

	return nil
}

func (server *Server) RegisterPlayer(g *game.Game, playerID string, conn *websocket.Conn) error {
	respChan := make(chan error, 1)
	g.EventChan <- game.GameEvent{
		Type:     game.EventTypeConnect,
		PlayerID: playerID,
		RespChan: respChan,
	}

	if err := <-respChan; err != nil {
		return err
	}

	server.mu.Lock()
	server.activeConns[playerID] = conn
	server.mu.Unlock()
	return nil
}

func (server *Server) streamFrames(g *game.Game, playerID string, conn *websocket.Conn) {
	g.Mu.RLock()
	var playerChan chan string
	for _, p := range g.Players {
		if p.GetID() == playerID {
			playerChan = p.DisplayChan
			break
		}
	}
	g.Mu.RUnlock()

	if playerChan == nil {
		return
	}

	for {
		select {
		case <-g.DestroyChan:
			return
		case frame := <-playerChan:
			if err := conn.WriteMessage(websocket.TextMessage, []byte(frame)); err != nil {
				return
			}
		}
	}
}

func (server *Server) readPlayerInputs(g *game.Game, playerID string, conn *websocket.Conn) {
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}

		if len(msg) > 0 {
			g.EventChan <- game.GameEvent{
				PlayerID: playerID,
				Type:     game.EventTypeKey,
				Key:      rune(msg[0]),
			}
		}
	}
}

func (server *Server) listenToGameEvents(g *game.Game) {
	for event := range g.ServerEventChan {
		switch event.Type {
		case game.EventTypeDisconnect:
			server.mu.Lock()
			conn, exists := server.activeConns[event.PlayerID]
			server.mu.Unlock()
			if exists {
				server.DisconnectPlayer(g, event.PlayerID, conn)
			}

		case game.EventTypeMassDisconnect:
			for _, p := range g.Players {
				server.mu.Lock()
				conn, exists := server.activeConns[p.PlayerID]
				server.mu.Unlock()
				if exists {
					server.DisconnectPlayer(g, p.PlayerID, conn)
				}
			}
			event.RespChan <- nil

		case game.EventTypeIdleCheck:
			server.mu.Lock()
			delete(server.activeGames, g.GameId)
			server.mu.Unlock()
			return

		case game.EventTypeCopyGame:
			if conn, exists := server.activeConns[event.PlayerID]; exists {
				server.mu.Lock()
				conn.WriteJSON(OutboundWSMessage{
					Type:    "copy_clipboard",
					Payload: event.Value,
				})
				server.mu.Unlock()
			}

		case game.EventTypeGlobalChatMsg:
			server.BroadcastGlobalChat(event.PlayerID, event.Value)
		}
	}
}

func (server *Server) DisconnectPlayer(g *game.Game, playerID string, targetConn *websocket.Conn) {
	server.mu.Lock()
	if activeConn, exists := server.activeConns[playerID]; exists && activeConn == targetConn {
		delete(server.activeConns, playerID)
	}
	server.mu.Unlock()

	targetConn.Close()

	g.EventChan <- game.GameEvent{
		Type:     game.EventTypeDisconnect,
		PlayerID: playerID,
	}
}
