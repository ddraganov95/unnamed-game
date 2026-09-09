package server

import (
	"context"
	"fmt"
	"log"
	"time"

	"unnamed-game/internal/game"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
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
			if conn, exists := server.activeConns[event.PlayerID]; exists {
				server.mu.Lock()
				conn.WriteJSON(OutboundWSMessage{
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
			if err := server.db.CallProcedure(context.Background(), "player_achievement_unlock", userID, event.Value); err != nil {
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
			server.BroadcastGlobalChat(event.PlayerID, event.Value)
		case game.EventTypeGameChatMsg:
			g.BroadcastGameChat(event.PlayerID, event.Value)
		}
	}

}
func (server *Server) handlePlayerDisconnectEvent(g *game.Game, playerID string) {
	server.mu.Lock()
	conn, exists := server.activeConns[playerID]
	server.mu.Unlock()
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
		log.Printf("[DB] Rank for player %s: %d\n", playerID, user.Rank)
		if exists {
			conn.WriteJSON(map[string]any{
				"type":         "session_summary",
				"user":         user,
				"achievements": achievementPayload,
			})
			log.Printf("[DB] Successfully SENT session summary for %s\n", playerID)
		}
	}
	server.DisconnectPlayer(g, playerID, conn)
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
func (server *Server) ListenToDBEvents(ctx context.Context) {
	connConfig := server.db.Pool.Config().ConnConfig
	conn, err := pgx.ConnectConfig(ctx, connConfig)
	if err != nil {
		log.Printf("[DB ERROR] Failed to connect for listening to events: %v", err)
		return
	}
	defer conn.Close(ctx)

	dbEvents := []string{"achievement_unlocked"}
	for _, event := range dbEvents {
		if _, err := conn.Exec(ctx, fmt.Sprintf("LISTEN %s", event)); err != nil {
			log.Printf("[DB ERROR] Failed to listen to event %s: %v", event, err)
			return
		}
	}
	for {
		notification, err := conn.WaitForNotification(ctx)
		if err != nil {
			if ctx.Err() != nil {
				log.Printf("[DB ERROR] Context error while waiting for notification: %v", err)
			} else {
				log.Printf("[DB ERROR] Error while waiting for notification: %v", err)
			}
			return
		}
		switch notification.Channel {
		case "achievement_unlocked":
			log.Printf("[DB EVENT] Received notification for achievement unlocked: %s", notification.Payload)
			server.handleAchievementUnlockedEvent(notification.Payload)
		default:
			log.Printf("[DB EVENT] Received notification for unknown channel: %s", notification.Channel)
		}
	}
}
