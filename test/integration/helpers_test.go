//go:build integration

// Package integration runs black-box end-to-end tests: real server(s) with
// the production mux, real Postgres (TEST_DB_NAME), real game loops and
// tickers, driven only through HTTP and websockets like a browser would.
//
//	TEST_DB_NAME=terminal_dungeon_test go test -tags integration ./test/integration/
package integration

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

	"unnamed-game/internal/db"
	"unnamed-game/internal/server"
	"unnamed-game/internal/testutil/pgtest"

	"github.com/gorilla/websocket"
)

const wait = 5 * time.Second

func TestMain(m *testing.M) {
	if os.Getenv("GAME_TEST_LOGS") != "1" {
		log.SetOutput(io.Discard)
	}
	os.Exit(m.Run())
}

type node struct {
	t   *testing.T
	srv *server.Server
	ts  *httptest.Server
}

// startNode starts one game server on the shared test database.
func startNode(t *testing.T, d *db.Database) *node {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	var srv *server.Server
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		srv.Handler().ServeHTTP(w, r)
	}))
	host, port, _ := net.SplitHostPort(ts.Listener.Addr().String())
	var err error
	srv, err = server.NewServerWithDB(ctx, d, server.ServerConfig{Host: host, Port: port})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	srv.RegisterRoutes("../../web")
	ts.Start()
	t.Cleanup(func() {
		ts.CloseClientConnections()
		ts.Close()
		cancel()
	})
	return &node{t: t, srv: srv, ts: ts}
}

type player struct {
	n    *node
	name string
	http *http.Client
}

func (n *node) login(name string) *player {
	n.t.Helper()
	jar, _ := cookiejar.New(nil)
	p := &player{n: n, name: name, http: &http.Client{Jar: jar, Timeout: 10 * time.Second}}
	resp := p.post("/api/users", fmt.Sprintf(`{"player_id":%q}`, name))
	if resp.StatusCode != http.StatusOK {
		n.t.Fatalf("login %s: %d", name, resp.StatusCode)
	}
	return p
}

func (p *player) post(path, body string) *http.Response {
	p.n.t.Helper()
	resp, err := p.http.Post(p.n.ts.URL+path, "application/json", strings.NewReader(body))
	if err != nil {
		p.n.t.Fatalf("POST %s: %v", path, err)
	}
	p.n.t.Cleanup(func() { resp.Body.Close() })
	return resp
}

type gameResp struct {
	GameID      string `json:"game_id"`
	RedirectURL string `json:"redirect_url"`
}

func (p *player) createGame() gameResp {
	p.n.t.Helper()
	resp := p.post("/api/games", "")
	if resp.StatusCode != http.StatusOK {
		p.n.t.Fatalf("%s create game: %d", p.name, resp.StatusCode)
	}
	var gr gameResp
	json.NewDecoder(resp.Body).Decode(&gr)
	return gr
}

func (p *player) join(gameID string) (int, gameResp) {
	p.n.t.Helper()
	resp := p.post("/api/games/"+gameID+"/join", "")
	var gr gameResp
	json.NewDecoder(resp.Body).Decode(&gr)
	return resp.StatusCode, gr
}

type socket struct {
	conn *websocket.Conn
	msgs chan string
}

func (p *player) dial(path string) *socket {
	p.n.t.Helper()
	u := "ws" + strings.TrimPrefix(p.n.ts.URL, "http") + path
	d := websocket.Dialer{Jar: p.http.Jar, HandshakeTimeout: wait}
	conn, _, err := d.Dial(u, nil)
	if err != nil {
		p.n.t.Fatalf("%s dial %s: %v", p.name, path, err)
	}
	s := &socket{conn: conn, msgs: make(chan string, 4096)}
	go func() {
		defer close(s.msgs)
		for {
			_, b, err := conn.ReadMessage()
			if err != nil {
				return
			}
			select {
			case s.msgs <- string(b):
			default:
			}
		}
	}()
	p.n.t.Cleanup(func() { conn.Close() })
	return s
}

// key sends one key press and waits a little more than one game tick, so
// consecutive keys are processed in separate ticks like real typing.
func (s *socket) key(t *testing.T, k string) {
	t.Helper()
	if err := s.conn.WriteMessage(websocket.TextMessage, []byte(k)); err != nil {
		t.Fatalf("send %q: %v", k, err)
	}
	time.Sleep(45 * time.Millisecond)
}

func (s *socket) typeText(t *testing.T, text string) {
	t.Helper()
	for _, r := range text {
		s.key(t, string(r))
	}
}

func (s *socket) waitFor(t *testing.T, what string, pred func(string) bool) string {
	t.Helper()
	deadline := time.After(wait)
	for {
		select {
		case m, ok := <-s.msgs:
			if !ok {
				t.Fatalf("socket closed while waiting for %s", what)
			}
			if pred(m) {
				return m
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

// waitClosed drains the socket until the server closes it.
func (s *socket) waitClosed(t *testing.T, what string) {
	t.Helper()
	deadline := time.After(wait)
	for {
		select {
		case _, ok := <-s.msgs:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

func isFrame(m string) bool { return strings.HasPrefix(m, "\033[H") }

func frameContains(s string) func(string) bool {
	return func(m string) bool { return isFrame(m) && strings.Contains(m, s) }
}

func jsonType(typ string) func(string) bool {
	return func(m string) bool {
		var v struct {
			Type string `json:"type"`
		}
		return json.Unmarshal([]byte(m), &v) == nil && v.Type == typ
	}
}

func hostOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Host
}

func openDB(t *testing.T) *db.Database { return pgtest.Open(t) }
