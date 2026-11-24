package game

import (
	"strings"
	"testing"
)

// A message longer than MaxMessageLength must be stored truncated, so the
// text saved to game_events matches what players see on the panel.
func TestCreateLogStoresTruncatedText(t *testing.T) {
	g := &Game{}
	long := strings.Repeat("x", MaxMessageLength+25)

	g.CreateLog("%s", long)

	if len(g.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(g.Events))
	}
	ev := g.Events[0]
	if got := len([]rune(ev.Text)); got != MaxMessageLength {
		t.Errorf("Text length = %d runes, want %d", got, MaxMessageLength)
	}
	if got := len(ev.Runes); got != MaxMessageLength {
		t.Errorf("Runes length = %d, want %d", got, MaxMessageLength)
	}
	if ev.Text != strings.Repeat("x", MaxMessageLength) {
		t.Errorf("Text = %q, want the first %d characters", ev.Text, MaxMessageLength)
	}
}

// Short messages are stored unchanged.
func TestCreateLogKeepsShortText(t *testing.T) {
	g := &Game{}
	g.CreateLog("hello %d", 42)

	if g.Events[0].Text != "hello 42" {
		t.Errorf("Text = %q, want %q", g.Events[0].Text, "hello 42")
	}
	if string(g.Events[0].Runes) != "hello 42" {
		t.Errorf("Runes = %q, want %q", string(g.Events[0].Runes), "hello 42")
	}
}
