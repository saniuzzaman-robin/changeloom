package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/saniuzzaman-robin/changeloom/backend/internal/db"
)

const (
	minFeedbackChars   = 5
	maxFeedbackChars   = 2000
	maxAppVersionChars = 32
	minFeedbackRating  = 1
	maxFeedbackRating  = 5
	feedbackCapWindow  = 24 * time.Hour
)

var errTooMuchFeedback = errors.New("too much feedback sent today")

// CreateFeedback stores feedback or a review the user sent from the app.
func (s *Server) CreateFeedback(w http.ResponseWriter, r *http.Request) {
	user := mustUser(r)

	var body CreateFeedbackJSONRequestBody
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	if !body.Category.Valid() {
		writeError(w, http.StatusBadRequest, "bad_request", "category must be one of bug, idea, praise, other")
		return
	}
	message := strings.TrimSpace(body.Message)
	if n := utf8.RuneCountInString(message); n < minFeedbackChars || n > maxFeedbackChars {
		writeError(w, http.StatusBadRequest, "bad_request",
			fmt.Sprintf("message must be between %d and %d characters", minFeedbackChars, maxFeedbackChars))
		return
	}
	var rating *int16
	if body.Rating != nil {
		if *body.Rating < minFeedbackRating || *body.Rating > maxFeedbackRating {
			writeError(w, http.StatusBadRequest, "bad_request",
				fmt.Sprintf("rating must be between %d and %d", minFeedbackRating, maxFeedbackRating))
			return
		}
		v := int16(*body.Rating)
		rating = &v
	}
	var appVersion *string
	if body.AppVersion != nil {
		v := strings.TrimSpace(*body.AppVersion)
		if utf8.RuneCountInString(v) > maxAppVersionChars {
			writeError(w, http.StatusBadRequest, "bad_request",
				fmt.Sprintf("app_version must be at most %d characters", maxAppVersionChars))
			return
		}
		if v != "" {
			appVersion = &v
		}
	}

	// The per-user lock makes the count and the insert atomic, so parallel requests can't exceed the cap.
	var row db.CreateAppFeedbackRow
	err := pgx.BeginFunc(r.Context(), s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if err := q.LockUserAppFeedback(r.Context(), user.ID); err != nil {
			return fmt.Errorf("lock app feedback: %w", err)
		}
		recent, err := q.CountRecentAppFeedback(r.Context(), db.CountRecentAppFeedbackParams{
			UserID: user.ID, Since: time.Now().Add(-feedbackCapWindow),
		})
		if err != nil {
			return fmt.Errorf("count recent app feedback: %w", err)
		}
		if recent >= int64(s.opts.FeedbackMaxPerDay) {
			return errTooMuchFeedback
		}
		row, err = q.CreateAppFeedback(r.Context(), db.CreateAppFeedbackParams{
			UserID: user.ID, Category: string(body.Category), Rating: rating, Message: message, AppVersion: appVersion,
		})
		return err
	})
	if errors.Is(err, errTooMuchFeedback) {
		writeError(w, http.StatusTooManyRequests, "too_many_requests", "you've sent a lot of feedback today; try again tomorrow")
		return
	}
	if err != nil {
		internalError(w, r, "create feedback", err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": row.ID, "created_at": row.CreatedAt})
}
