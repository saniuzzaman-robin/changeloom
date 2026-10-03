package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/saniuzzaman-robin/changeloom/backend/internal/auth"
	"github.com/saniuzzaman-robin/changeloom/backend/internal/db"
)

// topicsMaxAge is how long clients may reuse GET /v1/topics without revalidating.
const topicsMaxAge = 5 * time.Minute

// resolveUser returns the user for a verified identity, creating it on first sign-in and storing
// the token's email when it differs from the stored one.
func (s *Server) resolveUser(r *http.Request, id auth.Identity) (auth.User, error) {
	u, err := s.q.GetUserByFirebaseUID(r.Context(), id.UID)
	switch {
	case err == nil && !emailChanged(u.Email, id.Email):
		return auth.User{ID: u.ID, Email: u.Email}, nil
	case err != nil && !errors.Is(err, pgx.ErrNoRows):
		return auth.User{}, fmt.Errorf("get user: %w", err)
	}
	saved, err := s.q.UpsertUser(r.Context(), db.UpsertUserParams{FirebaseUid: id.UID, Email: id.Email})
	if err != nil {
		return auth.User{}, fmt.Errorf("save user: %w", err)
	}
	return auth.User{ID: saved.ID, Email: saved.Email}, nil
}

// emailChanged reports whether the token carries an email other than the stored one. A token
// without an email keeps the stored one.
func emailChanged(stored, token *string) bool {
	return token != nil && (stored == nil || *stored != *token)
}

// ListTopics returns every topic. The catalog is the same for every user and changes at most a few
// times a day, so the response is cacheable and carries an ETag of its body.
func (s *Server) ListTopics(w http.ResponseWriter, r *http.Request) {
	rows, err := s.q.ListTopics(r.Context())
	if err != nil {
		internalError(w, r, "list topics", err)
		return
	}
	items := make([]Topic, len(rows))
	for i, t := range rows {
		items[i] = Topic{Slug: t.Slug, Name: t.Name, Parent: t.ParentSlug, Description: t.Description}
	}
	profRows, err := s.q.ListProfessions(r.Context())
	if err != nil {
		internalError(w, r, "list professions", err)
		return
	}
	professions := make([]Profession, len(profRows))
	for i, p := range profRows {
		professions[i] = Profession{Slug: p.Slug, Name: p.Name, Description: p.Description, Topics: p.Topics}
	}
	body, err := json.Marshal(struct {
		Items       []Topic      `json:"items"`
		Professions []Profession `json:"professions"`
	}{items, professions})
	if err != nil {
		internalError(w, r, "encode topics", err)
		return
	}
	sum := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(sum[:16]) + `"`
	w.Header().Set("Cache-Control", fmt.Sprintf("private, max-age=%d", int(topicsMaxAge.Seconds())))
	w.Header().Set("ETag", etag)
	if etagMatches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(body); err != nil {
		slog.ErrorContext(r.Context(), "write topics response", "err", err)
	}
}

// etagMatches reports whether an If-None-Match header names etag, comparing weakly as RFC 9110
// requires for If-None-Match.
func etagMatches(header, etag string) bool {
	for candidate := range strings.SplitSeq(header, ",") {
		candidate = strings.TrimPrefix(strings.TrimSpace(candidate), "W/")
		if candidate == "*" || candidate == etag {
			return true
		}
	}
	return false
}

// GetMe returns the signed-in user.
func (s *Server) GetMe(w http.ResponseWriter, r *http.Request) {
	s.writeMe(w, r, mustUser(r))
}

// DeleteMe deletes the signed-in user and, by cascade, all of their data.
func (s *Server) DeleteMe(w http.ResponseWriter, r *http.Request) {
	if err := s.q.DeleteUser(r.Context(), mustUser(r).ID); err != nil {
		internalError(w, r, "delete user", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// PutMyTopics replaces the user's followed topics.
func (s *Server) PutMyTopics(w http.ResponseWriter, r *http.Request) {
	user := mustUser(r)

	var body PutMyTopicsJSONRequestBody
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	if body.Topics == nil {
		writeError(w, http.StatusBadRequest, "bad_request", "topics is required")
		return
	}
	slugs := slices.Compact(slices.Sorted(slices.Values(body.Topics)))

	var unknown []string
	err := pgx.BeginFunc(r.Context(), s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		found, err := q.GetTopicIDsBySlugs(r.Context(), slugs)
		if err != nil {
			return fmt.Errorf("look up topics: %w", err)
		}
		ids := make([]int64, 0, len(found))
		known := make(map[string]bool, len(found))
		for _, t := range found {
			ids = append(ids, t.ID)
			known[t.Slug] = true
		}
		for _, slug := range slugs {
			if !known[slug] {
				unknown = append(unknown, slug)
			}
		}
		if len(unknown) > 0 {
			return nil
		}
		if err := q.DeleteUserTopics(r.Context(), user.ID); err != nil {
			return fmt.Errorf("clear user topics: %w", err)
		}
		if err := q.InsertUserTopics(r.Context(), db.InsertUserTopicsParams{UserID: user.ID, TopicIds: ids}); err != nil {
			return fmt.Errorf("insert user topics: %w", err)
		}
		return nil
	})
	if err != nil {
		internalError(w, r, "put my topics", err)
		return
	}
	if len(unknown) > 0 {
		writeError(w, http.StatusBadRequest, "unknown_topics", "unknown topic slugs: "+strings.Join(unknown, ", "))
		return
	}
	s.writeMe(w, r, user)
}

// PutMyProfessions replaces the user's professions.
func (s *Server) PutMyProfessions(w http.ResponseWriter, r *http.Request) {
	user := mustUser(r)

	var body PutMyProfessionsJSONRequestBody
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	if body.Professions == nil {
		writeError(w, http.StatusBadRequest, "bad_request", "professions is required")
		return
	}
	slugs := slices.Compact(slices.Sorted(slices.Values(body.Professions)))

	var unknown []string
	err := pgx.BeginFunc(r.Context(), s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		found, err := q.GetProfessionIDsBySlugs(r.Context(), slugs)
		if err != nil {
			return fmt.Errorf("look up professions: %w", err)
		}
		ids := make([]int64, 0, len(found))
		known := make(map[string]bool, len(found))
		for _, p := range found {
			ids = append(ids, p.ID)
			known[p.Slug] = true
		}
		for _, slug := range slugs {
			if !known[slug] {
				unknown = append(unknown, slug)
			}
		}
		if len(unknown) > 0 {
			return nil
		}
		if err := q.DeleteUserProfessions(r.Context(), user.ID); err != nil {
			return fmt.Errorf("clear user professions: %w", err)
		}
		if err := q.InsertUserProfessions(r.Context(), db.InsertUserProfessionsParams{UserID: user.ID, ProfessionIds: ids}); err != nil {
			return fmt.Errorf("insert user professions: %w", err)
		}
		return nil
	})
	if err != nil {
		internalError(w, r, "put my professions", err)
		return
	}
	if len(unknown) > 0 {
		writeError(w, http.StatusBadRequest, "unknown_professions", "unknown profession slugs: "+strings.Join(unknown, ", "))
		return
	}
	s.writeMe(w, r, user)
}

func (s *Server) writeMe(w http.ResponseWriter, r *http.Request, user auth.User) {
	slugs, err := s.q.ListUserTopicSlugs(r.Context(), user.ID)
	if err != nil {
		internalError(w, r, "list user topics", err)
		return
	}
	professions, err := s.q.ListUserProfessionSlugs(r.Context(), user.ID)
	if err != nil {
		internalError(w, r, "list user professions", err)
		return
	}
	stats, err := s.q.GetUserStats(r.Context(), user.ID)
	if err != nil {
		internalError(w, r, "get user stats", err)
		return
	}
	writeJSON(w, http.StatusOK, Me{
		Id:          user.ID,
		Email:       user.Email,
		Topics:      slugs,
		Professions: professions,
		Stats:       MeStats{Saved: stats.Saved, Read: stats.Read},
	})
}
