# Changeloom

Layout: `backend/` (Go api), `curator/` (separate Go module; fetches content with `claude -p`), `mobile/` (Android app), `deploy/` (hosted setup, see `deploy/README.md`). List all make targets with `grep '##' Makefile`. Hosted setup (GCP, Firebase, Neon, Cloud Run) is documented in [`deploy/README.md`](deploy/README.md).

Environments: `dev` (local), `staging`, `prod`. Make targets that take `DEPLOY_ENV` default to `staging`.

## Dev database
```sh
make db-up              # start dev Postgres (docker) or check the local one (POSTGRES_MODE in .env)
make db-down            # stop docker Postgres (volume kept)
make migrate            # apply pending migrations
make migrate-down       # roll back the latest migration
make migrate-status
```

## Backend
```sh
make generate           # sqlc + oapi-codegen
make run-api            # run the api locally (dev auth: Authorization: Bearer dev:<name>, ENV=dev only)
make build              # binaries into backend/bin
make test               # needs make db-up
make lint
make fmt
```

## Curator
Config lives in `curator/.env` (copy `curator/.env.example`).
```sh
make curator-db curator-migrate curator-seed    # first-time local setup
make curator-generate   # after backend migrations change
make curator-build      # binary into curator/bin
make curator-test
make curator-lint
make curator-fmt
```
Run the binary from `curator/` (it does not read `.env` itself; load it first):
```sh
cd curator
export $(grep -E '^[A-Z_]+=' .env | xargs)
export CLAUDE_CONFIG_DIR="${CLAUDE_CONFIG_DIR:-$HOME/.claude-personal}"
./bin/curator run                    # everything, in order (real claude -p calls)
./bin/curator migrate [--remote --env <env>]
./bin/curator seed | requests | fetch | sync [--env <env>]
./bin/curator prune [--env <env>]
```

## Hosted (staging / prod)
Steps are in `deploy/README.md`. Add `DEPLOY_ENV=prod` only when you mean prod.
```sh
make migrate-remote DEPLOY_ENV=staging   # REMOTE_DATABASE_URL_<ENV> in curator/.env
make curator-sync DEPLOY_ENV=staging     # push the curator's local catalog and stories to the hosted DB
make deploy-api DEPLOY_ENV=staging       # Cloud Run; reads deploy/<env>.env; migrate first
make release                             # propose next version from commits, tag main and push (asks first)
make release PRE=alpha                   # same, as a closed-testing build (vX.Y.Z-alpha.N)
make release VERSION=x.y.z DRY_RUN=1     # only show what it would do
```

## Android
```sh
make android-apk DEPLOY_ENV=staging      # signed release APK
make android-bundle DEPLOY_ENV=prod      # signed AAB for Play Console
```
The version code defaults to 1; Play needs a higher one each upload:
```sh
make android-bundle DEPLOY_ENV=prod GRADLE="./gradlew -Pchangeloom.versionCode=3"
```
AAB output: `mobile/androidApp/build/outputs/bundle/prodRelease/androidApp-prod-release.aab`.
Release signing uses `mobile/keystore.properties`. If the wrapper download fails, use the system gradle:
```sh
cd mobile
ANDROID_HOME=~/Library/Android/sdk gradle :shared:testAndroidHostTest :androidApp:assembleDebug
```
Flavor tasks: `assembleStagingDebug`, `assembleProdDebug`, etc.
