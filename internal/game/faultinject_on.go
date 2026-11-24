//go:build faultinject

package game

import "strings"

// faultInjectChat panics on the game loop when a player sends "!panic" in game
// chat. Built only with -tags faultinject, so it is never in a normal build.
// The integration panic test uses it to crash a real game mid-session.
func faultInjectChat(msg string) {
	if strings.HasSuffix(msg, "]: !panic") {
		panic("fault injected: !panic from game chat")
	}
}
