# Backend (Go `api`)

Work here as a senior backend engineer running a production service. Before writing code, think through correctness
under concurrency and retries, failure modes, security, data integrity, query cost, and the Android clients already
in the field. Production grade means validated input, bounded work, explicit errors and tests, and no surprises in
prod.

## Done means verified
- Changed `api/openapi.yaml`, `internal/db/queries` or `migrations`? Run `make generate`. After a backend migration,
  also run `make curator-generate` and `make curator-test`.
- `make lint` and `make test` must be clean; tests need `make db-up`.
- Bug fix: write the failing test first. `internal/dbtest.New(t)` gives each test a throwaway database.
- New endpoints and queries get tests for the happy path, missing auth, invalid input, empty results, pagination
  boundaries, and another user's data.
- Add `-race` for concurrent code.
- Report what you couldn't verify (real Firebase or FCM, Cloud Run, Neon). Check a claim before stating it.

## API
- `api/openapi.yaml` is the contract: change it first, regenerate, then implement. Released apps can't update
  instantly, so changes are additive only. New fields are optional; never rename, remove or change the meaning of
  existing ones.
- Every handler authenticates, then authorizes by scoping every query to the caller's user id. It validates and
  bounds input (`http.MaxBytesReader`, list sizes, page limits) and returns the documented error shape.
- Internal errors are logged with context and never leaked to clients.
- Writes are idempotent (PUT replace semantics, upserts): clients and Cloud Run retry.

## Database
- Schema changes are new goose migrations in `migrations/`. Never edit one that has been applied.
- Migrations must be safe on a live database: no long locks on big tables, new columns nullable or with defaults,
  backfills separate, and indexes for new query patterns.
- Queries go through sqlc, parameterized.
- Paginate with keyset pagination, not OFFSET.
- Check `EXPLAIN ANALYZE` for anything touching stories or the timeline; see the JIT note on `ListTimeline`.
- Writes that must land together share a transaction. Avoid N+1 queries.

## Go
- Pass `context.Context` everywhere and respect cancellation. Outbound calls have timeouts; retries and goroutines
  are bounded.
- Wrap errors with context (`fmt.Errorf("save user: %w", err)`) and use `errors.Is`/`errors.As`. No ignored errors,
  no panics on request paths.
- Use structured `slog` through `internal/logging` at the right level. Never log PII or tokens.
- Config comes from `internal/config`. No hard-coded URLs, project ids or magic numbers.
- Handlers stay thin. No global mutable state.
