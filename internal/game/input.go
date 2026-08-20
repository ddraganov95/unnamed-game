package game

import (
	"errors"
	"fmt"
	"os"
)

type PlayerInput struct {
	PlayerID string
	Key      rune
}

func HandlePlayerInputChannel(game *Game, player *Player, inputChan chan PlayerInput) error {
	var buf [1]byte
	for game.Running {
		_, err := os.Stdin.Read(buf[:])
		if err != nil {
			return errors.New("failed to read input")
		}
		// Send the pressed key into the channel
		inputChan <- PlayerInput{PlayerID: player.ID, Key: rune(buf[0])}
	}
	return nil
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
	}
}
func (player *Player) InitTypingKeybindings() {
	// Potentially this should be the keybinds from the player config. However, for now, we will hardcode them.
	player.TypingKeyBindings = map[rune]func(g *Game){
		'\r': player.ChangeTypeState,
		'\b': player.RemoveLastByteMessage,
		127:  player.RemoveLastByteMessage,
	}
}
func (player *Player) QuitGame(game *Game) {
	game.Running = false
	fmt.Print("Quitting Game...\r\n")
}
func (player *Player) ChangeTypeState(game *Game) {
	if player.PlayerState == StateTyping {
		player.SendMessage(game)
		player.PlayerState = StatePlaying
		return
	}
	player.PlayerState = StateTyping
}
func (player *Player) RemoveLastByteMessage(game *Game) {
	if len(player.MessageBuffer) > 0 {
		player.MessageBuffer = player.MessageBuffer[:len(player.MessageBuffer)-1]
	}
}
func (player *Player) SendMessage(game *Game) {
	if len(player.MessageBuffer) > 0 {
		game.GlobalChat <- fmt.Sprintf("%s : %s", player.GetID(), player.MessageBuffer)
		player.MessageBuffer = ""
	}
}
