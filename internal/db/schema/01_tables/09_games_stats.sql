CREATE TABLE IF NOT EXISTS games_stats (
    game_id            UUID PRIMARY KEY REFERENCES games(game_id) ON DELETE CASCADE,
    total_levels       INT DEFAULT 0,
    levels_completed   INT DEFAULT 0,
    players_connected  INT DEFAULT 0,
    player_deaths      INT DEFAULT 0,
    enemies_spawned    INT DEFAULT 0,
    enemies_killed     INT DEFAULT 0,
    empty_minutes      INT DEFAULT 0,
    duration_seconds   INT DEFAULT 0,
    ended_at           TIMESTAMPTZ DEFAULT NOW(),
    termination_reason VARCHAR(20)
);