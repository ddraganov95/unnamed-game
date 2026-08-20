package game

import "fmt"

type Event struct {
	Text  string
	Runes []rune // Cached version for display.
}

func (game *Game) CreateLog(format string, args ...any) {
	text := fmt.Sprintf(format, args...)
	runes := []rune(text)
	if len(runes) > MaxMessageLength {
		runes = runes[:MaxMessageLength]
	}
	game.Events = append(game.Events, Event{
		Text:  text,
		Runes: []rune(text),
	})
}

func (game *Game) GetEventRuneAt(eventIndex int, messageIndex int) rune {
	totalEvents := len(game.Events)

	// Calculate the start of the sliding window to always show the last events
	startIndex := totalEvents - MaxEventsLength
	if startIndex < 0 {
		startIndex = 0
	}

	// Map the screen row (eventIndex) to the actual index in the slice
	actualIndex := startIndex + eventIndex
	if actualIndex >= totalEvents {
		return SymbolDefault
	}

	eventRunes := game.Events[actualIndex].Runes
	maxMessageLength := MaxMessageLength

	if messageIndex >= len(eventRunes) || messageIndex >= maxMessageLength {
		return SymbolDefault
	}

	return eventRunes[messageIndex]
}
