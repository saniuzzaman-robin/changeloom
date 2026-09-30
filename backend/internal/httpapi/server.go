// Package httpapi implements the REST API defined in api/openapi.yaml.
package httpapi

import (
	"encoding/json"
	"errors"
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

// Server implements ServerInterface.
type Server struct {
	pool           *pgxpool.Pool
	q              *db.Queries
	timelineWindow time.Duration
	now            func() time.Time
}

var _ ServerInterface = (*Server)(nil)

// NewServer returns a Server backed by pool.
func NewServer(pool *pgxpool.Pool, timelineWindow time.Duration) *Server {
	return &Server{pool: pool, q: db.New(pool), timelineWindow: timelineWindow, now: time.Now}
}

// NewHandler routes all API operations and requires a verified bearer token on /v1/ paths.
func NewHandler(s *Server, verifier auth.Verifier) http.Handler {
	h := HandlerWithOptions(s, StdHTTPServerOptions{
		BaseRouter: http.NewServeMux(),
		ErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		},
	})
	return s.authenticate(verifier, h)
}

// authenticate verifies the bearer token on /v1/ requests and attaches the user,
// creating the user row on first sign-in.
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
		user, err := s.resolveUser(r, id)
		if err != nil {
			internalError(w, r, "resolve user", err)
			return
		}
		next.ServeHTTP(w, r.WithContext(auth.WithUser(r.Context(), user)))
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

// GetHealthz reports liveness.
func (s *Server) GetHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}
