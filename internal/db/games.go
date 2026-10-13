package db

import (
	"context"
	"fmt"
	"time"
	"unnamed-game/internal/game"
	"uuid"

	"github.com/jackc/pgx/v5"
)

type Game struct {
	GameID     uuid.UUID `db:"game_id"`
	CreatedAt  time.Time `db:"created_at"`
	ServerHost string    `db:"server_host"`
	Status     string    `db:"status"`
}

func (db *Database) InsertNewGame(ctx context.Context, gameID uuid.UUID, serverHost string) error {
	_, err := db.pool.Exec(ctx, InsertGame, gameID, serverHost, "active")
	if err != nil {
		return err
	}
	return nil
}
func (db *Database) FetchGameHostByGameId(ctx context.Context, gameID uuid.UUID) (string, error) {
	var serverHost string
	err := db.pool.QueryRow(ctx, FindGameHostById, gameID).Scan(&serverHost)
	if err != nil {
		return "", err
	}
	return serverHost, nil
}
func (db *Database) SaveGameEventsCopy(ctx context.Context, gameID uuid.UUID, events []game.Event) error {
	//Purpose is to keep db.pool unexported
	return db.copyEvents(ctx, db.pool, gameID, events)
}
func (db *Database) copyEvents(ctx context.Context, exec copyExecutor, gameID uuid.UUID, events []game.Event) error {
	if len(events) == 0 {
		return nil
	}

	rows := make([][]any, len(events))
	for i, e := range events {
		rows[i] = []any{
			gameID,
			e.CreatedAt,
			e.Type.String(),
			e.Text,
		}
	}

	_, err := exec.CopyFrom(
		ctx,
		pgx.Identifier{"game_events"},
		[]string{"game_id", "created_at", "type", "text"},
		pgx.CopyFromRows(rows),
	)
	return err
}
func (db *Database) EndGameWithEvents(ctx context.Context, gameID uuid.UUID, finalStatus string, summary game.GameSummary, events []game.Event) error {
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	query := `
        WITH updated_game AS (
            UPDATE games 
            SET termination_reason = $1, 
                ended_at = NOW(), 
                status = 'inactive' 
            WHERE game_id = $2
            RETURNING game_id, created_at,ended_at
        )
        UPDATE games_stats 
        SET total_levels = $3,
            levels_completed = $4,
            players_connected = $5,
            player_deaths = $6,
            enemies_spawned = $7,
            enemies_killed = $8,
            empty_minutes = $9,
            duration_seconds = EXTRACT(EPOCH FROM (updated_game.ended_at - updated_game.created_at))::INT
        FROM updated_game
        WHERE games_stats.game_id = updated_game.game_id;
    `

	_, err = tx.Exec(
		ctx,
		query,
		finalStatus,              // $1
		gameID,                   // $2
		summary.TotalLevels,      // $3
		summary.LevelsCompleted,  // $4
		summary.PlayersConnected, // $5
		summary.PlayerDeaths,     // $6
		summary.EnemiesSpawned,   // $7
		summary.EnemiesKilled,    // $8
		summary.EmptyMinutes,     // $9
	)
	if err != nil {
		return fmt.Errorf("failed to finalize game and stats: %w", err)
	}

	// Update Game Related Events
	if err := db.copyEvents(ctx, tx, gameID, events); err != nil {
		return fmt.Errorf("failed to copy final game events: %w", err)
	}

	return tx.Commit(ctx)
}
