package db

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"
	"uuid"

	"unnamed-game/internal/game"

	"github.com/jackc/pgx/v5"
)

type UserRef struct {
	UserID   uuid.UUID `db:"user_id" json:"user_id"`
	PlayerID string    `db:"player_id" json:"player_id"`
}

type User struct {
	LastLogin              time.Time `db:"last_login" json:"last_login"`
	CreatedAt              time.Time `db:"created_at" json:"created_at"`
	UserID                 uuid.UUID `db:"user_id" json:"user_id"`
	PlayerID               string    `db:"player_id" json:"player_id"`
	TotalXPGained          int64     `db:"total_xp_gained" json:"total_xp_gained"`
	TotalDamageDealt       int64     `db:"total_damage_dealt" json:"total_damage_dealt"`
	TotalDamageTaken       int64     `db:"total_damage_taken" json:"total_damage_taken"`
	TotalGameTime          int64     `db:"total_game_time" json:"total_game_time"`
	TotalLevelsCompleted   int       `db:"total_levels_completed" json:"total_levels_completed"`
	TotalDeaths            int       `db:"total_deaths" json:"total_deaths"`
	HighestPlayerLevel     int       `db:"highest_player_level" json:"highest_player_level"`
	TotalEnemiesKilled     int       `db:"total_enemies_killed" json:"total_enemies_killed"`
	TotalAchievementPoints int       `db:"total_achievement_points" json:"total_achievement_points"`
	HighestScore           int       `db:"highest_score" json:"highest_score"`
	Rank                   int       `db:"rank" json:"rank"`
}

func (db *Database) UpsertUser(ctx context.Context, playerID string) (*UserRef, error) {
	var ref UserRef
	err := db.pool.QueryRow(ctx, upsertUserQuery, playerID).Scan(&ref.UserID, &ref.PlayerID)
	if err != nil {
		return nil, err
	}
	log.Printf("Returned user_id: %s, player_id: %s", ref.UserID, ref.PlayerID)
	return &ref, nil
}
func (db *Database) UpdatePlayerAfterDisconnect(
	ctx context.Context,
	userID uuid.UUID,
	summary game.PlayerSessionSummary,
	dtoList []game.AchProgressDTO,
) (*User, error) {
	gameTimeSeconds := int64(summary.SessionDuration.Seconds())

	var achJSON []byte
	var err error
	if len(dtoList) > 0 {
		achJSON, err = json.Marshal(dtoList)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal achievement DTOs: %w", err)
		}
	} else {
		achJSON = []byte("[]")
	}

	user, err := db.QueryOne[User](ctx, "update_player_after_disconnect",
		userID,
		summary.XPGained,
		summary.EnemiesKilled,
		summary.DamageDealt,
		summary.DamageTaken,
		summary.LevelsCompleted,
		gameTimeSeconds,
		summary.Deaths,
		summary.PlayerLevel,
		summary.Score,
		achJSON,
	)
	if err != nil {
		return nil, err
	}

	return &user, nil
}
func (db *Database) FetchUserSummary(ctx context.Context, userID string) (*User, error) {
	rows, err := db.pool.Query(ctx, GetUserSummary, userID)
	if err != nil {
		log.Printf("[DB ERROR]:GET User Summary query fail %v", err) // Print full error description
		return nil, fmt.Errorf("failed to scan updated user: %w", err)
	}
	user, err := pgx.CollectOneRow(rows, pgx.RowToStructByNameLax[User])
	if err != nil {
		log.Printf("[DB ERROR]: %v", err) // Print full error description
		return nil, fmt.Errorf("failed to scan updated user: %w", err)
	}

	return &user, nil
}
