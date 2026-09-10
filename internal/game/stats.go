package game

import (
	"fmt"
	"reflect"
	"strings"
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
	GameID          string        `json:"game_id"`
	PlayerID        string        `json:"player_id"`
	KilledBy        string        `json:"killed_by"`
	SessionStart    time.Time     `json:"session_start"`
	SessionDuration time.Duration `json:"session_duration"`
	LevelsCompleted int           `json:"levels_completed"`
	EnemiesKilled   int           `json:"enemies_killed"`
	XPGained        int           `json:"xp_gained"`
	DamageDealt     int           `json:"damage_dealt"`
	DamageTaken     int           `json:"damage_taken"`
	PlayerLevel     int           `json:"player_level"`
	Deaths          int           `json:"player_deaths"`
	Score           int           `json:"score"`
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
	deaths := 0
	score := player.Score
	if !player.IsAlive() {
		deaths = 1
		score = 0
	}
	return PlayerSessionSummary{
		GameID:          player.GameID,
		PlayerID:        player.ID,
		SessionStart:    player.SessionStart,
		SessionDuration: time.Since(player.SessionStart),
		LevelsCompleted: player.LevelsCompleted,
		EnemiesKilled:   player.EnemiesKilled,
		XPGained:        player.XPGained,
		DamageDealt:     player.DamageDealt,
		DamageTaken:     player.DamageTaken,
		PlayerLevel:     player.Level,
		Deaths:          deaths,
		Score:           score,
	}
}
func GetSummaryLines(summary PlayerSessionSummary, quitKey string) []string {
	duration := summary.SessionDuration.Truncate(time.Second).String()
	if quitKey == "" {
		quitKey = "Q"
	}

	return []string{
		"+--------------------------------------------------------+",
		"|                     Level Complete                     |",
		"+--------------------------------------------------------+",
		fmt.Sprintf("| Adventurer:     %-38s |", summary.PlayerID),
		fmt.Sprintf("| Game ID:        %-38s |", summary.GameID),
		fmt.Sprintf("| Playtime:       %-38s |", duration),
		"+--------------------------------------------------------+",
		fmt.Sprintf("| Levels Cleared: %-38d |", summary.LevelsCompleted),
		fmt.Sprintf("| Enemies Slain:  %-38d |", summary.EnemiesKilled),
		fmt.Sprintf("| XP Gained:      %-38d |", summary.XPGained),
		"+--------------------------------------------------------+",
		fmt.Sprintf("| Damage Dealt:   %-38d |", summary.DamageDealt),
		fmt.Sprintf("| Damage Taken:   %-38d |", summary.DamageTaken),
		fmt.Sprintf("| Score:          %-38d |", summary.Score),
		"+--------------------------------------------------------+",
		"|               Press [SPACE] to continue                |",
		fmt.Sprintf("|               Press [%1s] to exit game                  |", quitKey),
		"+--------------------------------------------------------+",
	}
}

func GetGameOverSummaryLines(summary PlayerSessionSummary, quitKey string) []string {
	duration := summary.SessionDuration.Truncate(time.Second).String()
	if quitKey == "" {
		quitKey = "Q"
	}

	return []string{
		"+--------------------------------------------------+",
		"|                    GAME OVER                     |",
		"+--------------------------------------------------+",
		fmt.Sprintf("| Adventurer:     %-32s |", summary.PlayerID),
		fmt.Sprintf("| Playtime:       %-32s |", duration),
		"+--------------------------------------------------+",
		fmt.Sprintf("| Levels Cleared: %-32d |", summary.LevelsCompleted),
		fmt.Sprintf("| Enemies Slain:  %-32d |", summary.EnemiesKilled),
		fmt.Sprintf("| XP Gained:      %-32d |", summary.XPGained),
		"+--------------------------------------------------+",
		fmt.Sprintf("| Damage Dealt:   %-32d |", summary.DamageDealt),
		fmt.Sprintf("| Damage Taken:   %-32d |", summary.DamageTaken),
		fmt.Sprintf("| Score:          %-32d |", summary.Score),
		"+--------------------------------------------------+",
		fmt.Sprintf("|             Press [%1s] to exit game...            |", quitKey),
		"+--------------------------------------------------+",
	}
}
func (player *Player) GetQuitKey() string {
	targetPtr := reflect.ValueOf(player.QuitGame).Pointer()

	for r, fn := range player.KeyBindings {
		if reflect.ValueOf(fn).Pointer() == targetPtr {
			return strings.ToUpper(string(r))
		}
	}

	return "Q"
}
