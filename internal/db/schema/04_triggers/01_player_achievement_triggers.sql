CREATE OR REPLACE TRIGGER trg_01_update_achievment_user
AFTER INSERT 
ON player_achievements
FOR EACH ROW
EXECUTE FUNCTION trg_01_update_achievment_user();

CREATE OR REPLACE TRIGGER trg_02_notify_achievement_earned
AFTER INSERT
ON player_achievements
FOR EACH ROW
EXECUTE FUNCTION trg_02_notify_achievement_earned();