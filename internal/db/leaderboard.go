package db

import (
	"context"
	"fmt"
	"log"
	"uuid"

	"github.com/jackc/pgx/v5"
)

type UserLeaderboardView struct {
	UserID       uuid.UUID `db:"user_id"`
	PlayerID     string    `db:"player_id"`
	HighestScore int       `db:"highest_score"`
	Rank         int       `db:"rank"`
}

func (db *Database) FetchUserLeaderboard(ctx context.Context, userID uuid.UUID, pageSize int) ([]UserLeaderboardView, error) {
	const query = `
WITH ranked_users AS (
    SELECT 
        us.user_id,
        u.player_id, 
        us.highest_score, 
        us.highest_score_achieved_at,
        ROW_NUMBER() OVER (ORDER BY us.highest_score DESC, us.highest_score_achieved_at ASC)::INT AS rank
    FROM user_stats us join users u on u.user_id = us.user_id
),
target_user AS (
    SELECT rank FROM ranked_users WHERE user_id = $1
)
SELECT 
    r.user_id,
    r.player_id,
    r.highest_score,
    r.rank
FROM ranked_users r
WHERE r.rank >= (((SELECT rank FROM target_user) - 1) / $2) * $2 + 1
ORDER BY r.rank ASC
LIMIT $2 + 1;
`

	rows, err := db.pool.Query(ctx, query, userID, pageSize)
	if err != nil {
		log.Printf("[DB ERROR]: GET User Leaderboard query fail %v", err)
		return nil, fmt.Errorf("failed to query user leaderboard page: %w", err)
	}
	defer rows.Close()

	users, err := pgx.CollectRows(rows, pgx.RowToStructByNameLax[UserLeaderboardView])
	if err != nil {
		log.Printf("[DB ERROR]: %v", err)
		return nil, fmt.Errorf("failed to scan leaderboard rows: %w", err)
	}
	return users, nil
}
func (db *Database) FetchLeaderboardPage(ctx context.Context, pageNum int, pageSize int) ([]UserLeaderboardView, error) {
	const query = `
SELECT 
    	us.user_id,
        u.player_id, 
        us.highest_score, 
        ROW_NUMBER() OVER (ORDER BY us.highest_score DESC, us.highest_score_achieved_at ASC)::INT AS rank
 FROM user_stats us join users u on u.user_id = us.user_id
ORDER BY rank ASC
LIMIT $1 OFFSET $2
;
`

	offset := (pageNum - 1) * pageSize
	fetchLimit := pageSize + 1
	rows, err := db.pool.Query(ctx, query, fetchLimit, offset)
	if err != nil {
		log.Printf("[DB ERROR]: GET User Leaderboard query fail %v", err)
		return nil, fmt.Errorf("failed to query user leaderboard page: %w", err)
	}
	defer rows.Close()

	users, err := pgx.CollectRows(rows, pgx.RowToStructByNameLax[UserLeaderboardView])
	if err != nil {
		log.Printf("[DB ERROR]: %v", err)
		return nil, fmt.Errorf("failed to scan leaderboard rows: %w", err)
	}
	return users, nil
}
