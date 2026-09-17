package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"campuspulse/internal/apperr"
	"campuspulse/internal/ids"
	"campuspulse/internal/model"
)

// requestInfo collects details about a request for its log line.
type requestInfo struct {
	id     string
	userID string
	role   string
}

type infoKey struct{}

func info(ctx context.Context) *requestInfo {
	if i, ok := ctx.Value(infoKey{}).(*requestInfo); ok {
		return i
	}
	return &requestInfo{}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// logRequests writes one structured log line per request. Logs never include
// request bodies, passwords, or tokens.
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		id := r.Header.Get("X-Request-Id")
		if !requestIDPattern.MatchString(id) {
			id = ids.New("req")
		}
		w.Header().Set("X-Request-Id", id)
		ri := &requestInfo{id: id}
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r.WithContext(context.WithValue(r.Context(), infoKey{}, ri)))

		level := slog.LevelInfo
		switch {
		case rec.status >= 500:
			level = slog.LevelError
		case r.URL.Path == "/health" || r.Method == http.MethodOptions:
			level = slog.LevelDebug
		}
		s.log.LogAttrs(r.Context(), level, "request",
			slog.String("request_id", id),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", rec.status),
			slog.Float64("duration_ms", float64(time.Since(start).Microseconds())/1000),
			slog.String("role", ri.role),
			slog.String("user_id", ri.userID),
		)
	})
}

func (s *Server) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				s.log.ErrorContext(r.Context(), "panic", "value", v, "request_id", info(r.Context()).id)
				s.writeError(w, r, errors.New("panic"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// cors lets the dashboard call the API from the allowed origins. Entries can be
// exact origins, "*", or wildcard subdomains such as "https://*.lovable.app".
func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && s.originAllowed(origin) {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Add("Vary", "Origin")
			h.Set("Access-Control-Expose-Headers", "X-Request-Id")
			if r.Method == http.MethodOptions {
				h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, OPTIONS")
				h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Device-Key, X-Request-Id")
				h.Set("Access-Control-Max-Age", "600")
			}
		}
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) originAllowed(origin string) bool {
	for _, allowed := range s.origins {
		if allowed == "*" || allowed == origin {
			return true
		}
		scheme, host, ok := strings.Cut(allowed, "://*.")
		if ok && strings.HasPrefix(origin, scheme+"://") && strings.HasSuffix(origin, "."+host) {
			return true
		}
	}
	return false
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// user requires a signed-in user with one of the allowed roles.
func (s *Server) user(allowed []model.Role, h func(http.ResponseWriter, *http.Request, model.User)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, err := s.svc.Identify(r)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		ri := info(r.Context())
		ri.userID, ri.role = u.ID, string(u.Role)
		if !slices.Contains(allowed, u.Role) {
			s.writeError(w, r, apperr.Forbidden())
			return
		}
		h(w, r, u)
	})
}

// device requires a sensor API key in the X-Device-Key header. Keys are compared
// as SHA-256 digests in constant time, so timing doesn't leak key contents.
func (s *Server) device(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		given := r.Header.Get("X-Device-Key")
		digest := sha256.Sum256([]byte(given))
		match := 0
		for _, k := range s.deviceKeys {
			match |= subtle.ConstantTimeCompare(digest[:], k[:])
		}
		if given == "" || match != 1 {
			s.writeError(w, r, apperr.Unauthorized("Missing or invalid device key"))
			return
		}
		info(r.Context()).role = "device"
		h(w, r)
	})
}

// limiter allows max attempts per key in each fixed window. It lives in memory,
// so each server (or Lambda instance) counts separately; API Gateway throttling
// is the shared limit in the cloud.
type limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	hits   map[string]*windowCount
	now    func() time.Time
}

type windowCount struct {
	start time.Time
	count int
}

func newLimiter(max int, window time.Duration) *limiter {
	return &limiter{max: max, window: window, hits: map[string]*windowCount{}, now: time.Now}
}

func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if len(l.hits) > 10000 {
		for k, w := range l.hits {
			if now.Sub(w.start) >= l.window {
				delete(l.hits, k)
			}
		}
	}
	w, ok := l.hits[key]
	if !ok || now.Sub(w.start) >= l.window {
		l.hits[key] = &windowCount{start: now, count: 1}
		return true
	}
	if w.count >= l.max {
		return false
	}
	w.count++
	return true
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

const maxBodyBytes = 64 << 10

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		var tooLarge *http.MaxBytesError
		switch {
		case errors.As(err, &tooLarge):
			return apperr.Invalid("request body is too large")
		case errors.Is(err, io.EOF):
			return apperr.Invalid("request body is empty")
		}
		return apperr.Invalid("request body is not valid: %s", strings.TrimPrefix(err.Error(), "json: "))
	}
	if dec.More() {
		return apperr.Invalid("request body must contain a single JSON object")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) writeError(w http.ResponseWriter, r *http.Request, err error) {
	var ae *apperr.Error
	if !errors.As(err, &ae) {
		s.log.ErrorContext(r.Context(), "internal error", "error", err, "request_id", info(r.Context()).id)
		ae = &apperr.Error{Status: http.StatusInternalServerError, Code: "internal", Message: "Something went wrong. Try again later."}
	}
	writeJSON(w, ae.Status, model.ErrorResponse{Error: model.ErrorBody{Code: ae.Code, Message: ae.Message}})
}
