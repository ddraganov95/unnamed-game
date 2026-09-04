package db

import (
	"context"
	"fmt"
	"log"
	"time"
	"uuid"

	"unnamed-game/internal/game"

	"github.com/jackc/pgx/v5"
)

type UserRef struct {
	UserID   uuid.UUID
	PlayerID string
}

type User struct {
	UserID               uuid.UUID `db:"user_id" json:"user_id"`
	PlayerID             string    `db:"player_id" json:"player_id"`
	LastLogin            time.Time `db:"last_login" json:"last_login"`
	CreatedAt            time.Time `db:"created_at" json:"created_at"`
	TotalXPGained        int64     `db:"total_xp_gained" json:"total_xp_gained"`
	TotalDamageDealt     int64     `db:"total_damage_dealt" json:"total_damage_dealt"`
	TotalDamageTaken     int64     `db:"total_damage_taken" json:"total_damage_taken"`
	TotalLevelsCompleted int       `db:"total_levels_completed" json:"total_levels_completed"`
	TotalGameTime        int64     `db:"total_game_time" json:"total_game_time"`
	TotalDeaths          int       `db:"total_deaths" json:"total_deaths"`
	HighestPlayerLevel   int       `db:"highest_player_level" json:"highest_player_level"`
	TotalEnemiesKilled   int       `db:"total_enemies_killed" json:"total_enemies_killed"`
}

func (db *Database) GetOrCreateUser(ctx context.Context, playerID string) (*UserRef, error) {
	var ref UserRef
	err := db.Pool.QueryRow(ctx, upsertUserQuery, playerID).Scan(&ref.UserID, &ref.PlayerID)
	if err != nil {
		return nil, err
	}
	log.Printf("Returned user_id: %s, player_id: %s", ref.UserID, ref.PlayerID)
	return &ref, nil
}
func (db *Database) UpdateUserWithSummary(ctx context.Context, summary game.PlayerSessionSummary) (*User, error) {

	log.Printf("[DB DEATHS]: %d", summary.Deaths)
	gameTimeSeconds := int64(summary.SessionDuration.Seconds())

	rows, err := db.Pool.Query(ctx, UptateUserWithSummary,
		summary.PlayerID,
		summary.XPGained,
		summary.EnemiesKilled,
		summary.DamageDealt,
		summary.DamageTaken,
		summary.LevelsCompleted,
		gameTimeSeconds,
		summary.Deaths,
		summary.PlayerLevel,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to execute session summary update: %w", err)
	}

	user, err := pgx.CollectOneRow(rows, pgx.RowToStructByNameLax[User])
	if err != nil {
		log.Printf("[DB SCAN ERROR DETAILS]: %v", err) // Print full error description
		return nil, fmt.Errorf("failed to scan updated user: %w", err)
	}

	return &user, nil
}
func (db *Database) GetUserSummary(ctx context.Context, playerID string) (*User, error) {
	rows, err := db.Pool.Query(ctx, GetUserSummary, playerID)
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
