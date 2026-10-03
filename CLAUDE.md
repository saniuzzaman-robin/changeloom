# Changeloom

## Status
- P0–P8 done (backend, ingestion, AI, auth, deploy, Android app, FCM/discovery/dedupe/search/bookmarks).
- **P9 iOS: deferred** by the user. Not started; don't work on it unless asked (iOS targets, SwiftUI/CMP app, Sign in with Apple, APNs, TestFlight).
- Local Claude curator + Neon/Cloud Run done in code (hosted setup is the user's Phase 8); production hardening H1–H9 done in code (user steps and unverified items in `PLAN.md` and `deploy/README.md`). The RSS/Gemini worker pipeline is deleted; the backend is the `api` binary only, and content comes from the curator (`curator/`, separate Go module; `migrate`/`seed`/`fetch`/`requests`/`sync`/`run` all implemented).
- Never verified live: real Firebase tokens/FCM sends, Android emulator/lint, CI workflow, real deploy (Neon, Cloud Run).

## Gotchas
- Dev Postgres: `POSTGRES_MODE` in `.env` is `docker` (compose, host port from `POSTGRES_PORT`; `make db-up`) or `local` (installed server at `DATABASE_URL`; `psql` must be on PATH, e.g. `/Library/PostgreSQL/18/bin`). Tests use `internal/dbtest.New(t)`.
- Android build: `ANDROID_HOME=~/Library/Android/sdk gradle :shared:testAndroidHostTest :androidApp:assembleDebug` (system gradle; wrapper download timed out).
- Codegen: `make generate` (sqlc + oapi-codegen).
- Dev auth `Bearer dev:<name>` only when `ENV=dev`; prod uses Firebase (`FIREBASE_PROJECT_ID`).
- Environments: local `dev` plus hosted `staging` and `prod`, each a separate GCP/Firebase project + Neon DB + Cloud Run service; steps in `deploy/README.md`. Make targets take `DEPLOY_ENV` (default `staging`; prod only when explicitly asked): `migrate-remote` (`REMOTE_DATABASE_URL_<ENV>` in `curator/.env`), `deploy-api` (`deploy/<env>.env`), `android-apk`/`android-bundle`. Never run deploys or migrate-remote yourself.
- Android UI rule: content never scrolls under the status bar or the navigation bar (buttons or gesture handle). Scrolling containers stop at the bars: `Modifier.tabContentBounds(shellPadding)` for tab screens, `windowInsetsPadding(WindowInsets.safeDrawing…)` elsewhere. Only backgrounds (`ScreenBackdrop`, bar fills) draw behind the bars.
- Android flavors `staging`/`prod` (tasks like `assembleStagingDebug`); api URL from `changeloom.<env>.apiBaseUrl`, app ids `dev.changeloom.android.staging` (staging, installs beside prod) and `dev.changeloom.android` (prod), Firebase config from `androidApp/src/<env>/google-services.json`, else `androidApp/google-services.json` (each must have a client for its flavor's app id), release signing from `mobile/keystore.properties`.
- The api no longer syncs topics at startup; tests sync `backend/seed/topics.yaml`.
- Curator: config in `curator/.env` (copy `.env.example`; the Makefile includes it). `make curator-db curator-migrate curator-seed`, then `curator-test`/`curator-lint`. The real topic catalog is `curator/seed/catalog.yaml`. `curator fetch` makes real `claude -p` calls (subscription usage); tests fake the CLI.
- Curator schema = `backend/migrations` (read from `BACKEND_MIGRATIONS_DIR`, default `../backend/migrations`, so run it from `curator/`) + `curator/migrations` (goose table `curator_goose_db_version`). Rerun `make curator-generate` after backend migrations change.
