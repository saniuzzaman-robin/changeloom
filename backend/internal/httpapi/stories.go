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

// cursorVersion marks timeline cursors whose score matches the current ranking; older ones (tier
// cursors, scores from before the affinity tier, or cursors without the followed flag) are rejected so the client reloads from the
// first page.
const cursorVersion = 4

// cursor is the keyset position of the last item on a page. Timeline cursors also carry their
// version and the time the first page was ranked at, which fixes every score for the pages that
// follow; bookmark and search cursors use only PublishedAt and ID.
type cursor struct {
	Version     int       `json:"v,omitzero"`
	AsOf        time.Time `json:"a,omitzero"`
	IsRead      bool      `json:"r"`
	IsFollowed  bool      `json:"f,omitzero"`
	Score       float64   `json:"s,omitzero"`
	PublishedAt time.Time `json:"p"`
	ID          int64     `json:"i"`
}

func (c cursor) encode() string {
	b, _ := json.Marshal(c) // cannot fail: plain struct of basic types, and scores are finite
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

// maxKindFilter bounds the kind filter: there are nine kinds, so more values can only be repeats.
const maxKindFilter = 9

// Timeline tiers, as ranked by ListTimeline.
const (
	tierFollowed   = 0
	tierAffinity   = 1
	tierProfession = 2
	tierRelated    = 3
	tierHeadline   = 4
	tierExplore    = 5
)

var tierMatch = map[int32]StorySummaryMatch{
	tierFollowed:   StorySummaryMatchFollowed,
	tierAffinity:   StorySummaryMatchAffinity,
	tierProfession: StorySummaryMatchProfession,
	tierRelated:    StorySummaryMatchRelated,
	tierHeadline:   StorySummaryMatchHeadline,
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

	kinds := []string{} // empty, not nil: a NULL array would filter out every story
	if params.Kind != nil {
		if len(*params.Kind) > maxKindFilter {
			writeError(w, http.StatusBadRequest, "bad_request", fmt.Sprintf("at most %d kind values", maxKindFilter))
			return
		}
		for _, k := range *params.Kind {
			if !k.Valid() {
				writeError(w, http.StatusBadRequest, "bad_request", fmt.Sprintf("unknown kind %q", k))
				return
			}
			kinds = append(kinds, string(k))
		}
	}

	asOf := s.now()
	var c *cursor
	if params.Cursor != nil {
		decoded, err := decodeCursor(*params.Cursor)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		if decoded.Version != cursorVersion || decoded.AsOf.IsZero() {
			writeError(w, http.StatusBadRequest, "bad_request", "outdated cursor: reload the timeline from the first page")
			return
		}
		c, asOf = &decoded, decoded.AsOf
	}
	score := s.opts.TimelineScore
	arg := db.ListTimelineParams{
		UserID:                user.ID,
		Kinds:                 kinds,
		ReadFilter:            params.Read,
		Since:                 asOf.Add(-s.opts.TimelineWindow),
		PageSize:              int32(limit + 1), //nolint:gosec // limit is bounded by maxPageSize
		HeadlineMinImportance: s.opts.HeadlineMinImportance,
		ExploreMinImportance:  s.opts.ExploreMinImportance,
		AffinitySince:         asOf.Add(-s.opts.AffinityWindow),
		AsOf:                  asOf,
		TierWeight:            score.TierWeight,
		ImportanceWeight:      score.ImportanceWeight,
		SeverityWeight:        score.SeverityWeight,
		DecayHours:            score.AgeDecay.Hours(),
		SeenPenalty:           score.SeenPenalty,
		SeenBefore:            asOf.Add(-score.SeenGrace),
	}
	if c != nil {
		arg.CursorRead, arg.CursorFollowed, arg.CursorScore, arg.CursorPublishedAt, arg.CursorID =
			&c.IsRead, &c.IsFollowed, &c.Score, &c.PublishedAt, &c.ID
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
		c := cursor{
			Version: cursorVersion, AsOf: asOf, IsRead: last.ReadAt != nil, IsFollowed: last.ReadAt == nil && last.Tier == tierFollowed,
			Score:       last.Score,
			PublishedAt: last.PublishedAt, ID: last.ID,
		}.encode()
		next = &c
	}
	rows = mixPage(rows, s.opts.TimelineMix)
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

// DismissStory hides the story from the user's timeline.
func (s *Server) DismissStory(w http.ResponseWriter, r *http.Request, id StoryID) {
	s.setDismissed(w, r, id, true)
}

// UndismissStory shows a dismissed story in the user's timeline again.
func (s *Server) UndismissStory(w http.ResponseWriter, r *http.Request, id StoryID) {
	s.setDismissed(w, r, id, false)
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

func (s *Server) setDismissed(w http.ResponseWriter, r *http.Request, id StoryID, dismissed bool) {
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
	if dismissed {
		err = s.q.DismissStory(r.Context(), db.DismissStoryParams{UserID: user.ID, StoryID: id})
	} else {
		err = s.q.UndismissStory(r.Context(), db.UndismissStoryParams{UserID: user.ID, StoryID: id})
	}
	if err != nil {
		internalError(w, r, "set story dismissed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
