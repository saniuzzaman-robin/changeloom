package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/saniuzzaman-robin/changeloom/backend/internal/db"
)

// cursor is the keyset position of the last item on a timeline page.
type cursor struct {
	IsRead      bool      `json:"r"`
	PublishedAt time.Time `json:"p"`
	ID          int64     `json:"i"`
}

func (c cursor) encode() string {
	b, _ := json.Marshal(c) // cannot fail: plain struct of basic types
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(s string) (cursor, error) {
	var c cursor
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return c, fmt.Errorf("invalid cursor: %w", err)
	}
	if err := json.Unmarshal(b, &c); err != nil || c.ID <= 0 || c.PublishedAt.IsZero() {
		return c, errors.New("invalid cursor")
	}
	return c, nil
}

// GetTimeline returns a page of stories for the user's followed topics.
func (s *Server) GetTimeline(w http.ResponseWriter, r *http.Request, params GetTimelineParams) {
	user := mustUser(r)

	limit := defaultPageSize
	if params.Limit != nil {
		limit = *params.Limit
	}
	if limit < 1 || limit > maxPageSize {
		writeError(w, http.StatusBadRequest, "bad_request", fmt.Sprintf("limit must be between 1 and %d", maxPageSize))
		return
	}

	arg := db.ListTimelineParams{
		UserID:   user.ID,
		Since:    s.now().Add(-s.timelineWindow),
		PageSize: int32(limit + 1), //nolint:gosec // limit is bounded by maxPageSize
	}
	if params.Cursor != nil {
		c, err := decodeCursor(*params.Cursor)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		arg.CursorRead, arg.CursorPublishedAt, arg.CursorID = &c.IsRead, &c.PublishedAt, &c.ID
	}

	rows, err := s.q.ListTimeline(r.Context(), arg)
	if err != nil {
		internalError(w, r, "list timeline", err)
		return
	}

	var next *string
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[limit-1]
		c := cursor{IsRead: last.ReadAt != nil, PublishedAt: last.PublishedAt, ID: last.ID}.encode()
		next = &c
	}
	items := make([]StorySummary, len(rows))
	for i, row := range rows {
		items[i] = StorySummary{
			Id:           row.ID,
			Title:        row.Title,
			Summary:      row.Summary,
			Kind:         StoryKind(row.Kind),
			Severity:     (*Severity)(row.Severity),
			Importance:   int(row.Importance),
			PublishedAt:  row.PublishedAt,
			Topics:       row.Topics,
			IsRead:       row.ReadAt != nil,
			ReadAt:       row.ReadAt,
			IsBookmarked: row.IsBookmarked,
		}
	}
	writeStoryPage(w, items, next)
}

// GetStory returns one story with its body and sources.
func (s *Server) GetStory(w http.ResponseWriter, r *http.Request, id StoryID) {
	user := mustUser(r)

	row, err := s.q.GetStory(r.Context(), db.GetStoryParams{UserID: user.ID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "story not found")
		return
	}
	if err != nil {
		internalError(w, r, "get story", err)
		return
	}
	srcRows, err := s.q.ListStorySources(r.Context(), id)
	if err != nil {
		internalError(w, r, "list story sources", err)
		return
	}
	sources := make([]StorySource, len(srcRows))
	for i, src := range srcRows {
		sources[i] = StorySource{Url: src.Url, Name: src.SourceName}
	}
	writeJSON(w, http.StatusOK, Story{
		Id:           row.ID,
		Title:        row.Title,
		Summary:      row.Summary,
		BodyMd:       row.BodyMd,
		Kind:         StoryKind(row.Kind),
		Severity:     (*Severity)(row.Severity),
		Importance:   int(row.Importance),
		PublishedAt:  row.PublishedAt,
		Topics:       row.Topics,
		IsRead:       row.ReadAt != nil,
		ReadAt:       row.ReadAt,
		IsBookmarked: row.IsBookmarked,
		Sources:      sources,
	})
}

// MarkStoryRead records that the user read the story.
func (s *Server) MarkStoryRead(w http.ResponseWriter, r *http.Request, id StoryID) {
	s.setRead(w, r, id, true)
}

// MarkStoryUnread clears the user's read state for the story.
func (s *Server) MarkStoryUnread(w http.ResponseWriter, r *http.Request, id StoryID) {
	s.setRead(w, r, id, false)
}

func (s *Server) setRead(w http.ResponseWriter, r *http.Request, id StoryID, read bool) {
	user := mustUser(r)

	exists, err := s.q.StoryExists(r.Context(), id)
	if err != nil {
		internalError(w, r, "check story exists", err)
		return
	}
	if !exists {
		writeError(w, http.StatusNotFound, "not_found", "story not found")
		return
	}
	if read {
		err = s.q.MarkStoryRead(r.Context(), db.MarkStoryReadParams{UserID: user.ID, StoryID: id})
	} else {
		err = s.q.MarkStoryUnread(r.Context(), db.MarkStoryUnreadParams{UserID: user.ID, StoryID: id})
	}
	if err != nil {
		internalError(w, r, "set story read state", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
