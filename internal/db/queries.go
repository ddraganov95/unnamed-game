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
	UptateUserWithSummary = `
	UPDATE users
	SET total_xp_gained = total_xp_gained + $2,
	total_enemies_killed = total_enemies_killed + $3,
	total_damage_dealt = total_damage_dealt + $4,
	total_damage_taken = total_damage_taken + $5,
	total_levels_completed = total_levels_completed + $6,
	total_game_time = total_game_time + $7,
	total_deaths = total_deaths + $8,
	highest_player_level = GREATEST(highest_player_level, $9)
	WHERE player_id = $1
	RETURNING player_id,
	total_xp_gained,
	total_enemies_killed,
	total_damage_dealt,
	total_damage_taken,
	total_levels_completed,
	total_game_time,
	total_deaths,
	highest_player_level;
	`
)
const (
	GetUserSummary = `
	Select player_id,
	total_enemies_killed,
	total_xp_gained,
	total_damage_dealt,
	total_damage_taken,
	total_levels_completed,
	total_game_time,
	total_deaths,
	highest_player_level
	FROM users
	WHERE player_id = $1;
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
