package sources

import (
	"strings"
	"testing"

	"github.com/saniuzzaman-robin/changeloom/backend/internal/dbtest"
	"github.com/saniuzzaman-robin/changeloom/backend/internal/topics"
	"github.com/saniuzzaman-robin/changeloom/backend/seed"
)

func TestSeedFileIsValid(t *testing.T) {
	if _, err := Parse(seed.SourcesYAML); err != nil {
		t.Fatal(err)
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct{ name, yaml, want string }{
		{"bad interval", "- {name: A, kind: kev, poll_interval: soon}", "poll_interval"},
		{"too frequent", "- {name: A, kind: kev, poll_interval: 10s}", "poll_interval"},
		{"duplicate", "- {name: A, kind: kev, poll_interval: 1h}\n- {name: A, kind: kev, poll_interval: 1h}", "duplicate"},
		{"bad config", "- {name: A, kind: rss, poll_interval: 1h}", "url"},
		{"unknown kind", "- {name: A, kind: carrier-pigeon, poll_interval: 1h}", "unsupported"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.yaml))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tt.want)
			}
		})
	}
}

func TestSyncIsIdempotentAndResolvesTopics(t *testing.T) {
	pool := dbtest.New(t)
	ctx := t.Context()
	if _, err := topics.Sync(ctx, pool, seed.TopicsYAML); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := Sync(ctx, pool, seed.SourcesYAML); err != nil {
			t.Fatal(err)
		}
	}
	entries, _ := Parse(seed.SourcesYAML)
	var count, withTopics int
	err := pool.QueryRow(ctx, "SELECT count(*), count(*) FILTER (WHERE cardinality(default_topic_ids) > 0) FROM sources").Scan(&count, &withTopics)
	if err != nil {
		t.Fatal(err)
	}
	if count != len(entries) || withTopics == 0 {
		t.Errorf("sources=%d (want %d), with topics=%d", count, len(entries), withTopics)
	}

	_, err = Sync(ctx, pool, []byte("- {name: X, kind: kev, poll_interval: 1h, topics: [nope/nothing]}"))
	if err == nil || !strings.Contains(err.Error(), "unknown topic") {
		t.Fatalf("err = %v, want unknown topic error", err)
	}
}
