package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/saniuzzaman-robin/changeloom/backend/internal/db"
)

const (
	minTopicRequestChars = 2
	maxTopicRequestChars = 100
	// maxTopicRequestsListed bounds GET /v1/topic-requests; pending requests are capped far lower.
	maxTopicRequestsListed = 100
	pgUniqueViolation      = "23505"
)

var errTooManyPending = errors.New("too many pending topic requests")

// ListMyTopicRequests returns the user's topic requests, newest first.
func (s *Server) ListMyTopicRequests(w http.ResponseWriter, r *http.Request) {
	user := mustUser(r)

	rows, err := s.q.ListMyTopicRequests(r.Context(), db.ListMyTopicRequestsParams{UserID: user.ID, MaxRows: maxTopicRequestsListed})
	if err != nil {
		internalError(w, r, "list topic requests", err)
		return
	}
	items := make([]TopicRequest, len(rows))
	for i, row := range rows {
		items[i] = TopicRequest{
			Id:         row.ID,
			Text:       row.Text,
			Status:     TopicRequestStatus(row.Status),
			Topic:      row.TopicSlug,
			Note:       row.Note,
			CreatedAt:  row.CreatedAt,
			ResolvedAt: row.ResolvedAt,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// CreateTopicRequest records a request for a new topic.
func (s *Server) CreateTopicRequest(w http.ResponseWriter, r *http.Request) {
	user := mustUser(r)

	var body CreateTopicRequestJSONRequestBody
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	text := strings.TrimSpace(body.Text)
	if n := utf8.RuneCountInString(text); n < minTopicRequestChars || n > maxTopicRequestChars {
		writeError(w, http.StatusBadRequest, "bad_request",
			fmt.Sprintf("text must be between %d and %d characters", minTopicRequestChars, maxTopicRequestChars))
		return
	}

	// The per-user lock makes the count and the insert atomic, so parallel requests can't exceed the cap.
	var (
		row     db.CreateTopicRequestRow
		pending int64
	)
	err := pgx.BeginFunc(r.Context(), s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if err := q.LockUserTopicRequests(r.Context(), user.ID); err != nil {
			return fmt.Errorf("lock topic requests: %w", err)
		}
		var err error
		if pending, err = q.CountPendingTopicRequests(r.Context(), user.ID); err != nil {
			return fmt.Errorf("count pending topic requests: %w", err)
		}
		if pending >= int64(s.opts.TopicRequestMaxPending) {
			return errTooManyPending
		}
		row, err = q.CreateTopicRequest(r.Context(), db.CreateTopicRequestParams{UserID: user.ID, Text: text})
		return err
	})
	if errors.Is(err, errTooManyPending) {
		writeError(w, http.StatusTooManyRequests, "too_many_requests",
			fmt.Sprintf("you already have %d pending topic requests; wait for them to be reviewed", pending))
		return
	}
	if pgErr := (*pgconn.PgError)(nil); errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
		writeError(w, http.StatusConflict, "conflict", "the same topic request is already pending")
		return
	}
	if err != nil {
		internalError(w, r, "create topic request", err)
		return
	}
	writeJSON(w, http.StatusCreated, TopicRequest{
		Id:         row.ID,
		Text:       row.Text,
		Status:     TopicRequestStatus(row.Status),
		Note:       row.Note,
		CreatedAt:  row.CreatedAt,
		ResolvedAt: row.ResolvedAt,
	})
}
