package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/saniuzzaman-robin/changeloom/backend/internal/db"
)

const (
	// dedupeLookback is how long after creation a story is still checked for near-duplicates.
	dedupeLookback = 24 * time.Hour
	// dedupeMaxCandidates bounds the stories compared against one new story.
	dedupeMaxCandidates = 15
)

const dedupeSystemPrompt = `You detect near-duplicate stories in a developer news feed.
You get one NEW story and a numbered list of CANDIDATE stories that share a topic with it.
Decide whether the NEW story reports the same event as one candidate (for example the same release, vulnerability, breaking change or announcement, covered by a different outlet).
Stories about the same project but a different event, version or vulnerability are NOT duplicates. Follow-ups that add a genuinely new development are NOT duplicates.
Answer with the id of the matching candidate, or 0 when none is the same event. When unsure, answer 0.`

var dedupeSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"duplicate_of": map[string]any{"type": "integer", "description": "Id of the candidate reporting the same event, or 0 for none."},
		"reason":       map[string]any{"type": "string", "description": "One short sentence."},
	},
	"required":             []string{"duplicate_of", "reason"},
	"additionalProperties": false,
}

type dedupeAnswer struct {
	DuplicateOf int64  `json:"duplicate_of"`
	Reason      string `json:"reason"`
}

// DedupeStories asks Claude whether each recently created story repeats an existing one and
// merges the repeats: sources, topics and bookmarks move to the older story and the new
// one is deleted. It complements the CVE and project+version merge done at insert time.
// Calls run synchronously at full price, so at most DedupeMaxPerRun stories are checked
// per call. A story whose check fails is retried on the next call.
func (p *Processor) DedupeStories(ctx context.Context) (merged int, err error) {
	q := db.New(p.pool)
	stories, err := q.ListStoriesToDedupe(ctx, db.ListStoriesToDedupeParams{
		Since: p.now().Add(-dedupeLookback), MaxRows: int32(p.settings.DedupeMaxPerRun), //nolint:gosec // small configured bound
	})
	if err != nil {
		return 0, fmt.Errorf("list stories to dedupe: %w", err)
	}
	var errs []error
	for _, st := range stories {
		ok, err := p.dedupeOne(ctx, q, st)
		if err != nil {
			errs = append(errs, fmt.Errorf("story %d: %w", st.ID, err))
			continue
		}
		if ok {
			merged++
		}
	}
	return merged, errors.Join(errs...)
}

func (p *Processor) dedupeOne(ctx context.Context, q *db.Queries, st db.ListStoriesToDedupeRow) (bool, error) {
	cands, err := q.ListDedupeCandidates(ctx, db.ListDedupeCandidatesParams{
		StoryID: st.ID, Since: p.now().Add(-p.settings.MergeWindow), MaxRows: dedupeMaxCandidates,
	})
	if err != nil {
		return false, fmt.Errorf("list candidates: %w", err)
	}
	if len(cands) == 0 {
		return false, q.MarkStoriesDedupeChecked(ctx, []int64{st.ID})
	}

	res, err := p.client.Complete(ctx, Request{
		CustomID: customID(st.ID), System: dedupeSystemPrompt, User: dedupeUser(st, cands), Schema: dedupeSchema,
	})
	if err != nil {
		return false, fmt.Errorf("ask model: %w", err)
	}
	if res.Outcome != OutcomeSucceeded {
		return false, fmt.Errorf("model request %s: %s", res.Outcome, res.Error)
	}
	slog.InfoContext(ctx, "dedupe check", "story_id", st.ID, "candidates", len(cands), "input_tokens", res.Usage.Input, "output_tokens", res.Usage.Output)
	if res.StopReason == "refusal" {
		slog.WarnContext(ctx, "model refused dedupe check, keeping story", "story_id", st.ID, "category", res.RefusalCategory)
		return false, q.MarkStoriesDedupeChecked(ctx, []int64{st.ID})
	}

	var ans dedupeAnswer
	dec := json.NewDecoder(strings.NewReader(res.Text))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&ans); err != nil {
		return false, fmt.Errorf("parse model answer %q: %w", res.Text, err)
	}
	target := int64(0)
	for _, c := range cands {
		if c.ID == ans.DuplicateOf {
			target = c.ID
		}
	}
	if ans.DuplicateOf != 0 && target == 0 {
		return false, fmt.Errorf("model named story %d, which is not one of the candidates", ans.DuplicateOf)
	}
	if target == 0 {
		return false, q.MarkStoriesDedupeChecked(ctx, []int64{st.ID})
	}

	err = pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		tq := q.WithTx(tx)
		if err := tq.MoveStorySources(ctx, db.MoveStorySourcesParams{IntoID: target, FromID: st.ID}); err != nil {
			return fmt.Errorf("move sources: %w", err)
		}
		if err := tq.MoveStoryTopics(ctx, db.MoveStoryTopicsParams{IntoID: target, FromID: st.ID}); err != nil {
			return fmt.Errorf("move topics: %w", err)
		}
		if err := tq.MoveStoryBookmarks(ctx, db.MoveStoryBookmarksParams{IntoID: target, FromID: st.ID}); err != nil {
			return fmt.Errorf("move bookmarks: %w", err)
		}
		if err := tq.RaiseStoryImportance(ctx, db.RaiseStoryImportanceParams{ID: target, Importance: st.Importance}); err != nil {
			return fmt.Errorf("raise importance: %w", err)
		}
		if err := tq.DeleteStory(ctx, st.ID); err != nil {
			return fmt.Errorf("delete merged story: %w", err)
		}
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("merge into story %d: %w", target, err)
	}
	slog.InfoContext(ctx, "near-duplicate story merged", "story_id", st.ID, "into", target, "reason", ans.Reason)
	return true, nil
}

func dedupeUser(st db.ListStoriesToDedupeRow, cands []db.ListDedupeCandidatesRow) string {
	var b strings.Builder
	fmt.Fprintf(&b, "NEW story\nkind: %s\ntitle: %s\nsummary: %s\n\nCANDIDATES\n", st.Kind, st.Title, st.Summary)
	for _, c := range cands {
		fmt.Fprintf(&b, "\nid: %d\nkind: %s\npublished: %s\ntitle: %s\nsummary: %s\n", c.ID, c.Kind, c.PublishedAt.UTC().Format(time.RFC3339), c.Title, c.Summary)
	}
	return b.String()
}
