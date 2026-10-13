CREATE INDEX IF NOT EXISTS idx_games_active 
ON games(game_id) 
WHERE status='active';