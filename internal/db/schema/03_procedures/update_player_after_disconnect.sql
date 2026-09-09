DROP FUNCTION IF EXISTS update_player_after_disconnect(UUID, BIGINT, BIGINT, BIGINT, BIGINT, INT, BIGINT, INT, INT, INT, JSONB);
DROP FUNCTION IF EXISTS update_player_after_disconnect(UUID, BIGINT, BIGINT, BIGINT, BIGINT, INT, BIGINT, INT, INT, JSONB);
DROP FUNCTION IF EXISTS update_player_after_disconnect(UUID, BIGINT, BIGINT, BIGINT, BIGINT, BIGINT, BIGINT, BIGINT, INT, JSONB);

CREATE OR REPLACE FUNCTION update_player_after_disconnect(
    p_user_id UUID,
    p_xp_gained BIGINT,
    p_enemies_killed BIGINT,
    p_damage_dealt BIGINT,
    p_damage_taken BIGINT,
    p_levels_completed INT,
    p_game_time BIGINT,
    p_deaths INT,
    p_player_level INT,
    p_score_gained INT,
    p_achievements JSONB
)
RETURNS SETOF users
LANGUAGE plpgsql
AS $$
DECLARE
    item JSONB;
BEGIN

    IF p_achievements IS NOT NULL AND jsonb_array_length(p_achievements) > 0 THEN
        FOR item IN SELECT * FROM jsonb_array_elements(p_achievements)
        LOOP
            IF item->'progress' IS NULL OR item->'progress' = '{}'::jsonb THEN
                DELETE FROM player_achievements_progress 
                WHERE user_id = p_user_id 
                  AND achievement_id = (item->>'achievement_id')::UUID;
            ELSE
                INSERT INTO player_achievements_progress (user_id, achievement_id, current_progress, updated_at)
                VALUES (
                    p_user_id,
                    (item->>'achievement_id')::UUID,
                    item->'progress',
                    NOW()
                )
                ON CONFLICT (user_id, achievement_id)
                DO UPDATE SET
                    current_progress = EXCLUDED.current_progress,
                    updated_at = NOW();
            END IF;
        END LOOP;
    END IF;

    RETURN QUERY
    UPDATE users
    SET total_xp_gained          = total_xp_gained + p_xp_gained,
        total_enemies_killed     = total_enemies_killed + p_enemies_killed,
        total_damage_dealt       = total_damage_dealt + p_damage_dealt,
        total_damage_taken       = total_damage_taken + p_damage_taken,
        total_levels_completed   = total_levels_completed + p_levels_completed,
        total_game_time          = total_game_time + p_game_time,
        total_deaths             = total_deaths + p_deaths,
        highest_player_level     = GREATEST(highest_player_level, p_player_level),
        highest_score            = GREATEST(highest_score, p_score_gained)
    WHERE user_id = p_user_id
    RETURNING *;
END;
$$;