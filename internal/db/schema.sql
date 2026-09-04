CREATE TABLE IF NOT EXISTS users (
user_id UUID Primary Key DEFAULT gen_random_uuid(), 
player_id varchar(20) Unique NOT NULL,
last_login TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
total_xp_gained bigint DEFAULT 0,
total_damage_dealt bigint DEFAULT 0,
total_damage_taken bigint DEFAULT 0,
total_levels_completed int DEFAULT 0,
total_game_time bigint DEFAULT 0,
total_deaths int DEFAULT 0,
highest_player_level int DEFAULT 1,
total_enemies_killed int DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_users_player_id ON users(player_id);

/*ALTER TABLE users 
ADD COLUMN IF NOT EXISTS total_enemies_killed bigint DEFAULT 0;*/