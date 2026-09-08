package game

import (
	"fmt"
	"log"
	"strings"
)

type EventType int

const (
	EventTypeKey EventType = iota
	EventTypeConnect
	EventTypeDisconnect
	EventTypeMassDisconnect
	EventTypeIdleCheck
	EventTypeCopyGame
	EventTypeGlobalChatMsg
	EventTypeGameChatMsg
	EventTypeAchievementUnlocked
)

type GameEvent struct {
	Type     EventType
	PlayerID string
	Key      rune
	RespChan chan error
}
type ServerEvent struct {
	Type     EventType
	PlayerID string
	Value    string
	RespChan chan error
	Object   any
}

func (player *Player) EnqueueKey(r rune) {
	player.KeyQueue = append(player.KeyQueue, r)
}
func (player *Player) InitKeybindings() {
	// Potentially this should be the keybinds from the player config. However, for now, we will hardcode them.
	player.KeyBindings = map[rune]func(g *Game){
		'w':  player.MoveUp,
		's':  player.MoveDown,
		'a':  player.MoveLeft,
		'd':  player.MoveRight,
		'f':  player.Attack,
		'q':  player.QuitGame,
		'e':  player.ChangeEquippedAttack,
		'\r': player.ChangeTypeState,
		' ':  player.GetNextLevel,
		'c':  player.CopyGameId,
	}
}
func (player *Player) InitTypingKeybindings() {
	// Potentially this should be the keybinds from the player config. However, for now, we will hardcode them.
	player.TypingKeyBindings = map[rune]func(g *Game){
		'\r': player.ChangeTypeState,
		'\b': player.RemoveLastByteMessage,
		127:  player.RemoveLastByteMessage,
		' ':  player.ChangePlayerTypingChannel,
	}
}
func (player *Player) QuitGame(game *Game) {
	game.AchievementEngine.PublishEvent(AchievementEvent{
		PlayerIDs: []string{player.GetID()},
		Key:       "GAME:default",
		Amount:    0,
	})
	game.EventChan <- GameEvent{
		Type:     EventTypeDisconnect,
		PlayerID: player.GetID(),
	}
	fmt.Print("Quitting Game...\r\n")
}
func (player *Player) GetNextLevel(game *Game) {
	log.Printf("[DEBUG] %s ,Pressed Space!", player.GetID())
	if !player.IsAlive() {
		return
	}
	if game.State == StateGameIntermission {
		NewLevel(game)
		game.EmptyMinutes = 0
		game.State = StateGamePlaying
	}
}
func (player *Player) CopyGameId(game *Game) {
	game.ServerEventChan <- ServerEvent{
		Type:     EventTypeCopyGame,
		PlayerID: player.GetID(),
		Value:    player.GameID,
	}
	fmt.Print("Game ID copied!\r\n")
}
func (player *Player) ChangeTypeState(game *Game) {
	if player.PlayerState == StateTyping {
		player.SendMessage(game)
		player.PlayerState = StatePlaying
		return
	}
	player.PlayerState = StateTyping
}
func (player *Player) ChangePlayerTypingChannel(game *Game) {
	if strings.HasPrefix(player.MessageBuffer, "/1") {
		player.PlayerTypingChannel = StateTypingGameChat
		player.MessageBuffer = strings.TrimPrefix(player.MessageBuffer, "/1")
		return
	}
	if strings.HasPrefix(player.MessageBuffer, "/2") {
		player.PlayerTypingChannel = StateTypingGlobalChat
		player.MessageBuffer = strings.TrimPrefix(player.MessageBuffer, "/2")
		return
	}
	player.AddToMessageBuffer(' ')
}
func (player *Player) GetCursor() string {
	switch player.PlayerTypingChannel {
	case StateTypingGameChat:
		return ChatCursorGameChat
	case StateTypingGlobalChat:
		return ChatCursorGlobalChat
	default:
		return ChatCursorDefault
	}
}
func (player *Player) RemoveLastByteMessage(game *Game) {
	if len(player.MessageBuffer) > 0 {
		player.MessageBuffer = player.MessageBuffer[:len(player.MessageBuffer)-1]
	}
}
func (player *Player) SendMessage(game *Game) {
	if len(player.MessageBuffer) > 0 {
		if player.PlayerTypingChannel == StateTypingGameChat {
			game.ServerEventChan <- ServerEvent{
				Type:     EventTypeGameChatMsg,
				PlayerID: player.GetID(),
				Value:    player.MessageBuffer}
		}
		if player.PlayerTypingChannel == StateTypingGlobalChat {
			game.ServerEventChan <- ServerEvent{
				Type:     EventTypeGlobalChatMsg,
				PlayerID: player.GetID(),
				Value:    player.MessageBuffer}
		}
		player.MessageBuffer = ""
	}
}
func (player *Player) AddToMessageBuffer(key rune) {
	maxAllowedLength := MaxChatMessageLength - 5
	if len(player.MessageBuffer) < maxAllowedLength {
		player.MessageBuffer += string(key)
	}
}
func (game *Game) BroadcastGameChat(playerid string, message string) {
	msg := fmt.Sprintf("[%s]: %s", playerid, message)
	log.Println(msg)
	select {
	case game.GameChat <- msg:
	default:
	}
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
				err := game.HandlePlayerConnect(event.PlayerID)
				event.RespChan <- err
			case EventTypeDisconnect:
				log.Printf("[DEBUG] Disco %s ", event.PlayerID)
				game.HandlePlayerDisconnect(event.PlayerID)
			case EventTypeKey:
				//log.Printf("[DEBUG] Reading Input %s %v", event.PlayerID, event.Key)
				if receiver, ok := game.GetActivePlayerById(event.PlayerID); ok {
					receiver.EnqueueKey(event.Key)
					receiver.AFKMinutes = 0
				}
			}
		default:
			break EventLoop
		}
	}
}
