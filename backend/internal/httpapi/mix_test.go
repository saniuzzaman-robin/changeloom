package httpapi

import (
	"slices"
	"testing"
	"time"

	"github.com/saniuzzaman-robin/changeloom/backend/internal/db"
)

func mixRow(id int64, tier int32, read bool, topics ...string) db.ListTimelineRow {
	r := db.ListTimelineRow{ID: id, Tier: tier, Topics: topics}
	if read {
		at := time.Unix(id, 0)
		r.ReadAt = &at
	}
	return r
}

func mixIDs(rows []db.ListTimelineRow) []int64 {
	out := make([]int64, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}

func TestMixPage(t *testing.T) {
	tests := []struct {
		name string
		rows []db.ListTimelineRow
		mix  TimelineMix
		want []int64
	}{
		{
			name: "breaks a run of one root after two",
			rows: []db.ListTimelineRow{
				mixRow(1, tierProfession, false, "web/react"), mixRow(2, tierProfession, false, "web/vue"),
				mixRow(3, tierProfession, false, "web"), mixRow(4, tierProfession, false, "cloud/aws"),
				mixRow(5, tierProfession, false, "web/react"),
			},
			mix:  TimelineMix{MaxTopicRun: 2},
			want: []int64{1, 2, 4, 3, 5},
		},
		{
			name: "a story shares a root through any of its topics",
			rows: []db.ListTimelineRow{
				mixRow(1, tierProfession, false, "web/react", "cloud/aws"), mixRow(2, tierProfession, false, "cloud/gcp"),
				mixRow(3, tierProfession, false, "databases/postgres", "cloud"), mixRow(4, tierProfession, false, "languages/go"),
			},
			mix:  TimelineMix{MaxTopicRun: 2},
			want: []int64{1, 2, 4, 3},
		},
		{
			name: "followed stories are never mixed with the others",
			rows: []db.ListTimelineRow{
				mixRow(1, tierFollowed, false, "web/react"), mixRow(2, tierFollowed, false, "web/vue"),
				mixRow(3, tierFollowed, false, "web/angular"), mixRow(4, tierProfession, false, "cloud/aws"),
			},
			mix:  TimelineMix{MaxTopicRun: 2},
			want: []int64{1, 2, 3, 4},
		},
		{
			name: "a page that can't be spread keeps its order",
			rows: []db.ListTimelineRow{
				mixRow(1, tierFollowed, false, "web/react"), mixRow(2, tierFollowed, false, "web/vue"),
				mixRow(3, tierFollowed, false, "web/react"),
			},
			mix:  TimelineMix{MaxTopicRun: 2},
			want: []int64{1, 2, 3},
		},
		{
			name: "read stories stay below unread ones",
			rows: []db.ListTimelineRow{
				mixRow(1, tierFollowed, false, "web/react"), mixRow(2, tierFollowed, false, "web/vue"),
				mixRow(3, tierFollowed, false, "web/react"), mixRow(4, tierFollowed, true, "cloud/aws"),
				mixRow(5, tierFollowed, true, "cloud/gcp"), mixRow(6, tierFollowed, true, "cloud/azure"),
				mixRow(7, tierFollowed, true, "web/react"),
			},
			mix:  TimelineMix{MaxTopicRun: 2},
			want: []int64{1, 2, 3, 4, 5, 7, 6},
		},
		{
			name: "zero limits keep the order",
			rows: []db.ListTimelineRow{
				mixRow(1, tierProfession, false, "web/react"), mixRow(2, tierProfession, false, "web/vue"),
				mixRow(3, tierProfession, false, "web/react"),
			},
			want: []int64{1, 2, 3},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mixIDs(mixPage(tt.rows, tt.mix)); !slices.Equal(got, tt.want) {
				t.Fatalf("mixPage = %v, want %v", got, tt.want)
			}
		})
	}
}
