// Package sync pushes the curator's content and request decisions to a hosted DB, and tells the
// hosted api to send push notifications.
package sync

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saniuzzaman-robin/changeloom/curator/internal/catalog"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/config"
)

// batchSize is how many stories are written to the hosted DB per statement.
const batchSize = 500

// Result counts what Push wrote.
type Result struct {
	Topics      int
	Professions int
	Stories     int
	Tombstones  int
	// SkippedTombstones are tombstones whose target story is not in the hosted DB; they are
	// retried on the next sync.
	SkippedTombstones int
	Decisions         int
}

type topicRow struct {
	Slug, Name, Description string
	ParentSlug              *string
}

type storyRow struct {
	ID            int64
	UID           pgtype.UUID
	Title         string
	Summary       string
	BodyMD        string
	Kind          string
	Severity      *string
	Importance    int16
	PublishedAt   time.Time
	DedupeKeys    []byte
	Model         string
	PromptVersion string
	Countries     []string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type tombstone struct {
	UID, MergedIntoUID pgtype.UUID
}

type decision struct {
	ID, RemoteID int64
	Status       string
	TopicSlug    *string
	Note         *string
	ResolvedAt   *time.Time
}

func watermarkKey(env config.Env) string { return string(env) + ":stories" }

// Push writes the local topics, changed stories, tombstones and request decisions for env to the
// hosted DB in one transaction, then records on the local side what was pushed. It is idempotent:
// a failure after the hosted commit only means the next push repeats the same writes. Stories
// published before minPublished are not pushed: the hosted DB prunes them anyway.
func Push(ctx context.Context, local, remote *pgxpool.Pool, env config.Env, minPublished time.Time) (Result, error) {
	var res Result

	var watermark time.Time
	err := local.QueryRow(ctx, `SELECT value FROM sync_state WHERE key = $1`, watermarkKey(env)).Scan(&watermark)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return res, fmt.Errorf("read stories watermark: %w", err)
	}

	topics, relations, err := readTopics(ctx, local)
	if err != nil {
		return res, err
	}
	professions, err := readProfessions(ctx, local)
	if err != nil {
		return res, err
	}
	stories, err := readStories(ctx, local, watermark, minPublished)
	if err != nil {
		return res, err
	}
	tombstones, err := readTombstones(ctx, local, env)
	if err != nil {
		return res, err
	}
	decisions, err := readDecisions(ctx, local, env)
	if err != nil {
		return res, err
	}

	var pushedTombstones []pgtype.UUID
	err = pgx.BeginFunc(ctx, remote, func(tx pgx.Tx) error {
		if err := pushTopics(ctx, tx, topics, relations); err != nil {
			return err
		}
		if err := pushProfessions(ctx, tx, professions); err != nil {
			return err
		}
		for start := 0; start < len(stories); start += batchSize {
			batch := stories[start:min(start+batchSize, len(stories))]
			if err := pushStories(ctx, local, tx, batch); err != nil {
				return err
			}
		}
		var err error
		if pushedTombstones, err = pushTombstones(ctx, tx, tombstones); err != nil {
			return err
		}
		return pushDecisions(ctx, tx, decisions)
	})
	if err != nil {
		return res, fmt.Errorf("push to %s: %w", env, err)
	}
	res = Result{
		Topics: len(topics), Professions: len(professions), Stories: len(stories), Tombstones: len(pushedTombstones),
		SkippedTombstones: len(tombstones) - len(pushedTombstones), Decisions: len(decisions),
	}

	err = pgx.BeginFunc(ctx, local, func(tx pgx.Tx) error {
		if len(stories) > 0 {
			newest := stories[len(stories)-1].UpdatedAt
			if _, err := tx.Exec(ctx, `
				INSERT INTO sync_state (key, value) VALUES ($1, $2)
				ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, watermarkKey(env), newest); err != nil {
				return fmt.Errorf("advance stories watermark: %w", err)
			}
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO tombstone_pushes (uid, env) SELECT uid, $2 FROM unnest($1::uuid[]) AS uid
			ON CONFLICT DO NOTHING`, pushedTombstones, string(env)); err != nil {
			return fmt.Errorf("mark tombstones pushed: %w", err)
		}
		ids := make([]int64, len(decisions))
		for i, d := range decisions {
			ids[i] = d.ID
		}
		if _, err := tx.Exec(ctx, `UPDATE request_inbox SET pushed_at = now() WHERE id = ANY($1)`, ids); err != nil {
			return fmt.Errorf("mark decisions pushed: %w", err)
		}
		return nil
	})
	if err != nil {
		return res, fmt.Errorf("pushed to %s but could not record it locally (the next sync repeats it): %w", env, err)
	}
	return res, nil
}

func readTopics(ctx context.Context, local *pgxpool.Pool) ([]topicRow, [][2]string, error) {
	// Parents first: the catalog is at most two levels deep.
	rows, err := local.Query(ctx, `
		SELECT t.slug, t.name, t.description, p.slug
		FROM topics t LEFT JOIN topics p ON p.id = t.parent_id
		ORDER BY t.parent_id IS NOT NULL, t.id`)
	if err != nil {
		return nil, nil, fmt.Errorf("read topics: %w", err)
	}
	topics, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (topicRow, error) {
		var t topicRow
		err := r.Scan(&t.Slug, &t.Name, &t.Description, &t.ParentSlug)
		return t, err
	})
	if err != nil {
		return nil, nil, fmt.Errorf("read topics: %w", err)
	}

	rows, err = local.Query(ctx, `
		SELECT a.slug, b.slug FROM topic_relations r
		JOIN topics a ON a.id = r.topic_id JOIN topics b ON b.id = r.related_id
		ORDER BY a.slug, b.slug`)
	if err != nil {
		return nil, nil, fmt.Errorf("read topic relations: %w", err)
	}
	relations, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) ([2]string, error) {
		var p [2]string
		err := r.Scan(&p[0], &p[1])
		return p, err
	})
	if err != nil {
		return nil, nil, fmt.Errorf("read topic relations: %w", err)
	}
	return topics, relations, nil
}

func readProfessions(ctx context.Context, local *pgxpool.Pool) ([]catalog.Profession, error) {
	rows, err := local.Query(ctx, `
		SELECT p.slug, p.name, p.description,
		       COALESCE(array_agg(t.slug ORDER BY pt.position) FILTER (WHERE t.id IS NOT NULL), '{}')
		FROM professions p
		LEFT JOIN profession_topics pt ON pt.profession_id = p.id
		LEFT JOIN topics t ON t.id = pt.topic_id
		GROUP BY p.id ORDER BY p.position, p.id`)
	if err != nil {
		return nil, fmt.Errorf("read professions: %w", err)
	}
	professions, err := pgx.CollectRows(rows, pgx.RowToStructByPos[catalog.Profession])
	if err != nil {
		return nil, fmt.Errorf("read professions: %w", err)
	}
	return professions, nil
}

func readStories(ctx context.Context, local *pgxpool.Pool, since, minPublished time.Time) ([]storyRow, error) {
	rows, err := local.Query(ctx, `
		SELECT id, uid, title, summary, body_md, kind, severity, importance, published_at,
		       dedupe_keys, model, prompt_version, countries, created_at, updated_at
		FROM stories WHERE updated_at > $1 AND published_at >= $2 ORDER BY updated_at, id`, since, minPublished)
	if err != nil {
		return nil, fmt.Errorf("read changed stories: %w", err)
	}
	stories, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (storyRow, error) {
		var s storyRow
		err := r.Scan(&s.ID, &s.UID, &s.Title, &s.Summary, &s.BodyMD, &s.Kind, &s.Severity, &s.Importance,
			&s.PublishedAt, &s.DedupeKeys, &s.Model, &s.PromptVersion, &s.Countries, &s.CreatedAt, &s.UpdatedAt)
		return s, err
	})
	if err != nil {
		return nil, fmt.Errorf("read changed stories: %w", err)
	}
	return stories, nil
}

func readTombstones(ctx context.Context, local *pgxpool.Pool, env config.Env) ([]tombstone, error) {
	rows, err := local.Query(ctx, `
		SELECT t.uid, t.merged_into_uid FROM story_tombstones t
		WHERE NOT EXISTS (SELECT 1 FROM tombstone_pushes p WHERE p.uid = t.uid AND p.env = $1)
		ORDER BY t.deleted_at`, string(env))
	if err != nil {
		return nil, fmt.Errorf("read tombstones: %w", err)
	}
	ts, err := pgx.CollectRows(rows, pgx.RowToStructByPos[tombstone])
	if err != nil {
		return nil, fmt.Errorf("read tombstones: %w", err)
	}
	return ts, nil
}

func readDecisions(ctx context.Context, local *pgxpool.Pool, env config.Env) ([]decision, error) {
	rows, err := local.Query(ctx, `
		SELECT id, remote_id, status, topic_slug, note, resolved_at FROM request_inbox
		WHERE env = $1 AND pushed_at IS NULL AND status <> 'pending' ORDER BY id`, string(env))
	if err != nil {
		return nil, fmt.Errorf("read request decisions: %w", err)
	}
	ds, err := pgx.CollectRows(rows, pgx.RowToStructByPos[decision])
	if err != nil {
		return nil, fmt.Errorf("read request decisions: %w", err)
	}
	return ds, nil
}

// pushTopics upserts every topic by slug (parents first) and replaces all relations. Topics are
// never deleted remotely, because users follow them.
func pushTopics(ctx context.Context, tx pgx.Tx, topics []topicRow, relations [][2]string) error {
	batch := &pgx.Batch{}
	for _, t := range topics {
		batch.Queue(`
			INSERT INTO topics (slug, name, parent_id, description)
			VALUES ($1, $2, (SELECT id FROM topics WHERE slug = $3), $4)
			ON CONFLICT (slug) DO UPDATE
			SET name = EXCLUDED.name, parent_id = EXCLUDED.parent_id, description = EXCLUDED.description,
			    updated_at = CASE
			        WHEN (topics.name, topics.parent_id, topics.description)
			            IS DISTINCT FROM (EXCLUDED.name, EXCLUDED.parent_id, EXCLUDED.description)
			        THEN now() ELSE topics.updated_at END`,
			t.Slug, t.Name, t.ParentSlug, t.Description)
	}
	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		return fmt.Errorf("upsert topics: %w", err)
	}

	if _, err := tx.Exec(ctx, `DELETE FROM topic_relations`); err != nil {
		return fmt.Errorf("clear topic relations: %w", err)
	}
	as, bs := make([]string, len(relations)), make([]string, len(relations))
	for i, r := range relations {
		as[i], bs[i] = r[0], r[1]
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO topic_relations (topic_id, related_id)
		SELECT LEAST(a.id, b.id), GREATEST(a.id, b.id)
		FROM unnest($1::text[], $2::text[]) AS x(a, b)
		JOIN topics a ON a.slug = x.a JOIN topics b ON b.slug = x.b
		ON CONFLICT DO NOTHING`, as, bs); err != nil {
		return fmt.Errorf("insert topic relations: %w", err)
	}
	return nil
}

// pushProfessions upserts every profession by slug and replaces its topic list. Professions are
// never deleted remotely, because users pick them.
func pushProfessions(ctx context.Context, tx pgx.Tx, professions []catalog.Profession) error {
	for i, p := range professions {
		if err := catalog.UpsertProfession(ctx, tx, p, i); err != nil {
			return err
		}
	}
	return nil
}

// pushStories upserts a batch of stories by uid, then replaces their sources and topics. The
// hosted notified_at is never touched.
func pushStories(ctx context.Context, local *pgxpool.Pool, tx pgx.Tx, stories []storyRow) error {
	if _, err := tx.Exec(ctx, `
		CREATE TEMP TABLE IF NOT EXISTS stage_stories (
			uid uuid, title text, summary text, body_md text, kind text, severity text, importance smallint,
			published_at timestamptz, dedupe_keys jsonb, model text, prompt_version text, countries text[],
			created_at timestamptz, updated_at timestamptz
		) ON COMMIT DROP`); err != nil {
		return fmt.Errorf("create staging table: %w", err)
	}
	if _, err := tx.Exec(ctx, `TRUNCATE stage_stories`); err != nil {
		return fmt.Errorf("clear staging table: %w", err)
	}
	cols := []string{"uid", "title", "summary", "body_md", "kind", "severity", "importance", "published_at",
		"dedupe_keys", "model", "prompt_version", "countries", "created_at", "updated_at"}
	_, err := tx.CopyFrom(ctx, pgx.Identifier{"stage_stories"}, cols, pgx.CopyFromSlice(len(stories), func(i int) ([]any, error) {
		s := stories[i]
		return []any{s.UID, s.Title, s.Summary, s.BodyMD, s.Kind, s.Severity, s.Importance, s.PublishedAt,
			s.DedupeKeys, s.Model, s.PromptVersion, s.Countries, s.CreatedAt, s.UpdatedAt}, nil
	}))
	if err != nil {
		return fmt.Errorf("stage stories: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO stories (uid, title, summary, body_md, kind, severity, importance, published_at,
		                     dedupe_keys, model, prompt_version, countries, created_at, updated_at)
		SELECT uid, title, summary, body_md, kind, severity, importance, published_at,
		       dedupe_keys, model, prompt_version, countries, created_at, updated_at
		FROM stage_stories
		ON CONFLICT (uid) DO UPDATE
		SET title = EXCLUDED.title, summary = EXCLUDED.summary, body_md = EXCLUDED.body_md,
		    kind = EXCLUDED.kind, severity = EXCLUDED.severity, importance = EXCLUDED.importance,
		    published_at = EXCLUDED.published_at, dedupe_keys = EXCLUDED.dedupe_keys,
		    model = EXCLUDED.model, prompt_version = EXCLUDED.prompt_version,
		    countries = EXCLUDED.countries, updated_at = EXCLUDED.updated_at`); err != nil {
		return fmt.Errorf("upsert stories: %w", err)
	}

	ids := make([]int64, len(stories))
	uids := make([]pgtype.UUID, len(stories))
	for i, s := range stories {
		ids[i], uids[i] = s.ID, s.UID
	}

	var srcUIDs []pgtype.UUID
	var urls, names []string
	rows, err := local.Query(ctx, `
		SELECT s.uid, ss.url, ss.source_name FROM story_sources ss
		JOIN stories s ON s.id = ss.story_id WHERE ss.story_id = ANY($1)`, ids)
	if err != nil {
		return fmt.Errorf("read story sources: %w", err)
	}
	for rows.Next() {
		var uid pgtype.UUID
		var url, name string
		if err := rows.Scan(&uid, &url, &name); err != nil {
			return fmt.Errorf("read story sources: %w", err)
		}
		srcUIDs, urls, names = append(srcUIDs, uid), append(urls, url), append(names, name)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read story sources: %w", err)
	}

	var topicUIDs []pgtype.UUID
	var slugs []string
	rows, err = local.Query(ctx, `
		SELECT s.uid, t.slug FROM story_topics st
		JOIN stories s ON s.id = st.story_id JOIN topics t ON t.id = st.topic_id
		WHERE st.story_id = ANY($1)`, ids)
	if err != nil {
		return fmt.Errorf("read story topics: %w", err)
	}
	for rows.Next() {
		var uid pgtype.UUID
		var slug string
		if err := rows.Scan(&uid, &slug); err != nil {
			return fmt.Errorf("read story topics: %w", err)
		}
		topicUIDs, slugs = append(topicUIDs, uid), append(slugs, slug)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read story topics: %w", err)
	}

	steps := []struct {
		what string
		sql  string
		args []any
	}{
		{"clear story sources", `DELETE FROM story_sources ss USING stories s WHERE s.id = ss.story_id AND s.uid = ANY($1)`, []any{uids}},
		{"clear story topics", `DELETE FROM story_topics st USING stories s WHERE s.id = st.story_id AND s.uid = ANY($1)`, []any{uids}},
		{"insert story sources", `
			INSERT INTO story_sources (story_id, url, source_name)
			SELECT s.id, x.url, x.name FROM unnest($1::uuid[], $2::text[], $3::text[]) AS x(uid, url, name)
			JOIN stories s ON s.uid = x.uid ON CONFLICT DO NOTHING`, []any{srcUIDs, urls, names}},
		{"insert story topics", `
			INSERT INTO story_topics (story_id, topic_id)
			SELECT s.id, t.id FROM unnest($1::uuid[], $2::text[]) AS x(uid, slug)
			JOIN stories s ON s.uid = x.uid JOIN topics t ON t.slug = x.slug ON CONFLICT DO NOTHING`, []any{topicUIDs, slugs}},
	}
	for _, step := range steps {
		if _, err := tx.Exec(ctx, step.sql, step.args...); err != nil {
			return fmt.Errorf("%s: %w", step.what, err)
		}
	}
	return nil
}

// pushTombstones moves the bookmarks and read state of each merged-away story to the story it
// was merged into, then deletes it. A tombstone whose target is not in the hosted DB is skipped,
// so no user state is lost. It returns the uids it handled.
func pushTombstones(ctx context.Context, tx pgx.Tx, tombstones []tombstone) ([]pgtype.UUID, error) {
	done := []pgtype.UUID{}
	for _, t := range tombstones {
		var targetID int64
		err := tx.QueryRow(ctx, `SELECT id FROM stories WHERE uid = $1`, t.MergedIntoUID).Scan(&targetID)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("find merge target: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO user_bookmarks (user_id, story_id, created_at)
			SELECT b.user_id, $2, b.created_at FROM user_bookmarks b JOIN stories d ON d.id = b.story_id
			WHERE d.uid = $1 ON CONFLICT DO NOTHING`, t.UID, targetID); err != nil {
			return nil, fmt.Errorf("move bookmarks: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO user_story_state (user_id, story_id, read_at)
			SELECT s.user_id, $2, s.read_at FROM user_story_state s JOIN stories d ON d.id = s.story_id
			WHERE d.uid = $1 ON CONFLICT DO NOTHING`, t.UID, targetID); err != nil {
			return nil, fmt.Errorf("move read state: %w", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM stories WHERE uid = $1`, t.UID); err != nil {
			return nil, fmt.Errorf("delete merged story: %w", err)
		}
		done = append(done, t.UID)
	}
	return done, nil
}

// pushDecisions resolves still-pending hosted requests. A request already resolved is left alone.
func pushDecisions(ctx context.Context, tx pgx.Tx, decisions []decision) error {
	batch := &pgx.Batch{}
	for _, d := range decisions {
		batch.Queue(`
			UPDATE topic_requests
			SET status = $2, topic_id = (SELECT id FROM topics WHERE slug = $3), note = $4, resolved_at = $5
			WHERE id = $1 AND status = 'pending'`, d.RemoteID, d.Status, d.TopicSlug, d.Note, d.ResolvedAt)
	}
	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		return fmt.Errorf("resolve topic requests: %w", err)
	}
	return nil
}
