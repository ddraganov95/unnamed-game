package game

import (
	"fmt"
	"time"
)

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
type PlayerSessionSummary struct {
	PlayerID        string        `json:"player_id"`
	SessionStart    time.Time     `json:"session_start"`
	SessionDuration time.Duration `json:"session_duration"`
	LevelsCompleted int           `json:"levels_completed"`
	EnemiesKilled   int           `json:"enemies_killed"`
	XPGained        int           `json:"xp_gained"`
	DamageDealt     int           `json:"damage_dealt"`
	DamageTaken     int           `json:"damage_taken"`
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
func (player *Player) GenerateSummary() PlayerSessionSummary {
	return PlayerSessionSummary{
		PlayerID:        player.ID,
		SessionStart:    player.SessionStart,
		SessionDuration: time.Since(player.SessionStart),
		LevelsCompleted: player.LevelsCompleted,
		EnemiesKilled:   player.EnemiesKilled,
		XPGained:        player.XPGained,
		DamageDealt:     player.DamageDealt,
		DamageTaken:     player.DamageTaken,
	}
}
func (summary PlayerSessionSummary) Print() {
	fmt.Println("\n==============================")
	fmt.Printf("   GAME OVER / SESSION SUMMARY\n")
	fmt.Println("==============================")
	fmt.Printf(" Player ID:        %s\n", summary.PlayerID)
	fmt.Printf(" Playtime:         %v\n", summary.SessionDuration.Round(time.Second))
	fmt.Printf(" XP Gained:        %d\n", summary.XPGained)
	fmt.Printf(" Enemies Killed:   %d\n", summary.EnemiesKilled)
	fmt.Printf(" Levels Completed: %d\n", summary.LevelsCompleted)
	fmt.Printf(" Damage Dealt:     %d\n", summary.DamageDealt)
	fmt.Printf(" Damage Taken:     %d\n", summary.DamageTaken)
	fmt.Printf("==============================\n")
}
