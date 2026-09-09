DROP FUNCTION IF EXISTS update_player_after_disconnect(UUID, BIGINT, BIGINT, BIGINT, BIGINT, INT, BIGINT, INT, INT, INT, JSONB);

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
RETURNS TABLE (
    user_id UUID,
    total_xp_gained BIGINT,
    total_enemies_killed BIGINT,
    total_damage_dealt BIGINT,
    total_damage_taken BIGINT,
    total_levels_completed INT,
    total_game_time BIGINT,
    total_deaths INT,
    highest_player_level INT,
    highest_score INT,
    global_rank INT
)
LANGUAGE plpgsql
AS $$
DECLARE
    item JSONB;
BEGIN
    UPDATE users
    SET total_xp_gained          = users.total_xp_gained + p_xp_gained,
        total_enemies_killed     = users.total_enemies_killed + p_enemies_killed,
        total_damage_dealt       = users.total_damage_dealt + p_damage_dealt,
        total_damage_taken       = users.total_damage_taken + p_damage_taken,
        total_levels_completed   = users.total_levels_completed + p_levels_completed,
        total_game_time          = users.total_game_time + p_game_time,
        total_deaths             = users.total_deaths + p_deaths,
        highest_player_level     = GREATEST(users.highest_player_level, p_player_level),
        highest_score            = GREATEST(users.highest_score, p_score_gained)
    WHERE users.user_id = p_user_id;

    IF p_achievements IS NOT NULL AND jsonb_array_length(p_achievements) > 0 THEN
        FOR item IN SELECT * FROM jsonb_array_elements(p_achievements)
        LOOP
            IF item->'progress' IS NULL OR item->'progress' = '{}'::jsonb THEN
               DELETE FROM player_achievements_progress pap
                WHERE pap.user_id = p_user_id 
                AND pap.achievement_id = (item->>'achievement_id')::UUID;
            ELSE
                INSERT INTO player_achievements_progress (user_id, achievement_id, current_progress, updated_at)
                VALUES (
                    p_user_id,
                    (item->>'achievement_id')::UUID,
                    item->'progress',
                    NOW()
                )
                ON CONFLICT ON CONSTRAINT player_achievements_progress_pkey
                DO UPDATE SET
                    current_progress = EXCLUDED.current_progress,
                    updated_at = NOW();
            END IF;
        END LOOP;
    END IF;
 
    RETURN QUERY
    WITH ranked_users AS (
        SELECT
            u.user_id AS r_user_id,
            u.player_id AS r_player_id,
            u.total_xp_gained AS r_total_xp,
            u.total_enemies_killed AS r_enemies,
            u.total_damage_dealt AS r_dmg_dealt,
            u.total_damage_taken AS r_dmg_taken,
            u.total_levels_completed AS r_levels,
            u.total_game_time AS r_time,
            u.total_deaths AS r_deaths,
            u.highest_player_level AS r_max_level,
            u.highest_score AS r_highest_score,
            ROW_NUMBER() OVER (ORDER BY u.highest_score DESC, u.created_at ASC)::INT as r_rank
        FROM users u
    )
    SELECT 
        ru.r_user_id,
        ru.r_player_id,
        ru.r_total_xp,
        ru.r_enemies,
        ru.r_dmg_dealt,
        ru.r_dmg_taken,
        ru.r_levels,
        ru.r_time,
        ru.r_deaths,
        ru.r_max_level,
        ru.r_highest_score,
        ru.r_rank
    FROM ranked_users ru
    WHERE ru.r_user_id = p_user_id;
END;
$$;