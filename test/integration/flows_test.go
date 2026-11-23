//go:build integration

package integration

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"testing"
	"strings"
	"time"

	"unnamed-game/internal/server"
	"unnamed-game/internal/testutil"

	"go.uber.org/goleak"
)

// One player: login, create, play, quit. Checks everything that lands in
// the DB.
func TestSinglePlayerFullSession(t *testing.T) {
	d := openDB(t)
	n := startNode(t, d)
	alice := n.login("alice")
	gr := alice.createGame()
	ws := alice.dial("/ws")
	ws.waitFor(t, "first frame", isFrame)

	for _, k := range []string{"d", "d", "s", "f", "e", "a"} {
		ws.key(t, k)
	}
	ws.key(t, "q")
	ws.waitFor(t, "session summary", jsonType("session_summary"))

	ctx := context.Background()
	deadline := time.Now().Add(wait)
	var status string
	for time.Now().Before(deadline) {
		d.Pool().QueryRow(ctx, "SELECT status FROM games WHERE game_id=$1", gr.GameID).Scan(&status)
		if status == "inactive" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if status != "inactive" {
		t.Fatalf("game status %q after quit", status)
	}
	var totalLevels, events int
	d.Pool().QueryRow(ctx, "SELECT total_levels FROM games_stats WHERE game_id=$1", gr.GameID).Scan(&totalLevels)
	d.Pool().QueryRow(ctx, "SELECT count(*) FROM game_events WHERE game_id=$1", gr.GameID).Scan(&events)
	if totalLevels < 1 || events == 0 {
		t.Errorf("games_stats total_levels=%d, game_events=%d", totalLevels, events)
	}
}

// Two players in one game: both see frames, game chat reaches both, global
// chat typed in-game reaches a lobby user and the DB.
func TestTwoPlayersChat(t *testing.T) {
	d := openDB(t)
	n := startNode(t, d)
	alice := n.login("alice")
	bob := n.login("bob")
	carol := n.login("carol") // sits in the lobby

	gr := alice.createGame()
	if code, _ := bob.join(gr.GameID); code != http.StatusOK {
		t.Fatalf("bob join: %d", code)
	}
	wa := alice.dial("/ws")
	wb := bob.dial("/ws")
	lobby := carol.dial("/ws/global-chat")
	wa.waitFor(t, "alice frame", isFrame)
	wb.waitFor(t, "bob frame", isFrame)

	// Game chat (default channel).
	wa.key(t, "\r")
	wa.typeText(t, "hi bob")
	wa.key(t, "\r")
	wb.waitFor(t, "game chat on bob's screen", frameContains("[alice]: hi bob"))
	wa.waitFor(t, "game chat on alice's screen", frameContains("[alice]: hi bob"))

	// Global chat: "/2" + space switches channel.
	time.Sleep(300 * time.Millisecond) // LISTEN connection is established asynchronously
	wb.key(t, "\r")
	wb.typeText(t, "/2 hello world")
	wb.key(t, "\r")
	lobby.waitFor(t, "global chat in the lobby", func(m string) bool { return m == "[bob]: hello world" })

	var rows int
	d.Pool().QueryRow(context.Background(), "SELECT count(*) FROM global_chat_messages WHERE message='hello world'").Scan(&rows)
	if rows != 1 {
		t.Errorf("global chat rows=%d want 1", rows)
	}
}

// An achievement unlocked in the DB is announced to the lobby through
// NOTIFY -> listener -> broadcast.
func TestAchievementAnnouncementReachesLobby(t *testing.T) {
	d := openDB(t)
	n := startNode(t, d)
	alice := n.login("alice")
	lobby := alice.dial("/ws/global-chat")
	time.Sleep(300 * time.Millisecond)

	var userID, achID string
	ctx := context.Background()
	d.Pool().QueryRow(ctx, "SELECT user_id FROM users WHERE player_id='alice'").Scan(&userID)
	d.Pool().QueryRow(ctx, "SELECT achievement_id FROM achievements WHERE code='gbl_slr_aprntc'").Scan(&achID)
	if _, err := d.Pool().Exec(ctx, "SELECT player_achievement_unlock($1,$2)", userID, achID); err != nil {
		t.Fatal(err)
	}
	lobby.waitFor(t, "achievement announcement", func(m string) bool {
		return m == "[Achievement]: alice unlocked Goblin Slayer Apprentice!"
	})
}

// Two servers share one DB. Joining a game hosted on the other server
// returns a redirect to that server, where the join then succeeds.
func TestMultiServerRouting(t *testing.T) {
	d := openDB(t)
	s1 := startNode(t, d)
	s2 := startNode(t, d)

	alice := s1.login("alice")
	gr := alice.createGame()
	alice.dial("/ws").waitFor(t, "alice frame", isFrame)

	bobOn2 := s2.login("bob")
	code, redirect := bobOn2.join(gr.GameID)
	if code != http.StatusOK {
		t.Fatalf("join via s2: %d", code)
	}
	if hostOf(redirect.RedirectURL) != hostOf(s1.ts.URL) {
		t.Fatalf("redirect %q should point to s1 (%s)", redirect.RedirectURL, s1.ts.URL)
	}

	bobOn1 := s1.login("bob")
	if code, _ := bobOn1.join(gr.GameID); code != http.StatusOK {
		t.Fatalf("join on s1: %d", code)
	}
	bobOn1.dial("/ws").waitFor(t, "bob frame on s1", isFrame)
}

// Shutting the server down (Ctrl+C) stops HTTP and the DB listener without
// leaking goroutines.
func TestGracefulShutdown(t *testing.T) {
	d := openDB(t)
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent(),
		goleak.IgnoreTopFunction("github.com/jackc/pgx/v5/pgxpool.(*Pool).backgroundHealthCheck"),
		goleak.IgnoreTopFunction("github.com/jackc/puddle/v2.(*Pool).Acquire.func1"))

	l, _ := net.Listen("tcp", "127.0.0.1:0")
	_, port, _ := net.SplitHostPort(l.Addr().String())
	l.Close()

	ctx, cancel := context.WithCancel(context.Background())
	srv, err := server.NewServerWithDB(ctx, d, server.ServerConfig{Host: "127.0.0.1", Port: port})
	if err != nil {
		t.Fatal(err)
	}
	srv.RegisterRoutes("../../web")
	errc := make(chan error, 1)
	go func() { errc <- srv.Run(ctx) }()

	ok := false
	for i := 0; i < 50 && !ok; i++ {
		resp, err := http.Get("http://127.0.0.1:" + port + "/api/leaderboard")
		if err == nil {
			resp.Body.Close()
			ok = true
		} else {
			time.Sleep(50 * time.Millisecond)
		}
	}
	if !ok {
		t.Fatal("server never came up")
	}
	cancel()
	select {
	case err := <-errc:
		if err != nil {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	time.Sleep(200 * time.Millisecond)
}

// On shutdown the active games are simply abandoned: their goroutines keep
// running until the process exits and their DB rows stay 'active' forever.
func TestShutdownFinalizesActiveGames(t *testing.T) {
	testutil.KnownBug(t, "server shutdown does not end active games; their DB rows stay 'active'")
	d := openDB(t)
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	_, port, _ := net.SplitHostPort(l.Addr().String())
	l.Close()
	ctx, cancel := context.WithCancel(context.Background())
	srv, err := server.NewServerWithDB(ctx, d, server.ServerConfig{Host: "127.0.0.1", Port: port})
	if err != nil {
		t.Fatal(err)
	}
	srv.RegisterRoutes("../../web")
	errc := make(chan error, 1)
	go func() { errc <- srv.Run(ctx) }()
	time.Sleep(200 * time.Millisecond)

	// Create a game through the real HTTP API.
	base := "http://127.0.0.1:" + port
	resp, err := http.Post(base+"/api/users", "application/json", strings.NewReader(`{"player_id":"alice"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	req, _ := http.NewRequest("POST", base+"/api/games", nil)
	for _, c := range resp.Cookies() {
		req.AddCookie(c)
	}
	resp, err = http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("create game: %v %v", err, resp)
	}
	resp.Body.Close()

	cancel()
	<-errc
	time.Sleep(500 * time.Millisecond)
	var active int
	d.Pool().QueryRow(context.Background(), "SELECT count(*) FROM games WHERE status='active'").Scan(&active)
	if active != 0 {
		t.Fatalf("%d game(s) still 'active' in the DB after shutdown", active)
	}
}

// Soak: many users in many games doing random things concurrently.
// Opt-in: SOAK_SECONDS=30 go test -tags integration -run Soak ./test/integration/
func TestSoak(t *testing.T) {
	secs, _ := strconv.Atoi(os.Getenv("SOAK_SECONDS"))
	if secs <= 0 {
		t.Skip("set SOAK_SECONDS to run the soak test")
	}
	d := openDB(t)
	n := startNode(t, d)
	const players = 40
	var wg sync.WaitGroup
	var mu sync.Mutex
	var gameIDs []string
	errs := make(chan error, players)
	stop := time.Now().Add(time.Duration(secs) * time.Second)

	keys := []string{"w", "a", "s", "d", "f", "e", " ", "c"}
	for i := 0; i < players; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					errs <- fmt.Errorf("player %d: %v", i, r)
				}
			}()
			p := n.login(fmt.Sprintf("soak%02d", i))
			r := rand.New(rand.NewSource(int64(i)))
			for time.Now().Before(stop) {
				var gid string
				mu.Lock()
				if len(gameIDs) > 0 && r.Intn(2) == 0 {
					gid = gameIDs[r.Intn(len(gameIDs))]
				}
				mu.Unlock()
				if gid != "" {
					if code, _ := p.join(gid); code != http.StatusOK {
						gid = ""
					}
				}
				if gid == "" {
					gr := p.createGame()
					mu.Lock()
					gameIDs = append(gameIDs, gr.GameID)
					mu.Unlock()
				}
				ws := p.dial("/ws")
				for j := 0; j < 20+r.Intn(40); j++ {
					if err := ws.conn.WriteMessage(1, []byte(keys[r.Intn(len(keys))])); err != nil {
						break
					}
					time.Sleep(time.Duration(10+r.Intn(40)) * time.Millisecond)
				}
				if r.Intn(2) == 0 {
					ws.conn.WriteMessage(1, []byte("q"))
					time.Sleep(100 * time.Millisecond)
				}
				ws.conn.Close()
			}
		}(i)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Duration(secs)*time.Second + 60*time.Second):
		t.Fatal("soak clients did not finish (server deadlock?)")
	}
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	// The server must still answer.
	resp, err := http.Get(n.ts.URL + "/api/leaderboard")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("server unhealthy after soak: %v", err)
	}
	resp.Body.Close()
}
