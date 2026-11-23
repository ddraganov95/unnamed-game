package db_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"unnamed-game/internal/db"
	"unnamed-game/internal/game"
	"unnamed-game/internal/testutil/pgtest"

	"github.com/jackc/pgx/v5"
)

// ---------------------------------------------------------------------------
// Schema
// ---------------------------------------------------------------------------

func TestInitSchemaIsIdempotent(t *testing.T) {
	d := pgtest.Open(t)
	for i := 0; i < 2; i++ {
		if err := db.InitSchema(ctxT(t), d.Pool()); err != nil {
			t.Fatalf("InitSchema run %d: %v", i+1, err)
		}
	}
	var n int
	d.Pool().QueryRow(ctxT(t), "SELECT count(*) FROM achievements").Scan(&n)
	if n != 5 {
		t.Fatalf("seeded achievements = %d, want 5 (seeds duplicated?)", n)
	}
}

// Two servers booting at the same time both run InitSchema.
// users holds login identity only; all stats live in user_stats.
func TestUsersTableHasOnlyIdentityColumns(t *testing.T) {
	d := pgtest.Open(t)
	rows, err := d.Pool().Query(ctxT(t), `
		SELECT column_name FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'users'
		ORDER BY ordinal_position`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var c string
		rows.Scan(&c)
		cols = append(cols, c)
	}
	want := []string{"user_id", "player_id", "last_login", "created_at"}
	if strings.Join(cols, ",") != strings.Join(want, ",") {
		t.Fatalf("users columns = %v, want %v", cols, want)
	}
}

func TestInitSchemaConcurrentBoot(t *testing.T) {
	d := pgtest.Open(t)
	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = db.InitSchema(ctxT(t), d.Pool())
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("concurrent InitSchema %d: %v", i, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Users
// ---------------------------------------------------------------------------

func TestUpsertUserCreatesStatsAndUpdatesLogin(t *testing.T) {
	d := pgtest.Open(t)
	ctx := ctxT(t)
	first, err := d.UpsertUser(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	var statsRows int
	d.Pool().QueryRow(ctx, "SELECT count(*) FROM user_stats WHERE user_id=$1", first.UserID).Scan(&statsRows)
	if statsRows != 1 {
		t.Fatalf("user_stats rows=%d want 1 (trigger)", statsRows)
	}
	var login1 time.Time
	d.Pool().QueryRow(ctx, "SELECT last_login FROM users WHERE user_id=$1", first.UserID).Scan(&login1)

	time.Sleep(10 * time.Millisecond)
	second, err := d.UpsertUser(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if second.UserID != first.UserID {
		t.Fatalf("re-login created a new user id")
	}
	var login2 time.Time
	d.Pool().QueryRow(ctx, "SELECT last_login FROM users WHERE user_id=$1", first.UserID).Scan(&login2)
	if !login2.After(login1) {
		t.Errorf("last_login not updated: %v -> %v", login1, login2)
	}
}

func TestUpsertUserConcurrentSameName(t *testing.T) {
	d := pgtest.Open(t)
	const n = 20
	ids := make([]uuid.UUID, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ref, err := d.UpsertUser(ctxT(t), "racer")
			errs[i] = err
			if err == nil {
				ids[i] = ref.UserID
			}
		}(i)
	}
	wg.Wait()
	for i := range ids {
		if errs[i] != nil {
			t.Fatalf("upsert %d: %v", i, errs[i])
		}
		if ids[i] != ids[0] {
			t.Fatalf("concurrent upserts returned different ids")
		}
	}
	var rows int
	d.Pool().QueryRow(ctxT(t), "SELECT count(*) FROM users WHERE player_id='racer'").Scan(&rows)
	if rows != 1 {
		t.Fatalf("users rows=%d want 1", rows)
	}
}

func TestUpsertUserRejectsLongName(t *testing.T) {
	d := pgtest.Open(t)
	if _, err := d.UpsertUser(ctxT(t), strings.Repeat("x", 21)); err == nil {
		t.Fatal("expected error for player_id > 20 chars")
	}
}

func TestFetchUserSummary(t *testing.T) {
	d := pgtest.Open(t)
	id := mustUser(t, d, "alice")
	s, err := d.FetchUserSummary(ctxT(t), id.String())
	if err != nil {
		t.Fatal(err)
	}
	if s.PlayerID != "alice" || s.UserID != id || s.HighestPlayerLevel != 1 {
		t.Fatalf("summary %+v", s)
	}
	if _, err := d.FetchUserSummary(ctxT(t), uuid.New().String()); err == nil {
		t.Errorf("unknown user should error")
	}
}

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

func TestUserConfigurationDefaultAndRoundTrip(t *testing.T) {
	d := pgtest.Open(t)
	id := mustUser(t, d, "alice")
	cfg, err := d.FetchUserConfiguration(ctxT(t), id)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Keybinds["move_up"] != "w" || cfg.MasterVol != 100 {
		t.Fatalf("default config %+v", cfg)
	}

	custom := db.UserConfiguration{Keybinds: map[string]string{"move_up": "i"}, MasterVol: 10, SfxVol: 20}
	if err := d.UpsertUserConfiguration(ctxT(t), id, custom); err != nil {
		t.Fatal(err)
	}
	custom.MasterVol = 55
	if err := d.UpsertUserConfiguration(ctxT(t), id, custom); err != nil {
		t.Fatal(err)
	}
	got, err := d.FetchUserConfiguration(ctxT(t), id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Keybinds["move_up"] != "i" || got.MasterVol != 55 || got.SfxVol != 20 {
		t.Fatalf("round trip %+v", got)
	}
}

func TestUpsertConfigurationUnknownUserFails(t *testing.T) {
	d := pgtest.Open(t)
	if err := d.UpsertUserConfiguration(ctxT(t), uuid.New(), db.DefaultUserConfiguration); err == nil {
		t.Fatal("expected FK error for unknown user")
	}
}

// ---------------------------------------------------------------------------
// Disconnect save
// ---------------------------------------------------------------------------

func summary(xp, kills, score, level int) game.PlayerSessionSummary {
	return game.PlayerSessionSummary{
		SessionDuration: 90 * time.Second,
		XPGained:        xp,
		EnemiesKilled:   kills,
		DamageDealt:     100,
		DamageTaken:     40,
		LevelsCompleted: 2,
		PlayerLevel:     level,
		Score:           score,
	}
}

func TestUpdatePlayerAfterDisconnectAccumulates(t *testing.T) {
	d := pgtest.Open(t)
	id := mustUser(t, d, "alice")
	ctx := ctxT(t)

	s1, err := d.UpdatePlayerAfterDisconnect(ctx, id, summary(50, 3, 100, 4), nil)
	if err != nil {
		t.Fatal(err)
	}
	if s1.TotalXPGained != 50 || s1.TotalEnemiesKilled != 3 || s1.HighestScore != 100 || s1.HighestPlayerLevel != 4 || s1.TotalGameTime != 90 || s1.TotalLevelsCompleted != 2 {
		t.Fatalf("after first save %+v", s1)
	}
	firstHighAt := s1.HighestScoreAchievedAt

	time.Sleep(10 * time.Millisecond)
	s2, err := d.UpdatePlayerAfterDisconnect(ctx, id, summary(10, 1, 40, 2), nil)
	if err != nil {
		t.Fatal(err)
	}
	if s2.TotalXPGained != 60 || s2.TotalEnemiesKilled != 4 || s2.TotalDamageDealt != 200 {
		t.Fatalf("totals not accumulated %+v", s2)
	}
	if s2.HighestScore != 100 || s2.HighestPlayerLevel != 4 {
		t.Fatalf("GREATEST lowered the record %+v", s2)
	}
	if !s2.HighestScoreAchievedAt.Equal(firstHighAt) {
		t.Errorf("highest_score_achieved_at moved without a new high score")
	}

	time.Sleep(10 * time.Millisecond)
	s3, err := d.UpdatePlayerAfterDisconnect(ctx, id, summary(0, 0, 500, 1), nil)
	if err != nil {
		t.Fatal(err)
	}
	if s3.HighestScore != 500 || !s3.HighestScoreAchievedAt.After(firstHighAt) {
		t.Errorf("new high score not recorded %+v", s3)
	}
}

func TestUpdatePlayerAfterDisconnectProgress(t *testing.T) {
	d := pgtest.Open(t)
	id := mustUser(t, d, "alice")
	gob := achievementID(t, d, "gbl_slr_aprntc")
	ctx := ctxT(t)

	_, err := d.UpdatePlayerAfterDisconnect(ctx, id, summary(0, 0, 0, 1), []game.AchProgressDTO{
		{AchievementID: gob, Progress: map[string]int64{"1:0": 4}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err := d.Pool().QueryRow(ctx, "SELECT current_progress FROM player_achievements_progress WHERE user_id=$1 AND achievement_id=$2", id, gob).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var prog map[string]int64
	json.Unmarshal(raw, &prog)
	if prog["1:0"] != 4 {
		t.Fatalf("stored progress %s", raw)
	}

	// Update, then empty progress deletes the row.
	_, err = d.UpdatePlayerAfterDisconnect(ctx, id, summary(0, 0, 0, 1), []game.AchProgressDTO{
		{AchievementID: gob, Progress: map[string]int64{"1:0": 7}},
	})
	if err != nil {
		t.Fatal(err)
	}
	states, err := d.FetchPlayerProgress(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range states {
		if s.AchievementID == gob && s.Progress["1:0"] != 7 {
			t.Fatalf("FetchPlayerProgress progress %v", s.Progress)
		}
	}
	_, err = d.UpdatePlayerAfterDisconnect(ctx, id, summary(0, 0, 0, 1), []game.AchProgressDTO{
		{AchievementID: gob, Progress: map[string]int64{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var rows int
	d.Pool().QueryRow(ctx, "SELECT count(*) FROM player_achievements_progress WHERE user_id=$1", id).Scan(&rows)
	if rows != 0 {
		t.Fatalf("empty progress should delete the row, %d left", rows)
	}
}

func TestUpdatePlayerAfterDisconnectRollsBackOnError(t *testing.T) {
	d := pgtest.Open(t)
	id := mustUser(t, d, "alice")
	gob := achievementID(t, d, "gbl_slr_aprntc")
	ctx := ctxT(t)
	_, err := d.UpdatePlayerAfterDisconnect(ctx, id, summary(50, 1, 10, 2), []game.AchProgressDTO{
		{AchievementID: gob, Progress: map[string]int64{"1:0": 1}},
		{AchievementID: uuid.New(), Progress: map[string]int64{"1:0": 1}}, // FK violation
	})
	if err == nil {
		t.Fatal("expected an error for an unknown achievement id")
	}
	var xp int64
	var progressRows int
	d.Pool().QueryRow(ctx, "SELECT total_xp_gained FROM user_stats WHERE user_id=$1", id).Scan(&xp)
	d.Pool().QueryRow(ctx, "SELECT count(*) FROM player_achievements_progress WHERE user_id=$1", id).Scan(&progressRows)
	if xp != 0 || progressRows != 0 {
		t.Fatalf("partial write after failed save: xp=%d progressRows=%d", xp, progressRows)
	}
}

// The same user disconnecting from several games/servers at once.
func TestUpdatePlayerAfterDisconnectConcurrent(t *testing.T) {
	d := pgtest.Open(t)
	id := mustUser(t, d, "alice")
	gob := achievementID(t, d, "gbl_slr_aprntc")
	const n = 20
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := d.UpdatePlayerAfterDisconnect(ctxT(t), id, summary(10, 1, i, 1), []game.AchProgressDTO{
				{AchievementID: gob, Progress: map[string]int64{"1:0": int64(i)}},
			})
			if err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent save: %v", err)
	}
	var xp int64
	var kills, high int
	d.Pool().QueryRow(ctxT(t), "SELECT total_xp_gained, total_enemies_killed, highest_score FROM user_stats WHERE user_id=$1", id).Scan(&xp, &kills, &high)
	if xp != 10*n || kills != n || high != n-1 {
		t.Fatalf("lost updates: xp=%d kills=%d high=%d", xp, kills, high)
	}
}

// ---------------------------------------------------------------------------
// Achievements
// ---------------------------------------------------------------------------

func TestFetchAchievementCatalogBuildsEngineCatalog(t *testing.T) {
	d := pgtest.Open(t)
	defs, err := d.FetchAchievementCatalog(ctxT(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 5 {
		t.Fatalf("catalog size %d want 5", len(defs))
	}
	for _, def := range defs {
		if len(def.Requirements) == 0 {
			t.Errorf("%s has no requirements", def.Code)
		}
	}
	cat, err := game.BuildAchievementCatalog(defs)
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.InvertedIndex["kill:goblin"]) == 0 {
		t.Errorf("seeded kill:goblin requirement not indexed")
	}
}

func TestFetchPlayerProgressNewUser(t *testing.T) {
	d := pgtest.Open(t)
	id := mustUser(t, d, "alice")
	states, err := d.FetchPlayerProgress(ctxT(t), id)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 5 {
		t.Fatalf("got %d rows, want one per achievement", len(states))
	}
	for _, s := range states {
		if s.IsUnlocked || len(s.Progress) != 0 {
			t.Errorf("new user state %+v", s)
		}
	}
}

func TestPlayerAchievementUnlock(t *testing.T) {
	d := pgtest.Open(t)
	id := mustUser(t, d, "alice")
	gob := achievementID(t, d, "gbl_slr_aprntc")
	ctx := ctxT(t)
	notif := listen(t, d, "achievement_unlocked")

	_, err := d.UpdatePlayerAfterDisconnect(ctx, id, summary(0, 0, 0, 1), []game.AchProgressDTO{
		{AchievementID: gob, Progress: map[string]int64{"1:0": 9}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.CallFunction(ctx, "player_achievement_unlock", id, gob); err != nil {
		t.Fatal(err)
	}

	states, _ := d.FetchPlayerProgress(ctx, id)
	for _, s := range states {
		if s.AchievementID == gob && (!s.IsUnlocked || len(s.Progress) != 0) {
			t.Fatalf("after unlock: %+v", s)
		}
	}

	n, ok := waitNotification(t, notif, 3*time.Second)
	if !ok {
		t.Fatal("no achievement_unlocked notification")
	}
	var payload struct {
		PlayerID string `json:"player_id"`
		Title    string `json:"title"`
		Global   bool   `json:"is_global_announcement"`
	}
	if err := json.Unmarshal([]byte(n.Payload), &payload); err != nil {
		t.Fatalf("payload %q: %v", n.Payload, err)
	}
	if payload.PlayerID != "alice" || !payload.Global || payload.Title == "" {
		t.Fatalf("payload %+v", payload)
	}
}

// Unlocking adds the achievement's points to user_stats (the points trigger
// used to update users.total_achievement_points, a column that no longer
// exists after the users -> user_stats migration, so every unlock failed).
func TestAchievementPointsVisibleInStats(t *testing.T) {
	d := pgtest.Open(t)
	id := mustUser(t, d, "alice")
	gob := achievementID(t, d, "gbl_slr_aprntc") // 10 points
	ctx := ctxT(t)
	if err := d.CallFunction(ctx, "player_achievement_unlock", id, gob); err != nil {
		t.Fatal(err)
	}
	s, err := d.FetchUserSummary(ctx, id.String())
	if err != nil {
		t.Fatal(err)
	}
	if s.TotalAchievementPoints != 10 {
		t.Fatalf("profile shows %d achievement points, want 10", s.TotalAchievementPoints)
	}
}

// A repeat unlock (leave-and-rejoin race where the next game loaded progress
// before the unlock committed, or the same account on two servers) must be a
// no-op: no error, no extra points, no second announcement.
func TestPlayerAchievementUnlockIsIdempotent(t *testing.T) {
	d := pgtest.Open(t)
	id := mustUser(t, d, "alice")
	gob := achievementID(t, d, "gbl_slr_aprntc") // 10 points
	ctx := ctxT(t)
	conn := listen(t, d, "achievement_unlocked")
	if err := d.CallFunction(ctx, "player_achievement_unlock", id, gob); err != nil {
		t.Fatal(err)
	}
	if _, ok := waitNotification(t, conn, 2*time.Second); !ok {
		t.Fatal("first unlock sent no announcement")
	}
	if err := d.CallFunction(ctx, "player_achievement_unlock", id, gob); err != nil {
		t.Fatalf("second unlock: %v", err)
	}
	if n, ok := waitNotification(t, conn, 500*time.Millisecond); ok {
		t.Errorf("second unlock announced again: %s", n.Payload)
	}
	var points, rows int
	d.Pool().QueryRow(ctx, "SELECT total_achievement_points FROM user_stats WHERE user_id=$1", id).Scan(&points)
	d.Pool().QueryRow(ctx, "SELECT count(*) FROM player_achievements WHERE user_id=$1", id).Scan(&rows)
	if points != 10 || rows != 1 {
		t.Errorf("after two unlocks: points=%d rows=%d, want 10 and 1", points, rows)
	}
}

// ---------------------------------------------------------------------------
// Games
// ---------------------------------------------------------------------------

func TestInsertAndFetchGame(t *testing.T) {
	d := pgtest.Open(t)
	ctx := ctxT(t)
	gid := uuid.NewV7()
	if err := d.InsertNewGame(ctx, gid, "localhost:8080"); err != nil {
		t.Fatal(err)
	}
	if err := d.InsertNewGame(ctx, gid, "other:1"); err != nil {
		t.Fatalf("duplicate insert should be a no-op: %v", err)
	}
	host, err := d.FetchGameHostByGameId(ctx, gid)
	if err != nil || host != "localhost:8080" {
		t.Fatalf("host=%q err=%v", host, err)
	}
	var statsRows int
	d.Pool().QueryRow(ctx, "SELECT count(*) FROM games_stats WHERE game_id=$1", gid).Scan(&statsRows)
	if statsRows != 1 {
		t.Fatalf("games_stats rows=%d want 1 (trigger)", statsRows)
	}
	if _, err := d.FetchGameHostByGameId(ctx, uuid.New()); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("missing game: err=%v want ErrNoRows", err)
	}
}

func makeEvents(n int) []game.Event {
	out := make([]game.Event, n)
	for i := range out {
		out[i] = game.Event{Text: fmt.Sprintf("event %d", i), CreatedAt: time.Now()}
	}
	return out
}

func TestSaveGameEventsCopy(t *testing.T) {
	d := pgtest.Open(t)
	ctx := ctxT(t)
	gid := uuid.NewV7()
	d.InsertNewGame(ctx, gid, "h:1")
	if err := d.SaveGameEventsCopy(ctx, gid, nil); err != nil {
		t.Fatalf("empty copy: %v", err)
	}
	if err := d.SaveGameEventsCopy(ctx, gid, makeEvents(1000)); err != nil {
		t.Fatal(err)
	}
	var n int
	d.Pool().QueryRow(ctx, "SELECT count(*) FROM game_events WHERE game_id=$1", gid).Scan(&n)
	if n != 1000 {
		t.Fatalf("events=%d want 1000", n)
	}
}

func TestEndGameWithEvents(t *testing.T) {
	d := pgtest.Open(t)
	ctx := ctxT(t)
	gid := uuid.NewV7()
	d.InsertNewGame(ctx, gid, "h:1")
	sum := game.GameSummary{TotalLevels: 3, LevelsCompleted: 2, PlayersConnected: 2, PlayerDeaths: 1, EnemiesSpawned: 9, EnemiesKilled: 8, EmptyMinutes: 1}
	if err := d.EndGameWithEvents(ctx, gid, "player_quit", sum, makeEvents(5)); err != nil {
		t.Fatal(err)
	}
	var status, reason string
	var ended *time.Time
	d.Pool().QueryRow(ctx, "SELECT status, termination_reason, ended_at FROM games WHERE game_id=$1", gid).Scan(&status, &reason, &ended)
	if status != "inactive" || reason != "player_quit" || ended == nil {
		t.Fatalf("game row: status=%s reason=%s ended=%v", status, reason, ended)
	}
	var levels, killed, events int
	d.Pool().QueryRow(ctx, "SELECT total_levels, enemies_killed FROM games_stats WHERE game_id=$1", gid).Scan(&levels, &killed)
	d.Pool().QueryRow(ctx, "SELECT count(*) FROM game_events WHERE game_id=$1", gid).Scan(&events)
	if levels != 3 || killed != 8 || events != 5 {
		t.Fatalf("stats levels=%d killed=%d events=%d", levels, killed, events)
	}
}

func TestEndGameWithEventsUnknownGameRollsBack(t *testing.T) {
	d := pgtest.Open(t)
	ctx := ctxT(t)
	if err := d.EndGameWithEvents(ctx, uuid.New(), "idle_check", game.GameSummary{}, makeEvents(2)); err == nil {
		t.Fatal("ending an unknown game with events should fail (FK)")
	}
	var n int
	d.Pool().QueryRow(ctx, "SELECT count(*) FROM game_events").Scan(&n)
	if n != 0 {
		t.Fatalf("events written despite failure: %d", n)
	}
}

func TestDBCallsRespectCancelledContext(t *testing.T) {
	d := pgtest.Open(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := d.InsertNewGame(ctx, uuid.NewV7(), "h:1"); err == nil {
		t.Errorf("InsertNewGame with cancelled ctx should fail")
	}
	if _, err := d.UpsertUser(ctx, "x"); err == nil {
		t.Errorf("UpsertUser with cancelled ctx should fail")
	}
}

// ---------------------------------------------------------------------------
// Leaderboard
// ---------------------------------------------------------------------------

// seedLeaderboard creates n users where user i has score (n-i)*10, so
// user000 is rank 1. Returns the ids in rank order.
func seedLeaderboard(t *testing.T, d *db.Database, n int) []uuid.UUID {
	t.Helper()
	ids := make([]uuid.UUID, n)
	for i := 0; i < n; i++ {
		ids[i] = mustUser(t, d, fmt.Sprintf("user%03d", i))
		if _, err := d.Pool().Exec(ctxT(t), "UPDATE user_stats SET highest_score=$2 WHERE user_id=$1", ids[i], (n-i)*10); err != nil {
			t.Fatal(err)
		}
	}
	return ids
}

func TestFetchLeaderboardPage(t *testing.T) {
	d := pgtest.Open(t)
	ids := seedLeaderboard(t, d, 25)
	page1, err := d.FetchLeaderboardPage(ctxT(t), 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page1) != 11 {
		t.Fatalf("page 1 rows=%d want 11 (page size + 1 for next-page detection)", len(page1))
	}
	for i, row := range page1 {
		if row.Rank != i+1 || row.UserID != ids[i] {
			t.Fatalf("page 1 row %d = %+v", i, row)
		}
	}
	page3, _ := d.FetchLeaderboardPage(ctxT(t), 3, 10)
	if len(page3) != 5 || page3[0].Rank != 21 {
		t.Fatalf("page 3 rows=%d first=%+v", len(page3), page3)
	}
}

func TestLeaderboardTieBreakByEarliestAchieved(t *testing.T) {
	d := pgtest.Open(t)
	late := mustUser(t, d, "late")
	early := mustUser(t, d, "early")
	ctx := ctxT(t)
	d.Pool().Exec(ctx, "UPDATE user_stats SET highest_score=100, highest_score_achieved_at=now() WHERE user_id=$1", late)
	d.Pool().Exec(ctx, "UPDATE user_stats SET highest_score=100, highest_score_achieved_at=now()-interval '1 day' WHERE user_id=$1", early)
	rows, _ := d.FetchLeaderboardPage(ctx, 1, 10)
	if len(rows) < 2 || rows[0].UserID != early {
		t.Fatalf("tie should rank the earlier achiever first: %+v", rows)
	}
}

func TestFetchUserLeaderboardPageBoundaries(t *testing.T) {
	d := pgtest.Open(t)
	ids := seedLeaderboard(t, d, 25)
	cases := []struct {
		rank      int
		firstRank int
	}{{1, 1}, {10, 1}, {11, 11}, {20, 11}, {21, 21}, {25, 21}}
	for _, c := range cases {
		rows, err := d.FetchUserLeaderboard(ctxT(t), ids[c.rank-1], 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) == 0 || rows[0].Rank != c.firstRank {
			t.Errorf("user rank %d: page starts at %+v, want rank %d", c.rank, rows, c.firstRank)
			continue
		}
		found := false
		for _, r := range rows {
			if r.UserID == ids[c.rank-1] && r.Rank == c.rank {
				found = true
			}
		}
		if !found {
			t.Errorf("user rank %d not on their page", c.rank)
		}
	}
}

// ---------------------------------------------------------------------------
// Chat & LISTEN/NOTIFY
// ---------------------------------------------------------------------------

func TestGlobalChatMessageSend(t *testing.T) {
	d := pgtest.Open(t)
	id := mustUser(t, d, "alice")
	notif := listen(t, d, "global_chat")
	if err := d.CallFunction(ctxT(t), "global_chat_message_send", id, "hello", "[alice]: hello"); err != nil {
		t.Fatal(err)
	}
	var n int
	d.Pool().QueryRow(ctxT(t), "SELECT count(*) FROM global_chat_messages WHERE user_id=$1 AND message='hello'", id).Scan(&n)
	if n != 1 {
		t.Fatalf("message rows=%d", n)
	}
	got, ok := waitNotification(t, notif, 3*time.Second)
	if !ok || got.Payload != "[alice]: hello" {
		t.Fatalf("notification %+v ok=%v", got, ok)
	}
}

func TestGlobalChatMessageTooLong(t *testing.T) {
	d := pgtest.Open(t)
	id := mustUser(t, d, "alice")
	msg := strings.Repeat("x", 71)
	if err := d.CallFunction(ctxT(t), "global_chat_message_send", id, msg, "[alice]: "+msg); err == nil {
		t.Fatal("expected error for a message over 70 chars")
	}
}

func startListener(t *testing.T, d *db.Database) (received chan string, cancel context.CancelFunc, done chan struct{}) {
	t.Helper()
	received = make(chan string, 10)
	ctx, cancel := context.WithCancel(context.Background())
	done = make(chan struct{})
	go func() {
		defer close(done)
		d.ListenToDBEvents(ctx, []string{"global_chat"}, func(ch, payload string) { received <- payload })
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return received, cancel, done
}

// notifyUntilReceived keeps sending NOTIFY until the listener sees it (the
// listener connects asynchronously).
func notifyUntilReceived(t *testing.T, d *db.Database, received chan string, payload string, within time.Duration) bool {
	t.Helper()
	deadline := time.After(within)
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		d.Pool().Exec(context.Background(), "SELECT pg_notify('global_chat', $1)", payload)
		select {
		case got := <-received:
			if got == payload {
				return true
			}
		case <-tick.C:
		case <-deadline:
			return false
		}
	}
}

func TestListenToDBEventsDeliversAndStops(t *testing.T) {
	d := pgtest.Open(t)
	received, cancel, done := startListener(t, d)
	if !notifyUntilReceived(t, d, received, "ping", 5*time.Second) {
		t.Fatal("listener never received the notification")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("listener did not stop after context cancel")
	}
}

// If the LISTEN connection drops, the listener must reconnect, every time
// (global chat and achievement announcements depend on it). Before the fix
// it logged "Reconnecting in 2s..." and then returned for good.
func TestListenToDBEventsReconnects(t *testing.T) {
	d := pgtest.Open(t)
	received, cancel, done := startListener(t, d)
	if !notifyUntilReceived(t, d, received, "before", 5*time.Second) {
		t.Fatal("listener never received the first notification")
	}
	for i := 1; i <= 3; i++ {
		_, err := d.Pool().Exec(ctxT(t), `
			SELECT pg_terminate_backend(pid) FROM pg_stat_activity
			WHERE datname = current_database() AND pid <> pg_backend_pid() AND query ILIKE 'LISTEN%'`)
		if err != nil {
			t.Fatal(err)
		}
		payload := fmt.Sprintf("after drop %d", i)
		if !notifyUntilReceived(t, d, received, payload, 8*time.Second) {
			t.Fatalf("listener did not recover after drop %d", i)
		}
	}
	// And it still stops promptly on shutdown after reconnecting.
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("listener did not stop after context cancel")
	}
}
