# Plan: local Claude curator, interest-ranked timeline, topic requests, Neon + Cloud Run

## Context
Curating stories per user on the server is too expensive. The new model:
- One shared category inventory, where categories can be related to each other.
- A **local curator** on the user's Mac fetches stories 3–4×/day using the Claude subscription (`claude -p`) and writes them to a **local Postgres**.
- A **sync** step pushes the content to a **hosted Neon Postgres**.
- The backend serves stories **ranked** by user interest, with followed topics first, then related ones, then the rest. The timeline is never empty.
- Users can **request topics**. The curator pulls the requests, groups them into new topics with AI, and the next fetch run picks those topics up.

Decisions from the questions:
- Hosting: Neon free plan for the DB, Cloud Run free tier for the API.
- The old RSS/Gemini pipeline is **deleted**.
- The curator is a new Go module at `curator/` in this monorepo.

Naming: "category" in the request means the existing `topics` table and API. No rename.

## Architecture
```
 Mac (launchd, 4×/day)                              Hosted
 ┌──────────────────────────────┐   sync (pgx)   ┌───────────────────────────┐
 │ curator run                   │ ───────────▶  │ Neon Postgres             │
 │  1 requests pull  ◀───────────┼── requests ── │  topics, relations,       │
 │  2 requests group (claude -p) │   + follower  │  stories, topic_requests, │
 │  3 fetch          (claude -p) │     counts    │  users/follows/bookmarks  │
 │  4 sync push ─────────────────┼─────────────▶ └────────────▲──────────────┘
 │  5 POST /internal/notify ─────┼──────────┐                 │
 │ local PG :5433 db changeloom_curator     │   ┌─────────────┴─────────────┐
 └──────────────────────────────┘          └─▶ │ Cloud Run: api (scale to 0)│◀── Android
                                                └───────────────────────────┘
```
Who owns what:
- The local DB owns **content**: topics, relations, stories, story_sources, story_topics.
- The hosted DB owns **user data**: users, follows, read state, bookmarks, devices, topic_requests.
- Sync is one-way for content. For requests it is a pull of the requests and a push of the decisions.

---

## - [x] Phase 1: Backend schema and API
**Migration `backend/migrations/00005_curated_content.sql`:**
- `topic_relations(topic_id, related_id, PRIMARY KEY(topic_id, related_id), CHECK (topic_id < related_id))`. Relations are symmetric and each pair is stored once. Both FKs go to `topics ON DELETE CASCADE`.
- `topics ADD updated_at timestamptz NOT NULL DEFAULT now()`.
- `stories ADD uid uuid NOT NULL UNIQUE DEFAULT gen_random_uuid()` as the sync key, plus `ADD updated_at timestamptz NOT NULL DEFAULT now()` with an index on `updated_at`.
- `topic_requests`:
  - Columns: `id`, `user_id` (FK, cascade), `text`, `status` CHECK (`pending|accepted|merged|rejected`), `topic_id` (FK, `ON DELETE SET NULL`), `note`, `created_at`, `resolved_at`.
  - Partial unique index on `(user_id, lower(text)) WHERE status='pending'`.
- Drop the old pipeline schema: `ai_batches`, `raw_items` (and `story_sources.raw_item_id`), `sources`, `stories.dedupe_checked_at`. This is **destructive, but no prod DB exists yet**. The Down migration recreates them.

**Queries (`backend/internal/db/queries/`):**
- `stories.sql` `ListTimeline` gets rewritten:
  - **Sets:** `followed` (the existing recursive CTE), then `related` = relation neighbours of `followed` plus the ancestors of followed topics, minus `followed`.
  - **Tier per story:** 0 if it is in a followed topic, 1 if it is in a related topic, 2 otherwise. The `EXISTS` filter on followed topics goes away, so every story in the window appears.
  - **Order:** `is_read ASC, tier ASC, published_at DESC, id DESC`. Keyset pagination gains `cursor_tier`.
  - A user with no follows gets a plain recency feed.
- New `topic_requests.sql`: `CreateTopicRequest`, `CountPendingTopicRequests`, `ListMyTopicRequests` (joins the topic slug).
- `topics.sql`: add `ListTopicRelations` only if `GET /v1/topics` needs it. It does not, so skip it.

**OpenAPI (`api/openapi.yaml`), then `make generate`:**
- `StorySummary.match`: enum `followed|related|explore`, mapped from the tier. This is an additive, optional field.
- `POST /v1/topic-requests` with body `{text}`:
  - Returns 201 with a `TopicRequest` object.
  - Returns 400 if the trimmed text is not between 2 and 100 characters.
  - Returns 409 if the same request is already pending.
  - Returns 429 when the user has more than `TOPIC_REQUEST_MAX_PENDING` pending requests (config, default 10).
- `GET /v1/topic-requests` returns the caller's requests with their status, topic slug and note.

**Handlers (`backend/internal/httpapi/`):**
- `stories.go`: the cursor struct gains `t`, and each row's tier maps to `match`.
- New `topic_requests.go`.
- `server.go`: register `POST /internal/notify` **outside** the `/v1` bearer middleware.
  - It checks `Authorization: Bearer $NOTIFY_SECRET` with `subtle.ConstantTimeCompare`.
  - It returns 404 when `NOTIFY_SECRET` is unset.
  - It calls the existing `push.Notifier.Run` (`backend/internal/push/push.go:108`) synchronously and returns `{sent:n}`.
  - When `FCM_ENABLED=false` it returns `{sent:0}`.

**Other backend changes:**
- `backend/cmd/api/main.go`: stop running `topics.Sync` at startup, because the curator now owns topics. If FCM is enabled, build the `push.FCMSender` here. That code currently lives in the worker.
- `backend/internal/config/config.go`: add `NOTIFY_SECRET` and `TOPIC_REQUEST_MAX_PENDING`. The AI/ingest variables are removed in Phase 2.
- `backend/internal/topics` and `seed/topics.yaml` stay, but **only as a test/dev fixture**: `httpapi/server_test.go:39` uses them. Update the header comment of the YAML file.
- Tests in `httpapi/server_test.go`:
  - The timeline returns followed, then related, then explore, and cursor pagination works across tiers.
  - A user with no follows still sees stories.
  - The topic-request rules: validation, 409, and the pending cap.
  - The notify endpoint's auth.

**Phase 1 notes:**
- The old-pipeline schema drop was **deferred to Phase 2** (`00006_drop_pipeline.sql`): sqlc fails while `ai.sql`/`sources.sql`/dedupe queries still reference those tables. `00005` only adds.
- `httpapi.NewServer(pool, httpapi.Options{TimelineWindow, TopicRequestMaxPending, NotifySecret, Notifier})`. `/internal/notify` is registered on the mux in `NewHandler`, not in `openapi.yaml`. The worker still notifies too until Phase 2 (harmless: `notified_at` guards).
- "Related" = relation neighbours of followed topics (incl. descendants) + ancestors of directly followed topics; children of a related topic are *not* related. The API no longer syncs topics at startup, so a fresh dev DB gets topics only from the worker (until Phase 2) or `curator seed` (Phase 3).

## - [x] Phase 2: Delete the old pipeline and move deploy to Cloud Run
- **Migration `00006_drop_pipeline.sql`** (moved from Phase 1): drop `ai_batches`, `raw_items` (and `story_sources.raw_item_id`), `sources`, `stories.dedupe_checked_at`; Down recreates them. Also remove `dedupe_checked_at IS NOT NULL` from `ListStoriesToNotify` in `devices.sql` (and its use in `push_test.go`), or curated stories never get pushed.
- **Delete:**
  - `backend/cmd/worker`, `internal/{ai,ingest,sources,jobs}`, `seed/sources.yaml` (and its embed).
  - Queries `ai.sql` and `sources.sql`, and the parts of `devices.sql` and `stories.sql` that only the worker used (keep `ListStoriesToNotify`, `MarkStoryNotified` and `ListDeviceTokensForStory`, which push needs).
  - The AI/INGEST config variables.
- Run `go mod tidy` to drop river, genai, go-readability and gofeed.
- `backend/Dockerfile`: build only `api`, keeping `goose` and the migrations. Make the image listen on `$PORT`, the Cloud Run convention: if `PORT` is set, `config` derives `HTTP_ADDR` from it.
- `deploy/`: delete `docker-compose.yml`, `Caddyfile` and `backup.sh`, since Neon handles backups and PITR. Rewrite `deploy/.env.example` as the Cloud Run env list: `DATABASE_URL` (Neon pooled), `FIREBASE_PROJECT_ID`, `FCM_ENABLED`, `NOTIFY_SECRET`, `TIMELINE_WINDOW_DAYS`.
- `.github/workflows/ci.yml`:
  - Drop the prod-compose `config` check and the worker build.
  - Add a `curator` job (build, test, lint) alongside `backend`.
- `Makefile`: add `migrate-remote` (goose up against `REMOTE_DATABASE_URL`, using the direct non-pooled Neon URL), plus `curator-*` targets.
- Update `CLAUDE.md`: the status and gotchas sections (worker removed, curator commands, Neon/Cloud Run).

**Phase 2 notes:**
- **Deferred to Phase 3:** the CI `curator` job and the `curator-*` Makefile targets (`curator/` doesn't exist yet, so they would fail). Also add curator commands to `CLAUDE.md` then.
- **Code to port is only in git history now:** read it with `git show c4b7c0d:backend/internal/ingest/urlnorm.go` (+ `urlnorm_test.go`), `…/internal/ai/prompt.go` (`ParseOutput`), `…/internal/ai/process.go`, `…/internal/db/queries/ai.sql` (`FindMergeTarget`), and `backend/seed/sources.yaml` (hint URLs for `catalog.yaml`).
- `deploy/.env.example` also sets `ENV=prod` (otherwise dev auth is on) and `LOG_LEVEL`. Existing dev DBs still contain River's `river_*` tables (never goose-managed); harmless, drop by hand if wanted.

## - [x] Phase 3: Curator scaffold (`curator/`)
- Module `github.com/saniuzzaman-robin/changeloom/curator`, with its own `go.mod`. It reuses pgx, goose and sqlc at the **same versions** as backend, as `tool` deps.
- **Local DB:** database `changeloom_curator` on the existing dev Postgres (:5433).
  - `make curator-db` creates it with `createdb`.
  - Pointing the backend `DATABASE_URL` at this DB lets you preview curated content in the app locally.
- **Schema:**
  - `curator migrate` applies `../backend/migrations` from `BACKEND_MIGRATIONS_DIR` (default is relative to the repo). This keeps the local and hosted schemas identical.
  - It then applies the embedded `curator/migrations` under a separate goose table, `curator_goose_db_version`.
- **Curator-only tables** (never synced):
  - `topic_hints(topic_id, url)`: source URLs and feeds Claude should check for a topic.
  - `topic_stats(topic_id, followers, updated_at)`: aggregate follower counts pulled from the hosted DB.
  - `request_inbox(remote_id PK, text, created_at, status, topic_slug, note, resolved_at, pushed_at)`. It holds **no user IDs**.
  - `story_tombstones(uid, merged_into_uid, deleted_at, pushed_at)`.
  - `fetch_runs(id, group_slug, started_at, finished_at, status, stories_added, error, cost_usd)`.
  - `sync_state(key PK, value timestamptz)` for watermarks.
- **sqlc:** `curator/sqlc.yaml` with schema `[../backend/migrations, migrations]` and queries in `internal/db/queries`.
- **Seed:** `curator seed` loads `curator/seed/catalog.yaml`.
  - The tree starts as a copy of `backend/seed/topics.yaml`, extended with `related: [slug]` and `hints: [url]`. The hints are the feed and blog URLs from the deleted `sources.yaml`.
  - It upserts by slug, never deletes, and keeps depth at most 2, because the Android topic picker assumes two levels. The slug rules match `backend/internal/topics/sync.go:36-54`.
- **Config** (env, with a `.env.example`): `LOCAL_DATABASE_URL`, `REMOTE_DATABASE_URL`, `API_BASE_URL`, `NOTIFY_SECRET`, `CLAUDE_BIN` (default `claude`), `CLAUDE_MODEL`, `CLAUDE_TIMEOUT`, `CURATOR_MAX_CALLS_PER_RUN`, `CURATOR_TOPICS_PER_CALL`, `CURATOR_MAX_NEW_TOPICS_PER_RUN`, `CURATOR_ITEM_MAX_AGE_DAYS`.
- **`cmd/curator/main.go` subcommands:** `migrate [--remote]`, `seed`, `fetch`, `requests pull|group [--dry-run]`, `sync`, `run`. `--remote` applies only the backend migrations to Neon.

**Phase 3 notes:**
- Packages: `internal/{config,migrate,catalog,db,dbtest}`, `migrations/` (`00001_curator.sql`), `seed/catalog.yaml`. `fetch`/`requests`/`sync`/`run` are stubs in `main.go` that return "not implemented yet"; `--dry-run` for `requests group` isn't parsed yet. Defaults: `CLAUDE_MODEL=sonnet`, `CLAUDE_TIMEOUT=10m`, calls/run 8, topics/call 5, new topics/run 5, max age 7d. `fetch_runs.status` is `running|succeeded|failed`; `cost_usd` is `double precision`.
- `catalog.Seed` bumps `topics.updated_at` only when a field changes; new relations/hints don't bump it, so Phase 6 should sync **all** topics (there are ~45), not by watermark. Slug validation (`checkSlug`, depth ≤ 2) lives in `internal/catalog`; export it for Phase 5.
- `dbtest.New` applies backend + curator migrations (local schema). Phase 6 needs a second helper for a "remote" DB with backend migrations only. `make curator-test` passes root `DATABASE_URL` as `TEST_DATABASE_URL`; `curator-db` uses `docker compose exec` (dev Postgres in Docker).

## - [x] Phase 4: Curator fetch
- **`internal/claude`** runs `claude -p <prompt> --output-format json --json-schema <schema> --allowedTools WebSearch,WebFetch --model $CLAUDE_MODEL --no-session-persistence`. All of these flags were verified in `claude --help` (v2.1.287).
  - It uses a context timeout.
  - It parses the JSON envelope. During implementation, verify which field holds the structured output, plus `is_error`, the usage and the cost.
  - It returns an actionable error on non-zero exit or when auth is missing ("run `claude` once to log in").
- **`internal/fetch` planning:** topics are grouped into calls of up to `CURATOR_TOPICS_PER_CALL`, keeping siblings together.
  - Topics with followers are fetched on every run.
  - Topics with zero followers are fetched at most once a day. This saves subscription usage while keeping explore content fresh.
  - The total number of calls is capped by `CURATOR_MAX_CALLS_PER_RUN`.
- **Prompt for each call:**
  - The topics with their name, description and hints.
  - The "since" time: the last successful `fetch_runs` row for the group, capped at the max age.
  - The titles and URLs of the last 7 days of stories in those topics, so Claude avoids duplicates.
- **Output schema:** the fields of the `stories` row plus `topics` (an enum of all slugs) and `sources[{url,name}]`.
- **Validation** ports the rules of the deleted `ai/prompt.go` `ParseOutput`:
  - Unknown slugs are dropped, and a story needs at least one valid topic.
  - `kind`, `severity` and `importance` must be in range.
  - `published_at` must fall within the max age, and there must be at least one source URL.
- **Dedupe:**
  - By normalized URL. Port `ingest/urlnorm.go` before deleting it, together with its tests.
  - By the same CVE, or the same project+version, within 14 days. That window was `AI_MERGE_WINDOW_DAYS`; it becomes a config value. On a match, the new sources and topics are merged into the existing story and its importance is bumped, as `ai/process.go:315-351` does now. Bump `updated_at`.
- **Insert:** one transaction per call. `model` = the Claude model, `prompt_version` = a constant. Record the run in `fetch_runs`.

**Phase 4 notes:**
- Packages `internal/{claude,fetch,urlnorm}`; `fetch.Runner` is the interface tests fake. Envelope: answer in `structured_output`, plus `is_error`/`subtype`/`total_cost_usd`/`modelUsage` (stored `model` = the modelUsage entry with most output tokens). The CLI also gets `--tools WebSearch,WebFetch --strict-mcp-config`, the prompt on stdin, and runs in `os.TempDir()` so no project CLAUDE.md leaks in. Live call verified (1 call, 28s, ~$0.14).
- Deviations: curator migration `00002` adds `fetch_runs.topic_ids` (per-topic last success; `group_slug` is just a label). Only **leaf** topics are fetched; a parent's followers count for its children. New config `CURATOR_MERGE_WINDOW_DAYS` (14) and `CURATOR_UNFOLLOWED_INTERVAL_HOURS` (24). URL merging ignores hint URLs and stories from the same answer (the live run merged two stories citing one aggregator page).
- Phase 6 sync: merges bump `stories.updated_at`, so the watermark catches them. Also added `POSTGRES_MODE=docker|local` to the root Makefile/.env (user request).

## - [x] Phase 4b: Staging and production environments (user request)
Decisions: a separate GCP/Firebase project and Neon database per env; the curator syncs **both** envs (`--env staging|prod`); Android "deploy" = signed flavor builds plus docs (no new deps). `DEPLOY_ENV` defaults to `staging`.
- `backend/internal/config/config.go`: `ENV` accepts `staging`; everything but `dev` requires Firebase.
- `deploy/{staging,prod}.env.example` replace `deploy/.env.example`: GCP project/region/service/SA, Secret Manager names, scaling, runtime vars. Real `deploy/*.env` gitignored.
- `deploy/deploy-api.sh <env>`: `gcloud run deploy --source backend` with that env's file; prod needs a clean tree and a typed confirmation. Never run by me.
- `Makefile`: `DEPLOY_ENV`, `migrate-remote` per env (`REMOTE_DATABASE_URL_<ENV>`), `deploy-api`, `android-apk`/`android-bundle`.
- Curator: `REMOTE_DATABASE_URL_*`, `API_BASE_URL_*`, `NOTIFY_SECRET_*` per env; `migrate --remote --env`.
- Android: `env` flavors `staging` (app id `dev.changeloom.android.staging` since 2026-10-03, so it installs beside prod and both Firebase projects can list the same signing keys; own name) and `prod`; per-flavor `changeloom.<env>.apiBaseUrl` and `src/<env>/google-services.json`; release signing from `mobile/keystore.properties`; release builds verify the config.
- `deploy/README.md`: one-time setup and the build/deploy steps per env. `CLAUDE.md` gotchas.
- Phases 5/6/8 below updated for two envs.

**Phase 4b notes:**
- Curator config is now `cfg.Remotes[config.Env]` (`RemoteDatabaseURL`/`APIBaseURL`/`NotifySecret` are gone); `config.Envs` lists staging, prod. `make migrate-remote` delegates to `curator migrate --remote --env`.
- Android: the google-services plugin is applied only when some `src/<env>/google-services.json` exists (missing ones WARN); the module-root `androidApp/google-services.json` is the shared fallback for staging and debug builds. `verify<Env>ReleaseConfig` gates `pre<Env>ReleaseBuild`.
- Not verified live: any gcloud/Firebase/Neon step, a signed build with real Firebase config.

## - [x] Phase 5: Topic requests
- **`requests pull`** (remote → local):
  - Copy the remote `topic_requests WHERE status='pending'` into `request_inbox`, taking only the id, text and created_at.
  - Refresh `topic_stats` from `SELECT topic_id, count(*) FROM user_topics GROUP BY 1`. This is aggregate data only.
- **`requests group`** makes one `claude -p` call (WebSearch only, to understand unfamiliar terms). The input is the topic tree plus the pending inbox texts. The output is:
  - `new_topics[{slug,name,description,parent_slug,related[],hints[]}]`
  - `decisions[{request_id, action: accepted|merged|rejected, topic_slug, note}]`
  `accepted` means a new topic was created, and `merged` means the request maps to an existing topic.
- **Validation:** the slug format and depth at most 2, the parent must exist, there are at most `CURATOR_MAX_NEW_TOPICS_PER_RUN` new topics, and every decision must reference an inbox row.
- Apply locally in one transaction: insert the topics, relations and hints, and resolve the inbox rows.
- `--dry-run` prints the proposals without applying them. New topics take part in the same run's `fetch`.
- **Two envs (Phase 4b):** `requests pull --env staging|prod`. Curator migration: `request_inbox` key becomes `(env, remote_id)`; `topic_stats` sums follower counts across envs (prod only if staging test data skews fetch priorities — decide then).

**Phase 5 notes:**
- Package `internal/requests` (`Pull`, `Grouper.Run`, `ParseOutput`). Curator migration `00003` rebuilds both tables: `request_inbox` has a local identity `id` (what Claude's `request_id` refers to) plus `UNIQUE(env, remote_id)`, and `topic_stats` is keyed `(topic_id, env)`; `ListFetchTopics` sums followers across envs. Phase 6 resolves decisions by `(env, remote_id)` where `pushed_at IS NULL AND status <> 'pending'`.
- `requests pull [--env]` defaults to staging like `migrate`; `curator run` must loop over both envs. Hosted follower counts are mapped by topic **slug**, since ids differ between DBs; unknown slugs are skipped.
- Validation rejects the whole answer on any violation. Extra rule: a new topic must be an accepted target or the new parent of one. The user sees `note`, so a rejection requires one. `catalog.CheckSlug`/`CheckHint` are now exported.
- Not verified live: a real `claude -p` grouping call, and a pull from a real Neon DB (tests use a second local DB as the hosted one).

## - [x] Phase 6: Sync and scheduling
- **`curator sync`** runs one remote transaction in batches of 500:
  1. **Topics:** upsert by slug, parents first, and resolve `parent_id` by slug. Replace the relations of the synced topics. Topics are never deleted remotely, because users follow them.
  2. **Stories:** take rows with `updated_at > watermark('stories')` and upsert by `uid`, using `CopyFrom` into a temp table followed by `INSERT … ON CONFLICT (uid) DO UPDATE`. For those stories, replace `story_sources` and `story_topics` using a slug → remote id map.
  3. **Tombstones:** move `user_bookmarks` and `user_story_state` to the `merged_into` story (`ON CONFLICT DO NOTHING`), then delete the duplicate. This is the same pattern as the deleted `ai/dedupe.go`.
  4. **Request decisions:** `UPDATE topic_requests SET status, topic_id (by slug), note, resolved_at WHERE id = remote_id AND status='pending'`.
  5. Commit, advance the watermarks, and mark the inbox and tombstones as `pushed_at`.
  6. Call `POST $API_BASE_URL_<ENV>/internal/notify` with a 30s timeout. A failure is logged but does not undo the sync. The next run's notify picks up anything un-notified, since notification is driven by `notified_at IS NULL`.
- **Two envs (Phase 4b):** `sync --env staging|prod` uses that env's `REMOTE_DATABASE_URL_*`/`API_BASE_URL_*`/`NOTIFY_SECRET_*`. Watermarks keyed `<env>:stories`; tombstone and inbox push state per env (a `(uid, env)` push table instead of `story_tombstones.pushed_at`).
- **`curator run`** = pull → group → fetch → sync, with pull and sync done for each env (staging, then prod). A failing step is logged, and the run continues where that is safe: if fetch fails, sync still pushes request decisions. The exit code is non-zero if any step failed.
- **Scheduling:** `curator/launchd/dev.changeloom.curator.plist` uses `StartCalendarInterval` at 07:00, 12:00, 17:00 and 22:00, logs to `~/Library/Logs/changeloom-curator.log`, and runs from the repo with the env file. launchd runs a missed job when the Mac wakes. You install it with `launchctl bootstrap gui/$UID …`; I will show the command, not run it.

**Phase 6 notes:**
- Package `internal/sync` (`Push`, `Notify`), plain pgx SQL rather than sqlc. Curator migration `00004` drops `story_tombstones.pushed_at` for a per-env `tombstone_pushes` table. Nothing writes tombstones yet (fetch merges never insert the duplicate), so that path is only tested. A tombstone whose target is not hosted yet is skipped and retried.
- Remote `stories.created_at` is the local value, so the notifier's `since` window ignores old stories on a first sync. `sync` never writes `notified_at`. Notify failure is a warning only. `curator run` skips envs without `REMOTE_DATABASE_URL_<ENV>` and errors if none is set.
- Not verified live: a sync to real Neon, `/internal/notify` on Cloud Run, launchd firing. The plist is a template (`__REPO__`, `__HOME__`); install steps are in its header comment.

## - [x] Phase 7: Android
- `mobile/shared/.../data/Models.kt`: add `StorySummary.match` (nullable) and `TopicRequest`.
- `data/ChangeloomApi.kt`: add `requestTopic(text)` and `topicRequests()`.
- `data/TimelineRepository.kt`: the client-side sort mirrors the server: unread, then tier, then newest, then id.
- `androidApp/.../ui`:
  - A "Suggested" label on story cards where `match != followed`.
  - A "Request a topic" section in the profile or topic picker: a text field, plus a list of your requests with status chips.
  - New state in `ViewModels.kt`.
- Tests in `shared` for sorting and models.

**Phase 7 notes:**
- `StorySummary.match` and `TopicRequest` added; `timelineOrder` is now unread, tier (`matchTier`: null counts as explore), newest, id. The "Suggested" label shows only when `match` is non-null and not `followed`, so search/bookmarks never show it. The request UI is `TopicRequestSection.kt` on the Profile screen (state in `ProfileViewModel`; error messages for 400/409/429).
- Verified: `:shared:testAndroidHostTest` and `:androidApp:compileStagingDebugKotlin` pass. `assembleStagingDebug` fails locally because `androidApp/src/staging/google-services.json` has no client for `dev.changeloom.android.staging` (config, not code); not run on a device or emulator.

## - [ ] Phase 8: Hosted setup (you run these; I show the commands)
- Follow `deploy/README.md` (Phase 4b) once per env, staging first: GCP/Firebase project, Neon database, secrets, `make migrate-remote`, `make deploy-api`, then `curator sync --env`.
- Set `changeloom.<env>.apiBaseUrl` to each Cloud Run URL and build the Android flavors.

---

## Reuse
- `push.Notifier.Run`: `backend/internal/push/push.go:108`. It is now called from `/internal/notify`.
- The recursive followed CTE: `backend/internal/db/queries/stories.sql:1-31`.
- Ported into the curator before deletion: the slug validation in `topics/sync.go`, `ingest/urlnorm.go` (with its tests), the `ParseOutput` rules from `ai/prompt.go`, and the merge logic from `ai/process.go` and `queries/ai.sql:69-85`.
- `internal/dbtest.New(t)` for backend tests. The curator gets an equivalent test helper for its own tests.

## Risks and notes
- **Subscription limits:** 4 runs × up to `CURATOR_MAX_CALLS_PER_RUN` web-search calls. Start at about 8 calls per run and tune.
- **Neon free plan** (as of 2026-10-01): 1 GB storage and 100 CU-hours/month, with scale-to-zero. An occasional cold start of about 1–2s (Cloud Run plus Neon) is expected. Storage is ample, around 5 KB per story.
- **Requests stay pending** while the Mac is off. That is acceptable.
- **New API surface:** `/internal/notify` is protected only by a shared secret. Store the secret in Secret Manager and in the curator's local env file, never in the repo.
- **Verify during implementation:** pgx prepared statements over Neon's pooled endpoint. If they fail, use `default_query_exec_mode=exec` on the API URL.

## Verification
- **Backend:** run `make test` (dbtest-backed) for the new timeline tiers, cursor, topic requests and notify auth, then `make lint`.
- **Curator unit tests:** validation, URL normalization, the merge logic, and sync against two dbtest databases acting as local and remote. The sync tests cover idempotent re-sync, tombstones, and request decision push-back.
- **Curator end-to-end:**
  1. `make curator-db && curator migrate && curator seed`.
  2. `curator fetch` with `CURATOR_MAX_CALLS_PER_RUN=1`. This is a real `claude -p` call; check the inserted stories.
  3. Create a request through `POST /v1/topic-requests` on a local API pointed at a second local DB that acts as "remote".
  4. `curator run`, then check that the request status changed, the new topic exists remotely, and `/internal/notify` was hit.
- **Android:** `ANDROID_HOME=~/Library/Android/sdk gradle :shared:testAndroidHostTest :androidApp:assembleDebug`.
- **Not verifiable by me:** a real Neon or Cloud Run deploy, FCM sends, and launchd firing on schedule.

## Execution
Save this as `PLAN.md` in the repo root with a checkbox per phase, and do one phase per session as `CLAUDE.md` requires. Phase 1 first.

---

# Plan: production hardening (H1–H9)

## Context
This plan comes from a production-readiness audit on 2026-10-03. Decisions taken:
- **Ads:** AdMob native in-feed ads with UMP consent.
- **CI/CD:** GitHub Actions with Workload Identity Federation (WIF). Build each image once and promote the same image to prod behind a manual approval. No Terraform.
- **Observability:** GCP and Firebase native tools (Cloud Logging, Error Reporting, Cloud Trace, Crashlytics, GA4, Performance Monitoring).

**Rules:**
- Do one phase per session, in order.
- Ask before adding any new dependency (listed under each phase).
- Never run gcloud, deploys or Play/AdMob actions. Write the commands for the user to run.

## - [x] H1: Backend HTTP safety and logging
New dependencies: none.
- `backend/internal/config/config.go`:
  - `ENV` becomes required, with no dev default. Today a missing `ENV` turns on `DevVerifier`.
  - Add `DB_MAX_CONNS`, `REQUEST_TIMEOUT` and `DB_STATEMENT_TIMEOUT`.
- `backend/cmd/api/main.go`:
  - Set Read/Write/Idle timeouts on `http.Server`. Keep the shutdown budget under Cloud Run's 10s.
  - Build the pool with `pgxpool.ParseConfig`: max connections, health-check period, and `statement_timeout`. Check that this works with Neon's pooled URL.
- New `backend/internal/logging`: a slog handler that writes `severity`/`message` and `logging.googleapis.com/trace` from the context, plus `stack_trace` on errors so Error Reporting picks them up.
- New `backend/internal/httpapi/middleware.go`, wired in `NewHandler` in this order: trace/request ID → recover (JSON 500) → access log (no PII) → request timeout.
- JSON 404/405 responses. The binding-error handler stops echoing the raw `err.Error()`.
- Add `/health` (liveness) and `/ready` (DB ping) to `api/openapi.yaml`, then `make generate`. Keep `/healthz` working.
- Tests: panic returns a JSON 500; missing `ENV` is an error; the trace ID propagates; timeouts fire.

**Notes:**
- `DB_STATEMENT_TIMEOUT` was dropped. Neon's PgBouncer may reject `statement_timeout` as a startup parameter, and pgx already cancels in-flight queries when the request deadline passes.
- Middleware order is trace → observe (access log + recover) → timeout → auth → `jsonFallback(mux)`.
  - The access log records the path, not the route pattern: auth copies the request, so `r.Pattern` isn't visible outside.
  - Cloud Run also writes its own request logs. If log volume matters, lower the app's access log to debug.
- `REQUEST_TIMEOUT` defaults to 30s because `/internal/notify` must fit inside it, and `WriteTimeout` is that plus 5s. H2's batched notify can lower it.
- Trace project is `FIREBASE_PROJECT_ID` (same as the GCP project) outside dev.
- New settings in `.env.example`: `DB_MAX_CONNS` (default 10) and `REQUEST_TIMEOUT`. H5 should add them to `deploy/<env>.env` and deploy with `/ready` and `/health` probes.

## - [x] H2: Backend correctness, efficiency and policy
New dependency: `golang.org/x/time/rate`.
- **Notify:**
  - Claim stories with `FOR UPDATE SKIP LOCKED`, at most N per call, and mark each one notified as soon as it is sent.
  - Return the remaining count; `curator/internal/sync/notify.go` keeps calling until it reaches 0.
  - Add a partial index on `stories(notified_at) WHERE notified_at IS NULL`.
- **Topic-request cap:** run it in a single transaction with `pg_advisory_xact_lock` per user (today it can race).
- **Migration:** add an index on `user_topics(topic_id)`.
- **Timeline:** seed about 50k stories, run `EXPLAIN ANALYZE ListTimeline`, then either precompute the rank inputs or narrow the candidate set before ranking.
- **Caching:** `Cache-Control`/`ETag` on `/v1/topics`; `private, no-store` on per-user routes.
- **Rate limiting:** in-memory per-user and per-IP token bucket.
- **`/v1/me`:** refresh the stored email when it changes.
- **Account deletion:** `DELETE /v1/me` (Play requires it), plus the text for a web deletion page.
- **Curator:** switch its logs to JSON.

**Notes:**
- Timeline at 50k stories: 415ms, of which ~360ms was JIT compilation; per-row subplans pushed the cost past `jit_above_cost`. Rewrote `ListTimeline` to use a topic→tier aggregate and build slugs only for the page: 45ms, no JIT. No precompute needed.
- Notify claims one story per transaction (`FOR UPDATE SKIP LOCKED`), at most 10 per call, and returns `{sent, remaining}`. The curator repeats while `remaining > 0` and the last call sent something, up to 50 calls. Migration 00007 has the partial index, filtered on the notify conditions rather than only `notified_at IS NULL`, because non-security stories keep `notified_at` NULL forever. `REQUEST_TIMEOUT` is still 30s.
- Rate limits are `RATE_LIMIT_IP_PER_MIN` (300) and `RATE_LIMIT_USER_PER_MIN` (120), per instance. The client IP is the last `X-Forwarded-For` entry (Cloud Run appends it). H5 should add them to `deploy/<env>.env`. `DELETE /v1/me` removes only DB data; H9 deletes the Firebase user. The page text is in `deploy/account-deletion.md`, with placeholders.

## - [x] H3: Observability and alerting
New dependencies: `go.opentelemetry.io/otel`, `otelhttp`, the GCP Cloud Trace exporter, `otelpgx`.
- **Tracing:** OpenTelemetry via `otelhttp` and `otelpgx`, exported to Cloud Trace. Turned on with `OTEL_ENABLED`; about 10% sampling in prod.
- **`deploy/README.md` Monitoring section:** commands for the user to run:
  - an uptime check on `/health`
  - alerts on 5xx rate, p95 latency and instance max-out
  - an absence alert on the "notify received" log line over 12h (catches a dead curator)
  - a billing budget
  - Error Reporting notifications
- **Curator:** when `run` fails, show a macOS notification and exit non-zero.

**Notes:**
- The Cloud Trace exporter is deprecated, so tracing uses OTLP (`otlptracegrpc`) to `OTEL_EXPORTER_OTLP_ENDPOINT` (telemetry.googleapis.com) with ADC. It needs `roles/telemetry.tracesWriter`. Sampling is `TraceIDRatioBased(OTEL_SAMPLE_RATIO)` and ignores Cloud Run's sampled flag. Spans are named after the mux route; DB spans after the sqlc query name, without parameters. Sampled requests' logs point at the span.
- `deploy-api.sh` passes the OTEL vars only when `OTEL_ENABLED=true`; H5 must carry this over. Alert policies are YAML in `deploy/monitoring/` with `__SERVICE__`/`__MAX_INSTANCES__` placeholders. Notification channels need `gcloud beta`, which isn't installed locally; the README gives a console alternative.
- Never verified live: trace export, alert policies, the uptime check and the budget. The osascript notification was tested locally.

## - [x] H4: CI hardening
New dependencies: none (pinned actions only).
- **`ci.yml`:**
  - `concurrency` and job timeouts.
  - Generated-code drift check: `make generate` / `curator-generate`, then `git diff --exit-code`.
  - `govulncheck`.
  - Trivy scan on the image.
  - Android `lintStagingDebug` and `assembleStagingRelease`, using a CI property that skips only signing.
  - Pin actions by SHA.
- **`.github/dependabot.yml`:** gomod ×2, gradle, actions and docker; weekly, grouped.
- **Stale docs:** update the `CLAUDE.md` status line (requests/sync/run are implemented) and the PLAN.md line that still mentions an app-id suffix.

**Notes:**
- Actions are pinned to the latest release of the majors already in use (checkout v4.4.0, setup-go v5.6.0, setup-java v4.9.1, setup-gradle v4.4.4, trivy-action v0.36.0); Dependabot will propose the newer majors. govulncheck runs as `go run …@v1.8.0` (no go.mod change). Trivy fails on fixable HIGH/CRITICAL.
- The signing skip is `-Pchangeloom.allowUnsignedRelease=true`. CI also writes a placeholder `androidApp/google-services.json` and passes `changeloom.staging.apiBaseUrl=https://ci.invalid` so the release config check passes; H5's release workflow must use real secrets instead and must not set the skip.
- Verified locally: codegen has no drift, govulncheck and Trivy (0.67.2 container) are clean, and the mobile tasks pass in a copy without real config/keystore (unsigned APK). The workflow itself has not run on GitHub.

## - [x] H5: CD with GitHub Actions and WIF
- **One-time setup:** `deploy/README.md` gets the WIF pool/provider and the `changeloom-deployer` SA setup for the user to run. Roles: `run.developer`, `iam.serviceAccountUser` on the runtime SA, `artifactregistry.writer`, and secret access for the migration URL. **IAM change.**
- **`.github/workflows/deploy-api.yml`:**
  - **On `main`:**
    1. Build the image once and push it by digest.
    2. Run goose migrations, reusing the table and settings of `curator migrate --remote`.
    3. Deploy to staging with `--no-traffic --tag`.
    4. Smoke-test `/ready`, then shift traffic.
  - **Prod:** a `workflow_dispatch` behind the `production` environment. It copies the same digest to the prod registry, then migrates, deploys, smoke-tests and promotes.
- **Cloud Run flags:** move into `deploy/<env>.env`, shared by the workflow and `deploy-api.sh`:
  - concurrency, cpu, memory, timeout and cpu-boost
  - probes on `/health`
  - pinned secret versions
  - `min-instances=1` in prod. **Billing impact.**
- **`.github/workflows/android-release.yml`:**
  - On a `v*` tag: `bundleProdRelease`, with `versionCode` from a Gradle property and the keystore and `google-services.json` from secrets. Upload to Play's internal track.
  - On `main`: build staging and send it to Firebase App Distribution.

**Notes:**
- `deploy/<env>.env` stays gitignored; the workflows read it from the GitHub environment variable `DEPLOY_ENV_FILE` (`staging`/`production` environments, which also hold `WIF_PROVIDER`, the SAs and the Android secrets). WIF bindings use the `environment:<name>` subject, so only approved prod jobs get prod credentials. Staging api deploys run on `workflow_run` after CI succeeds on a push (not PRs); prod takes the staging `image@digest` as input, copies it with `crane` (`go run …@v0.22.1`), reads the commit from the image's `org.opencontainers.image.revision` label and checks that commit out before deploying.
- `deploy-api.sh --image` deploys with `--no-traffic --tag candidate` and no `--allow-unauthenticated` (`run.developer` can't set IAM; the first deploy per env stays manual). New `deploy/promote-api.sh` smoke-tests `/ready` on the candidate and runs `update-traffic --to-latest`. Migrations run the image's own `goose` (`goose_db_version`) with the direct URL from secret `changeloom-migrate-database-url`. Probes use `/health`; secrets are pinned to version numbers; prod `MIN_INSTANCES=1` (billing).
- Android: one `android-release.yml` job; `versionCode` = run number + `ANDROID_VERSION_CODE_OFFSET`, `versionName` from the `v1.2.3` tag (new Gradle props `changeloom.versionCode`/`versionName`). Play upload via `r0adkll/upload-google-play` v1.1.5 with WIF credentials; App Distribution via `npx firebase-tools@15.32.1`. Verified locally: actionlint + shellcheck clean, scripts dry-run with a fake gcloud, Gradle props. Never run on GitHub or against GCP/Play.

## - [x] H6: Android stability, crash reporting and analytics
New dependencies: Firebase Crashlytics, Analytics and Performance (all under the existing BOM).
- **Firebase setup:** add the three SDKs. Collection is off in debug. No user ID is set.
- **Analytics:** an `Analytics` interface provided through Koin, with manual `screen_view` events and the event set from the plan: `login`/`sign_up`, `story_open`, `bookmark_add`, `share`, `search`, `topics_update`, `topic_request`, `notification_open`.
- **Errors:** `shared` throws a typed `ApiException(status, code)`. One mapper turns these into user-facing messages and replaces the raw `e.message` text.
- **Network:** Ktor `HttpRequestRetry` for GETs. On a 401, force a token refresh once, then sign out.
- **Crash fixes:**
  - Guard `openUri` against `ActivityNotFoundException`.
  - Cancel the FCM service scope in `onDestroy`.
  - One `SessionManager.signOut()` used by every sign-out path.
- **Logging:** a small logger. In release it sends non-fatals to Crashlytics; in debug it writes to logcat.

**Notes:**
- Added the Crashlytics Gradle plugin 3.0.8 (required for the build id), applied only when Firebase config exists, like google-services. Skipped the Performance Gradle plugin: the SDK gives app-start/screen traces, but no automatic HTTP traces (add the plugin later if wanted). Collection is switched per build type with the `firebaseCollectionEnabled` manifest placeholder; automatic screen reporting is off and `TrackScreen(name)` logs `screen_view` (sign_in, onboarding, feed/search/saved/profile, story, topics_edit). New code lives in `androidApp/.../telemetry/` (`AppLog`, `Analytics`) and `auth/SessionManager.kt`.
- Shared API: `ApiException(status, code, message)` parses the api's `{code,message}` body; GETs retry twice on IOException/502/503/504; a 401 forces one token refresh (`AuthRepository.idToken(forceRefresh)`) and a second 401 emits `ChangeloomApi.sessionExpired`, which `SessionManager` (app scope, created at start) turns into sign-out. `signInWithGoogleIdToken` now returns isNewUser (for `sign_up`). Messages: shared `userMessage`/`isUnexpected`, Android `errorText` for Firebase/Credential errors; `AppLog.failure` records only unexpected errors as non-fatals. `StoryPagerTest` now expects the mapped text instead of the raw "boom".
- Also fixed: MainActivity re-opened the notification's story after rotation (now only reads the intent when `savedInstanceState == null`), and the onboarding exit didn't clear the cache. Verified: shared tests (37), `testStagingDebugUnitTest`, `lintStagingDebug` (no new warnings), staging debug + unsigned release builds, merged manifests. Not verified on a device or in the Firebase console.

## - [x] H7: Android cold start and performance (benchmark numbers and the baseline profile are still pending: the user runs them)
New dependencies: `core-splashscreen`, `profileinstaller`, `benchmark-macro` (new `:baselineprofile` module), `leakcanary` (debug only).
- **Measure first:** Macrobenchmark cold start and timeline scroll; record the numbers here.
- **R8:** turn on minify and shrinkResources. Add `proguard-rules.pro` covering serialization and Ktor, and `res/raw/keep.xml` for `default_web_client_id`.
- **Baseline profile:** generate it covering startup → timeline → story.
- **Offline-first start:**
  - Cache the followed topic slugs.
  - `AppRoot` opens `MainScreen` from that cache and refreshes in the background. This fixes offline users being sent to onboarding.
  - Load `/v1/topics` and `/v1/me` in parallel.
- **Startup work:**
  - `core-splashscreen` with `setKeepOnScreenCondition`.
  - Create OkHttp lazily.
  - Bundle the fonts instead of using downloadable ones.
  - A Compose stability config only where the compiler reports show it is needed.
- **Debug tooling:** StrictMode and LeakCanary in debug.
- **Re-measure** and record before/after.

**Notes:**
- **Not measured, no profile yet.** The user skipped measuring. The benchmarks (`:baselineprofile`, `Benchmarks.kt`: cold start and feed scroll, with `None` and `Partial(Require)`) and the generator need a signed-in device that follows topics. Always pass `-Pandroid.injected.androidTest.leaveApksInstalledAfterRun=true`, because connected runs uninstall the app (and its sign-in). On an emulator, also pass `-Pandroid.testInstrumentationRunnerArguments.androidx.benchmark.suppressErrors=EMULATOR`. Run `:androidApp:generateStagingReleaseBaselineProfile`, commit the generated `src/staging/generated/baselineProfiles/`, then run `:baselineprofile:connectedStagingBenchmarkReleaseAndroidTest`. `benchmarkRelease`/`nonMinifiedRelease` are debug-signed, use debug's network config (cleartext to 10.0.2.2) and collect no Firebase data. UI hooks are the test tags `feed`, `story_card` and `story_detail`.
- R8: release builds are minified and resource-shrunk. `proguard-rules.pro` keeps `@Serializable` classes, the Credential Manager provider and Crashlytics line numbers, and `res/raw/keep.xml` keeps `default_web_client_id`. Release builds now upload Crashlytics mapping files. CI turns this off with `-Pchangeloom.crashlyticsMappingUpload=false` (the plugin's variant extension is internal, so it is set on the build type's `firebaseCrashlytics` extension). Verified: the R8 build starts and the Google picker opens. Not verified: signed-in api calls under R8, and the mapping upload in `android-release.yml`.
- Startup changes:
  - The followed slugs are cached (`StoryCache.loadFollowed`/`saveFollowed`, `followed.json`), and `/v1/topics` and `/v1/me` load in parallel.
  - `ChangeloomApi` takes a `Lazy<HttpClient>`, and `sharedModule` takes an engine factory.
  - `core-splashscreen` keeps the splash until `AppRoot` reports a real screen, for at most 1.5s. Before API 31 the splash icon is static.
  - The fonts are bundled variable TTFs (~1.2MB; `ui-text-google-fonts` and `font_certs.xml` were removed; `splash_window.xml` was deleted).
  - The stability config covers only `StorySummary`, `TimelineState` and `PagerState`.
- StrictMode (debug) shows `FirebaseAuth.getInstance()` reading from disk on the main thread in `Application.onCreate` (~290ms in a debug cold start, via `SessionManager` `createdAtStart`). Auth state is needed for the first screen, so this was left as is; check it in the profile.

## - [x] H8: Ads (AdMob native in-feed + UMP)
New dependencies: `play-services-ads`, `user-messaging-platform`, Firebase Remote Config.
- **IDs:** AdMob IDs come from Gradle properties; staging and debug use Google's test IDs.
- **`ConsentManager`:** UMP runs after the first frame. `MobileAds` is initialised off the main thread, and only when ads are allowed. Profile gets a "Privacy options" entry. Consent also drives Analytics Consent Mode.
- **Ad loading:** `NativeAdRepository` keeps a pool of 2–3 ads.
- **Placement:** `NativeAdCard` in the timeline after item 3 and then every 6, with stable keys. Never in story detail, search or bookmarks.
- **Remote Config:** `ads_enabled` and `ads_interval`.
- **User tasks:** create the AdMob account and ad units, publish `app-ads.txt` and a privacy policy, and update the Play Data safety form.

**Notes:**
- Versions: `play-services-ads` 25.5.0, UMP 4.0.0, `firebase-config` from the BOM. Code is in `androidApp/.../ads/`. Only prod release uses real ids (`changeloom.prod.admobAppId`/`admobNativeAdUnitId`, required by `verifyProdReleaseConfig`; the release workflow reads `ANDROID_ADMOB_*` variables); everything else uses Google's test ids. `changeloom.umpTestDeviceId` forces the EEA form in debug. The user's AdMob/UMP/Play/Remote Config steps are in `deploy/README.md` → Android app → Ads.
- Consent is gathered once per activity when the first real screen shows (`MainActivity.onContentReady`). Analytics consent mode defaults to denied in the manifest; `AnalyticsConsent.fromTcf` maps the TCF purposes (1, 3+4, 7) and grants all where GDPR doesn't apply. The pool is 3 ads (`loadAds`), reloaded on feed refresh once an hour old, cleared when consent or `ads_enabled` turns ads off; with one ad only the first slot shows it. Ads use the `ad-<slot>` key and `ad` content type; the card is a code-built `NativeAdView` in an `AndroidView`, with the manifest's `OPTIMIZE_INITIALIZATION`/`OPTIMIZE_AD_LOADING` flags on.
- Not verified: ads or the consent form on a device, and R8 with the ads SDK. The unit tests for slots and the TCF mapping are in H9's androidApp test setup.

## - [x] H9: Android product and policy polish (App Links still blocked on a domain)
New dependencies: Play `app-update` and `review`, Firebase App Check (Play Integrity), `kotlinx-coroutines-test`.
- **Account deletion UI:** calls `DELETE /v1/me`, then `FirebaseUser.delete()` (re-authenticating if needed).
- **Notifications:**
  - Rename the "Security alerts" channel to "Story alerts" and split channels by importance.
  - Show a rationale screen before asking for notification permission.
- **In-app update and review:**
  - Flexible in-app update.
  - Review prompt after about 5 story reads, at most once per 90 days.
- **Process death:** `SavedStateHandle` for the search query, the sign-in form and the selected tab.
- **App Check:** the client sends `X-Firebase-AppCheck`; the backend verifies it behind `APPCHECK_ENFORCE`.
- **App Links:** `/s/{id}`. Blocked until the user has a domain that can host `assetlinks.json`.
- **Tests:** androidApp ViewModel tests and one Compose UI smoke test.
- **Strings and accessibility:** move strings into `strings.xml` and give icons content descriptions.

**Notes:**
- New deps: Play `app-update` 2.1.0 and `review` 2.0.2, `firebase-appcheck-playintegrity` (+ `-debug` in debug only), and for tests `kotlin-test-junit`, `kotlinx-coroutines-test`, `ktor-client-mock`, Compose `ui-test-junit4`/`ui-test-manifest`, `androidx.test:runner` 1.7.0. The App Check provider is per build type (`src/debug`, `src/release`; the benchmark build types use `src/release`). Tokens are fetched lazily, time out after 3s, and are skipped for 5 minutes after a failure.
- Backend: `X-Firebase-AppCheck` is checked on `/v1/` outside dev (`internal/auth/appcheck.go`, `checkApp` middleware); `APPCHECK_ENFORCE=false` only logs "app check would reject", true returns a 403 `app_check_failed` (never a 401: the app would sign out). If the keys can't be fetched at startup, the api starts unchecked unless enforcing. FCM messages now name the `story_alerts` channel; the app deletes the old `security_alerts` channel. `deploy-api.sh` passes `APPCHECK_ENFORCE` (default false).
- Deletion: `SessionManager.deleteAccount` runs `DELETE /v1/me`, clears the cache, then `FirebaseUser.delete()` in the app scope. Re-auth happens first when the last sign-in is older than 4 minutes (password dialog, or the Google picker for Google accounts) and again if Firebase still asks. Process death: the session ViewModel store now has its own `SavedStateRegistryOwner` (`SessionOwner` in `Screens.kt`), so session ViewModels can take a `SavedStateHandle`, restored for the same account only. Search (query + last search, re-run on restore) and sign-in (mode + email; the password is never saved) use it. The tab, open story and editor were already in `rememberSaveable`, which survives process death, so they were left as they are.
- Strings: all Android UI and ViewModel text is in `strings.xml` (ViewModels use a `Strings` lookup; `errorText` is `Strings.errorText(e, @StringRes)`); the shared module's `userMessage` text is still English in Kotlin (it's shared with a future iOS app). Dates use locale-ordered patterns. Swipe actions are exposed as accessibility custom actions. Search suggestion chips stay English: they're queries against English stories.
- Verified: backend `make test lint`; shared tests, 17 androidApp unit tests, `lintStagingDebug`, the unsigned R8 `assembleStagingRelease`; 3 Compose smoke tests (`FeedSmokeTest`) on the Pixel_9 emulator via `connectedStagingDebugAndroidTest`; on the emulator, the app starts with the new SDKs, UMP runs (not-EEA), and the sign-in email survives process death while the password doesn't. Not verified: ads/consent form, in-app update, review dialog, App Check tokens and deletion against real Firebase/Play.

## Verification (H phases)
- **Backend:** `make test lint`; `curl` the local api for `/ready`, a JSON 404 and the log fields; timeline `EXPLAIN` before and after.
- **Android:** `gradle :shared:testAndroidHostTest :androidApp:testStagingDebugUnitTest :androidApp:lintStagingDebug :androidApp:assembleStagingRelease`; Macrobenchmark numbers; Crashlytics, DebugView and AdMob test ads on a staging device (the user runs these).
- **Not verified unless the user runs it:** GCP, Firebase, Play and AdMob.
