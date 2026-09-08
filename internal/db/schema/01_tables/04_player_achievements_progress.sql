CREATE TABLE IF NOT EXISTS player_achievements_progress(
    user_id UUID REFERENCES users(user_id) ON DELETE CASCADE,
    achievement_id UUID REFERENCES achievements(achievement_id) ON DELETE CASCADE,
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    current_progress JSONB NOT NULL,
    PRIMARY KEY(user_id,achievement_id)
);