package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
	"uuid"

	"unnamed-game/internal/db"
	"unnamed-game/internal/game"
	"unnamed-game/internal/testutil/pgtest"

	"github.com/gorilla/websocket"
)

func TestMain(m *testing.M) {
	if os.Getenv("GAME_TEST_LOGS") != "1" {
		log.SetOutput(io.Discard)
	}
	os.Exit(m.Run())
}

// testEnv is a real server (prod mux, real Postgres test DB) behind httptest.
type testEnv struct {
	t   *testing.T
	srv *Server
	ts  *httptest.Server
	db  *db.Database
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	d := pgtest.Open(t)
	return newTestEnvOnDB(t, d)
}

func newTestEnvOnDB(t *testing.T, d *db.Database) *testEnv {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	var srv *Server
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		srv.Mux.ServeHTTP(w, r)
	}))
	host, port, _ := net.SplitHostPort(ts.Listener.Addr().String())
	var err error
	srv, err = NewServerWithDB(ctx, d, ServerConfig{Host: host, Port: port})
	if err != nil {
		cancel()
		t.Fatalf("NewServerWithDB: %v", err)
	}
	srv.RegisterRoutes("../../web")
	ts.Start()
	env := &testEnv{t: t, srv: srv, ts: ts, db: d}
	t.Cleanup(func() {
		ts.CloseClientConnections()
		ts.Close()
		env.destroyAllGames()
		cancel()
	})
	return env
}

// destroyAllGames stops every game's goroutines so tests don't leak loops.
func (e *testEnv) destroyAllGames() {
	e.srv.mu.RLock()
	var games []*game.Game
	for _, g := range e.srv.activeGames {
		games = append(games, g)
	}
	e.srv.mu.RUnlock()
	for _, g := range games {
		done := make(chan struct{})
		go func() { g.Destroy("test_cleanup"); close(done) }()
		select {
		case <-done:
		case <-time.After(7 * time.Second):
		}
	}
}

func (e *testEnv) game(id uuid.UUID) (*game.Game, bool) {
	e.srv.mu.RLock()
	defer e.srv.mu.RUnlock()
	g, ok := e.srv.activeGames[id]
	return g, ok
}

// client is one browser: an http client with its own cookie jar.
type client struct {
	env      *testEnv
	http     *http.Client
	playerID string
	userID   uuid.UUID
}

func (e *testEnv) newClient(playerID string) *client {
	e.t.Helper()
	jar, _ := cookiejar.New(nil)
	c := &client{env: e, http: &http.Client{Jar: jar, Timeout: 10 * time.Second}, playerID: playerID}
	resp := c.do("POST", "/api/users", fmt.Sprintf(`{"player_id":%q}`, playerID))
	if resp.StatusCode != http.StatusOK {
		e.t.Fatalf("create user %s: status %d", playerID, resp.StatusCode)
	}
	for _, ck := range jar.Cookies(mustURL(e.ts.URL)) {
		if ck.Name == "player_session" {
			c.userID = uuid.MustParse(strings.SplitN(ck.Value, ":", 2)[0])
		}
	}
	return c
}

func (c *client) do(method, path, body string) *http.Response {
	c.env.t.Helper()
	req, _ := http.NewRequest(method, c.env.ts.URL+path, strings.NewReader(body))
	resp, err := c.http.Do(req)
	if err != nil {
		c.env.t.Fatalf("%s %s: %v", method, path, err)
	}
	c.env.t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func decode[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	var v T
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatalf("decode %T: %v", v, err)
	}
	return v
}

func (c *client) createGame() uuid.UUID {
	c.env.t.Helper()
	resp := c.do("POST", "/api/games", "")
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		c.env.t.Fatalf("create game: %d %s", resp.StatusCode, b)
	}
	gr := decode[GameResponse](c.env.t, resp)
	return uuid.MustParse(gr.GameID)
}

func (c *client) join(id uuid.UUID) *http.Response {
	c.env.t.Helper()
	return c.do("POST", "/api/games/"+id.String()+"/join", "")
}

// wsConn wraps a websocket client with a single reader goroutine so tests can
// wait for specific messages without racing on ReadMessage.
type wsConn struct {
	conn *websocket.Conn
	msgs chan string
}

func (c *client) dial(path string) (*wsConn, error) {
	c.env.t.Helper()
	wsURL := "ws" + strings.TrimPrefix(c.env.ts.URL, "http") + path
	d := websocket.Dialer{Jar: c.http.Jar, HandshakeTimeout: 5 * time.Second}
	conn, _, err := d.Dial(wsURL, nil)
	if err != nil {
		return nil, err
	}
	w := &wsConn{conn: conn, msgs: make(chan string, 4096)}
	go func() {
		defer close(w.msgs)
		for {
			_, b, err := conn.ReadMessage()
			if err != nil {
				return
			}
			select {
			case w.msgs <- string(b):
			default: // drop frames nobody is reading
			}
		}
	}()
	c.env.t.Cleanup(func() { conn.Close() })
	return w, nil
}

func (c *client) mustDial(path string) *wsConn {
	c.env.t.Helper()
	w, err := c.dial(path)
	if err != nil {
		c.env.t.Fatalf("dial %s as %s: %v", path, c.playerID, err)
	}
	return w
}

func (w *wsConn) send(t *testing.T, s string) {
	t.Helper()
	if err := w.conn.WriteMessage(websocket.TextMessage, []byte(s)); err != nil {
		t.Fatalf("ws send %q: %v", s, err)
	}
}

// waitFor returns the first message matching pred, or fails after d.
func (w *wsConn) waitFor(t *testing.T, d time.Duration, what string, pred func(string) bool) string {
	t.Helper()
	msg, ok := w.tryWaitFor(d, pred)
	if !ok {
		select {
		case _, open := <-w.msgs:
			if !open {
				t.Fatalf("connection closed by server while waiting for %s", what)
			}
		default:
		}
		t.Fatalf("timed out after %v waiting for %s", d, what)
	}
	return msg
}

func (w *wsConn) tryWaitFor(d time.Duration, pred func(string) bool) (string, bool) {
	deadline := time.After(d)
	for {
		select {
		case m, ok := <-w.msgs:
			if !ok {
				return "", false
			}
			if pred(m) {
				return m, true
			}
		case <-deadline:
			return "", false
		}
	}
}

// closed reports whether the server closed the connection within d.
func (w *wsConn) closed(d time.Duration) bool {
	deadline := time.After(d)
	for {
		select {
		case _, ok := <-w.msgs:
			if !ok {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

func isFrame(m string) bool { return strings.HasPrefix(m, "\033[H") }

func isJSONType(typ string) func(string) bool {
	return func(m string) bool {
		var v struct {
			Type string `json:"type"`
		}
		return json.Unmarshal([]byte(m), &v) == nil && v.Type == typ
	}
}

func mustURL(s string) *url.URL { u, _ := url.Parse(s); return u }

// waitUntil polls cond until it is true or d elapses.
func waitUntil(d time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cond()
}

// activePlayers reads the game's active player ids under its lock.
func activePlayers(g *game.Game) []string {
	g.Mu.Lock()
	defer g.Mu.Unlock()
	var ids []string
	for _, p := range g.GetActivePlayers() {
		ids = append(ids, p.GetID())
	}
	return ids
}
