package server

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/websocket"
)

// syncBuffer is a log sink that other goroutines may write to concurrently.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// captureLogs routes the default logger into a buffer for the rest of the test.
func captureLogs(t *testing.T, level slog.Level) *syncBuffer {
	t.Helper()
	buf := &syncBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: level})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

// linesFor returns the parsed log records that carry the given request ID.
func linesFor(t *testing.T, buf *syncBuffer, requestID string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("log line is not JSON: %v: %s", err, line)
		}
		if rec["request_id"] == requestID {
			out = append(out, rec)
		}
	}
	return out
}

func newLoggedMux(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	s := &Server{Mux: http.NewServeMux()}
	s.Mux.HandleFunc("GET /ping", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("pong"))
	})
	s.Mux.HandleFunc("GET /api/games/{id}/join", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Game not found", http.StatusNotFound)
	})
	s.Mux.HandleFunc("GET /boom", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	s.Mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Write([]byte("static page"))
			return
		}
		http.NotFound(w, r)
	})
	return s, s.Handler()
}

func serve(h http.Handler, method, target, requestID, remote string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	req.RemoteAddr = remote
	if requestID != "" {
		req.Header.Set(requestIDHeader, requestID)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRequestLogsOneLineWithRouteStatusAndIP(t *testing.T) {
	buf := captureLogs(t, slog.LevelInfo)
	_, h := newLoggedMux(t)

	serve(h, http.MethodGet, "/api/games/abc/join", "req-join-1", "203.0.113.7:5555")

	lines := linesFor(t, buf, "req-join-1")
	if len(lines) != 1 {
		t.Fatalf("expected 1 request line, got %d: %s", len(lines), buf.String())
	}
	rec := lines[0]
	if rec["msg"] != "request" || rec["level"] != "INFO" {
		t.Errorf("msg/level = %v/%v, want request/INFO", rec["msg"], rec["level"])
	}
	if rec["route"] != "GET /api/games/{id}/join" {
		t.Errorf("route = %v, want the route pattern", rec["route"])
	}
	if rec["status"] != float64(http.StatusNotFound) {
		t.Errorf("status = %v, want 404", rec["status"])
	}
	if rec["client_ip"] != "203.0.113.7" {
		t.Errorf("client_ip = %v, want 203.0.113.7", rec["client_ip"])
	}
	if _, ok := rec["duration_ms"]; !ok {
		t.Errorf("duration_ms missing: %v", rec)
	}
}

func TestRequestIDIsEchoedWhenSafeAndReplacedWhenNot(t *testing.T) {
	captureLogs(t, slog.LevelInfo)
	_, h := newLoggedMux(t)

	ok := serve(h, http.MethodGet, "/ping", "abc-123.x_Y", "192.0.2.1:1")
	if got := ok.Header().Get(requestIDHeader); got != "abc-123.x_Y" {
		t.Errorf("safe request ID not echoed: got %q", got)
	}

	bad := serve(h, http.MethodGet, "/ping", "has spaces and \"quotes\"", "192.0.2.1:1")
	if got := bad.Header().Get(requestIDHeader); got == "" || strings.ContainsAny(got, " \"") {
		t.Errorf("unsafe request ID should be replaced, got %q", got)
	}

	long := serve(h, http.MethodGet, "/ping", strings.Repeat("a", maxRequestIDLength+1), "192.0.2.1:1")
	if got := long.Header().Get(requestIDHeader); len(got) > maxRequestIDLength {
		t.Errorf("overlong request ID should be replaced, got length %d", len(got))
	}
}

func TestStaticSuccessIsDebugOnlyButMissingPageIsInfo(t *testing.T) {
	buf := captureLogs(t, slog.LevelInfo)
	_, h := newLoggedMux(t)

	serve(h, http.MethodGet, "/", "req-static", "192.0.2.1:1")
	if lines := linesFor(t, buf, "req-static"); len(lines) != 0 {
		t.Errorf("successful static request should not appear at info: %v", lines)
	}

	serve(h, http.MethodGet, "/api/auth/me", "req-missing", "192.0.2.1:1")
	lines := linesFor(t, buf, "req-missing")
	if len(lines) != 1 || lines[0]["status"] != float64(http.StatusNotFound) {
		t.Errorf("a 404 on the file-server route should be logged at info: %v", lines)
	}
}

func TestServerErrorIsLoggedAtErrorLevel(t *testing.T) {
	buf := captureLogs(t, slog.LevelInfo)
	_, h := newLoggedMux(t)

	serve(h, http.MethodGet, "/boom", "req-500", "192.0.2.1:1")

	lines := linesFor(t, buf, "req-500")
	if len(lines) != 1 || lines[0]["level"] != "ERROR" {
		t.Errorf("a 500 should be logged at ERROR: %v", lines)
	}
}

func TestWebSocketUpgradeIsLoggedOnceAtUpgrade(t *testing.T) {
	buf := captureLogs(t, slog.LevelInfo)
	s := &Server{
		Mux:      http.NewServeMux(),
		Upgrader: websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }},
	}
	s.Mux.HandleFunc("GET /ws-test", func(w http.ResponseWriter, r *http.Request) {
		conn, err := s.Upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		conn.Close()
	})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	hdr := http.Header{}
	hdr.Set(requestIDHeader, "ws-1")
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/ws-test", hdr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	conn.Close()

	lines := linesFor(t, buf, "ws-1")
	if len(lines) != 1 {
		t.Fatalf("expected exactly one line for the websocket, got %d: %v", len(lines), lines)
	}
	if lines[0]["msg"] != "websocket upgraded" || lines[0]["status"] != float64(http.StatusSwitchingProtocols) {
		t.Errorf("websocket line = %v, want msg 'websocket upgraded' and status 101", lines[0])
	}
}
