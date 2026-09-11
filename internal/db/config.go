package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"uuid"

	"github.com/jackc/pgx/v5"
)

type UserConfiguration struct {
	Keybinds  map[string]string `json:"keybinds"`
	MasterVol int               `json:"master_volume"`
	SfxVol    int               `json:"sfx_volume"`
}

var DefaultUserConfiguration = UserConfiguration{
	Keybinds: map[string]string{
		"move_up":      "w",
		"move_down":    "s",
		"move_left":    "a",
		"move_right":   "d",
		"attack_enemy": "f",
		"swap_attack":  "e",
		"quit_game":    "q",
	},
	MasterVol: 100,
	SfxVol:    70,
}
var ActionLabels = map[string]string{
	"move_up":      "Move Up",
	"move_down":    "Move Down",
	"move_left":    "Move Left",
	"move_right":   "Move Right",
	"attack_enemy": "Attack Enemy",
	"swap_attack":  "Swap Attack",
	"quit_game":    "Quit Game",
}

func GetActionLabel(action string) string {
	if label, ok := ActionLabels[action]; ok {
		return label
	}
	return action // Fallback if an unknown key slips through
}

func (db *Database) FetchUserConfiguration(ctx context.Context, userID uuid.UUID) (UserConfiguration, error) {
	var rawBytes []byte

	err := db.Pool.QueryRow(ctx, GetUserConfiguration, userID).Scan(&rawBytes)

	if errors.Is(err, pgx.ErrNoRows) {
		return DefaultUserConfiguration, nil
	} else if err != nil {
		return UserConfiguration{}, fmt.Errorf("failed to fetch user config: %w", err)
	}

	var config UserConfiguration
	if err := json.Unmarshal(rawBytes, &config); err != nil {
		return UserConfiguration{}, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return config, nil
}
func (db *Database) UpsertUserConfiguration(ctx context.Context, userID uuid.UUID, config UserConfiguration) error {
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal user config: %w", err)
	}

	_, err = db.Pool.Exec(ctx, UpsertUserConfig, userID, configBytes)
	if err != nil {
		return fmt.Errorf("failed to upsert user config: %w", err)
	}
	return nil
}
