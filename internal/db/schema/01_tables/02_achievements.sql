CREATE TABLE IF NOT EXISTS achievements (
    achievement_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code VARCHAR(50) UNIQUE NOT NULL, 
    title VARCHAR(100) NOT NULL,
    description TEXT NOT NULL,
    points INT NOT NULL DEFAULT 0,
    requirements JSONB NOT NULL,
    is_global_announcement BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);