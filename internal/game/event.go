package game

import "log"

type EventType int

const (
	EventTypeKey EventType = iota
	EventTypeConnect
	EventTypeDisconnect
	EventTypeGameDestroy
	EventTypePlayerQuit
	EventTypeDeactivateGame
	EventTypeCopyGame
	EventTypeGlobalChatMsg
	EventTypeGameChatMsg
	EventTypeAchievementUnlocked
	EventTypeGetNextLevel
	EventTypeLevelCompleted
)

type GameEvent struct {
	RespChan chan error
	Object   any
	PlayerID string
	Type     EventType
	Key      rune
}
type ServerEvent struct {
	RespChan chan error
	Object   any
	PlayerID string
	Value    string
	Type     EventType
}

func (game *Game) ProcessInputs() {
EventLoop:
	for {
		select {
		case event, ok := <-game.EventChan:
			if !ok {
				return
			}
			switch event.Type {
			case EventTypeConnect:
				log.Printf("[DEBUG] Conn %s ", event.PlayerID)
				err := game.HandlePlayerConnect(event)
				event.RespChan <- err
			case EventTypeDisconnect:
				log.Printf("[DEBUG] Disco %s ", event.PlayerID)
				game.HandlePlayerDisconnect(event.PlayerID)
			case EventTypePlayerQuit:
				game.HandlePlayerQuit(event.PlayerID)
			case EventTypeKey:
				if receiver, ok := game.GetActivePlayerById(event.PlayerID); ok {
					receiver.EnqueueKey(event.Key)
					receiver.AFKMinutes = 0
				}
			case EventTypeGetNextLevel:
				game.HandleNextLevel()
			}
		default:
			break EventLoop
		}
	}
}
