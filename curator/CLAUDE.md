# Curator (Go, separate module)

Work here as a senior backend engineer owning a production data pipeline. Before writing code, think through
reruns, partial failure, cost, data quality, and the hosted databases the app reads from. Production grade means
idempotent steps, bounded work, validated data, explicit errors and tests.

## Done means verified
- Run `make curator-lint` and `make curator-test` (needs `make db-up`) from the repo root; both must be clean.
  Rerun `make curator-generate` after backend migrations or `internal/db/queries` change.
- Bug fix: write the failing test first. `internal/dbtest` gives a throwaway database.
- Tests never call a real model: fake the CLI or API (see `fakeCLI` in `internal/claude/claude_test.go`).
- Never run `fetch`, `sync`, `prune --env` or `migrate --remote` yourself. Real fetches use the user's
  subscription, and the others touch hosted databases. Show the command instead.
- Report what you couldn't verify. Check a claim before stating it.

## Pipeline
- Every command is safe to rerun and to interrupt: upserts and dedupe keys, no duplicate stories, no half-applied
  batches. Writes that must land together share a transaction.
- Model output is untrusted input. Parse it strictly; validate enums, lengths, dates and countries; normalise and
  check URLs (`internal/urlnorm`); dedupe. Drop or log bad items and never write them.
- Bound everything: model calls (`--max-calls`, demand tiers), timeouts on every CLI or HTTP call, retries, batch and
  query sizes.
- Prune never deletes saved stories. Any change near it needs a test proving that.
- The schema is `../backend/migrations` plus `curator/migrations` (the curator's goose version table). Curator-only
  tables go in `curator/migrations`. Never change shared tables from here; change them in the backend.

## Go
- Pass `context.Context` everywhere. Wrap errors with context and use `errors.Is`/`errors.As`. No ignored errors.
- Use structured `slog` logging. Never log secrets or API keys.
- Config comes from `internal/config`; every key is documented in `.env.example`. No hard-coded models, URLs or
  limits.
