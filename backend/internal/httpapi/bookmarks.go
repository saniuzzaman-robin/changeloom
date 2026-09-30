package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/saniuzzaman-robin/changeloom/backend/internal/db"
)

const maxSearchChars = 200

// writeStoryPage writes a page of stories in the shape shared by timeline, search and bookmarks.
func writeStoryPage(w http.ResponseWriter, items []StorySummary, next *string) {
	writeJSON(w, http.StatusOK, struct {
		Items      []StorySummary `json:"items"`
		NextCursor *string        `json:"next_cursor,omitempty"`
	}{items, next})
}

// pageParams validates limit and cursor. It writes the error response and returns false
// when either is invalid; the returned cursor is nil for the first page.
func pageParams(w http.ResponseWriter, limitParam *int, cursorParam *string) (limit int, c *cursor, ok bool) {
	limit = defaultPageSize
	if limitParam != nil {
		limit = *limitParam
	}
	if limit < 1 || limit > maxPageSize {
		writeError(w, http.StatusBadRequest, "bad_request", fmt.Sprintf("limit must be between 1 and %d", maxPageSize))
		return 0, nil, false
	}
	if cursorParam != nil {
		dec, err := decodeCursor(*cursorParam)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return 0, nil, false
		}
		c = &dec
	}
	return limit, c, true
}

// AddBookmark bookmarks a story for the user.
func (s *Server) AddBookmark(w http.ResponseWriter, r *http.Request, id StoryID) {
	s.setBookmark(w, r, id, true)
}

// RemoveBookmark removes the user's bookmark of a story.
func (s *Server) RemoveBookmark(w http.ResponseWriter, r *http.Request, id StoryID) {
	s.setBookmark(w, r, id, false)
}

func (s *Server) setBookmark(w http.ResponseWriter, r *http.Request, id StoryID, on bool) {
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
	if on {
		err = s.q.AddBookmark(r.Context(), db.AddBookmarkParams{UserID: user.ID, StoryID: id})
	} else {
		err = s.q.RemoveBookmark(r.Context(), db.RemoveBookmarkParams{UserID: user.ID, StoryID: id})
	}
	if err != nil {
		internalError(w, r, "set bookmark", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListBookmarks returns the user's bookmarked stories, most recently bookmarked first.
// The cursor's PublishedAt field holds the bookmark time.
func (s *Server) ListBookmarks(w http.ResponseWriter, r *http.Request, params ListBookmarksParams) {
	user := mustUser(r)
	limit, c, ok := pageParams(w, params.Limit, params.Cursor)
	if !ok {
		return
	}

	arg := db.ListBookmarksParams{UserID: user.ID, PageSize: int32(limit + 1)} //nolint:gosec // limit is bounded by maxPageSize
	if c != nil {
		arg.CursorTime, arg.CursorID = &c.PublishedAt, &c.ID
	}
	rows, err := s.q.ListBookmarks(r.Context(), arg)
	if err != nil {
		internalError(w, r, "list bookmarks", err)
		return
	}

	var next *string
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[limit-1]
		n := cursor{PublishedAt: last.BookmarkedAt, ID: last.ID}.encode()
		next = &n
	}
	items := make([]StorySummary, len(rows))
	for i, row := range rows {
		items[i] = StorySummary{
			Id: row.ID, Title: row.Title, Summary: row.Summary, Kind: StoryKind(row.Kind), Severity: (*Severity)(row.Severity),
			Importance: int(row.Importance), PublishedAt: row.PublishedAt, Topics: row.Topics,
			IsRead: row.ReadAt != nil, ReadAt: row.ReadAt, IsBookmarked: true,
		}
	}
	writeStoryPage(w, items, next)
}

// SearchStories runs a full-text search over all stories, newest first.
func (s *Server) SearchStories(w http.ResponseWriter, r *http.Request, params SearchStoriesParams) {
	user := mustUser(r)
	q := strings.TrimSpace(params.Q)
	if q == "" || len(q) > maxSearchChars {
		writeError(w, http.StatusBadRequest, "bad_request", fmt.Sprintf("q must be 1-%d characters", maxSearchChars))
		return
	}
	limit, c, ok := pageParams(w, params.Limit, params.Cursor)
	if !ok {
		return
	}

	arg := db.SearchStoriesParams{UserID: user.ID, Query: q, PageSize: int32(limit + 1)} //nolint:gosec // limit is bounded by maxPageSize
	if c != nil {
		arg.CursorPublishedAt, arg.CursorID = &c.PublishedAt, &c.ID
	}
	rows, err := s.q.SearchStories(r.Context(), arg)
	if err != nil {
		internalError(w, r, "search stories", err)
		return
	}

	var next *string
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[limit-1]
		n := cursor{PublishedAt: last.PublishedAt, ID: last.ID}.encode()
		next = &n
	}
	items := make([]StorySummary, len(rows))
	for i, row := range rows {
		items[i] = StorySummary{
			Id: row.ID, Title: row.Title, Summary: row.Summary, Kind: StoryKind(row.Kind), Severity: (*Severity)(row.Severity),
			Importance: int(row.Importance), PublishedAt: row.PublishedAt, Topics: row.Topics,
			IsRead: row.ReadAt != nil, ReadAt: row.ReadAt, IsBookmarked: row.IsBookmarked,
		}
	}
	writeStoryPage(w, items, next)
}

// PutMyDevice registers a push token for the user's device.
func (s *Server) PutMyDevice(w http.ResponseWriter, r *http.Request) {
	user := mustUser(r)

	var body PutMyDeviceJSONRequestBody
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	token := strings.TrimSpace(body.Token)
	if token == "" || len(token) > maxTokenChars {
		writeError(w, http.StatusBadRequest, "bad_request", fmt.Sprintf("token must be 1-%d characters", maxTokenChars))
		return
	}
	if body.Platform != Android && body.Platform != Ios {
		writeError(w, http.StatusBadRequest, "bad_request", "platform must be android or ios")
		return
	}
	err := s.q.UpsertDeviceToken(r.Context(), db.UpsertDeviceTokenParams{Token: token, UserID: user.ID, Platform: string(body.Platform)})
	if err != nil {
		internalError(w, r, "register device token", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DeleteMyDevice unregisters a push token.
func (s *Server) DeleteMyDevice(w http.ResponseWriter, r *http.Request, params DeleteMyDeviceParams) {
	user := mustUser(r)
	if params.Token == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "token is required")
		return
	}
	if err := s.q.DeleteDeviceToken(r.Context(), db.DeleteDeviceTokenParams{Token: params.Token, UserID: user.ID}); err != nil {
		internalError(w, r, "unregister device token", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
