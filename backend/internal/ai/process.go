package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saniuzzaman-robin/changeloom/backend/internal/db"
)

const (
	budgetWindow = 24 * time.Hour
	// Submit-time token estimation: ~3 characters per token for the input, and an
	// assumed typical output size. Actual usage replaces the estimate once a batch ends.
	charsPerToken   = 3
	estimatedOutput = 2000
	customIDPrefix  = "raw-"
)

// Settings tunes the pipeline. All values come from config.AIConfig.
type Settings struct {
	Model            string
	DailyTokenBudget int64
	BatchMaxItems    int
	MaxAttempts      int
	InputMaxChars    int
	MergeWindow      time.Duration
	// DedupeMaxPerRun bounds the stories checked for near-duplicates per DedupeStories call.
	DedupeMaxPerRun int
}

// Processor submits pending raw items to Claude and applies the results as stories.
type Processor struct {
	pool     *pgxpool.Pool
	client   Client
	settings Settings
	now      func() time.Time
}

// NewProcessor creates a Processor.
func NewProcessor(pool *pgxpool.Pool, client Client, settings Settings) *Processor {
	return &Processor{pool: pool, client: client, settings: settings, now: time.Now}
}

type promptContext struct {
	system      string
	schema      map[string]any
	validTopics map[string]bool
}

func (p *Processor) promptContext(ctx context.Context, q *db.Queries) (promptContext, error) {
	rows, err := q.ListTopics(ctx)
	if err != nil {
		return promptContext{}, fmt.Errorf("list topics: %w", err)
	}
	if len(rows) == 0 {
		return promptContext{}, errors.New("no topics in database (start the api or run `worker sources sync`)")
	}
	topics := make([]Topic, len(rows))
	valid := make(map[string]bool, len(rows))
	for i, r := range rows {
		topics[i] = Topic{Slug: r.Slug, Name: r.Name, Description: r.Description}
		valid[r.Slug] = true
	}
	return promptContext{system: SystemPrompt(topics), schema: Schema(topics), validTopics: valid}, nil
}

func buildUser(content *string, title, url, source string, published *time.Time, maxChars int) (msg string, truncated bool, origLen int) {
	text := ""
	if content != nil {
		text = *content
	}
	origLen = utf8.RuneCountInString(text)
	if origLen > maxChars {
		text = string([]rune(text)[:maxChars])
		truncated = true
	}
	pub := "unknown"
	if published != nil {
		pub = published.UTC().Format(time.RFC3339)
	}
	return UserMessage(Item{SourceName: source, URL: url, Title: title, Content: text, PublishedAt: pub}, truncated), truncated, origLen
}

func customID(id int64) string { return customIDPrefix + strconv.FormatInt(id, 10) }

func parseCustomID(s string) (int64, error) {
	return strconv.ParseInt(strings.TrimPrefix(s, customIDPrefix), 10, 64)
}

// Submit sends pending items to Claude as one batch, within the daily token budget.
// It returns the number of items submitted.
func (p *Processor) Submit(ctx context.Context) (int, error) {
	now := p.now()
	used, err := db.New(p.pool).TokensUsedSince(ctx, now.Add(-budgetWindow))
	if err != nil {
		return 0, fmt.Errorf("read token usage: %w", err)
	}
	remaining := p.settings.DailyTokenBudget - used
	if remaining <= 0 {
		slog.WarnContext(ctx, "daily AI token budget reached, not submitting", "budget", p.settings.DailyTokenBudget, "used", used)
		return 0, nil
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	q := db.New(tx)

	pc, err := p.promptContext(ctx, q)
	if err != nil {
		return 0, err
	}
	rows, err := q.ClaimPendingRawItems(ctx, int32(min(p.settings.BatchMaxItems, 1<<30))) //nolint:gosec // bounded by min
	if err != nil {
		return 0, fmt.Errorf("claim pending items: %w", err)
	}

	var reqs []Request
	var ids []int64
	var estimated int64
	for _, r := range rows {
		user, truncated, origLen := buildUser(r.Content, r.Title, r.Url, r.SourceName, r.PublishedAt, p.settings.InputMaxChars)
		est := int64(utf8.RuneCountInString(pc.system)+utf8.RuneCountInString(user))/charsPerToken + estimatedOutput
		if estimated+est > remaining {
			slog.InfoContext(ctx, "daily AI token budget limits this batch", "submitting", len(reqs), "pending_left", len(rows)-len(reqs))
			break
		}
		if truncated {
			slog.WarnContext(ctx, "raw item content trimmed before sending", "raw_item_id", r.ID, "chars", origLen, "limit", p.settings.InputMaxChars)
		}
		reqs = append(reqs, Request{CustomID: customID(r.ID), System: pc.system, User: user, Schema: pc.schema})
		ids = append(ids, r.ID)
		estimated += est
	}
	if len(reqs) == 0 {
		return 0, nil
	}

	batchID, err := p.client.SubmitBatch(ctx, reqs)
	if err != nil {
		return 0, err
	}
	rowID, err := q.InsertAIBatch(ctx, db.InsertAIBatchParams{
		AnthropicBatchID: batchID, Status: "in_progress", ItemCount: int32(len(reqs)), EstimatedTokens: estimated, //nolint:gosec // bounded by BatchMaxItems
	})
	if err != nil {
		return 0, fmt.Errorf("record batch %s (its items will be resubmitted): %w", batchID, err)
	}
	if err := q.MarkRawItemsSubmitted(ctx, db.MarkRawItemsSubmittedParams{BatchID: &rowID, Ids: ids}); err != nil {
		return 0, fmt.Errorf("mark items submitted: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit batch %s (its items will be resubmitted): %w", batchID, err)
	}
	slog.InfoContext(ctx, "AI batch submitted", "batch", batchID, "items", len(reqs), "estimated_tokens", estimated)
	return len(reqs), nil
}

// Poll checks every open batch and applies the results of those that have ended.
func (p *Processor) Poll(ctx context.Context) error {
	batches, err := db.New(p.pool).ListOpenAIBatches(ctx)
	if err != nil {
		return fmt.Errorf("list open batches: %w", err)
	}
	var errs []error
	for _, b := range batches {
		if err := p.pollBatch(ctx, b.ID, b.AnthropicBatchID); err != nil {
			errs = append(errs, fmt.Errorf("batch %s: %w", b.AnthropicBatchID, err))
		}
	}
	return errors.Join(errs...)
}

func (p *Processor) pollBatch(ctx context.Context, rowID int64, batchID string) error {
	q := db.New(p.pool)
	st, err := p.client.GetBatch(ctx, batchID)
	if err != nil {
		return err
	}
	if !st.Ended {
		return q.UpdateAIBatchStatus(ctx, db.UpdateAIBatchStatusParams{ID: rowID, Status: st.Status})
	}
	results, err := p.client.BatchResults(ctx, batchID)
	if err != nil {
		return err
	}
	pc, err := p.promptContext(ctx, q)
	if err != nil {
		return err
	}
	// Only items still waiting on this batch are applied, so a crash halfway through
	// leaves the batch open and the next poll finishes the remainder.
	waiting, err := q.ListSubmittedRawItems(ctx, &rowID)
	if err != nil {
		return fmt.Errorf("list submitted items: %w", err)
	}
	byID := make(map[int64]db.ListSubmittedRawItemsRow, len(waiting))
	for _, w := range waiting {
		byID[w.ID] = w
	}

	var usage Usage
	var errs []error
	for _, res := range results {
		usage.Add(res.Usage)
		id, err := parseCustomID(res.CustomID)
		if err != nil {
			errs = append(errs, fmt.Errorf("unexpected custom_id %q", res.CustomID))
			continue
		}
		item, ok := byID[id]
		if !ok {
			continue
		}
		it := rawItem{ID: item.ID, URL: item.Url, SourceName: item.SourceName, PublishedAt: item.PublishedAt, FetchedAt: item.FetchedAt}
		if err := p.applyResult(ctx, it, res, pc.validTopics); err != nil {
			errs = append(errs, fmt.Errorf("raw item %d: %w", id, err))
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	// Anything still submitted got no result at all: count it as a failed attempt.
	left, err := q.ListSubmittedRawItems(ctx, &rowID)
	if err != nil {
		return fmt.Errorf("list unanswered items: %w", err)
	}
	for _, l := range left {
		if err := p.retry(ctx, q, l.ID, "no result returned for batch request"); err != nil {
			return err
		}
	}
	err = q.FinishAIBatch(ctx, db.FinishAIBatchParams{
		ID: rowID, Status: "ended",
		InputTokens: usage.Input, OutputTokens: usage.Output, CacheReadTokens: usage.CacheRead, CacheCreationTokens: usage.CacheCreation,
	})
	if err != nil {
		return fmt.Errorf("finish batch: %w", err)
	}
	slog.InfoContext(ctx, "AI batch finished", "batch", batchID, "results", len(results),
		"input_tokens", usage.Input, "output_tokens", usage.Output, "cache_read_tokens", usage.CacheRead, "cache_creation_tokens", usage.CacheCreation)
	return nil
}

type rawItem struct {
	ID          int64
	URL         string
	SourceName  string
	PublishedAt *time.Time
	FetchedAt   time.Time
}

func (p *Processor) retry(ctx context.Context, q *db.Queries, id int64, reason string) error {
	status, err := q.RetryRawItem(ctx, db.RetryRawItemParams{ID: id, Error: &reason, MaxAttempts: int16(p.settings.MaxAttempts)}) //nolint:gosec // small config value
	if err != nil {
		return fmt.Errorf("record failed attempt: %w", err)
	}
	slog.WarnContext(ctx, "AI processing failed for item", "raw_item_id", id, "reason", reason, "next_status", status)
	return nil
}

// applyResult moves one raw item to its next state based on Claude's result. It never
// reports a model-side problem as an error: those are recorded on the item.
func (p *Processor) applyResult(ctx context.Context, it rawItem, res Result, validTopics map[string]bool) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	q := db.New(tx)

	if err := p.applyResultTx(ctx, q, it, res, validTopics); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (p *Processor) applyResultTx(ctx context.Context, q *db.Queries, it rawItem, res Result, validTopics map[string]bool) error {
	setStatus := func(status, msg string) error {
		return q.SetRawItemStatus(ctx, db.SetRawItemStatusParams{ID: it.ID, Status: status, Error: nilIfEmpty(msg)})
	}
	switch {
	case res.Outcome != OutcomeSucceeded:
		return p.retry(ctx, q, it.ID, string(res.Outcome)+": "+res.Error)
	case res.StopReason == "refusal":
		return setStatus("needs_review", "model refused (category: "+orNone(res.RefusalCategory)+")")
	case res.StopReason == "max_tokens":
		return p.retry(ctx, q, it.ID, "output hit max tokens; raise AI_MAX_OUTPUT_TOKENS or lower AI_EFFORT")
	}
	out, err := ParseOutput(res.Text, validTopics)
	if err != nil {
		return p.retry(ctx, q, it.ID, err.Error())
	}
	if !out.Relevant {
		return setStatus("skipped", "not relevant: "+out.RejectReason)
	}
	if err := p.storeStory(ctx, q, it, out); err != nil {
		return err
	}
	return setStatus("processed", "")
}

func (p *Processor) storeStory(ctx context.Context, q *db.Queries, it rawItem, out Output) error {
	topicRows, err := q.GetTopicIDsBySlugs(ctx, out.Topics)
	if err != nil {
		return fmt.Errorf("resolve topics: %w", err)
	}
	topicIDs := make([]int64, len(topicRows))
	for i, r := range topicRows {
		topicIDs[i] = r.ID
	}

	keys := normalizeDedupe(out.Dedupe)
	storyID, err := q.FindMergeTarget(ctx, db.FindMergeTargetParams{
		Since: p.now().Add(-p.settings.MergeWindow), CveIds: keys.CVEIDs, Project: keys.Project, Version: keys.Version,
	})
	switch {
	case err == nil:
		if err := q.RaiseStoryImportance(ctx, db.RaiseStoryImportanceParams{ID: storyID, Importance: int16(out.Importance)}); err != nil { //nolint:gosec // validated 1-5
			return fmt.Errorf("raise importance: %w", err)
		}
		slog.InfoContext(ctx, "raw item merged into existing story", "raw_item_id", it.ID, "story_id", storyID)
	case errors.Is(err, pgx.ErrNoRows):
		storyID, err = p.insertStory(ctx, q, it, out, keys)
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("find merge target: %w", err)
	}

	if err := q.AddStorySource(ctx, db.AddStorySourceParams{StoryID: storyID, RawItemID: &it.ID, Url: it.URL, SourceName: it.SourceName}); err != nil {
		return fmt.Errorf("add story source: %w", err)
	}
	if err := q.AddStoryTopics(ctx, db.AddStoryTopicsParams{StoryID: storyID, TopicIds: topicIDs}); err != nil {
		return fmt.Errorf("add story topics: %w", err)
	}
	return nil
}

func (p *Processor) insertStory(ctx context.Context, q *db.Queries, it rawItem, out Output, keys Dedupe) (int64, error) {
	published := it.FetchedAt
	if it.PublishedAt != nil {
		published = *it.PublishedAt
	}
	if now := p.now(); published.After(now) {
		published = now
	}
	rawKeys, err := json.Marshal(keys)
	if err != nil {
		return 0, fmt.Errorf("encode dedupe keys: %w", err)
	}
	var severity *string
	if out.Severity != "none" {
		severity = &out.Severity
	}
	id, err := q.InsertStory(ctx, db.InsertStoryParams{
		Title: strings.TrimSpace(out.Title), Summary: strings.TrimSpace(out.Summary), BodyMd: strings.TrimSpace(out.BodyMD),
		Kind: out.Kind, Severity: severity, Importance: int16(out.Importance), //nolint:gosec // validated 1-5
		PublishedAt: published, DedupeKeys: rawKeys, Model: p.settings.Model, PromptVersion: PromptVersion,
	})
	if err != nil {
		return 0, fmt.Errorf("insert story: %w", err)
	}
	return id, nil
}

// normalizeDedupe canonicalizes the model's keys so equal events compare equal.
func normalizeDedupe(d Dedupe) Dedupe {
	out := Dedupe{
		Project: strings.ToLower(strings.TrimSpace(d.Project)),
		Version: strings.ToLower(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(d.Version)), "v")),
		CVEIDs:  []string{},
	}
	seen := map[string]bool{}
	for _, c := range d.CVEIDs {
		c = strings.ToUpper(strings.TrimSpace(c))
		if c != "" && !seen[c] {
			seen[c] = true
			out.CVEIDs = append(out.CVEIDs, c)
		}
	}
	return out
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

// Preview is what ProcessOne returns for an item.
type Preview struct {
	Result Result
	Output Output
	// ParseErr is set when the model's answer was unusable.
	ParseErr error
	System   string
	User     string
}

// ProcessOne runs one raw item through Claude synchronously (full price, no batch).
// With apply, the outcome is written like a batch result; otherwise nothing is stored.
func (p *Processor) ProcessOne(ctx context.Context, rawItemID int64, apply bool) (Preview, error) {
	q := db.New(p.pool)
	row, err := q.GetRawItem(ctx, rawItemID)
	if err != nil {
		return Preview{}, fmt.Errorf("load raw item %d: %w", rawItemID, err)
	}
	if apply && row.Status == "processed" {
		return Preview{}, fmt.Errorf("raw item %d is already processed", rawItemID)
	}
	pc, err := p.promptContext(ctx, q)
	if err != nil {
		return Preview{}, err
	}
	user, truncated, origLen := buildUser(row.Content, row.Title, row.Url, row.SourceName, row.PublishedAt, p.settings.InputMaxChars)
	if truncated {
		slog.WarnContext(ctx, "raw item content trimmed before sending", "raw_item_id", row.ID, "chars", origLen, "limit", p.settings.InputMaxChars)
	}
	res, err := p.client.Complete(ctx, Request{CustomID: customID(row.ID), System: pc.system, User: user, Schema: pc.schema})
	if err != nil {
		return Preview{}, err
	}
	prev := Preview{Result: res, System: pc.system, User: user}
	if res.Outcome == OutcomeSucceeded && res.StopReason != "refusal" && res.StopReason != "max_tokens" {
		prev.Output, prev.ParseErr = ParseOutput(res.Text, pc.validTopics)
	}
	if apply {
		it := rawItem{ID: row.ID, URL: row.Url, SourceName: row.SourceName, PublishedAt: row.PublishedAt, FetchedAt: row.FetchedAt}
		if err := p.applyResult(ctx, it, res, pc.validTopics); err != nil {
			return prev, err
		}
	}
	return prev, nil
}
