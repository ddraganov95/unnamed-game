package db

import (
	"context"
	"encoding/json"
	"fmt"
	"uuid"

	"unnamed-game/internal/game"
)

func (db *Database) FetchAchievementCatalog(ctx context.Context) ([]game.AchCatalogDTO, error) {
	rows, err := db.Pool.Query(ctx, GetAchievementCatalog)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch achievements: %w", err)
	}
	defer rows.Close()

	var catalog []game.AchCatalogDTO

	for rows.Next() {
		var (
			def      game.AchCatalogDTO
			reqsJSON []byte
		)

		if err := rows.Scan(
			&def.ID,
			&def.Code,
			&def.Title,
			&def.Description,
			&reqsJSON,
			&def.IsGlobalAnnouncement,
		); err != nil {
			return nil, err
		}

		if len(reqsJSON) > 0 {
			if err := json.Unmarshal(reqsJSON, &def.Requirements); err != nil {
				return nil, fmt.Errorf("failed to unmarshal requirements for %s: %w", def.Code, err)
			}
		}

		catalog = append(catalog, def)
	}

	return catalog, rows.Err()
}
func (db *Database) FetchPlayerProgress(ctx context.Context, userID uuid.UUID) ([]game.AchProgressDTO, error) {
	rows, err := db.Pool.Query(ctx, GetPlayerFullAchievementState, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch player achievement state: %w", err)
	}
	defer rows.Close()

	var dtoList []game.AchProgressDTO

	for rows.Next() {
		var (
			dto              game.AchProgressDTO
			requirementsJSON []byte
			progressJSON     []byte
		)

		if err := rows.Scan(&dto.AchievementID, &dto.Code, &requirementsJSON, &dto.IsUnlocked, &progressJSON); err != nil {
			return nil, err
		}

		dto.Progress = make(map[string]int64)
		if len(progressJSON) > 0 {
			_ = json.Unmarshal(progressJSON, &dto.Progress)
		}

		dto.MaxProgress = make(map[string]int64)
		if len(requirementsJSON) > 0 {
			_ = json.Unmarshal(requirementsJSON, &dto.MaxProgress)
		}

		dtoList = append(dtoList, dto)
	}

	return dtoList, rows.Err()
}
