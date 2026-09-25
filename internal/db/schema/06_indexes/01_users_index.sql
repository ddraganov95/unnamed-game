CREATE INDEX IF NOT EXISTS idx_users_player_id ON users(player_id);
CREATE INDEX IF NOT EXISTS idx_users_leaderboard ON user_stats(highest_score DESC, highest_score_achieved_at ASC);