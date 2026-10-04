package fetch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/saniuzzaman-robin/changeloom/curator/internal/db"
)

// storeOptions are what storing needs besides the stories.
type storeOptions struct {
	// topicIDs maps every known slug to its id.
	topicIDs map[string]int64
	// hintURLs are normalized topic hint URLs. They are index pages shared by many stories, so a
	// story is never matched to another by one of them.
	hintURLs   map[string]bool
	mergeSince time.Time
	model      string
	promptVer  string
}

type storeResult struct {
	added, merged int
}

// storeStories inserts each story, or merges it into an existing story with one of the same
// source URLs, or about the same CVE or project+version since o.mergeSince. A merge adds the new
// sources and topics, raises the importance and bumps updated_at.
//
// A story only merges into one for the same countries, so a deal found for one country never
// stands in for another's.
//
// Stories inserted from the same answer are never matched by URL: Claude already put each event
// in one story, so two of its stories sharing a URL means an index or aggregator page.
func storeStories(ctx context.Context, q *db.Queries, stories []Story, o storeOptions) (storeResult, error) {
	var res storeResult
	inserted := []int64{}
	for _, s := range stories {
		id, found, err := findExisting(ctx, q, s, o, inserted)
		if err != nil {
			return storeResult{}, err
		}
		if found {
			if err := q.TouchMergedStory(ctx, db.TouchMergedStoryParams{ID: id, Importance: s.Importance}); err != nil {
				return storeResult{}, fmt.Errorf("merge into story %d: %w", id, err)
			}
			res.merged++
		} else {
			keys, err := json.Marshal(s.Dedupe)
			if err != nil {
				return storeResult{}, fmt.Errorf("encode dedupe keys: %w", err)
			}
			id, err = q.InsertStory(ctx, db.InsertStoryParams{
				Title: s.Title, Summary: s.Summary, BodyMd: s.BodyMD,
				Kind: s.Kind, Severity: s.Severity, Importance: s.Importance,
				PublishedAt: s.PublishedAt, DedupeKeys: keys, Model: o.model, PromptVersion: o.promptVer, Countries: s.Countries,
			})
			if err != nil {
				return storeResult{}, fmt.Errorf("insert story %q: %w", s.Title, err)
			}
			inserted = append(inserted, id)
			res.added++
		}

		for _, src := range s.Sources {
			if err := q.AddStorySource(ctx, db.AddStorySourceParams{StoryID: id, Url: src.URL, SourceName: src.Name}); err != nil {
				return storeResult{}, fmt.Errorf("add story source: %w", err)
			}
		}
		topicIDs := make([]int64, len(s.Topics))
		for i, slug := range s.Topics {
			topicIDs[i] = o.topicIDs[slug]
		}
		if err := q.AddStoryTopics(ctx, db.AddStoryTopicsParams{StoryID: id, TopicIds: topicIDs}); err != nil {
			return storeResult{}, fmt.Errorf("add story topics: %w", err)
		}
	}
	return res, nil
}

func findExisting(ctx context.Context, q *db.Queries, s Story, o storeOptions, inserted []int64) (int64, bool, error) {
	var urls []string
	for _, src := range s.Sources {
		if !o.hintURLs[src.URL] {
			urls = append(urls, src.URL)
		}
	}
	if len(urls) > 0 {
		id, err := q.FindStoryBySourceURL(ctx, db.FindStoryBySourceURLParams{Urls: urls, ExcludeIds: inserted, Countries: s.Countries})
		switch {
		case err == nil:
			return id, true, nil
		case !errors.Is(err, pgx.ErrNoRows):
			return 0, false, fmt.Errorf("find story by source url: %w", err)
		}
	}

	id, err := q.FindMergeTarget(ctx, db.FindMergeTargetParams{
		Since: o.mergeSince, CveIds: s.Dedupe.CVEIDs, Project: s.Dedupe.Project, Version: s.Dedupe.Version, Countries: s.Countries,
	})
	switch {
	case err == nil:
		return id, true, nil
	case errors.Is(err, pgx.ErrNoRows):
		return 0, false, nil
	default:
		return 0, false, fmt.Errorf("find merge target: %w", err)
	}
}
