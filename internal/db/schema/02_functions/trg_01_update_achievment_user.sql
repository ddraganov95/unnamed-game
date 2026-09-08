CREATE OR REPLACE FUNCTION trg_01_update_achievment_user()
    RETURNS TRIGGER AS $$
    DECLARE 
    achievement_points INT;
    BEGIN
    SELECT points
    INTO achievement_points
    FROM achievements
    WHERE achievement_id = NEW.achievement_id;

    UPDATE users
    SET total_achievement_points = total_achievement_points + achievement_points
    WHERE user_id = NEW.user_id;
    RETURN NEW;
    END;
    $$
    LANGUAGE plpgsql;
;