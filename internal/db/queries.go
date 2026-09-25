package db

const (
	upsertUserQuery = `
	INSERT INTO users(player_id)
	VALUES ($1)
	ON CONFLICT (player_id) DO UPDATE
	SET last_login = CURRENT_TIMESTAMP
	RETURNING user_id, player_id;
	`
)
const (
	GetUserSummary = `
	Select 
	u.user_id,
	u.player_id,
	us.total_enemies_killed,
	us.total_xp_gained,
	us.total_damage_dealt,
	us.total_damage_taken,
	us.total_levels_completed,
	us.total_game_time,
	us.total_deaths,
	us.highest_player_level,
	us.total_achievement_points,
	us.highest_score
	FROM user_stats us JOIN users u ON u.user_id = us.user_id
	WHERE us.user_id = $1;
	`
)
const GetPlayerFullAchievementState = `
    SELECT 
        a.achievement_id,
        a.code,
        a.requirements,
        CASE WHEN pa.achievement_id IS NOT NULL THEN true ELSE false END AS is_unlocked,
        COALESCE(pap.current_progress, '{}'::jsonb) AS progress
    FROM achievements a
    LEFT JOIN player_achievements pa 
        ON a.achievement_id = pa.achievement_id AND pa.user_id = $1
    LEFT JOIN player_achievements_progress pap 
        ON a.achievement_id = pap.achievement_id AND pap.user_id = $1;
`
const GetAchievementCatalog = `SELECT achievement_id, code, title, description, requirements, is_global_announcement FROM achievements;`

const GetUserLeaderboardPage = `
WITH ranked_users AS (
    SELECT 
        us.user_id,
        u.player_id, 
        us.highest_score, 
        us.highest_score_achieved_at,
        ROW_NUMBER() OVER (ORDER BY us.highest_score DESC, us.highest_score_achieved_at ASC)::INT AS rank
    FROM user_stats us join users u on u.user_id = us.user_id
),
target_user AS (
    SELECT rank FROM ranked_users WHERE user_id = $1
)
SELECT 
    r.user_id,
    r.player_id,
    r.highest_score,
    r.rank
FROM ranked_users r
WHERE r.rank >= (((SELECT rank FROM target_user) - 1) / $2) * $2 + 1
ORDER BY r.rank ASC
LIMIT $2 + 1;
`
const GetLeaderboardPage = `
SELECT 
    	us.user_id,
        u.player_id, 
        us.highest_score, 
        ROW_NUMBER() OVER (ORDER BY us.highest_score DESC, us.highest_score_achieved_at ASC)::INT AS rank
 FROM user_stats us join users u on u.user_id = us.user_id
ORDER BY rank ASC
LIMIT $1 OFFSET $2
;
`
const GetUserConfiguration = `
SELECT config
FROM user_configuration
WHERE user_id = $1
`
const UpsertUserConfig = `
INSERT INTO user_configuration (user_id, config)
VALUES ($1, $2)
ON CONFLICT (user_id) DO UPDATE
SET config = EXCLUDED.config;
`
