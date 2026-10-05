package requests

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saniuzzaman-robin/changeloom/curator/internal/catalog"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/claude"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/config"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/db"
)

// Decision actions.
const (
	ActionAccepted = "accepted"
	ActionMerged   = "merged"
	ActionRejected = "rejected"
)

// maxNoteRunes caps a decision note, which the requesting user sees.
const maxNoteRunes = 280

// tools are the only tools a grouping call may use.
var tools = []string{"WebSearch"}

// Runner makes one Claude call; *claude.Client implements it.
type Runner interface {
	Run(ctx context.Context, req claude.Request) (claude.Result, error)
}

// NewTopic is a topic Claude proposes to create.
type NewTopic struct {
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	ParentSlug  string   `json:"parent_slug"`
	Related     []string `json:"related"`
	Hints       []string `json:"hints"`
	// Professions are slugs of the professions a new root topic serves; empty for a child.
	Professions []string `json:"professions"`
}

// Decision resolves one inbox request.
type Decision struct {
	RequestID int64  `json:"request_id"`
	Action    string `json:"action"`
	TopicSlug string `json:"topic_slug"`
	Note      string `json:"note"`
}

// Plan is Claude's validated answer.
type Plan struct {
	NewTopics []NewTopic `json:"new_topics"`
	Decisions []Decision `json:"decisions"`
}

// Result describes a grouping run.
type Result struct {
	// Pending is the number of inbox requests that were sent to Claude.
	Pending int
	Plan    Plan
	// Applied is false for a dry run.
	Applied bool
	CostUSD float64
	// Account is the Claude account email the call ran under.
	Account string
}

// Grouper groups pending requests into topics.
type Grouper struct {
	pool   *pgxpool.Pool
	claude Runner
	cfg    config.Config
}

// New returns a grouper that reads and writes pool.
func New(pool *pgxpool.Pool, runner Runner, cfg config.Config) *Grouper {
	return &Grouper{pool: pool, claude: runner, cfg: cfg}
}

// Run asks Claude to turn the pending inbox requests into new topics and decisions, and applies
// them in one transaction unless dryRun is set. With no pending requests it makes no call.
func (g *Grouper) Run(ctx context.Context, dryRun bool) (Result, error) {
	q := db.New(g.pool)
	inbox, err := q.ListPendingInbox(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("list pending requests: %w", err)
	}
	if len(inbox) == 0 {
		slog.InfoContext(ctx, "no pending topic requests")
		return Result{}, nil
	}
	rows, err := q.ListFetchTopics(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("list topics: %w", err)
	}
	existing := make(map[string]string, len(rows)) // slug -> parent slug
	for _, r := range rows {
		parent := ""
		if r.ParentSlug != nil {
			parent = *r.ParentSlug
		}
		existing[r.Slug] = parent
	}
	professions, err := listProfessions(ctx, g.pool)
	if err != nil {
		return Result{}, err
	}
	known := make(map[string]bool, len(professions))
	for _, p := range professions {
		known[p.Slug] = true
	}
	pending := make(map[int64]bool, len(inbox))
	for _, r := range inbox {
		pending[r.ID] = true
	}

	slog.InfoContext(ctx, "grouping topic requests", "requests", len(inbox), "topics", len(rows))
	out, err := g.claude.Run(ctx, claude.Request{Prompt: Prompt(g.cfg.PromptStyle, rows, professions, inbox, g.cfg.MaxNewTopicsPerRun), Schema: Schema(), Tools: tools})
	if err != nil {
		return Result{}, err
	}
	res := Result{Pending: len(inbox), CostUSD: out.CostUSD, Account: out.Account}
	res.Plan, err = ParseOutput(out.Output, existing, known, pending, g.cfg.MaxNewTopicsPerRun)
	if err != nil {
		return res, err
	}
	if dryRun {
		return res, nil
	}
	if err := g.apply(ctx, res.Plan); err != nil {
		return res, err
	}
	res.Applied = true
	return res, nil
}

func listProfessions(ctx context.Context, pool *pgxpool.Pool) ([]catalog.Profession, error) {
	rows, err := pool.Query(ctx, `SELECT slug, name, description, '{}'::text[] FROM professions ORDER BY position, id`)
	if err != nil {
		return nil, fmt.Errorf("list professions: %w", err)
	}
	professions, err := pgx.CollectRows(rows, pgx.RowToStructByPos[catalog.Profession])
	if err != nil {
		return nil, fmt.Errorf("list professions: %w", err)
	}
	return professions, nil
}

// apply stores the new topics with their relations and hints and resolves the requests, in one
// transaction. Parents are inserted before children.
func (g *Grouper) apply(ctx context.Context, p Plan) error {
	err := pgx.BeginFunc(ctx, g.pool, func(tx pgx.Tx) error {
		q := db.New(tx)
		ids := map[string]int64{}
		idOf := func(slug string) (int64, error) {
			if id, ok := ids[slug]; ok {
				return id, nil
			}
			var id int64
			if err := tx.QueryRow(ctx, `SELECT id FROM topics WHERE slug = $1`, slug).Scan(&id); err != nil {
				return 0, fmt.Errorf("look up topic %q: %w", slug, err)
			}
			ids[slug] = id
			return id, nil
		}
		for _, roots := range []bool{true, false} {
			for _, t := range p.NewTopics {
				if (t.ParentSlug == "") != roots {
					continue
				}
				var parentID *int64
				if t.ParentSlug != "" {
					id, err := idOf(t.ParentSlug)
					if err != nil {
						return err
					}
					parentID = &id
				}
				id, err := q.UpsertTopic(ctx, db.UpsertTopicParams{Slug: t.Slug, Name: t.Name, ParentID: parentID, Description: t.Description})
				if err != nil {
					return fmt.Errorf("create topic %q: %w", t.Slug, err)
				}
				ids[t.Slug] = id
			}
		}
		for _, t := range p.NewTopics {
			for _, r := range t.Related {
				rid, err := idOf(r)
				if err != nil {
					return err
				}
				if _, err := q.AddTopicRelation(ctx, db.AddTopicRelationParams{A: ids[t.Slug], B: rid}); err != nil {
					return fmt.Errorf("relate %q to %q: %w", t.Slug, r, err)
				}
			}
			for _, h := range t.Hints {
				if _, err := q.AddTopicHint(ctx, db.AddTopicHintParams{TopicID: ids[t.Slug], Url: h}); err != nil {
					return fmt.Errorf("add hint %q to %q: %w", h, t.Slug, err)
				}
			}
			for _, p := range t.Professions {
				// Appended after the profession's existing topics.
				if _, err := tx.Exec(ctx, `
					INSERT INTO profession_topics (profession_id, topic_id, position)
					SELECT p.id, $2, COALESCE((SELECT max(position) + 1 FROM profession_topics WHERE profession_id = p.id), 0)
					FROM professions p WHERE p.slug = $1
					ON CONFLICT DO NOTHING`, p, ids[t.Slug]); err != nil {
					return fmt.Errorf("add %q to profession %q: %w", t.Slug, p, err)
				}
			}
		}
		for _, d := range p.Decisions {
			var slug, note *string
			if d.TopicSlug != "" {
				slug = &d.TopicSlug
			}
			if d.Note != "" {
				note = &d.Note
			}
			n, err := q.ResolveInboxRequest(ctx, db.ResolveInboxRequestParams{ID: d.RequestID, Status: d.Action, TopicSlug: slug, Note: note})
			if err != nil {
				return fmt.Errorf("resolve request %d: %w", d.RequestID, err)
			}
			if n == 0 {
				return fmt.Errorf("request %d is no longer pending", d.RequestID)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("apply topic requests: %w", err)
	}
	return nil
}

// ParseOutput decodes Claude's answer and checks it against the catalog (existing maps each
// topic slug to its parent slug, "" for a root), the known profession slugs and the pending inbox ids. Any violation rejects
// the whole answer, since decisions and new topics depend on each other.
func ParseOutput(raw []byte, existing map[string]string, professions map[string]bool, pending map[int64]bool, maxNew int) (Plan, error) {
	var p Plan
	if err := json.Unmarshal(raw, &p); err != nil {
		return Plan{}, fmt.Errorf("decode grouping output: %w", err)
	}
	var errs []error
	bad := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }

	if len(p.NewTopics) > maxNew {
		bad("%d new topics, at most %d allowed (CURATOR_MAX_NEW_TOPICS_PER_RUN)", len(p.NewTopics), maxNew)
	}
	added := map[string]string{} // new slug -> parent slug
	for _, t := range p.NewTopics {
		switch {
		case strings.TrimSpace(t.Name) == "":
			bad("new topic %q has no name", t.Slug)
		case hasKey(existing, t.Slug):
			bad("new topic %q already exists", t.Slug)
		case hasKey(added, t.Slug):
			bad("new topic %q is listed twice", t.Slug)
		}
		if err := catalog.CheckSlug(t.Slug, t.ParentSlug); err != nil {
			bad("%v", err)
		}
		added[t.Slug] = t.ParentSlug
	}
	for _, t := range p.NewTopics {
		if t.ParentSlug != "" {
			// Topics nest two levels deep, so the parent must be a root, existing or new.
			if parent, ok := existing[t.ParentSlug]; ok && parent != "" {
				bad("new topic %q: parent %q is not a root topic", t.Slug, t.ParentSlug)
			} else if !ok && (!hasKey(added, t.ParentSlug) || added[t.ParentSlug] != "") {
				bad("new topic %q: parent %q is not an existing or new root topic", t.Slug, t.ParentSlug)
			}
		}
		for _, r := range t.Related {
			if r == t.Slug || (!hasKey(existing, r) && !hasKey(added, r)) {
				bad("new topic %q: related topic %q is itself or unknown", t.Slug, r)
			}
		}
		for _, h := range t.Hints {
			if err := catalog.CheckHint(h); err != nil {
				bad("new topic %q: %v", t.Slug, err)
			}
		}
		if t.ParentSlug == "" && len(t.Professions) == 0 {
			bad("new root topic %q needs at least one profession", t.Slug)
		}
		if t.ParentSlug != "" && len(t.Professions) > 0 {
			bad("new topic %q has a parent, so it must not list professions", t.Slug)
		}
		for _, p := range t.Professions {
			if !professions[p] {
				bad("new topic %q: unknown profession %q", t.Slug, p)
			}
		}
	}

	decided := map[int64]bool{}
	accepted := map[string]bool{}
	for i := range p.Decisions {
		d := &p.Decisions[i]
		d.Note = strings.TrimSpace(d.Note)
		switch {
		case !pending[d.RequestID]:
			bad("decision for request %d, which is not pending in the inbox", d.RequestID)
			continue
		case decided[d.RequestID]:
			bad("request %d is decided twice", d.RequestID)
			continue
		}
		decided[d.RequestID] = true
		if utf8.RuneCountInString(d.Note) > maxNoteRunes {
			bad("request %d: note is longer than %d characters", d.RequestID, maxNoteRunes)
		}
		switch d.Action {
		case ActionAccepted:
			if !hasKey(added, d.TopicSlug) {
				bad("request %d accepted into %q, which is not a new topic", d.RequestID, d.TopicSlug)
			}
			accepted[d.TopicSlug] = true
		case ActionMerged:
			if !hasKey(existing, d.TopicSlug) {
				bad("request %d merged into %q, which is not an existing topic", d.RequestID, d.TopicSlug)
			}
		case ActionRejected:
			if d.TopicSlug != "" || d.Note == "" {
				bad("request %d rejected needs an empty topic_slug and a note for the user", d.RequestID)
			}
		default:
			bad("request %d has unknown action %q", d.RequestID, d.Action)
		}
	}
	// A new topic must be an accepted request's target, or the new parent of such a topic.
	for _, t := range p.NewTopics {
		if accepted[t.Slug] {
			accepted[t.ParentSlug] = true
		}
	}
	for _, t := range p.NewTopics {
		if !accepted[t.Slug] {
			bad("new topic %q is not the target of any accepted request", t.Slug)
		}
	}
	if len(errs) > 0 {
		return Plan{}, fmt.Errorf("invalid grouping output: %w", errors.Join(errs...))
	}
	return p, nil
}

func hasKey[V any](m map[string]V, k string) bool {
	_, ok := m[k]
	return ok
}
