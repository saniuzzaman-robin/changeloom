# Changeloom

## Status
- P0–P8 done (backend, ingestion, AI, auth, deploy, Android app, FCM/discovery/dedupe/search/bookmarks).
- **P9 iOS: deferred** by the user. Not started; don't work on it unless asked (iOS targets, SwiftUI/CMP app, Sign in with Apple, APNs, TestFlight).
- In progress: local Claude curator + Neon/Cloud Run (see `PLAN.md`). The RSS/Gemini worker pipeline is deleted; the backend is the `api` binary only, and content comes from the curator (`curator/`, separate Go module; `migrate`/`seed`/`fetch` work, `requests`/`sync`/`run` are stubs until Phases 5–6).
- Never verified live: real Firebase tokens/FCM sends, Android emulator/lint, CI workflow, real deploy (Neon, Cloud Run).

## Gotchas
- Dev Postgres: `POSTGRES_MODE` in `.env` is `docker` (compose, host port from `POSTGRES_PORT`; `make db-up`) or `local` (installed server at `DATABASE_URL`; `psql` must be on PATH, e.g. `/Library/PostgreSQL/18/bin`). Tests use `internal/dbtest.New(t)`.
- Android build: `ANDROID_HOME=~/Library/Android/sdk gradle :shared:testAndroidHostTest :androidApp:assembleDebug` (system gradle; wrapper download timed out).
- Codegen: `make generate` (sqlc + oapi-codegen).
- Dev auth `Bearer dev:<name>` only when `ENV=dev`; prod uses Firebase (`FIREBASE_PROJECT_ID`).
- Deploy target: Cloud Run (api image from `backend/Dockerfile`, listens on `$PORT`) + Neon Postgres. Env list in `deploy/.env.example`. Hosted migrations: `make migrate-remote` with `REMOTE_DATABASE_URL` = Neon's direct (non-pooled) URL.
- The api no longer syncs topics at startup; tests sync `backend/seed/topics.yaml`.
- Curator: config in `curator/.env` (copy `.env.example`; the Makefile includes it). `make curator-db curator-migrate curator-seed`, then `curator-test`/`curator-lint`. The real topic catalog is `curator/seed/catalog.yaml`. `curator fetch` makes real `claude -p` calls (subscription usage); tests fake the CLI.
- Curator schema = `backend/migrations` (read from `BACKEND_MIGRATIONS_DIR`, default `../backend/migrations`, so run it from `curator/`) + `curator/migrations` (goose table `curator_goose_db_version`). Rerun `make curator-generate` after backend migrations change.
