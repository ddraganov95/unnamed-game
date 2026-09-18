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
	rows, err := db.pool.Query(ctx, GetUserLeaderboardPage, userID, pageSize)
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
	rows, err := db.pool.Query(ctx, GetLeaderboardPage, pageSize, (pageNum-1)*pageSize)
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
