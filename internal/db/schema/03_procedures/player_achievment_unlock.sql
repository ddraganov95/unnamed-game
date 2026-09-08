CREATE OR REPLACE PROCEDURE player_achievement_unlock(p_user_id UUID,p_achievement_id UUID)
LANGUAGE plpgsql
AS $$
BEGIN

INSERT INTO player_achievements (user_id,achievement_id)
VALUES (p_user_id,p_achievement_id);

DELETE FROM player_achievements_progress
WHERE user_id=p_user_id AND achievement_id = p_achievement_id;
END;
$$;
