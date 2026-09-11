package game

type Team uint8

const (
	TeamEnvironment Team = iota
	TeamPlayer
	TeamEnemy
)

type Entity struct {
	ID       string
	Position Position
	Team     Team
	Blocker  bool
}
type Position struct {
	X, Y int
}

func (entity *Entity) GetID() string {
	return entity.ID
}
func (entity *Entity) GetPosition() Position {
	return entity.Position
}
func (entity *Entity) SetPosition(pos Position) {
	entity.Position = pos
}
func (entity *Entity) GetTeam() Team {
	return entity.Team
}
func (entity *Entity) IsBlocking() bool {
	return entity.Blocker
}
func CreateEntity(id string, pos Position, team Team) Entity {
	return Entity{
		ID:       id,
		Position: pos,
		Blocker:  false,
		Team:     team,
	}
}
func (level *Level) AddEntity(object GameObject) {
	level.Entities[object.GetID()] = object
	level.posEntities[object.GetPosition()] = append(level.posEntities[object.GetPosition()], object)
}
func (level *Level) RemoveEntity(object GameObject) {
	delete(level.Entities, object.GetID())

	slice := level.posEntities[object.GetPosition()]
	for i, entity := range slice {
		if entity.GetID() == object.GetID() {
			level.posEntities[object.GetPosition()] = append(slice[:i], slice[i+1:]...)
			break
		}
	}
	// Clean up empty slices from the map to save memory
	if len(level.posEntities[object.GetPosition()]) == 0 {
		delete(level.posEntities, object.GetPosition())
	}
}
func (level *Level) MoveEntity(object GameObject, newPos Position) {
	//Remove from old position slice
	oldSlice := level.posEntities[object.GetPosition()]
	for i, e := range oldSlice {
		if e.GetID() == object.GetID() {
			level.posEntities[object.GetPosition()] = append(oldSlice[:i], oldSlice[i+1:]...)
			break
		}
	}
	if len(level.posEntities[object.GetPosition()]) == 0 {
		delete(level.posEntities, object.GetPosition())
	}

	//Update position and add to new position slice
	object.SetPosition(newPos)
	level.posEntities[newPos] = append(level.posEntities[newPos], object)
}
