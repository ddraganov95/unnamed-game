package db

import (
	"context"
	"encoding/json"
	"fmt"
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
	UserID    uuid.UUID `db:"user_id" json:"user_id"`
	PlayerID  string    `db:"player_id" json:"player_id"`
	LastLogin time.Time `db:"last_login" json:"last_login"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

type UserStats struct {
	UserID                 uuid.UUID `db:"user_id" json:"user_id"`
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
	HighestScoreAchievedAt time.Time `db:"highest_score_achieved_at" json:"highest_score_achieved_at"`
}

func (db *Database) UpsertUser(ctx context.Context, playerID string) (*UserRef, error) {
	const query = `
	INSERT INTO users(player_id)
	VALUES ($1)
	ON CONFLICT (player_id) DO UPDATE
	SET last_login = CURRENT_TIMESTAMP
	RETURNING user_id, player_id;
	`

	var ref UserRef
	err := db.pool.QueryRow(ctx, query, playerID).Scan(&ref.UserID, &ref.PlayerID)
	if err != nil {
		return nil, err
	}
	return &ref, nil
}
func (db *Database) UpdatePlayerAfterDisconnect(
	ctx context.Context,
	userID uuid.UUID,
	summary game.PlayerSessionSummary,
	dtoList []game.AchProgressDTO,
) (*UserStats, error) {
	gameTimeSeconds := int64(summary.SessionDuration.Seconds())

	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	batch := &pgx.Batch{}

	for _, dto := range dtoList {
		if len(dto.Progress) == 0 {
			batch.Queue(`
				DELETE FROM player_achievements_progress
				WHERE user_id = $1 AND achievement_id = $2
			`, userID, dto.AchievementID)
			continue
		}

		progressJSON, err := json.Marshal(dto.Progress)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal achievement progress: %w", err)
		}

		batch.Queue(`
			INSERT INTO player_achievements_progress
				(user_id, achievement_id, current_progress, updated_at)
			VALUES ($1, $2, $3, NOW())
			ON CONFLICT (user_id, achievement_id)
			DO UPDATE SET
				current_progress = EXCLUDED.current_progress,
				updated_at = NOW()
		`, userID, dto.AchievementID, progressJSON)
	}

	batch.Queue(`
		UPDATE user_stats
		SET total_xp_gained          = total_xp_gained + $2,
			total_enemies_killed     = total_enemies_killed + $3,
			total_damage_dealt       = total_damage_dealt + $4,
			total_damage_taken       = total_damage_taken + $5,
			total_levels_completed   = total_levels_completed + $6,
			total_game_time          = total_game_time + $7,
			total_deaths             = total_deaths + $8,
			highest_player_level     = GREATEST(highest_player_level, $9),
			highest_score            = GREATEST(highest_score, $10),
			highest_score_achieved_at =
				CASE WHEN $10 > highest_score THEN NOW()
					 ELSE highest_score_achieved_at END
		WHERE user_id = $1
		RETURNING
			user_id,
			total_xp_gained,
			total_damage_dealt,
			total_damage_taken,
			total_game_time,
			total_levels_completed,
			total_deaths,
			highest_player_level,
			total_enemies_killed,
			total_achievement_points,
			highest_score,
			highest_score_achieved_at
	`, userID, summary.XPGained, summary.EnemiesKilled, summary.DamageDealt,
		summary.DamageTaken, summary.LevelsCompleted, gameTimeSeconds,
		summary.Deaths, summary.PlayerLevel, summary.Score)

	br := tx.SendBatch(ctx, batch)
	defer br.Close()

	for range dtoList {
		if _, err := br.Exec(); err != nil {
			return nil, fmt.Errorf("failed to persist achievement progress: %w", err)
		}
	}

	var stats UserStats
	if err := br.QueryRow().Scan(
		&stats.UserID,
		&stats.TotalXPGained,
		&stats.TotalDamageDealt,
		&stats.TotalDamageTaken,
		&stats.TotalGameTime,
		&stats.TotalLevelsCompleted,
		&stats.TotalDeaths,
		&stats.HighestPlayerLevel,
		&stats.TotalEnemiesKilled,
		&stats.TotalAchievementPoints,
		&stats.HighestScore,
		&stats.HighestScoreAchievedAt,
	); err != nil {
		return nil, fmt.Errorf("failed to scan updated user stats: %w", err)
	}

	if err := br.Close(); err != nil {
		return nil, fmt.Errorf("failed to close batch: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit disconnect update: %w", err)
	}

	return &stats, nil
}

type UserProfileSummary struct {
	UserID                 uuid.UUID `db:"user_id" json:"user_id"`
	PlayerID               string    `db:"player_id" json:"player_id"`
	TotalEnemiesKilled     int       `db:"total_enemies_killed" json:"total_enemies_killed"`
	TotalXPGained          int64     `db:"total_xp_gained" json:"total_xp_gained"`
	TotalDamageDealt       int64     `db:"total_damage_dealt" json:"total_damage_dealt"`
	TotalDamageTaken       int64     `db:"total_damage_taken" json:"total_damage_taken"`
	TotalLevelsCompleted   int       `db:"total_levels_completed" json:"total_levels_completed"`
	TotalGameTime          int64     `db:"total_game_time" json:"total_game_time"`
	TotalDeaths            int       `db:"total_deaths" json:"total_deaths"`
	HighestPlayerLevel     int       `db:"highest_player_level" json:"highest_player_level"`
	TotalAchievementPoints int       `db:"total_achievement_points" json:"total_achievement_points"`
	HighestScore           int       `db:"highest_score" json:"highest_score"`
}

func (db *Database) FetchUserSummary(ctx context.Context, userID string) (*UserProfileSummary, error) {
	const query = `
	Select 
	u.user_id,
	u.player_id,
	us.total_enemies_killed,
	us.total_xp_gained,
	us.total_damage_dealt,
	us.total_damage_taken,
	us.total_levels_completed,
	us.total_game_time,
	us.total_deaths,
	us.highest_player_level,
	us.total_achievement_points,
	us.highest_score
	FROM user_stats us JOIN users u ON u.user_id = us.user_id
	WHERE us.user_id = $1;
	`

	rows, err := db.pool.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to scan updated user: %w", err)
	}
	userProfile, err := pgx.CollectOneRow(rows, pgx.RowToStructByNameLax[UserProfileSummary])
	if err != nil {
		return nil, fmt.Errorf("failed to scan updated user: %w", err)
	}

	return &userProfile, nil
}
