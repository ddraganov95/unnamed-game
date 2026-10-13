CREATE OR REPLACE TRIGGER trg_01_create_default_game_stats
AFTER INSERT
ON games
FOR EACH ROW EXECUTE FUNCTION trg_01_create_default_game_stats();