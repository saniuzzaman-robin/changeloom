// Package httpapi implements the REST API defined in api/openapi.yaml.
package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saniuzzaman-robin/changeloom/backend/internal/auth"
	"github.com/saniuzzaman-robin/changeloom/backend/internal/db"
)

const (
	defaultPageSize = 20
	maxPageSize     = 100
	maxBodyBytes    = 1 << 20
	maxTokenChars   = 4096
)

// Notifier sends a batch of pending push notifications and reports how many stories it pushed
// and how many are still pending.
type Notifier interface {
	Run(ctx context.Context) (pushed, remaining int, err error)
}

// Options configures a Server.
type Options struct {
	// TimelineWindow hides stories published longer ago than this from the timeline.
	TimelineWindow time.Duration
	// TopicRequestMaxPending caps the pending topic requests per user.
	TopicRequestMaxPending int
	// NotifySecret is the bearer token POST /internal/notify requires; empty disables the route.
	NotifySecret string
	// Notifier is called by POST /internal/notify; nil when push is disabled.
	Notifier Notifier
	// RequestTimeout bounds each request's context; zero means no limit.
	RequestTimeout time.Duration
	// RateLimitPerIP and RateLimitPerUser are the requests per minute a client IP (on every route
	// but health checks) and a signed-in user (on /v1/) may make; zero disables that limit.
	RateLimitPerIP   int
	RateLimitPerUser int
	// AppCheck checks the App Check token on /v1/ requests; nil skips the check (dev). Unless
	// AppCheckEnforce is set, a missing or invalid token is only logged.
	AppCheck        auth.AppCheckVerifier
	AppCheckEnforce bool
}

// Server implements ServerInterface.
type Server struct {
	pool *pgxpool.Pool
	q    *db.Queries
	opts Options
	now  func() time.Time
	// ipLimiter and userLimiter are nil when their limit is disabled.
	ipLimiter   *rateLimiter
	userLimiter *rateLimiter
}

var _ ServerInterface = (*Server)(nil)

// NewServer returns a Server backed by pool.
func NewServer(pool *pgxpool.Pool, opts Options) *Server {
	s := &Server{pool: pool, q: db.New(pool), opts: opts, now: time.Now}
	if opts.RateLimitPerIP > 0 {
		s.ipLimiter = newRateLimiter(opts.RateLimitPerIP, s.now)
	}
	if opts.RateLimitPerUser > 0 {
		s.userLimiter = newRateLimiter(opts.RateLimitPerUser, s.now)
	}
	return s
}

// NewHandler routes all API operations and requires a verified bearer token on /v1/ paths.
// POST /internal/notify sits outside /v1/ and checks its own shared secret. Every request is
// traced, logged and protected from panics, bounded by Options.RequestTimeout and rate limited.
func NewHandler(s *Server, verifier auth.Verifier) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(http.MethodPost+" /internal/notify", s.notify)
	HandlerWithOptions(s, StdHTTPServerOptions{
		BaseRouter:       mux,
		ErrorHandlerFunc: badParam,
	})
	h := noStore(s.checkApp(s.authenticate(verifier, jsonFallback(mux))))
	if s.ipLimiter != nil {
		h = limitIP(s.ipLimiter, h)
	}
	if s.opts.RequestTimeout > 0 {
		h = withTimeout(s.opts.RequestTimeout, h)
	}
	return withTrace(observe(h))
}

// badParam reports a request parameter that failed to bind, naming the parameter but not echoing
// the parser's internals.
func badParam(w http.ResponseWriter, _ *http.Request, err error) {
	msg := "invalid request parameters"
	var (
		format   *InvalidParamFormatError
		required *RequiredParamError
		tooMany  *TooManyValuesForParamError
		unmarsh  *UnmarshalingParamError
	)
	switch {
	case errors.As(err, &format):
		msg = fmt.Sprintf("invalid value for parameter %q", format.ParamName)
	case errors.As(err, &required):
		msg = fmt.Sprintf("missing required parameter %q", required.ParamName)
	case errors.As(err, &tooMany):
		msg = fmt.Sprintf("too many values for parameter %q", tooMany.ParamName)
	case errors.As(err, &unmarsh):
		msg = fmt.Sprintf("invalid value for parameter %q", unmarsh.ParamName)
	}
	writeError(w, http.StatusBadRequest, "bad_request", msg)
}

// noStore keeps /v1/ responses, which are per user, out of every cache. A handler may override it.
func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/") {
			w.Header().Set("Cache-Control", "private, no-store")
		}
		next.ServeHTTP(w, r)
	})
}

// authenticate verifies the bearer token on /v1/ requests, applies the per-user rate limit and
// attaches the user, creating the user row on first sign-in.
func (s *Server) authenticate(verifier auth.Verifier, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v1/") {
			next.ServeHTTP(w, r)
			return
		}
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || token == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "missing bearer token")
			return
		}
		id, err := verifier.Verify(r.Context(), token)
		if err != nil {
			if !errors.Is(err, auth.ErrInvalidToken) {
				slog.ErrorContext(r.Context(), "verify token", "err", err)
			}
			writeError(w, http.StatusUnauthorized, "unauthorized", "invalid bearer token")
			return
		}
		if s.userLimiter != nil {
			if ok, wait := s.userLimiter.allow(id.UID); !ok {
				tooManyRequests(w, wait)
				return
			}
		}
		user, err := s.resolveUser(r, id)
		if err != nil {
			internalError(w, r, "resolve user", err)
			return
		}
		next.ServeHTTP(w, r.WithContext(auth.WithUser(r.Context(), user)))
	})
}

// checkApp checks the Firebase App Check token on /v1/ requests, before the bearer token. Rejections
// are 403s: a 401 would make the app refresh its ID token and then sign the user out. Until
// Options.AppCheckEnforce is on, it logs what it would reject, so the share of requests without a
// valid token is known before enforcing.
func (s *Server) checkApp(next http.Handler) http.Handler {
	if s.opts.AppCheck == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v1/") {
			next.ServeHTTP(w, r)
			return
		}
		problem := ""
		if token := r.Header.Get(auth.AppCheckHeader); token == "" {
			problem = "missing"
		} else if err := s.opts.AppCheck.VerifyAppCheck(token); err != nil {
			problem = "invalid"
			if !errors.Is(err, auth.ErrInvalidToken) {
				slog.ErrorContext(r.Context(), "verify app check token", "err", err)
			}
		}
		if problem != "" {
			if s.opts.AppCheckEnforce {
				writeError(w, http.StatusForbidden, "app_check_failed", "missing or invalid App Check token")
				return
			}
			slog.InfoContext(r.Context(), "app check would reject", "app_check", problem)
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("write json response", "err", err)
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, Error{Code: code, Message: message})
}

func internalError(w http.ResponseWriter, r *http.Request, op string, err error) {
	slog.ErrorContext(r.Context(), op, "err", err, "method", r.Method, "path", r.URL.Path)
	writeError(w, http.StatusInternalServerError, "internal", "internal server error")
}

// mustUser returns the user set by authenticate; routes under /v1/ always have one.
func mustUser(r *http.Request) auth.User {
	u, ok := auth.UserFrom(r.Context())
	if !ok {
		panic("httpapi: handler reached without authenticated user")
	}
	return u
}

// notify sends one batch of pending push notifications. The curator calls it after a sync and
// repeats while "remaining" is above zero.
func (s *Server) notify(w http.ResponseWriter, r *http.Request) {
	if s.opts.NotifySecret == "" {
		writeError(w, http.StatusNotFound, "not_found", "no such endpoint")
		return
	}
	token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if subtle.ConstantTimeCompare([]byte(token), []byte(s.opts.NotifySecret)) != 1 {
		writeError(w, http.StatusUnauthorized, "unauthorized", "invalid notify secret")
		return
	}
	// Monitoring alerts when this line is absent for 12h: the curator has stopped calling.
	slog.InfoContext(r.Context(), "notify received")
	sent, remaining := 0, 0
	if s.opts.Notifier != nil {
		var err error
		sent, remaining, err = s.opts.Notifier.Run(r.Context())
		if err != nil {
			internalError(w, r, "send notifications", err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]int{"sent": sent, "remaining": remaining})
}

// GetHealthz reports liveness; kept for existing callers of the old path.
func (s *Server) GetHealthz(w http.ResponseWriter, r *http.Request) {
	s.GetHealth(w, r)
}

// GetHealth reports liveness without checking dependencies, so a database outage doesn't get
// healthy instances restarted.
func (s *Server) GetHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// GetReady reports whether the database is reachable.
func (s *Server) GetReady(w http.ResponseWriter, r *http.Request) {
	if err := s.pool.Ping(r.Context()); err != nil {
		slog.WarnContext(r.Context(), "readiness check: database unreachable", "err", err)
		writeError(w, http.StatusServiceUnavailable, "unavailable", "database unreachable")
		return
	}
	w.WriteHeader(http.StatusOK)
}
