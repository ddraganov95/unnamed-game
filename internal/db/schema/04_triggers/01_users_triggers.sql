CREATE OR REPLACE TRIGGER trg_01_create_default_user_stats
AFTER INSERT
ON users
FOR EACH ROW EXECUTE FUNCTION trg_01_create_default_user_stats();