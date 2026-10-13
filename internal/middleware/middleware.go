// Package middleware holds net/http middleware: request logging, panic
// recovery, session resolution, role checks and JSON-only CSRF protection.
package middleware

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/bmurray/alexandria/internal/auth"
)

type ctxKey int

const (
	userKey ctxKey = iota
	sessionKey
	tokenKey
)

// Authenticator resolves a session cookie token.
type Authenticator interface {
	CookieName() string
	Authenticate(ctx context.Context, token string) (*auth.User, *auth.Session, error)
}

// UserFrom returns the authenticated user, if any.
func UserFrom(ctx context.Context) *auth.User {
	u, _ := ctx.Value(userKey).(*auth.User)
	return u
}

// SessionFrom returns the current session, if any.
func SessionFrom(ctx context.Context) *auth.Session {
	s, _ := ctx.Value(sessionKey).(*auth.Session)
	return s
}

// TokenFrom returns the raw session cookie token, if any.
func TokenFrom(ctx context.Context) string {
	t, _ := ctx.Value(tokenKey).(string)
	return t
}

// WithUser attaches a user to ctx (tests).
func WithUser(ctx context.Context, u *auth.User) context.Context {
	return context.WithValue(ctx, userKey, u)
}

// Session resolves the session cookie on every request. It never rejects;
// use RequireRole for that.
func Session(a Authenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, err := r.Cookie(a.CookieName())
			if err != nil || c.Value == "" {
				next.ServeHTTP(w, r)
				return
			}
			ctx := context.WithValue(r.Context(), tokenKey, c.Value)
			if u, s, err := a.Authenticate(ctx, c.Value); err == nil {
				ctx = context.WithValue(ctx, userKey, u)
				ctx = context.WithValue(ctx, sessionKey, s)
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// RequireRole returns 401 when nobody is signed in and 403 when the user's
// role is below min.
func RequireRole(min auth.Role, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := UserFrom(r.Context())
		if u == nil {
			writeJSONError(w, http.StatusUnauthorized, "not signed in")
			return
		}
		if !u.Role.AtLeast(min) {
			writeJSONError(w, http.StatusForbidden, string(min)+" role required")
			return
		}
		next(w, r)
	}
}

// RequireUser is RequireRole(member).
func RequireUser(next http.HandlerFunc) http.HandlerFunc { return RequireRole(auth.RoleMember, next) }

// JSONOnly rejects state-changing API requests that are not JSON. Browsers
// cannot send application/json cross-site without a CORS preflight, so this
// is the CSRF guard for the cookie session (alongside SameSite=Lax).
func JSONOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			ct := r.Header.Get("Content-Type")
			if r.ContentLength != 0 && !strings.HasPrefix(ct, "application/json") {
				writeJSONError(w, http.StatusUnsupportedMediaType, "content-type must be application/json")
				return
			}
			if r.ContentLength == 0 && r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				writeJSONError(w, http.StatusForbidden, "cross-site request refused")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// Recover converts panics into 500s and logs the stack.
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.ErrorContext(r.Context(), "panic", slog.Any("panic", rec), slog.String("stack", string(debug.Stack())))
				writeJSONError(w, http.StatusInternalServerError, "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusWriter) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusWriter) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// Unwrap lets http.ResponseController reach the underlying writer (needed by
// the reverse proxy and websockets).
func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// Log writes one structured line per request with the trace id when present.
func Log(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		if sw.status == 0 {
			sw.status = http.StatusOK
		}
		attrs := []slog.Attr{
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", sw.status),
			slog.Int("bytes", sw.bytes),
			slog.Duration("duration", time.Since(start)),
			slog.String("ip", ClientIP(r)),
		}
		if sc := trace.SpanContextFromContext(r.Context()); sc.IsValid() {
			attrs = append(attrs, slog.String("trace_id", sc.TraceID().String()))
		}
		if u := UserFrom(r.Context()); u != nil {
			attrs = append(attrs, slog.String("user_id", u.ID))
		}
		level := slog.LevelInfo
		switch {
		case sw.status >= 500:
			level = slog.LevelError
		case !strings.HasPrefix(r.URL.Path, "/api/"):
			level = slog.LevelDebug
		}
		slog.LogAttrs(r.Context(), level, "http", attrs...)
	})
}

// ClientIP returns the peer address, trusting X-Forwarded-For for the first
// hop (the Gateway sets it).
func ClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.Index(xff, ","); i > 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
