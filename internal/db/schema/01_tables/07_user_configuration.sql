CREATE TABLE IF NOT EXISTS user_configuration (
    user_id UUID PRIMARY KEY REFERENCES users(user_id) ON DELETE CASCADE, 
    config JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW() 
);