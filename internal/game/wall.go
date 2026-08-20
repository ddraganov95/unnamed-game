package game

type TopWall struct {
	Entity
}
type SideWall struct {
	Entity
}

func (wall *TopWall) GetSymbol() rune {
	return SymbolTopWall
}
func (wall *SideWall) GetSymbol() rune {
	return SymbolSideWall
}
func CreateTopWall(id string, pos Position) GameObject {

	return &TopWall{
		Entity: Entity{
			ID:       id,
			Position: pos,
		},
	}
}
func CreateSideWall(id string, pos Position) GameObject {
	return &SideWall{
		Entity: Entity{
			ID:       id,
			Position: pos,
		},
	}
}

func (wall *TopWall) IsBlocking() bool {
	return true
}
func (wall *SideWall) IsBlocking() bool {
	return true
}
func (wall *TopWall) IsEnemy() bool {
	return false
}
func (wall *SideWall) IsEnemy() bool {
	return false
}
