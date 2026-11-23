package game

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"time"
	"uuid"
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
	NextLevelXP   int
}
type PlayerSessionSummary struct {
	SessionStart    time.Time     `json:"session_start"`
	SessionDuration time.Duration `json:"session_duration"`
	GameID          string        `json:"game_id"`
	PlayerID        string        `json:"player_id"`
	KilledBy        string        `json:"killed_by"`
	LevelsCompleted int           `json:"levels_completed"`
	EnemiesKilled   int           `json:"enemies_killed"`
	XPGained        int           `json:"xp_gained"`
	DamageDealt     int           `json:"damage_dealt"`
	DamageTaken     int           `json:"damage_taken"`
	PlayerLevel     int           `json:"player_level"`
	Deaths          int           `json:"player_deaths"`
	Score           int           `json:"score"`
}

type GameSummary struct {
	GameID           uuid.UUID `json:"game_id"`
	TotalLevels      int       `json:"total_levels"`
	PlayersConnected int       `json:"players_connected"`
	LevelsCompleted  int       `json:"levels_completed"`
	EnemiesSpawned   int       `json:"enemies_spawned"`
	EnemiesKilled    int       `json:"enemies_killed"`
	PlayerDeaths     int       `json:"player_deaths"`
	EmptyMinutes     int       `json:"empty_minutes"`
}

var XpRequirements []int

func InitXpTable() {
	XpRequirements = make([]int, MaxPlayerLevel+1)
	for lvl := 1; lvl <= MaxPlayerLevel; lvl++ {
		XpRequirements[lvl] = int(math.Round(
			PlayerLevelOneExperience * math.Pow(PlayerLevelXpRequirementMultiplier, float64(lvl-1)),
		))
	}
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
// startPlayTime marks the player as connected from now (no-op if already).
func (player *Player) startPlayTime() {
	if player.playStart.IsZero() {
		player.playStart = time.Now()
	}
}

// stopPlayTime closes the current connected stretch.
func (player *Player) stopPlayTime() {
	if !player.playStart.IsZero() {
		player.playedTime += time.Since(player.playStart)
		player.playStart = time.Time{}
	}
}

// PlayTime is the total time the player has been connected in this game.
func (player *Player) PlayTime() time.Duration {
	d := player.playedTime
	if !player.playStart.IsZero() {
		d += time.Since(player.playStart)
	}
	return d
}

// TakeUnsavedSummary returns what has accumulated since the previous call
// and marks it as saved. user_stats adds these values to its totals, so
// saving the full summary on every disconnect would count earlier play again
// (quit, rejoin the same game, quit). Counters and play time are deltas;
// PlayerLevel and Score are absolute because the DB keeps their maximum.
// Caller must hold game.Mu.
func (player *Player) TakeUnsavedSummary() PlayerSessionSummary {
	cur := player.GenerateSummary()
	cur.SessionDuration = player.PlayTime()
	prev := player.saved
	player.saved = cur

	delta := cur
	delta.SessionDuration = cur.SessionDuration - prev.SessionDuration
	delta.LevelsCompleted = cur.LevelsCompleted - prev.LevelsCompleted
	delta.EnemiesKilled = cur.EnemiesKilled - prev.EnemiesKilled
	delta.XPGained = cur.XPGained - prev.XPGained
	delta.DamageDealt = cur.DamageDealt - prev.DamageDealt
	delta.DamageTaken = cur.DamageTaken - prev.DamageTaken
	// Deaths is 0/1 for "dead right now"; count a death only once.
	delta.Deaths = max(0, cur.Deaths-prev.Deaths)
	return delta
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
func (game *Game) GenerateSummary() GameSummary {
	return GameSummary{
		GameID:           game.GameID,
		TotalLevels:      game.TotalLevels,
		PlayersConnected: len(game.Players),
		EmptyMinutes:     game.EmptyMinutes,
		LevelsCompleted:  game.LevelsCompleted,
		EnemiesSpawned:   game.EnemiesSpawned,
		EnemiesKilled:    game.EnemiesKilled,
		PlayerDeaths:     game.PlayerDeaths,
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
		fmt.Sprintf("|               Press [%1s] to exit game                   |", quitKey),
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
