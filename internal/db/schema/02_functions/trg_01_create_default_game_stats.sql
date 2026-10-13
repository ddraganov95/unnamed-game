CREATE OR REPLACE FUNCTION trg_01_create_default_game_stats()
RETURNS TRIGGER AS $$
BEGIN
INSERT INTO games_stats(game_id)
VALUES (NEW.game_id)
ON CONFLICT (game_id) DO NOTHING;
RETURN NEW;
END;
$$
Language plpgsql;