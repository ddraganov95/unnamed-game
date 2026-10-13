CREATE TABLE IF NOT EXISTS games(
    game_id UUID PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    server_host VARCHAR(255) NOT NULL,
    status VARCHAR(20) NOT NULL,
    ended_at           TIMESTAMPTZ DEFAULT NOW(),
    termination_reason VARCHAR(20)
);