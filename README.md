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
./bin/curator run --if-due           # skip if the last fetch is under CURATOR_RUN_MIN_GAP_HOURS ago
./bin/curator migrate [--remote --env <env>]
./bin/curator seed | requests | fetch | sync [--env <env>]
./bin/curator backfill [--max-calls N]
./bin/curator prune [--env <env>]
```

### Scheduling (macOS launchd)
Runs `curator run --if-due` at 02:00, 08:00, 14:00, 20:00 and at login. From the repo root:
```sh
make curator-build
sed -e "s|__REPO__|$PWD|g" -e "s|__HOME__|$HOME|g" curator/launchd/dev.changeloom.curator.plist \
  > ~/Library/LaunchAgents/dev.changeloom.curator.plist
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/dev.changeloom.curator.plist
launchctl print gui/$(id -u)/dev.changeloom.curator          # check it is loaded
launchctl kickstart gui/$(id -u)/dev.changeloom.curator      # run once now
launchctl bootout gui/$(id -u)/dev.changeloom.curator        # remove (also before reinstalling)
tail -n 30 ~/Library/Logs/changeloom-curator.log
```
Keep the repo outside `~/Documents`; launchd jobs cannot read it (`Operation not permitted`).

## Hosted (staging / prod)
Steps are in `deploy/README.md`. Add `DEPLOY_ENV=prod` only when you mean prod.
```sh
make migrate-remote DEPLOY_ENV=staging   # REMOTE_DATABASE_URL_<ENV> in curator/.env
make deploy-api DEPLOY_ENV=staging       # Cloud Run; reads deploy/<env>.env; migrate first
make release                             # propose next version from commits, tag main and push (asks first)
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
