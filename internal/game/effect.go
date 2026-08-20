package game

type HitEffect struct {
	Entity
	timer int
}

func CreateHitEffect(id string, pos Position, timer int) *HitEffect {
	return &HitEffect{
		Entity: CreateEntity(id, pos),
		timer:  timer,
	}
}
func (effect *HitEffect) GetSymbol() rune {
	return SymbolHitEffect
}
func (effect *HitEffect) Update(game *Game) {
	effect.timer--
	if effect.IsExpired() {
		game.Level.RemoveEffect(effect)
	}
}
func (effect *HitEffect) IsExpired() bool {
	return effect.timer <= 0
}
func (level *Level) AddEffect(effect Effect) {
	level.Effects[effect.GetID()] = effect
}
func (level *Level) RemoveEffect(effect Effect) {
	delete(level.Effects, effect.GetID())
}
