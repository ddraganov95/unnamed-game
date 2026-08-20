package main

import (
	"fmt"
	"unnamed-game/internal/game"
)

func main() {
	fmt.Println("Hello To Unnamed RPG Game")
	globalChat := make(chan string, game.MaxChatHistory)
	defer close(globalChat)
	game.NewGame(globalChat)
}
