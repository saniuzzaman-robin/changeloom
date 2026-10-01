# Changeloom

## Status
- P0–P8 done (backend, ingestion, AI, auth, deploy, Android app, FCM/discovery/dedupe/search/bookmarks).
- **P9 iOS: deferred** by the user. Not started; don't work on it unless asked (iOS targets, SwiftUI/CMP app, Sign in with Apple, APNs, TestFlight).
- Never verified live: AI provider (Gemini) calls, real Firebase tokens/FCM sends, Android emulator/lint, CI workflow, real deploy.

## Gotchas
- Dev Postgres on host port 5433 (`make db-up`); tests use `internal/dbtest.New(t)`.
- Android build: `ANDROID_HOME=~/Library/Android/sdk gradle :shared:testAndroidHostTest :androidApp:assembleDebug` (system gradle; wrapper download timed out).
- Codegen: `make generate` (sqlc + oapi-codegen).
- Dev auth `Bearer dev:<name>` only when `ENV=dev`; prod uses Firebase (`FIREBASE_PROJECT_ID`).
