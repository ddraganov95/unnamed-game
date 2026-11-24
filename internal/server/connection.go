package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"uuid"

	"unnamed-game/internal/game"

	"github.com/gorilla/websocket"
)

var errGameEnded = errors.New("game has ended")

func (server *Server) InitializeConnection(g *game.Game, username string) error {
	server.mu.Lock()
	oldConn, exists := server.activeConns[username]
	if exists {
		delete(server.activeConns, username)
	}
	server.mu.Unlock()

	if exists && oldConn != nil {
		slog.Info("closing old connection for reconnecting player", "player_id", username)
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
	if !g.SendEvent(game.GameEvent{
		Type:     game.EventTypeConnect,
		PlayerID: playerID,
		RespChan: respChan,
		Object:   userConfig.Keybinds,
		ConnID:   conn.ConnID,
	}) {
		return errGameEnded
	}

	select {
	case err := <-respChan:
		if err != nil {
			return err
		}
	case <-g.DestroyChan:
		return errGameEnded
	}

	server.mu.Lock()
	server.activeConns[playerID] = conn
	server.mu.Unlock()
	return nil
}

func (server *Server) streamFrames(g *game.Game, playerID string, conn *GameWSConnection) {
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
			if err := conn.WriteText(frame); err != nil {
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
			if !g.SendEvent(game.GameEvent{
				PlayerID: playerID,
				Type:     game.EventTypeKey,
				Key:      rune(msg[0]),
			}) {
				return // game ended
			}
		}
	}
}

func (server *Server) listenToGameEvents(g *game.Game) {
	for event := range g.ServerEventChan {
		switch event.Type {
		case game.EventTypeDisconnect:
			payload, _ := event.Object.(game.PlayerDisconnectPayload)
			server.handlePlayerDisconnectEvent(g, event.PlayerID, event.ConnID, payload)
		case game.EventTypeGameDestroy:
			// Players still connected were disconnected by the game just
			// before this event, so their sessions are already saved.
			err := server.handleGameFinalization(g, event.Value)
			event.RespChan <- err
		case game.EventTypeDeactivateGame:
			server.mu.Lock()
			delete(server.activeGames, g.GameId)
			server.mu.Unlock()
			return
		case game.EventTypeCopyGame:
			server.mu.RLock()
			connection, exists := server.activeConns[event.PlayerID]
			server.mu.RUnlock()
			if exists {
				if err := connection.WriteJSON(OutboundWSMessage{
					Type:    "copy_clipboard",
					Payload: event.Value,
				}); err != nil {
					slog.Debug("copy clipboard message not sent", "game_id", g.GameId, "player_id", event.PlayerID, "error", err)
				}
			}
		case game.EventTypeAchievementUnlocked:
			server.mu.RLock()
			userID, exists := server.playerUsers[event.PlayerID]
			server.mu.RUnlock()
			if !exists {
				slog.Error("achievement unlock: no DB user for player", "player_id", event.PlayerID)
				break
			}

			slog.Debug("triggering achievement unlock", "game_id", g.GameId, "user_id", userID, "achievement_id", event.Value)

			ctx, cancel := context.WithTimeout(context.Background(), DBWriteTimeout)
			err := server.db.CallFunction(ctx, "player_achievement_unlock", userID, event.Value)
			cancel()

			//ALWAYS capture and log the error returned by CallFunction
			if err != nil {
				slog.Error("failed to persist achievement unlock", "user_id", userID, "achievement_id", event.Value, "error", err)
			} else {
				slog.Debug("achievement unlock persisted", "user_id", userID)
			}
			payload, ok := event.Object.(game.AchievementUnlockedPayload)
			if ok {
				if !payload.IsGlobalAnnouncement {
					g.GameChat <- payload.Message
				}
			}
		case game.EventTypeLevelCompleted:
			server.processAndSaveEvents(g, event)
		case game.EventTypeGlobalChatMsg:
			server.mu.RLock()
			_, exists := server.activeConns[event.PlayerID]
			server.mu.RUnlock()
			if !exists {
				slog.Warn("global chat dropped, no connection for player", "player_id", event.PlayerID)
				continue
			}
			server.SendGlobalChatToDB(event.PlayerID, event.Value)
		case game.EventTypeGameChatMsg:
			g.BroadcastGameChat(event.PlayerID, event.Value)
		default:
		}
	}

}
// handlePlayerDisconnectEvent runs for every player the game disconnected
// (quit, closed tab, AFK kick, game end). It always saves the unsaved part of
// the session; then, if the player's socket is still registered, sends it the
// session summary and closes it. connID is the connection the game
// disconnected (0 = whichever is registered); a newer socket from a
// reconnect is left alone.
func (server *Server) handlePlayerDisconnectEvent(g *game.Game, playerID string, connID uint64, payload game.PlayerDisconnectPayload) {
	server.mu.RLock()
	userID, ok := server.playerUsers[playerID]
	connection, connExists := server.activeConns[playerID]
	server.mu.RUnlock()
	if connExists && connID != 0 && connection.ConnID != connID {
		slog.Debug("disconnect of old connection ignored", "game_id", g.GameId, "player_id", playerID, "conn_id", connID, "current_conn_id", connection.ConnID)
		connExists = false
	}

	var userStats any
	saved := false
	if !ok {
		slog.Error("no user ID for player on disconnect, session not saved", "game_id", g.GameId, "player_id", playerID)
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), DBWriteTimeout)
		stats, err := server.db.UpdatePlayerAfterDisconnect(ctx, userID, payload.Unsaved, payload.AchievementProgress)
		cancel()
		if err != nil {
			slog.Error("failed to save player stats on disconnect", "game_id", g.GameId, "player_id", playerID, "error", err)
		} else {
			slog.Debug("session summary saved", "game_id", g.GameId, "player_id", playerID)
			userStats, saved = stats, true
		}
	}

	if !connExists {
		return // socket already gone (closed tab) or replaced by a reconnect
	}
	if saved {
		if err := connection.WriteJSON(map[string]any{
			"type":         "session_summary",
			"player_id":    playerID,
			"user_stats":   userStats,
			"achievements": payload.Achievements,
		}); err != nil {
			slog.Debug("session summary not sent", "game_id", g.GameId, "player_id", playerID, "error", err)
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

	g.SendEvent(game.GameEvent{ // no-op if the game already ended
		Type:     game.EventTypeDisconnect,
		PlayerID: playerID,
		ConnID:   targetConn.ConnID, // ignored by the game if this socket was replaced
	})
}
func (s *Server) processAndSaveEvents(g *game.Game, event game.ServerEvent) {
	events, ok := event.Object.([]game.Event)
	if !ok {
		slog.Error("level completed event has unexpected payload", "game_id", g.GameId, "type", fmt.Sprintf("%T", event.Object))
		return
	}
	if len(events) == 0 {
		return
	}

	//Call DB async
	s.runAsync("save game events", g.GameId, func(ctx context.Context) error {
		return s.db.SaveGameEventsCopy(ctx, g.GameId, events)
	})
}
func (s *Server) runAsync(taskName string, gameID uuid.UUID, dbOp func(ctx context.Context) error) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), DBWriteTimeout)
		defer cancel()

		if err := dbOp(ctx); err != nil {
			slog.Error("async db task failed", "task", taskName, "game_id", gameID, "error", err)
			return
		}
		slog.Debug("async db task completed", "task", taskName, "game_id", gameID)
	}()
}
func (s *Server) handleGameFinalization(g *game.Game, status string) error {
	gameID := g.GameId
	summary := g.GenerateSummary()
	events := g.ExtractLogsToSave()

	// Enforce a strict timeout for the final write
	ctx, cancel := context.WithTimeout(context.Background(), DBWriteTimeout)
	defer cancel()

	if err := s.db.EndGameWithEvents(ctx, gameID, status, summary, events); err != nil {
		slog.Error("failed to finalize game in db", "game_id", gameID, "reason", status, "error", err)
		return err
	}

	slog.Info("game finalized", "game_id", gameID, "reason", status)
	return nil
}
