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
	Tier        int32     `json:"t"`
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
	if err := json.Unmarshal(b, &c); err != nil || c.ID <= 0 || c.PublishedAt.IsZero() || c.Tier < tierFollowed || c.Tier > tierExplore {
		return c, errors.New("invalid cursor")
	}
	return c, nil
}

// Timeline tiers, as ranked by ListTimeline.
const (
	tierFollowed   = 0
	tierProfession = 1
	tierRelated    = 2
	tierExplore    = 3
)

var tierMatch = map[int32]StorySummaryMatch{
	tierFollowed:   StorySummaryMatchFollowed,
	tierProfession: StorySummaryMatchProfession,
	tierRelated:    StorySummaryMatchRelated,
	tierExplore:    StorySummaryMatchExplore,
}

// GetTimeline returns a page of recent stories ranked by the user's interests.
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
		Since:    s.now().Add(-s.opts.TimelineWindow),
		PageSize: int32(limit + 1), //nolint:gosec // limit is bounded by maxPageSize
	}
	if params.Cursor != nil {
		c, err := decodeCursor(*params.Cursor)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		arg.CursorRead, arg.CursorTier, arg.CursorPublishedAt, arg.CursorID = &c.IsRead, &c.Tier, &c.PublishedAt, &c.ID
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
		c := cursor{IsRead: last.ReadAt != nil, Tier: last.Tier, PublishedAt: last.PublishedAt, ID: last.ID}.encode()
		next = &c
	}
	items := make([]StorySummary, len(rows))
	for i, row := range rows {
		match, ok := tierMatch[row.Tier]
		if !ok {
			internalError(w, r, "list timeline", fmt.Errorf("unknown tier %d for story %d", row.Tier, row.ID))
			return
		}
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
			Match:        &match,
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

// maxViewIDs is the most story ids one views request may carry.
const maxViewIDs = 100

// RecordStoryViews records that stories were visible in the user's feed. It is idempotent and
// ignores ids that are not stories.
func (s *Server) RecordStoryViews(w http.ResponseWriter, r *http.Request) {
	user := mustUser(r)

	var body RecordStoryViewsJSONRequestBody
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	if body.Ids == nil || len(body.Ids) > maxViewIDs {
		writeError(w, http.StatusBadRequest, "bad_request", fmt.Sprintf("ids is required and holds at most %d ids", maxViewIDs))
		return
	}
	if err := s.q.RecordStoryViews(r.Context(), db.RecordStoryViewsParams{UserID: user.ID, Ids: body.Ids}); err != nil {
		internalError(w, r, "record story views", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
