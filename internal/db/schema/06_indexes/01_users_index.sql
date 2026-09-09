CREATE INDEX IF NOT EXISTS idx_users_player_id ON users(player_id);
CREATE INDEX IF NOT EXISTS idx_users_score ON users(highest_score DESC);
CREATE INDEX IF NOT EXISTS idx_users_leaderboard ON users(highest_score DESC, created_at ASC)