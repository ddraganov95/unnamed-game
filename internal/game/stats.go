package game

type Health struct {
	CurrentHealth int
	MaxHealth     int
}
type Speed struct {
	CurrentMovementSpeed int
	MaxMovementSpeed     int
	CurrentAttackSpeed   int
	MaxAttackSpeed       int
}
type Experience struct {
	Level         int
	ExperienceVal int
}

func (h *Health) TakeDamage(amount int) {
	h.CurrentHealth -= amount
	if h.CurrentHealth < 0 {
		h.CurrentHealth = 0
	}
}
func (h *Health) HealToFull() {
	h.CurrentHealth = h.MaxHealth
}
func CreateHealth(maxHealth int) Health {
	return Health{
		CurrentHealth: maxHealth,
		MaxHealth:     maxHealth,
	}
}
