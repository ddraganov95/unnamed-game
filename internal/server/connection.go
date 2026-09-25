package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"
	"uuid"

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
		oldConn.Conn.Close()
	}

	g.Mu.RLock()
	defer g.Mu.RUnlock()

	return nil
}

func (server *Server) RegisterPlayer(g *game.Game, playerID string, conn *GameWSConnection) error {
	userID, err := uuid.Parse(conn.UserID)
	if err != nil {
		return errors.New("Invalid user ID format in session")
	}
	userConfig, err := server.db.FetchUserConfiguration(conn.Ctx, userID)
	if err != nil {
		return errors.New(err.Error())
	}

	respChan := make(chan error, 1)
	g.EventChan <- game.GameEvent{
		Type:     game.EventTypeConnect,
		PlayerID: playerID,
		RespChan: respChan,
		Object:   userConfig.Keybinds,
	}

	if err := <-respChan; err != nil {
		return err
	}

	server.mu.RLock()
	server.activeConns[playerID] = conn
	server.mu.RUnlock()
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
			server.handlePlayerDisconnectEvent(g, event.PlayerID)

		case game.EventTypeMassDisconnect:
			playersToDisconnect := make([]*game.Player, len(g.Players))
			copy(playersToDisconnect, g.Players)
			for _, p := range playersToDisconnect {
				server.handlePlayerDisconnectEvent(g, p.PlayerID)
			}

		case game.EventTypeIdleCheck:
			server.mu.Lock()
			delete(server.activeGames, g.GameId)
			server.mu.Unlock()
			return

		case game.EventTypeCopyGame:
			server.mu.Lock()
			if connection, exists := server.activeConns[event.PlayerID]; exists {
				connection.Conn.WriteJSON(OutboundWSMessage{
					Type:    "copy_clipboard",
					Payload: event.Value,
				})
				server.mu.Unlock()
			}
		case game.EventTypeAchievementUnlocked:
			userID, exists := server.playerUsers[event.PlayerID]
			if !exists {
				log.Printf("[ACHIEVEMENT ERROR] Could not map PlayerID '%s' to DB userID", event.PlayerID)
				break
			}

			log.Printf("[ACHIEVEMENT DB] Triggering unlock for User: %s | Ach: %v", userID, event.Value)

			//ALWAYS capture and log the error returned by CallProcedure
			if err := server.db.CallFunction(context.Background(), "player_achievement_unlock", userID, event.Value); err != nil {
				log.Printf("[ACHIEVEMENT DB ERROR] Failed to unlock achievement for user %s: %v", userID, err)
			} else {
				log.Printf("[ACHIEVEMENT DB SUCCESS] Successfully persisted unlock for user %s", userID)
			}
			payload, ok := event.Object.(game.AchievementUnlockedPayload)
			if ok {
				if !payload.IsGlobalAnnouncement {
					g.GameChat <- payload.Message
				}
			}

		case game.EventTypeGlobalChatMsg:
			connection, exists := server.activeConns[event.PlayerID]
			if !exists {
				log.Printf("[SERVER ERROR] No connection found for player %s", event.PlayerID)
				continue
			}
			server.SendGlobalChatToDB(connection.Ctx, event.PlayerID, event.Value)
		case game.EventTypeGameChatMsg:
			g.BroadcastGameChat(event.PlayerID, event.Value)
		default:
		}
	}

}
func (server *Server) handlePlayerDisconnectEvent(g *game.Game, playerID string) {
	server.mu.RLock()
	connection, exists := server.activeConns[playerID]
	server.mu.RUnlock()
	if !exists {
		log.Printf("[WARN] No active connection found for player %s on disconnect\n", playerID)
		return
	}
	userID, ok := server.playerUsers[playerID]
	if !ok {
		log.Printf("[ERROR] No user ID found for player %s on disconnect\n", playerID)
		return
	}
	if targetGame, tGameFound := server.FindGameByPlayerId(playerID); tGameFound {
		if targetGame.GameId != g.GameId {
			log.Printf("[ERROR] Player %s is associated with a different game (%s) than the current game (%s) on disconnect\n", playerID, targetGame.GameId, g.GameId)
			return
		}
	}
	player, ok := g.GetPlayerByID(playerID)
	if !ok {
		log.Printf("[ERROR] Failed to get player %s from game to disconnect\n", playerID)
		return
	}
	achievementProgress := g.AchievementEngine.ExportPlayerProgress(player.PlayerID)
	log.Printf("[DISCONNECT DEBUG] PlayerID: '%s' maps to UserID: '%s'", playerID, userID)
	log.Printf("[DISCONNECT DEBUG] Exported Achievement Progress payload type: %T, value: %+v", achievementProgress, achievementProgress)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	user, err := server.db.UpdatePlayerAfterDisconnect(ctx, userID, player.GenerateSummary(), achievementProgress)
	cancel()
	achievementPayload := g.AchievementEngine.BuildClientAchievementPayload(player.PlayerID)
	if err != nil {
		log.Printf("[ERROR] Failed to save stats for %s on disconnect: %v\n", playerID, err)
	} else {
		log.Printf("[DB] Successfully saved session summary for %s\n", playerID)
		if exists {
			connection.Conn.WriteJSON(map[string]any{
				"type":         "session_summary",
				"user":         user,
				"achievements": achievementPayload,
			})
			log.Printf("[DB] Successfully SENT session summary for %s\n", playerID)
		}
	}
	server.DisconnectPlayer(g, playerID, connection)
}
func (server *Server) DisconnectPlayer(g *game.Game, playerID string, targetConn *GameWSConnection) {
	server.mu.Lock()
	if activeConn, exists := server.activeConns[playerID]; exists && activeConn.Conn == targetConn.Conn {
		delete(server.activeConns, playerID)
	}
	server.mu.Unlock()

	targetConn.Conn.Close()

	g.EventChan <- game.GameEvent{
		Type:     game.EventTypeDisconnect,
		PlayerID: playerID,
	}
}
