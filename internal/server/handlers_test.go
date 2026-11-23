package server

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"uuid"

	"unnamed-game/internal/db"
	"unnamed-game/internal/game"
	"unnamed-game/internal/testutil"

	"github.com/gorilla/websocket"
	"go.uber.org/goleak"
)

// ---------------------------------------------------------------------------
// Pure helpers (no DB)
// ---------------------------------------------------------------------------

func TestExtractSessionIDs(t *testing.T) {
	cases := []struct {
		name, cookie string
		wantErr      bool
		user, player string
	}{
		{"missing", "", true, "", ""},
		{"no separator", "abc", true, "", ""},
		{"too many parts", "a:b:c", true, "", ""},
		{"ok", "11111111-1111-1111-1111-111111111111:alice", false, "11111111-1111-1111-1111-111111111111", "alice"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			if c.cookie != "" {
				r.AddCookie(&http.Cookie{Name: "player_session", Value: c.cookie})
			}
			u, p, err := ExtractSessionIDs(r)
			if (err != nil) != c.wantErr || u != c.user || p != c.player {
				t.Fatalf("got (%q,%q,%v)", u, p, err)
			}
		})
	}
}

func TestGetPrevNext(t *testing.T) {
	next, prev := getPrevNext(make([]int, 11), 1, 10)
	if !next || prev {
		t.Errorf("11 rows on page 1: next=%v prev=%v", next, prev)
	}
	next, prev = getPrevNext(make([]int, 10), 2, 10)
	if next || !prev {
		t.Errorf("10 rows on page 2: next=%v prev=%v", next, prev)
	}
}

func TestGetCurrentUserFromPage(t *testing.T) {
	id := uuid.New()
	rows := []db.UserLeaderboardView{{UserID: uuid.New(), Rank: 1}, {UserID: id, Rank: 2}}
	got, err := getCurrentUserFromPage(id, rows)
	if err != nil || got.Rank != 2 {
		t.Fatalf("got %+v err=%v", got, err)
	}
	if _, err := getCurrentUserFromPage(uuid.New(), rows); err == nil {
		t.Errorf("missing user should error")
	}
}

func TestBuildGamePageURL(t *testing.T) {
	s := &Server{}
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	r := httptest.NewRequest("POST", "/", nil)
	if got := s.BuildGamePageURL(r, "h:1", id); got != "http://h:1/game.html?gameId="+id.String() {
		t.Errorf("plain: %s", got)
	}
	r.Header.Set("X-Forwarded-Proto", "https")
	if got := s.BuildGamePageURL(r, "h:1", id); !strings.HasPrefix(got, "https://") {
		t.Errorf("forwarded https: %s", got)
	}
	r2 := httptest.NewRequest("POST", "/", nil)
	r2.TLS = &tls.ConnectionState{}
	if got := s.BuildGamePageURL(r2, "h:1", id); !strings.HasPrefix(got, "https://") {
		t.Errorf("tls: %s", got)
	}
}

func TestEnqueueDBWriteDropsWhenFull(t *testing.T) {
	s := &Server{dbWriteChan: make(chan dbWriteTask, 2)}
	for i := 0; i < 5; i++ {
		s.enqueueDBWrite(fmt.Sprintf("task %d", i), func(context.Context) error { return nil })
	}
	if len(s.dbWriteChan) != 2 {
		t.Fatalf("queue len %d want 2", len(s.dbWriteChan))
	}
}

// ---------------------------------------------------------------------------
// HTTP handlers (real DB)
// ---------------------------------------------------------------------------

func TestEndpointsRequireSession(t *testing.T) {
	env := newTestEnv(t)
	anon := &http.Client{}
	routes := []struct{ method, path string }{
		{"GET", "/api/users/me"},
		{"GET", "/api/users/me/achievements"},
		{"GET", "/api/users/me/leaderboard"},
		{"GET", "/api/users/me/config"},
		{"PUT", "/api/users/me/config"},
		{"POST", "/api/games"},
		{"POST", "/api/games/" + uuid.New().String() + "/join"},
	}
	for _, r := range routes {
		req, _ := http.NewRequest(r.method, env.ts.URL+r.path, strings.NewReader("{}"))
		resp, err := anon.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s without cookie: %d want 401", r.method, r.path, resp.StatusCode)
		}
	}
	// Malformed user id in the cookie.
	for _, r := range routes[1:] {
		req, _ := http.NewRequest(r.method, env.ts.URL+r.path, strings.NewReader("{}"))
		req.AddCookie(&http.Cookie{Name: "player_session", Value: "not-a-uuid:alice"})
		resp, err := anon.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s %s with bad uuid: %d want 401/400", r.method, r.path, resp.StatusCode)
		}
	}
}

func TestCreateUser(t *testing.T) {
	env := newTestEnv(t)
	for _, body := range []string{"", "not json", `{"player_id":""}`} {
		resp, err := http.Post(env.ts.URL+"/api/users", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("body %q: %d want 400", body, resp.StatusCode)
		}
	}

	c := env.newClient("alice")
	if c.userID == (uuid.UUID{}) {
		t.Fatal("no session cookie set")
	}
	resp := c.do("GET", "/api/users/me", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET me: %d", resp.StatusCode)
	}
	me := decode[db.UserProfileSummary](t, resp)
	if me.PlayerID != "alice" || me.UserID != c.userID {
		t.Fatalf("me=%+v", me)
	}

	// Logging in again with the same name returns the same user.
	again := env.newClient("alice")
	if again.userID != c.userID {
		t.Errorf("second login got a different user id")
	}
}

func TestCreateUserNameTooLong(t *testing.T) {
	env := newTestEnv(t)
	resp, err := http.Post(env.ts.URL+"/api/users", "application/json", strings.NewReader(`{"player_id":"`+strings.Repeat("x", 30)+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("30-char name accepted")
	}
}

func TestConfigEndpoints(t *testing.T) {
	env := newTestEnv(t)
	c := env.newClient("alice")

	cfg := decode[db.UserConfiguration](t, c.do("GET", "/api/users/me/config", ""))
	if cfg.Keybinds["move_up"] != "w" {
		t.Fatalf("default config %+v", cfg)
	}

	bad := []string{
		`{"keybinds":{"move_up":"ww"}}`,
		`{"keybinds":{"move_up":""}}`,
		`{"keybinds":{"move_up":"x","move_down":"x"}}`,
		`not json`,
	}
	for _, b := range bad {
		if resp := c.do("PUT", "/api/users/me/config", b); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("PUT %s: %d want 400", b, resp.StatusCode)
		}
	}

	good := `{"keybinds":{"move_up":"i","move_down":"k","move_left":"j","move_right":"l","attack_enemy":"h","swap_attack":"u","quit_game":"x"},"master_volume":10,"sfx_volume":5}`
	if resp := c.do("PUT", "/api/users/me/config", good); resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT valid config: %d", resp.StatusCode)
	}
	cfg = decode[db.UserConfiguration](t, c.do("GET", "/api/users/me/config", ""))
	if cfg.Keybinds["move_up"] != "i" || cfg.MasterVol != 10 {
		t.Fatalf("config not persisted: %+v", cfg)
	}
}

// When the DB write fails, PUT /api/users/me/config still answers 200 with
// the config echoed back, so the client believes it was saved.
func TestUpdateConfigReportsDBFailure(t *testing.T) {
	testutil.KnownBug(t, "PUT /api/users/me/config returns 200 even when saving to the DB fails")
	env := newTestEnv(t)
	req, _ := http.NewRequest("PUT", env.ts.URL+"/api/users/me/config", strings.NewReader(`{"keybinds":{"move_up":"i"}}`))
	// Valid session format, but the user does not exist: the FK insert fails.
	req.AddCookie(&http.Cookie{Name: "player_session", Value: uuid.New().String() + ":ghost"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("got 200 although the config was not saved")
	}
}

func TestCreateGame(t *testing.T) {
	env := newTestEnv(t)
	c := env.newClient("alice")
	resp := c.do("POST", "/api/games", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	gr := decode[GameResponse](t, resp)
	id := uuid.MustParse(gr.GameID)
	if !strings.Contains(gr.RedirectURL, env.srv.buildServerHost()) || !strings.Contains(gr.RedirectURL, id.String()) {
		t.Errorf("redirect %q", gr.RedirectURL)
	}
	if _, ok := env.game(id); !ok {
		t.Fatal("game not registered in activeGames")
	}
	env.srv.mu.RLock()
	sess := env.srv.playerSessions["alice"]
	env.srv.mu.RUnlock()
	if sess != id {
		t.Errorf("playerSessions[alice]=%v want %v", sess, id)
	}
	host, err := env.db.FetchGameHostByGameId(context.Background(), id)
	if err != nil || host != env.srv.buildServerHost() {
		t.Errorf("db host=%q err=%v", host, err)
	}
}

func TestJoinGame(t *testing.T) {
	env := newTestEnv(t)
	alice := env.newClient("alice")
	bob := env.newClient("bob")
	id := alice.createGame()

	if resp := bob.do("POST", "/api/games/not-a-uuid/join", ""); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("bad id: %d want 400", resp.StatusCode)
	}
	if resp := bob.join(uuid.New()); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown game: %d want 404", resp.StatusCode)
	}

	resp := bob.join(id)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("join: %d", resp.StatusCode)
	}
	gr := decode[GameResponse](t, resp)
	if gr.GameID != id.String() {
		t.Errorf("join response %+v", gr)
	}
	env.srv.mu.RLock()
	sess := env.srv.playerSessions["bob"]
	env.srv.mu.RUnlock()
	if sess != id {
		t.Errorf("bob not mapped to the game")
	}
}

func TestJoinGameHostedElsewhereRedirects(t *testing.T) {
	env := newTestEnv(t)
	bob := env.newClient("bob")
	remote := uuid.NewV7()
	if err := env.db.InsertNewGame(context.Background(), remote, "other-host:9999"); err != nil {
		t.Fatal(err)
	}
	resp := bob.join(remote)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	gr := decode[GameResponse](t, resp)
	if !strings.Contains(gr.RedirectURL, "other-host:9999") {
		t.Fatalf("redirect %q should point to the hosting server", gr.RedirectURL)
	}
}

func TestJoinGameInDBButNotActive(t *testing.T) {
	env := newTestEnv(t)
	bob := env.newClient("bob")
	stale := uuid.NewV7()
	env.db.InsertNewGame(context.Background(), stale, env.srv.buildServerHost())
	if resp := bob.join(stale); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("stale game: %d want 404", resp.StatusCode)
	}
}

func TestJoinFullGameRejected(t *testing.T) {
	env := newTestEnv(t)
	host := env.newClient("p0")
	id := host.createGame()
	host.mustDial("/ws").waitFor(t, defaultWait, "first frame", isFrame)
	for i := 1; i < game.MaxPlayerCount; i++ {
		c := env.newClient(fmt.Sprintf("p%d", i))
		if resp := c.join(id); resp.StatusCode != http.StatusOK {
			t.Fatalf("join %d: %d", i, resp.StatusCode)
		}
		c.mustDial("/ws").waitFor(t, defaultWait, "first frame", isFrame)
	}
	late := env.newClient("late")
	if resp := late.join(id); resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("join full game: %d want 503", resp.StatusCode)
	}
}

// Join only checks currently CONNECTED players and reserves nothing, and the
// websocket connect path never checks capacity. Players who joined but have
// not connected yet are not counted, so any number can get in.
func TestGameCapacityEnforcedOnConnect(t *testing.T) {
	env := newTestEnv(t)
	host := env.newClient("p0")
	id := host.createGame()
	clients := []*client{host}
	for i := 1; i <= game.MaxPlayerCount+2; i++ {
		c := env.newClient(fmt.Sprintf("p%d", i))
		c.join(id)
		clients = append(clients, c)
	}
	for _, c := range clients {
		c.dial("/ws") // some may be rejected; that is fine
	}
	g, _ := env.game(id)
	// Passing case: the count never exceeds the max, so this waits the full
	// second; all dials above have completed their handshake by then.
	waitUntil(time.Second, func() bool { return len(activePlayers(g)) > game.MaxPlayerCount })
	if n := len(activePlayers(g)); n > game.MaxPlayerCount {
		t.Fatalf("%d players connected, max is %d", n, game.MaxPlayerCount)
	}
}

// A player who passed the join pre-check but finds the game full when the
// websocket connects gets a close frame with code 4001 and the reason, which
// web/game.js shows in the lobby.
func TestRejectedConnectSendsCloseReason(t *testing.T) {
	env := newTestEnv(t)
	host := env.newClient("p0")
	id := host.createGame()
	var late []*client
	for i := 1; i <= game.MaxPlayerCount; i++ {
		c := env.newClient(fmt.Sprintf("p%d", i))
		if resp := c.join(id); resp.StatusCode != http.StatusOK {
			t.Fatalf("join %d: %d", i, resp.StatusCode)
		}
		late = append(late, c)
	}
	host.mustDial("/ws").waitFor(t, defaultWait, "host frame", isFrame)
	for _, c := range late[:game.MaxPlayerCount-1] {
		c.mustDial("/ws").waitFor(t, defaultWait, "frame", isFrame)
	}

	ws := late[len(late)-1].mustDial("/ws") // game is full now
	if !ws.closed(defaultWait) {
		t.Fatal("server did not close the rejected connection")
	}
	var ce *websocket.CloseError
	if !errors.As(ws.readErr, &ce) || ce.Code != CloseCodeJoinRejected {
		t.Fatalf("close error = %v, want code %d", ws.readErr, CloseCodeJoinRejected)
	}
	if !strings.Contains(ce.Text, "full") {
		t.Errorf("close reason %q should say the game is full", ce.Text)
	}
}

// HandleCreateGame starts the game's goroutines (NewGame) before writing to
// the DB. If the insert fails the game is dropped but its loop, idle ticker
// and achievement processor keep running forever.
func TestCreateGameDBFailureDoesNotLeakGoroutines(t *testing.T) {
	testutil.KnownBug(t, "HandleCreateGame leaks the game's goroutines when the DB insert fails")
	env := newTestEnv(t)
	c := env.newClient("alice")
	var cookie *http.Cookie
	for _, ck := range c.http.Jar.Cookies(mustURL(env.ts.URL)) {
		cookie = ck
	}
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // the DB insert fails with context canceled
	req := httptest.NewRequest("POST", "/api/games", nil).WithContext(ctx)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	env.srv.HandleCreateGame(rec, req)
	if rec.Code != http.StatusInternalServerError {
		body, _ := io.ReadAll(rec.Body)
		t.Fatalf("status %d (%s), want 500", rec.Code, body)
	}
}

func TestLeaderboardEndpoint(t *testing.T) {
	env := newTestEnv(t)
	for i := 0; i < 12; i++ {
		c := env.newClient(fmt.Sprintf("u%02d", i))
		env.db.Pool().Exec(context.Background(), "UPDATE user_stats SET highest_score=$2 WHERE user_id=$1", c.userID, 100-i)
	}
	me := env.newClient("me") // score 0, rank 13

	page1 := decode[LeaderboardReponse](t, me.do("GET", "/api/leaderboard", ""))
	if page1.CurrentPage != 1 || len(page1.UsersOnPage) != LeaderBoardPageSize || !page1.NextPageAvailable || page1.PreviousPageAvailable {
		t.Fatalf("page 1: %+v", page1)
	}
	page2 := decode[LeaderboardReponse](t, me.do("GET", "/api/leaderboard?page=2", ""))
	if page2.CurrentPage != 2 || len(page2.UsersOnPage) != 3 || page2.NextPageAvailable || !page2.PreviousPageAvailable {
		t.Fatalf("page 2: %+v", page2)
	}
	junk := decode[LeaderboardReponse](t, me.do("GET", "/api/leaderboard?page=-4", ""))
	if junk.CurrentPage != 1 {
		t.Errorf("invalid page should fall back to 1, got %d", junk.CurrentPage)
	}

	self := decode[SelfLeaderboardReponse](t, me.do("GET", "/api/users/me/leaderboard", ""))
	if self.CurrentUser.Rank != 13 || self.CurrentPage != 2 {
		t.Fatalf("self leaderboard: rank=%d page=%d", self.CurrentUser.Rank, self.CurrentPage)
	}
}

func TestAchievementsEndpoint(t *testing.T) {
	env := newTestEnv(t)
	c := env.newClient("alice")
	list := decode[[]game.AchProgressDTO](t, c.do("GET", "/api/users/me/achievements", ""))
	if len(list) != 5 {
		t.Fatalf("got %d achievements want 5", len(list))
	}
}

func TestStaticFilesServed(t *testing.T) {
	env := newTestEnv(t)
	resp, err := http.Get(env.ts.URL + "/lobby.html")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("lobby.html: %d", resp.StatusCode)
	}
}
