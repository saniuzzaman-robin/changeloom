package httpapi

import (
	"slices"
	"strings"

	"github.com/saniuzzaman-robin/changeloom/backend/internal/db"
)

// mixPage re-orders one timeline page so that at most mix.MaxTopicRun stories in a row share a
// root topic and headline and explore stories are at least mix.HeadlineSpacing and
// mix.ExploreSpacing apart. Each slot takes the
// highest-ranked remaining story that keeps both limits, or the highest-ranked one when none
// does, so a page that can't be spread keeps its order. Read stories, unread followed-topic
// stories and the other unread stories are mixed separately and stay in their sections. The page's set of stories never changes, so the cursor
// taken from the ranked page stays valid.
func mixPage(rows []db.ListTimelineRow, mix TimelineMix) []db.ListTimelineRow {
	if len(rows) < 2 || (mix.MaxTopicRun < 1 && mix.HeadlineSpacing < 2 && mix.ExploreSpacing < 2) {
		return rows
	}
	out := make([]db.ListTimelineRow, 0, len(rows))
	for start := 0; start < len(rows); {
		end := start + 1
		for end < len(rows) && sameSection(rows[end], rows[start]) {
			end++
		}
		out = append(out, mixSection(rows[start:end], mix)...)
		start = end
	}
	return out
}

// sameSection reports whether a and b are ranked in the same section of the timeline.
func sameSection(a, b db.ListTimelineRow) bool {
	return (a.ReadAt != nil) == (b.ReadAt != nil) && (a.Tier == tierFollowed) == (b.Tier == tierFollowed)
}

func mixSection(rows []db.ListTimelineRow, mix TimelineMix) []db.ListTimelineRow {
	remaining := make([]db.ListTimelineRow, len(rows))
	copy(remaining, rows)
	roots := make(map[int64][]string, len(rows))
	for _, r := range rows {
		roots[r.ID] = rootTopics(r.Topics)
	}
	out := make([]db.ListTimelineRow, 0, len(rows))
	for len(remaining) > 0 {
		pick := 0
		for i, r := range remaining {
			if fitsMix(out, r, roots, mix) {
				pick = i
				break
			}
		}
		out = append(out, remaining[pick])
		remaining = append(remaining[:pick], remaining[pick+1:]...)
	}
	return out
}

// fitsMix reports whether r can follow placed without breaking a limit.
func fitsMix(placed []db.ListTimelineRow, r db.ListTimelineRow, roots map[int64][]string, mix TimelineMix) bool {
	spacing := map[int32]int{tierHeadline: mix.HeadlineSpacing, tierExplore: mix.ExploreSpacing}[r.Tier]
	for i := len(placed) - 1; spacing > 1 && i >= 0 && i >= len(placed)-(spacing-1); i-- {
		if placed[i].Tier == r.Tier {
			return false
		}
	}
	if mix.MaxTopicRun < 1 || len(placed) < mix.MaxTopicRun {
		return true
	}
	recent := placed[len(placed)-mix.MaxTopicRun:]
	for _, root := range roots[r.ID] {
		inAll := true
		for _, p := range recent {
			if !slices.Contains(roots[p.ID], root) {
				inAll = false
				break
			}
		}
		if inAll {
			return false
		}
	}
	return true
}

// rootTopics returns the distinct root slugs of topic slugs ("web/react" has root "web").
func rootTopics(slugs []string) []string {
	var out []string
	for _, s := range slugs {
		root, _, _ := strings.Cut(s, "/")
		if !slices.Contains(out, root) {
			out = append(out, root)
		}
	}
	return out
}
