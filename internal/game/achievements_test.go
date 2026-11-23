package game

import (
	"fmt"
	"sync"
	"testing"
	"time"
	"uuid"
)

var (
	achGoblin2 = uuid.MustParse("00000000-0000-0000-0000-000000000001") // kill 2 goblins
	achOr      = uuid.MustParse("00000000-0000-0000-0000-000000000002") // 5 goblins OR 1 archer
	achAnd     = uuid.MustParse("00000000-0000-0000-0000-000000000003") // 1 goblin AND 1 archer
	achSpeed   = uuid.MustParse("00000000-0000-0000-0000-000000000004") // level in <= 60s
	achGlobal  = uuid.MustParse("00000000-0000-0000-0000-000000000005") // kill an imp, announced globally
	achGame    = uuid.MustParse("00000000-0000-0000-0000-000000000006") // game:default event
)

func testCatalogDefs() []AchCatalogDTO {
	req := func(reqType string, target int64, group int) AchCatalogRequirement {
		return AchCatalogRequirement{ReqType: reqType, TargetValue: target, GroupID: group}
	}
	return []AchCatalogDTO{
		{ID: achGoblin2, Code: "gob2", Title: "Goblin x2", Requirements: []AchCatalogRequirement{req("kill:goblin", 2, 1)}},
		{ID: achOr, Code: "or", Title: "Goblins or archer", Requirements: []AchCatalogRequirement{req("kill:goblin", 5, 1), req("kill:archer", 1, 2)}},
		{ID: achAnd, Code: "and", Title: "Goblin and archer", Requirements: []AchCatalogRequirement{req("kill:goblin", 1, 1), req("kill:archer", 1, 1)}},
		{ID: achSpeed, Code: "speed", Title: "Speedrun", Requirements: []AchCatalogRequirement{req("time:level_speedrun", 60, 1)}},
		{ID: achGlobal, Code: "glob", Title: "Imp slayer", IsGlobalAnnouncement: true, Requirements: []AchCatalogRequirement{req("kill:imp", 1, 1)}},
		{ID: achGame, Code: "game", Title: "Played", Requirements: []AchCatalogRequirement{req("game:default", 0, 1)}},
	}
}

func testCatalog(t *testing.T) *AchievementCatalog {
	t.Helper()
	c, err := BuildAchievementCatalog(testCatalogDefs())
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// freshStates is what FetchPlayerProgress returns for a brand new user:
// one row per achievement, nothing unlocked, no progress.
func freshStates(c *AchievementCatalog) []AchProgressDTO {
	var out []AchProgressDTO
	for id := range c.Defs {
		out = append(out, AchProgressDTO{AchievementID: id, Progress: map[string]int64{}})
	}
	return out
}

func newTestEngine(t *testing.T) (*AchievementEngine, chan ServerEvent) {
	t.Helper()
	sc := make(chan ServerEvent, 100)
	return NewAchievementEngine(testCatalog(t), make(chan struct{}), sc), sc
}

// pump synchronously evaluates every queued event (instead of running the
// processor goroutine).
func pump(e *AchievementEngine) {
	for {
		select {
		case ev := <-e.eventChan:
			e.evaluateEvent(ev)
		default:
			return
		}
	}
}

func unlocks(sc chan ServerEvent) map[string]int {
	got := map[string]int{}
	for {
		select {
		case ev := <-sc:
			if ev.Type == EventTypeAchievementUnlocked {
				got[ev.Value]++
			}
		default:
			return got
		}
	}
}

func kill(e *AchievementEngine, kind string, players ...string) {
	e.PublishEvent(AchievementEvent{PlayerIDs: players, Key: "KILL:" + kind, Amount: 1})
	pump(e)
}

func TestBuildAchievementCatalog(t *testing.T) {
	c := testCatalog(t)
	if n := len(c.InvertedIndex["kill:goblin"]); n != 3 {
		t.Errorf("kill:goblin should map to 3 targets, got %d", n)
	}
	and := c.Defs[achAnd]
	if and.Groups[1].TargetMask != 0b11 {
		t.Errorf("AND group mask=%b want 11", and.Groups[1].TargetMask)
	}
	if and.MaxProgress["1:0"] != 1 || and.MaxProgress["1:1"] != 1 {
		t.Errorf("MaxProgress=%v", and.MaxProgress)
	}
	or := c.Defs[achOr]
	if len(or.Groups) != 2 || or.Groups[1].TargetMask != 1 || or.Groups[2].TargetMask != 1 {
		t.Errorf("OR groups=%+v", or.Groups)
	}
}

func TestBuildAchievementCatalogRejectsTooManyRequirements(t *testing.T) {
	var reqs []AchCatalogRequirement
	for i := 0; i < 65; i++ {
		reqs = append(reqs, AchCatalogRequirement{ReqType: fmt.Sprintf("kill:x%d", i), TargetValue: 1, GroupID: 1})
	}
	_, err := BuildAchievementCatalog([]AchCatalogDTO{{ID: uuid.New(), Code: "big", Requirements: reqs}})
	if err == nil {
		t.Fatal("expected an error for >64 requirements in one group")
	}
}

func TestCumulativeAchievementUnlocksOnce(t *testing.T) {
	e, sc := newTestEngine(t)
	e.LoadPlayerAchievements("alice", freshStates(testCatalog(t)))

	kill(e, "goblin", "alice")
	if got := unlocks(sc); got[achGoblin2.String()] != 0 {
		t.Fatalf("unlocked after 1 of 2 kills")
	}
	kill(e, "goblin", "alice")
	got := unlocks(sc)
	if got[achGoblin2.String()] != 1 {
		t.Fatalf("gob2 should unlock exactly once after 2 kills, got %v", got)
	}
	for i := 0; i < 5; i++ {
		kill(e, "goblin", "alice")
	}
	got = unlocks(sc)
	if got[achGoblin2.String()] != 0 {
		t.Fatalf("gob2 unlocked again: %v", got)
	}
	if got[achOr.String()] != 1 {
		t.Fatalf("OR achievement should unlock via 5 goblins (total 7), got %v", got)
	}
	if !e.IsUnlocked("alice", achGoblin2) {
		t.Errorf("IsUnlocked false after unlock")
	}
}

func TestOrAndGroups(t *testing.T) {
	e, sc := newTestEngine(t)
	e.LoadPlayerAchievements("alice", freshStates(testCatalog(t)))

	kill(e, "archer", "alice")
	got := unlocks(sc)
	if got[achOr.String()] != 1 {
		t.Fatalf("1 archer should satisfy the OR group, got %v", got)
	}
	if got[achAnd.String()] != 0 {
		t.Fatalf("AND achievement unlocked with only the archer")
	}
	kill(e, "goblin", "alice")
	if got := unlocks(sc); got[achAnd.String()] != 1 {
		t.Fatalf("AND achievement should unlock after goblin+archer, got %v", got)
	}
}

func TestTimeAchievement(t *testing.T) {
	e, sc := newTestEngine(t)
	e.LoadPlayerAchievements("alice", freshStates(testCatalog(t)))

	e.PublishEvent(AchievementEvent{PlayerIDs: []string{"alice"}, Key: "TIME:level_speedrun", Amount: 100})
	pump(e)
	if got := unlocks(sc); got[achSpeed.String()] != 0 {
		t.Fatalf("slow level unlocked the speedrun")
	}
	if p := e.GetProgress("alice", achSpeed); len(p) != 0 {
		t.Errorf("failed time attempt should reset progress, got %v", p)
	}
	e.PublishEvent(AchievementEvent{PlayerIDs: []string{"alice"}, Key: "TIME:level_speedrun", Amount: 30})
	pump(e)
	if got := unlocks(sc); got[achSpeed.String()] != 1 {
		t.Fatalf("fast level should unlock the speedrun, got %v", got)
	}
}

func TestGameEventAchievement(t *testing.T) {
	e, sc := newTestEngine(t)
	e.LoadPlayerAchievements("alice", freshStates(testCatalog(t)))
	e.PublishEvent(AchievementEvent{PlayerIDs: []string{"alice"}, Key: "GAME:default"})
	pump(e)
	if got := unlocks(sc); got[achGame.String()] != 1 {
		t.Fatalf("game:default should unlock, got %v", got)
	}
}

func TestAlreadyUnlockedFromDBNeverFires(t *testing.T) {
	e, sc := newTestEngine(t)
	states := freshStates(testCatalog(t))
	for i := range states {
		if states[i].AchievementID == achGoblin2 {
			states[i].IsUnlocked = true
		}
	}
	e.LoadPlayerAchievements("alice", states)
	kill(e, "goblin", "alice")
	kill(e, "goblin", "alice")
	if got := unlocks(sc); got[achGoblin2.String()] != 0 {
		t.Fatalf("achievement unlocked in DB fired again: %v", got)
	}
}

func TestProgressRoundTripAcrossSessions(t *testing.T) {
	e, sc := newTestEngine(t)
	e.LoadPlayerAchievements("alice", freshStates(testCatalog(t)))
	kill(e, "goblin", "alice")
	unlocks(sc)

	exported := e.ExportPlayerProgress("alice")
	var gob *AchProgressDTO
	for i := range exported {
		if exported[i].AchievementID == achGoblin2 {
			gob = &exported[i]
		}
	}
	if gob == nil || gob.Progress["1:0"] != 1 {
		t.Fatalf("exported gob2 progress = %+v", gob)
	}

	// New session (e.g. next game) loads the saved progress.
	e2, sc2 := newTestEngine(t)
	states := freshStates(testCatalog(t))
	for i := range states {
		for _, ex := range exported {
			if states[i].AchievementID == ex.AchievementID {
				states[i].Progress = ex.Progress
				states[i].IsUnlocked = ex.IsUnlocked
			}
		}
	}
	e2.LoadPlayerAchievements("alice", states)
	kill(e2, "goblin", "alice")
	if got := unlocks(sc2); got[achGoblin2.String()] != 1 {
		t.Fatalf("saved progress not resumed: %v", got)
	}
}

func TestPublishWithoutListenersIsDropped(t *testing.T) {
	e, _ := newTestEngine(t)
	e.PublishEvent(AchievementEvent{PlayerIDs: []string{"alice"}, Key: "kill:goblin", Amount: 1})
	if len(e.eventChan) != 0 {
		t.Fatalf("event queued with no listeners")
	}
}

func TestRemovedSessionIgnoresEvents(t *testing.T) {
	e, sc := newTestEngine(t)
	e.LoadPlayerAchievements("alice", freshStates(testCatalog(t)))
	e.RemovePlayerSession("alice")
	kill(e, "goblin", "alice")
	kill(e, "goblin", "alice")
	if got := unlocks(sc); len(got) != 0 {
		t.Fatalf("removed session still progressed: %v", got)
	}
	if e.ExportPlayerProgress("alice") != nil {
		t.Errorf("export for removed session should be nil")
	}
}

func TestUnlockPayloadCarriesGlobalFlag(t *testing.T) {
	e, sc := newTestEngine(t)
	e.LoadPlayerAchievements("alice", freshStates(testCatalog(t)))
	kill(e, "imp", "alice")
	select {
	case ev := <-sc:
		pl, ok := ev.Object.(AchievementUnlockedPayload)
		if !ok || !pl.IsGlobalAnnouncement || ev.PlayerID != "alice" {
			t.Fatalf("unexpected unlock event %+v", ev)
		}
	default:
		t.Fatal("no unlock event")
	}
}

func TestBuildClientAchievementPayload(t *testing.T) {
	e, sc := newTestEngine(t)
	e.LoadPlayerAchievements("alice", freshStates(testCatalog(t)))
	kill(e, "goblin", "alice")
	unlocks(sc)
	views := e.BuildClientAchievementPayload("alice")
	if len(views) != len(testCatalogDefs()) {
		t.Fatalf("got %d views", len(views))
	}
	for _, v := range views {
		if v.AchievementID == achGoblin2 && (v.Progress["1:0"] != 1 || v.MaxProgress["1:0"] != 2 || v.Title != "Goblin x2") {
			t.Errorf("gob2 view %+v", v)
		}
	}
}

// Many goroutines using the engine at once while the processor runs.
// Meaningful mainly under -race; without it, it still catches map panics.
func TestAchievementEngineConcurrentUse(t *testing.T) {
	sc := make(chan ServerEvent, 10)
	destroy := make(chan struct{})
	e := NewAchievementEngine(testCatalog(t), destroy, sc)
	go e.StartEventProcessor()
	stopDrain := make(chan struct{})
	go func() { // stands in for the server listener
		for {
			select {
			case <-sc:
			case <-stopDrain:
				return
			}
		}
	}()
	defer close(stopDrain)
	defer close(destroy)

	cat := testCatalog(t)
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			pid := fmt.Sprintf("p%d", w%3)
			for i := 0; i < 200; i++ {
				switch i % 6 {
				case 0:
					e.LoadPlayerAchievements(pid, freshStates(cat))
				case 1, 2:
					e.PublishEvent(AchievementEvent{PlayerIDs: []string{"p0", "p1", "p2"}, Key: "kill:goblin", Amount: 1})
				case 3:
					e.ExportPlayerProgress(pid)
				case 4:
					e.BuildClientAchievementPayload(pid)
				case 5:
					e.RemovePlayerSession(pid)
				}
			}
		}(w)
	}
	withTimeout(t, 10*time.Second, "concurrent engine use", wg.Wait)
}

// The processor sends the unlock event while holding the player's progress
// lock. If ServerEventChan is full, it blocks with the lock held; the server
// listener (the only consumer of that channel) then calls
// ExportPlayerProgress on disconnect and blocks on the same lock: deadlock.
func TestUnlockWithFullServerChannelDoesNotBlockExport(t *testing.T) {
	sc := make(chan ServerEvent, 1)
	destroy := make(chan struct{})
	e := NewAchievementEngine(testCatalog(t), destroy, sc)
	e.LoadPlayerAchievements("alice", freshStates(testCatalog(t)))
	sc <- ServerEvent{} // channel full
	go e.StartEventProcessor()
	t.Cleanup(func() {
		close(destroy)
		for { // unblock the processor so it can exit
			select {
			case <-sc:
			case <-time.After(100 * time.Millisecond):
				return
			}
		}
	})

	e.PublishEvent(AchievementEvent{PlayerIDs: []string{"alice"}, Key: "kill:imp", Amount: 1})
	time.Sleep(100 * time.Millisecond) // let the processor reach the blocking send

	withTimeout(t, 2*time.Second, "ExportPlayerProgress", func() { e.ExportPlayerProgress("alice") })
}

// If the game is destroyed while an unlock is waiting on a full
// ServerEventChan (the listener is gone), the processor must exit instead of
// leaking forever.
func TestUnlockAfterDestroyDoesNotLeakProcessor(t *testing.T) {
	sc := make(chan ServerEvent, 1)
	destroy := make(chan struct{})
	e := NewAchievementEngine(testCatalog(t), destroy, sc)
	e.LoadPlayerAchievements("alice", freshStates(testCatalog(t)))
	sc <- ServerEvent{} // channel full, nobody will drain it
	done := make(chan struct{})
	go func() { e.StartEventProcessor(); close(done) }()

	e.PublishEvent(AchievementEvent{PlayerIDs: []string{"alice"}, Key: "kill:imp", Amount: 1})
	time.Sleep(100 * time.Millisecond) // processor is now blocked on the send
	close(destroy)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("achievement processor still blocked after game destroy")
	}
}
