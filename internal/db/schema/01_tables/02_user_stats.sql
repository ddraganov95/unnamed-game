CREATE TABLE IF NOT EXISTS user_stats (
user_id UUID PRIMARY KEY references users(user_id) ON DELETE CASCADE,
total_xp_gained bigint DEFAULT 0,
total_damage_dealt bigint DEFAULT 0,
total_damage_taken bigint DEFAULT 0,
total_levels_completed int DEFAULT 0,
total_game_time bigint DEFAULT 0,
total_deaths int DEFAULT 0,
highest_player_level int DEFAULT 1,
total_enemies_killed int DEFAULT 0,
total_achievement_points int DEFAULT 0,
highest_score int DEFAULT 0,
highest_score_achieved_at TIMESTAMPTZ DEFAULT NOW()
);