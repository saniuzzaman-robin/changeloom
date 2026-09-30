package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/saniuzzaman-robin/changeloom/backend/internal/auth"
	"github.com/saniuzzaman-robin/changeloom/backend/internal/db"
)

func (s *Server) resolveUser(r *http.Request, id auth.Identity) (auth.User, error) {
	u, err := s.q.GetUserByFirebaseUID(r.Context(), id.UID)
	if err == nil {
		return auth.User{ID: u.ID, Email: u.Email}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return auth.User{}, fmt.Errorf("get user: %w", err)
	}
	created, err := s.q.UpsertUser(r.Context(), db.UpsertUserParams{FirebaseUid: id.UID, Email: id.Email})
	if err != nil {
		return auth.User{}, fmt.Errorf("create user: %w", err)
	}
	return auth.User{ID: created.ID, Email: created.Email}, nil
}

// ListTopics returns every topic.
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
	writeJSON(w, http.StatusOK, struct {
		Items []Topic `json:"items"`
	}{items})
}

// GetMe returns the signed-in user.
func (s *Server) GetMe(w http.ResponseWriter, r *http.Request) {
	s.writeMe(w, r, mustUser(r))
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

func (s *Server) writeMe(w http.ResponseWriter, r *http.Request, user auth.User) {
	slugs, err := s.q.ListUserTopicSlugs(r.Context(), user.ID)
	if err != nil {
		internalError(w, r, "list user topics", err)
		return
	}
	writeJSON(w, http.StatusOK, Me{Id: user.ID, Email: user.Email, Topics: slugs})
}
