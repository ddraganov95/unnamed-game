package game

type Wall struct {
	Entity
	Symbol rune
}

func (w *Wall) GetSymbol() rune {
	return w.Symbol
}

func CreateWall(id string, pos Position) GameObject {
	return &Wall{
		ID:       id,
		Position: pos,
		Blocker:  true,
		Team:     TeamEnvironment,
		Symbol:   SymbolWallDefault,
	}
}
