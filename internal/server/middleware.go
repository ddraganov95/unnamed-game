package server

import (
	"bufio"
	"context"
	"log/slog"
	"net"
	"net/http"
	"time"

	"uuid"
)

const (
	requestIDHeader    = "X-Request-ID"
	maxRequestIDLength = 64
	// staticRoutePattern is the file-server route ("/"). It also catches
	// unknown paths, so only successful responses on it are treated as static.
	staticRoutePattern = "/"
	unmatchedRoute     = "unmatched"
)

type requestIDKey struct{}

// Handler returns the router wrapped with request logging. The server must
// serve this, not Mux, so every request gets one log line.
func (s *Server) Handler() http.Handler {
	return s.withRequestLogging(s.Mux)
}

func (s *Server) withRequestLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		id := requestIDFrom(r.Header.Get(requestIDHeader))
		w.Header().Set(requestIDHeader, id)
		r = r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id))

		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK, req: r, start: start}
		next.ServeHTTP(rec, r)

		// A websocket is logged when it is upgraded (see Hijack). Its
		// handler blocks for the whole session, so logging here would report
		// the session length as the request duration.
		if !rec.hijacked {
			logRequest(r, rec.status, time.Since(start), "request")
		}
	})
}

// statusRecorder captures the status code and passes through the optional
// interfaces the websocket upgrade and static files rely on.
type statusRecorder struct {
	http.ResponseWriter
	req      *http.Request
	start    time.Time
	status   int
	written  bool
	hijacked bool
}

func (rec *statusRecorder) WriteHeader(code int) {
	if !rec.written {
		rec.status = code
		rec.written = true
	}
	rec.ResponseWriter.WriteHeader(code)
}

func (rec *statusRecorder) Write(b []byte) (int, error) {
	rec.written = true
	return rec.ResponseWriter.Write(b)
}

func (rec *statusRecorder) Flush() {
	if f, ok := rec.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (rec *statusRecorder) Unwrap() http.ResponseWriter {
	return rec.ResponseWriter
}

// Hijack is called by the websocket upgrade. The connection is then owned by
// the handler, so the request is logged here with the handshake duration.
func (rec *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := rec.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	conn, rw, err := h.Hijack()
	if err == nil {
		rec.hijacked = true
		rec.status = http.StatusSwitchingProtocols
		logRequest(rec.req, rec.status, time.Since(rec.start), "websocket upgraded")
	}
	return conn, rw, err
}

func logRequest(r *http.Request, status int, duration time.Duration, msg string) {
	attrs := []any{
		"request_id", requestIDOf(r.Context()),
		"method", r.Method,
		"route", routeOf(r),
		"status", status,
		"duration_ms", duration.Milliseconds(),
		"client_ip", clientIP(r),
	}
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		attrs = append(attrs, "forwarded_for", fwd)
	}
	if userID, _, err := parseSessionCookie(r); err == nil {
		attrs = append(attrs, "user_id", userID)
	}
	slog.Default().Log(r.Context(), requestLevel(r, status), msg, attrs...)
}

// requestLevel: 5xx is an error, successful static files are debug, and
// everything else (including 4xx) is info.
func requestLevel(r *http.Request, status int) slog.Level {
	switch {
	case status >= http.StatusInternalServerError:
		return slog.LevelError
	case routeOf(r) == staticRoutePattern && status < http.StatusBadRequest:
		return slog.LevelDebug
	default:
		return slog.LevelInfo
	}
}

func routeOf(r *http.Request) string {
	if r.Pattern == "" {
		return unmatchedRoute
	}
	return r.Pattern
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// requestIDFrom keeps a client-supplied ID when it is short and made of safe
// characters, and generates a new one otherwise.
func requestIDFrom(incoming string) string {
	if incoming != "" && len(incoming) <= maxRequestIDLength && isSafeRequestID(incoming) {
		return incoming
	}
	return uuid.NewV7().String()
}

func isSafeRequestID(s string) bool {
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_', c == '.':
		default:
			return false
		}
	}
	return true
}

func requestIDOf(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// requestLogger returns the default logger tagged with the request's ID, so
// handler errors can be matched to the request line.
func requestLogger(ctx context.Context) *slog.Logger {
	return slog.Default().With("request_id", requestIDOf(ctx))
}
