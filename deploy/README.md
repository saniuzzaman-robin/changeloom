# Environments, builds and deploys

Changeloom has a local dev setup plus two hosted environments, **staging** and **prod**. Each hosted
env is fully separate: its own GCP project (which is also its Firebase project), Neon database,
Cloud Run service and Android app.

| | dev (local) | staging | prod |
|---|---|---|---|
| api `ENV` | `dev` (dev auth: `Bearer dev:<name>`) | `staging` (Firebase auth) | `prod` (Firebase auth) |
| api settings | root `.env` | `deploy/staging.env` (CI: GitHub variable) | `deploy/prod.env` (CI: GitHub variable) |
| Database | dev Postgres (`make db-up`) | Neon, staging | Neon, prod |
| Android flavor | `staging`/`prod` debug, local api by default | `staging`: `dev.changeloom.android.staging` (installs beside prod), "Changeloom Staging" | `prod`: `dev.changeloom.android` |
| Curator target | local DB | `--env staging` | `--env prod` |

Make targets that touch a hosted env take `DEPLOY_ENV=staging|prod`, which **defaults to staging**.
Prod is only ever targeted by passing `DEPLOY_ENV=prod` explicitly.

Files holding secrets are gitignored and never committed: `curator/.env`, `mobile/keystore.properties`,
the keystore itself, and `google-services.json`. The deploy settings `deploy/<env>.env` are committed
(the repo is public): they hold no secret values, only the names of Secret Manager secrets.

## One-time setup (per env: staging first, then prod)

These commands create cloud resources and may cost money. Read them, then run them yourself.
`P` is the env's GCP project id, `R` its Cloud Run region (e.g. `us-east4`).

1. **GCP + Firebase project.** Create the project (e.g. `changeloom-staging`, `changeloom-prod`),
   link billing, then add Firebase to it in the Firebase console. Enable the APIs:
   ```sh
   gcloud services enable run.googleapis.com cloudbuild.googleapis.com \
     artifactregistry.googleapis.com secretmanager.googleapis.com --project "$P"
   ```
2. **Firebase Auth + Android app.** In the Firebase console, enable the Google sign-in provider and add
   an Android app with the env's application id (table above). Add the SHA-1 of every key that signs
   that app: your debug key (`~/.android/changeloom-debug.keystore` if you use one), the upload key,
   and for prod the Play app signing key (Play Console → Test and release → App integrity).
   The two app ids differ, so both projects can list the same debug and upload keys.
   Download `google-services.json`: staging's goes to `mobile/androidApp/src/staging/google-services.json`
   and prod's to `mobile/androidApp/src/prod/google-services.json` (debug builds of each flavor use their
   env's file).
   Release builds report to Crashlytics, Analytics and Performance Monitoring (debug builds don't):
   link Google Analytics to the Firebase project (Project settings → Integrations), and declare crash
   logs, diagnostics and app interactions in Play's Data safety form.
3. **Neon.** Create a database for the env, in a region near `R`. Note two URLs:
   - the **direct** (non-pooled) URL → `REMOTE_DATABASE_URL_<ENV>` in `curator/.env` (migrations and sync);
   - the **pooled** URL → the api's `DATABASE_URL` secret (next step). If the api reports prepared
     statement errors on it, append `default_query_exec_mode=exec` to that URL.
4. **Secrets and the api's service account.**
   ```sh
   gcloud iam service-accounts create changeloom-api --project "$P"
   SA="changeloom-api@$P.iam.gserviceaccount.com"

   printf '%s' "<pooled Neon URL>" | gcloud secrets create changeloom-database-url --project "$P" --data-file=-
   NOTIFY=$(openssl rand -hex 32)   # also goes into curator/.env as NOTIFY_SECRET_<ENV>
   printf '%s' "$NOTIFY" | gcloud secrets create changeloom-notify-secret --project "$P" --data-file=-

   for s in changeloom-database-url changeloom-notify-secret; do
     gcloud secrets add-iam-policy-binding "$s" --project "$P" \
       --member "serviceAccount:$SA" --role roles/secretmanager.secretAccessor
   done
   ```
   With `FCM_ENABLED=true`, the service account also needs permission to send FCM messages, e.g.
   `roles/firebasecloudmessaging.admin` on the project. Verifying Firebase ID tokens needs no IAM role.
5. **Deploy settings.** Edit the committed `deploy/<env>.env`
   (`GCP_PROJECT`, `SERVICE_ACCOUNT=$SA`, `FIREBASE_PROJECT_ID`, ...). Secrets are pinned to a
   version: after `gcloud secrets versions add`, bump the matching `*_VERSION` and redeploy.
6. **Curator.** In `curator/.env`, set `REMOTE_DATABASE_URL_<ENV>`, `NOTIFY_SECRET_<ENV>`, and after
   the first deploy `API_BASE_URL_<ENV>` (the Cloud Run URL).

## Backend (api on Cloud Run)

Normally the deploy workflow releases the api (see Continuous deployment): every push to `staging` that
passes CI goes to staging, and merging `staging` into `main` deploys to prod. The first deploy of each
env must be manual, because it creates the service and makes it public; the workflow only adds
revisions to an existing service.

By hand, release to staging, check it, then release the same commit to prod:

```sh
make migrate-remote                      # staging DB: pending backend migrations
make deploy-api                          # Cloud Build from backend/, deploy to staging Cloud Run
curl -fsS https://<staging url>/ready

make migrate-remote DEPLOY_ENV=prod
make deploy-api DEPLOY_ENV=prod          # committed code only; asks you to type 'prod'
```

- `deploy/deploy-api.sh` prints the full `gcloud run deploy` command before running it and labels the
  revision with `env` and `commit` (`<sha>-dirty` for uncommitted staging deploys). Instance size,
  concurrency, timeout, probes (`/health`) and secret versions all come from `deploy/<env>.env`.
- With `--image <image@sha256:...>` it deploys that image with no traffic under the tag `candidate`;
  `deploy/promote-api.sh <env>` then checks `/ready` on it and sends it all traffic.
- The first `--source` deploy in a project asks to create an Artifact Registry repository; accept it.
- Migrations are forward-only and run before the new code, so keep them compatible with the code that
  is still serving (add columns first, drop them in a later release).
- Roll back by sending traffic to an earlier revision:
  ```sh
  gcloud run revisions list --service changeloom-api --project "$P" --region "$R"
  gcloud run services update-traffic changeloom-api --to-revisions <revision>=100 --project "$P" --region "$R"
  ```

## VPS (shared server for changeloom prod and cou-alumni)

One OVHcloud VPS-1 in Singapore runs Docker Compose projects under `/srv/<project>/` behind Caddy, which owns
ports 80/443 and TLS. Each project ships a site file into `/etc/caddy/sites/`. Neon stays the database.

1. **Order** a VPS-1 in Asia-Pacific, Singapore, with the plain **Debian 13** image (not one with apps).
   OVH emails the `debian` user's password; the first login makes you change it.
2. **SSH key** on your machine, then copy it over and add a `Host sg1` entry (`HostName <vps-ip>`,
   `User debian`, `IdentityFile ~/.ssh/ovh_sg1`, `IdentitiesOnly yes`) to `~/.ssh/config`:
   ```sh
   ssh-keygen -t ed25519 -C ovh-sg1 -f ~/.ssh/ovh_sg1
   ssh-copy-id -i ~/.ssh/ovh_sg1.pub sg1
   ```
3. **Bootstrap** (as root; safe to rerun). It copies the `debian` user's keys to a new `deploy` user,
   turns off root and password SSH logins, and installs ufw, swap, unattended upgrades, Docker and Caddy:
   ```sh
   scp deploy/vps/bootstrap.sh sg1:
   ssh sg1 'sudo bash bootstrap.sh'
   ```
   Keep that session open and check in a new terminal that the deploy user works:
   ```sh
   ssh -i ~/.ssh/ovh_sg1 deploy@<vps-ip> 'docker compose version && sudo systemctl reload caddy'
   ```
4. **CI deploy key**: a key only GitHub Actions uses. Add its public half to the deploy user by rerunning
   bootstrap with it, and store the private half and the host key as secrets `VPS_SSH_KEY` and
   `VPS_KNOWN_HOSTS` on the `production` environment, plus `VPS_HOST` (the IP, the same one you scan):
   ```sh
   ssh-keygen -t ed25519 -C github-deploy -N '' -f ~/.ssh/vps_github_deploy
   scp ~/.ssh/vps_github_deploy.pub sg1: && ssh sg1 'sudo bash bootstrap.sh vps_github_deploy.pub'
   ssh-keyscan -t ed25519 <vps-ip>   # the VPS_KNOWN_HOSTS value
   ```
   Delete the local private key once it is in GitHub.
5. **DNS**: an A (and AAAA) record per api domain pointing at the VPS. Caddy gets the certificate on first
   request once the project's site file is in place.
6. **changeloom prod files** in `/srv/changeloom/` (the api domain is `changeloom-api.saniuzzaman.dev`, set
   in `deploy/vps/changeloom.caddy`; the image is public on GHCR, so the server needs no registry login):
   ```sh
   scp deploy/vps/{compose.yaml,changeloom.caddy,deploy.sh} deploy@<vps-ip>:/srv/changeloom/
   scp deploy/vps/api.env.example deploy@<vps-ip>:/srv/changeloom/api.env
   scp deploy/vps/migrate.env.example deploy@<vps-ip>:/srv/changeloom/migrate.env
   ```
   Fill in `api.env` (pooled Neon URL, `NOTIFY_SECRET`) and `migrate.env` (direct Neon URL) on the server with
   the same values as the prod Secret Manager secrets, then `chmod 600` both. `migrate.env` is read only by
   `deploy.sh`, so the api never holds the direct URL.
7. **FCM key.** **IAM change:** a service account in the prod project whose only role is the FCM one, with a
   JSON key at `/srv/changeloom/fcm.json` (mode 600, owned by `deploy`). The api reads it through
   `GOOGLE_APPLICATION_CREDENTIALS` (set in `compose.yaml`). If key creation is refused, the org policy
   `iam.disableServiceAccountKeyCreation` is on for the project. Keep the key off your machine afterwards:
   ```sh
   P=changeloom-prod-510511 SA=changeloom-vps-fcm@changeloom-prod-510511.iam.gserviceaccount.com
   gcloud iam service-accounts create changeloom-vps-fcm --project "$P" --display-name "changeloom VPS FCM sender"
   gcloud projects add-iam-policy-binding "$P" --member "serviceAccount:$SA" \
     --role roles/firebasecloudmessaging.admin --condition None
   gcloud iam service-accounts keys create fcm.json --iam-account "$SA" --project "$P"
   scp fcm.json deploy@<vps-ip>:/srv/changeloom/ && ssh deploy@<vps-ip> chmod 600 /srv/changeloom/fcm.json
   rm fcm.json
   ```
8. **Deploy** a tag (CI does this on each prod deploy; by hand only to test or roll back):
   `ssh deploy@<vps-ip> /srv/changeloom/deploy.sh <tag>`. It installs the Caddy site file when it changed,
   pulls `ghcr.io/saniuzzaman-robin/changeloom-api:<tag>`, runs the goose migrations, starts the api and
   waits for `/ready`. If the api does not become healthy it restarts the previous tag (`current-tag`);
   migrations are not rolled back, so they must stay compatible with the previous release.
   The first CI push creates the GHCR package `changeloom-api` (private by default, linked to this repo
   through the image's source label): make it public in its package settings, or the server cannot pull, then rerun the failed job.
9. **Cutover of changeloom prod** (once; `D=changeloom-api.saniuzzaman.dev`, `<vps-ip>` as above):
   1. Steps 4–7 are done and `dig +short $D` prints the VPS IP.
   2. **First deploy:** merge `staging` into `main` and approve the prod run (step 8 for the GHCR package).
      Cloud Run prod still gets the same release, so the current app keeps working.
   3. **Smoke test the domain** (the api logs go to the container: `ssh deploy@<vps-ip> docker logs --since 10m changeloom-api-1`;
      `docker compose logs` needs the `API_IMAGE` that only `deploy.sh` sets):
      - `curl -fsS https://$D/ready` passes, so Caddy has the certificate and the api reaches Neon.
      - **Client IP.** From your machine, use up your IP's burst (`RATE_LIMIT_IP_PER_MIN`, 300), then
        try a forged header. Send in parallel: the bucket refills 5 per second, faster than sequential curls drain it.
        ```sh
        seq 400 | xargs -P 40 -I{} curl -s -o /dev/null -w '%{http_code}\n' https://$D/v1/topics | sort | uniq -c
        curl -s -o /dev/null -w '%{http_code}\n' -H 'X-Forwarded-For: 203.0.113.9' https://$D/v1/topics
        ```
        The loop ends in 429s, and the forged header still gets 429 (Caddy replaces it). A request from another
        network (phone data) in the same minute is not 429, so each client has its own bucket rather than
        sharing Caddy's loopback address. Your IP recovers within a minute.
      - **Sign-in, App Check, FCM:** set the `production` environment variable `ANDROID_API_BASE_URL` to
        `https://$D`, then `make release PRE=alpha` and install the closed-testing build from Play (App
        Check's Play Integrity only passes for Play-installed builds). Sign in, open the timeline and save a
        story; the api logs show no 401/403. For a push, set `API_BASE_URL_PROD=https://$D` in
        `curator/.env` and `make curator-sync DEPLOY_ENV=prod` with a new high/critical security story in
        a topic the test account follows; the phone gets it and the logs show no FCM errors.
   4. **Release the app:** `make release`, then roll it out in Play. Old versions keep calling Cloud Run prod,
      which the workflow deploys too while `CLOUD_RUN_DEPLOY=true`; both share the Neon DB, which is safe
      because the api is stateless and push sends lock their rows with `SKIP LOCKED`.
   5. **Retire Cloud Run prod** only after Play's stats show the old versions are gone: delete the service
      (`gcloud run services delete changeloom-api --project changeloom-prod-510511 --region asia-southeast1`,
      run it yourself), its uptime check, and set `CLOUD_RUN_DEPLOY=false` in `deploy/prod.env`.
      `MIN_INSTANCES` is already 0, so until then Cloud Run prod only bills for requests.

Notes:
- Containers must publish ports on `127.0.0.1` only. Docker's port rules bypass ufw, so a `0.0.0.0` port is public.
- Unattended upgrades install Debian security updates but never reboot. Check `ls /var/run/reboot-required`
  after kernel updates and reboot in a quiet hour (`ssh sg1 'sudo reboot'`); containers restart on their own.
- The `deploy` user is in the `docker` group, which is root-equivalent; guard its keys accordingly.

## Monitoring (per env; prod first, staging optional)

These commands create billable resources and change IAM. Read them, then run them yourself. Load the
env's settings first so `P`, `CLOUD_RUN_SERVICE` and `MAX_INSTANCES` match the deploy:

```sh
set -a; source deploy/prod.env; set +a; P=$GCP_PROJECT
gcloud services enable monitoring.googleapis.com logging.googleapis.com cloudtrace.googleapis.com \
  telemetry.googleapis.com billingbudgets.googleapis.com --project "$P"
```

1. **Tracing.** The api sends traces over OTLP to `OTEL_EXPORTER_OTLP_ENDPOINT` when
   `OTEL_ENABLED=true` in `deploy/<env>.env`. Its sample rate is `OTEL_SAMPLE_RATIO`: staging's example
   uses 1, prod's 0.1. **IAM:** the runtime service account needs the traces writer role:
   ```sh
   gcloud projects add-iam-policy-binding "$P" \
     --member "serviceAccount:$SERVICE_ACCOUNT" --role roles/telemetry.tracesWriter
   ```
   Redeploy afterwards (`make deploy-api`). Cloud Run gives the instance CPU only while it serves
   requests, so spans can sit in the buffer until the next request or shutdown, which flushes them.
2. **Notification channel** (where alerts go). Create an email channel in the console (Monitoring →
   Alerting → Edit notification channels), or with the beta component
   (`gcloud components install beta`):
   ```sh
   gcloud beta monitoring channels create --project "$P" --display-name "changeloom alerts" \
     --type email --channel-labels email_address=<you@example.com>
   CHANNEL=$(gcloud beta monitoring channels list --project "$P" \
     --filter 'displayName="changeloom alerts"' --format 'value(name)')
   ```
3. **Uptime check** on `/health` every 15 minutes. Each check can wake a scaled-to-zero instance, so
   don't make it more frequent. Alert on it in the console (Monitoring → Uptime checks → the check →
   Create alert), choosing `$CHANNEL`.
   ```sh
   HOST=$(gcloud run services describe "$CLOUD_RUN_SERVICE" --project "$P" --region "$GCP_REGION" \
     --format 'value(status.url)' | sed 's|https://||')
   gcloud monitoring uptime create "changeloom api health" --project "$P" \
     --resource-type uptime-url --resource-labels "host=$HOST,project_id=$P" \
     --protocol https --path /health --period 15
   ```
4. **Alert policies** from `deploy/monitoring/`:
   - 5xx responses
   - p95 latency
   - instances at `MAX_INSTANCES`
   ```sh
   mkdir -p /tmp/changeloom-monitoring
   for f in deploy/monitoring/*.yaml; do
     out=/tmp/changeloom-monitoring/$(basename "$f")
     sed -e "s/__SERVICE__/$CLOUD_RUN_SERVICE/g" -e "s/__MAX_INSTANCES__/$MAX_INSTANCES/g" "$f" > "$out"
     gcloud monitoring policies create --project "$P" --notification-channels "$CHANNEL" --policy-from-file "$out"
   done
   ```
5. **Error Reporting.** The api's error logs show up there automatically. Turn on notifications in the
   console: Error Reporting → Configure notifications → `$CHANNEL`.
6. **Billing budget.** Alerts the billing account's admins by email at 50%, 90% and 100% of the
   amount. **Billing:** pick an amount that fits; the free tiers should keep normal use near 0.
   ```sh
   gcloud billing budgets create --billing-account <BILLING_ACCOUNT_ID> \
     --display-name "changeloom $P" --budget-amount 10USD --filter-projects "projects/$P" \
     --threshold-rule percent=0.5 --threshold-rule percent=0.9 --threshold-rule percent=1.0
   ```

### Curator: catalog and prune

The topic catalog (`curator/seed/catalog/`) and the keys below are tuned in `curator/.env`; defaults are in `curator/.env.example`.

- **Loading a changed catalog.** `make curator-migrate curator-seed` (local DB), then `make migrate-remote DEPLOY_ENV=<env>`, then `make curator-sync DEPLOY_ENV=<env>` (`curator sync --env <env>` from `curator/` only works with `curator/.env` exported, which breaks on unquoted `&` in URLs; make parses it fine). The old software-only topics were dropped, so reset the dev and staging DBs once before the first seed.
- **Fetch frequency.** Hot topics (at least `CURATOR_HOT_MIN_ENGAGED` distinct users who opened or saved one of their stories in 7 days) are fetched every run; warm ones (followed, serving a user's profession, headlines, or with fewer engaged users) every `CURATOR_WARM_INTERVAL_HOURS`, or their priority's interval when shorter; cold ones (launched with no demand, or whose stories were seen but opened or saved by nobody) every `CURATOR_PRIORITY_INTERVAL_HOURS` entry for their topic priority (1 highest to 5). Within a tier, higher-priority topics go first, then the most engaged, then the most viewed. Views come from the app's feed impressions and are only a tiebreak.
- **Prune** (keeps Neon under its 1 GB limit). Run `make curator-prune` (every env in `CURATOR_RUN_ENVS`, or `DEPLOY_ENV=<env>` for one); `curator run` does not prune. Saved stories are never deleted. Unsaved stories are deleted when published over `CURATOR_PRUNE_MAX_AGE_DAYS` ago.
- `curator run` only touches the envs in `CURATOR_RUN_ENVS` (default `prod`, so staging is never synced by it; use `curator sync --env staging` by hand).
- **User topic requests** are not handled by `curator run` or `make curator-full` (they only pull each env's demand). Run `make curator-requests DEPLOY_ENV=<env>` to pull that env's pending requests, group them into topics with AI (real calls; `DRY_RUN=1` only prints the proposal) and sync, so users see the new topics and the decisions.
- **Removed catalog entries.** Seed and sync never delete. `make curator-clean` reports topics and professions no longer in the catalog (with the unsaved stories only in them) for the local DB and `ENVS` (default `staging,prod`); `APPLY=1` deletes them. Anything a user follows, muted, picked or requested is kept.

### Curator: nightly job on the VPS (prod)

Every night at 03:00 Asia/Singapore, `changeloom-curator.timer` on the VPS runs `curator.sh`, which starts the
`curator-db` Postgres container, runs the curator image once (`curator/deploy/job.sh`: migrate, seed, `run --full`
for every due topic with Claude, then `sync --env prod` and `prune --env prod`) and stops `curator-db` again.
**The job publishes to prod on its own**; there is no `make curator-sync` review step for prod any more. If the
fetch stops partway (e.g. a subscription limit), what it fetched is still synced and the run exits non-zero;
`CURATOR_FETCH_COOLDOWN=48h` makes the next night start with the topics it missed. The files are
`deploy/vps/curator.{compose.yaml,sh}`, `curator-deploy.sh`, `changeloom-curator.{service,timer}` and the two
env examples. Do this after the VPS steps above (the `deploy` user, Docker and the CI deploy key exist).
`<vps-ip>` is the server; commands run on it as `deploy` (`ssh deploy@<vps-ip>`) use
`C='docker compose -f /srv/changeloom/curator.compose.yaml'`.

1. **Claude credential.** On a machine logged in to the subscription, run `claude setup-token` and keep the
   token for `curator.env`. Anthropic's terms say Pro/Max limits assume ordinary individual use and that
   products should use API keys, so an unattended job feeding a production app is a grey area for the
   subscription token: an API key (`ANTHROPIC_API_KEY` instead of `CLAUDE_CODE_OAUTH_TOKEN`, metered per token)
   is the clearly allowed option. Check [the terms](https://code.claude.com/docs/en/legal-and-compliance) before choosing.
2. **Env files** (mode 600, owned by `deploy`; never commit the filled-in files):
   ```sh
   scp deploy/vps/curator.env.example deploy@<vps-ip>:/srv/changeloom/curator.env
   scp deploy/vps/curator-db.env.example deploy@<vps-ip>:/srv/changeloom/curator-db.env
   ssh deploy@<vps-ip> chmod 600 /srv/changeloom/curator.env /srv/changeloom/curator-db.env
   ```
   In `curator-db.env` set a `POSTGRES_PASSWORD` (`openssl rand -hex 24`); `LOCAL_DATABASE_URL` in `curator.env`
   uses the same user, password and database. Fill in the token, `REMOTE_DATABASE_URL_PROD` (the prod
   **direct** Neon URL, as in `migrate.env`) and `NOTIFY_SECRET_PROD` (`NOTIFY_SECRET` from `api.env`).
3. **First image deploy.** Merge `staging` into `main` and approve the **Deploy curator** run (Continuous
   deployment below): it copies the curator files to `/srv/changeloom/` and pulls the image. The first push
   creates the GHCR package `changeloom-curator` as **private**: make it public in its package settings, as
   for the api, then rerun the failed `vps` job.
4. **Move the curator DB** (once, before the first run). The laptop's curator DB holds the fetch history,
   backoff and known stories prod was synced from; without it the first nightly sync starts from an empty
   history. Dump it on the laptop (the `pg_dump` must be version 18 or older, like `curator-db`):
   ```sh
   pg_dump -Fc --no-owner --no-acl "$LOCAL_DATABASE_URL" -f curator.dump   # LOCAL_DATABASE_URL from curator/.env
   scp curator.dump deploy@<vps-ip>:/srv/changeloom/
   ```
   Then restore it on the server into the still-empty `curator-db` (`CURATOR_IMAGE` is only needed so compose
   can read the file; the job is not started):
   ```sh
   cd /srv/changeloom && export CURATOR_IMAGE=unused
   $C up -d --wait curator-db
   $C exec -T curator-db sh -c 'pg_restore --no-owner --no-acl --exit-on-error -U "$POSTGRES_USER" -d "$POSTGRES_DB"' < curator.dump
   $C stop curator-db
   rm curator.dump
   ```
   Delete the laptop's `curator.dump` too. If the restore fails on an existing object, the DB was not empty
   (a run already happened): `$C down -v` deletes it (this destroys its data), then repeat.
   **From now on the laptop never syncs or prunes prod**, or two sources diverge: set `CURATOR_RUN_ENVS=staging`
   in the laptop's `curator/.env`, and use `DEPLOY_ENV=staging` (the default) for `curator-sync`,
   `curator-prune` and `curator-requests`. Laptop runs stay for local and staging.
5. **Timer** (as root, once; CI keeps the copies in `/srv/changeloom/` current, so rerun this when a unit changes):
   ```sh
   ssh sg1 'sudo install -m 644 /srv/changeloom/changeloom-curator.{service,timer} /etc/systemd/system/ \
     && sudo systemctl daemon-reload && sudo systemctl enable --now changeloom-curator.timer'
   ```
6. **First run** by hand, then follow it (a full run takes hours):
   ```sh
   ssh sg1 'sudo systemctl start --no-block changeloom-curator.service'
   ssh sg1 'journalctl -u changeloom-curator -f'
   ```
   It ends with `==> End ...: exit 0`, and the app's timeline shows the new stories.
   `systemctl list-timers changeloom-curator.timer` shows the next run.

- **Logs:** `journalctl -u changeloom-curator --since today` (the job's output goes there; `docker logs` only
  works while the run's container `changeloom-curator-job` exists). A run that is cut short exits non-zero
  and shows as failed in `systemctl status changeloom-curator`.
- **Stop a run:** `sudo systemctl stop changeloom-curator`; `curator.sh` stops the job and `curator-db`.
  What was fetched is not synced until the next run.
- **Rollback:** `ssh deploy@<vps-ip> /srv/changeloom/curator-deploy.sh <older-tag>` (a commit SHA that was
  pushed to GHCR); the next run uses it. A deploy never interrupts a running job.
- **User topic requests for prod** now run on the VPS against `curator-db` (not on the laptop), outside the
  03:00 run. The default provider is Claude, as in the job:
  ```sh
  cd /srv/changeloom && export CURATOR_IMAGE=ghcr.io/saniuzzaman-robin/changeloom-curator:$(cat curator-tag)
  $C up -d --wait curator-db
  $C run --rm curator /app/curator requests pull --env prod
  $C run --rm curator /app/curator requests group --dry-run   # then without --dry-run
  $C run --rm curator /app/curator sync --env prod
  $C stop curator-db
  ```
- **Catalog changes** reach prod with the next image: merge to `main`, approve the curator deploy, and the next
  run seeds and syncs them.

## Android app

Per-env api URLs go in `~/.gradle/gradle.properties` (or `-P` on the command line):

```properties
changeloom.staging.apiBaseUrl=https://<staging Cloud Run url>
changeloom.prod.apiBaseUrl=https://changeloom-api.saniuzzaman.dev
```

Without them, both flavors call the local api from the emulator (`http://10.0.2.2:8080`), which is
what debug builds use for local development. `changeloom.apiBaseUrl` overrides both flavors at once.

**Signing.** Create one upload key and keep it outside the repo, backed up:

```sh
keytool -genkeypair -v -keystore ~/keys/changeloom-upload.jks -alias upload \
  -keyalg RSA -keysize 2048 -validity 10000
```

Then create `mobile/keystore.properties` (`storeFile` is relative to `mobile/` or absolute):

```properties
storeFile=/Users/<you>/keys/changeloom-upload.jks
storePassword=...
keyAlias=upload
keyPassword=...
```

**Ads (AdMob).** Every build except prod release uses Google's test ids, so debug and staging builds only
ever show test ads (real ads on test devices count as invalid traffic). Prod release needs the real ids:

```properties
changeloom.prod.admobAppId=ca-app-pub-<publisher>~<app>
changeloom.prod.admobNativeAdUnitId=ca-app-pub-<publisher>/<unit>
```

One-time setup (you do these; none of it can be scripted from here):
1. In AdMob, create the account, add the Android app (`dev.changeloom.android`) and one **Native advanced**
   ad unit. The app id and the unit id are the two properties above.
2. AdMob → Privacy & messaging: create and publish a **European regulations (GDPR)** message for the app,
   and a **US state regulations** message if you serve US users. The app shows them through UMP; without
   a published message UMP reports consent as not required. Turn on **Consent mode** for Google Analytics in
   the same section if it is offered.
3. Publish `app-ads.txt` (AdMob → Apps → app-ads.txt) at the root of the developer website listed in Play
   Console, and a privacy policy that covers ads, Analytics and Crashlytics. Link the policy in Play Console.
4. Play Console → App content: answer **Contains ads: yes**, declare the **Advertising ID** use (ads and
   analytics), and update **Data safety**: device or other IDs, app interactions, crash logs and diagnostics,
   collected and shared for advertising and analytics.
5. Firebase (prod and staging) → Remote Config: add `ads_enabled` (boolean, default `true`; the kill switch)
   and `ads_interval` (number, default `6`: stories between feed ads, clamped to 3–100), then publish. The
   app's built-in defaults match, so this step only matters when you want to change them.

To see the consent form outside the EEA, run a debug build once, copy the device hash UMP logs
(`adb logcat | grep -i UserMessagingPlatform`), and put `changeloom.umpTestDeviceId=<hash>` in
`~/.gradle/gradle.properties`: debug builds on that device then act as if in the EEA. To start over,
clear the app's data.

**App Check.** The app sends a Firebase App Check token with every api request (Play Integrity in
release builds, the debug provider in debug builds), and the api checks it outside dev. Until
`APPCHECK_ENFORCE=true` in `deploy/<env>.env`, the api only logs requests it would reject
(`"app check would reject"`, with `app_check: missing|invalid`); enforced, they get a 403. Per env:
1. Firebase console → App Check → Apps: register the Android app with **Play Integrity**. Add the SHA-256
   of the app signing key (Play Console → Test and release → App integrity → App signing) in Firebase
   project settings, and link the Firebase project to the app in Play Console (App integrity → Play
   Integrity API) so the API is enabled for it.
2. For debug builds, run one, find the secret it logs (`adb logcat | grep DebugAppCheckProvider`) and add
   it under App Check → Apps → Manage debug tokens. Keep these out of shared notes.
3. Watch the "app check would reject" lines in Cloud Logging after a release. Enforce in prod only once
   they come almost only from versions older than this one, then redeploy the api. Leave staging
   unenforced: App Distribution installs aren't from Play, so Play Integrity rejects them.

**Notifications.** Pushes go to the "Story alerts" channel (high importance); "Story updates" (default
importance) is the fallback for anything else. The app explains alerts once before Android 13+ asks for
the notification permission.

**Builds.** A release build stops before compiling unless its env has an https api URL, its
`google-services.json`, the AdMob ids (prod) and the signing key (`:androidApp:verify<Env>ReleaseConfig`).

| What | Command | Output (under `mobile/androidApp/build/outputs/`) |
|---|---|---|
| Debug, both flavors | `cd mobile && ./gradlew :androidApp:assembleDebug` | `apk/<env>/debug/` |
| Staging release APK | `make android-apk` | `apk/staging/release/androidApp-staging-release.apk` |
| Prod release AAB | `make android-bundle DEPLOY_ENV=prod` | `bundle/prodRelease/androidApp-prod-release.aab` |

Add `GRADLE=gradle` to the make targets to use the system Gradle instead of the wrapper.
Local builds use `versionCode` 1 and `versionName` 0.1.0 unless you pass
`-Pchangeloom.versionCode=<n>` and `-Pchangeloom.versionName=<x.y.z>`. The release workflow sets
them (see Continuous deployment). Play rejects a `versionCode` it has seen before.

**Distribution.** The release workflow does this for you; by hand:
- Staging: upload the APK to Firebase App Distribution in the staging Firebase project (console, or
  `firebase appdistribution:distribute <apk> --app <staging Firebase Android app id> --groups <group>`).
- Prod: upload the AAB in Play Console, to the internal testing track first, then promote it to production.
  Play also needs a public account-deletion URL (App content → Data safety). Host the text in
  `deploy/account-deletion.md` after filling in its placeholders.

## Continuous deployment (GitHub Actions)

- `.github/workflows/deploy-api.yml`
  - **Staging:** runs after CI passes on a push to `staging`. It builds the image once, pushes it to
    staging's Artifact Registry, runs the backend migrations with the image's `goose` (same
    `goose_db_version` table as `curator migrate --remote`), deploys with no traffic, checks `/ready`,
    then shifts traffic. The run summary prints the image (`...api@sha256:...`).
  - **Prod:** runs after CI passes on a push to `main` (a `staging` → `main` merge). After the
    `production` environment's reviewers approve, it builds the image from that commit and pushes it to
    GHCR (`VPS_IMAGE_REPO:<commit>`) and, while `CLOUD_RUN_DEPLOY=true` in `deploy/prod.env`, to prod's
    registry, then migrates, deploys, checks and promotes Cloud Run the same way. Last it copies
    `deploy/vps/{compose.yaml,changeloom.caddy,deploy.sh}` to the VPS and runs `deploy.sh <commit>` there
    over SSH (VPS step 8). To redeploy or roll back an image that already ran in staging, run Actions →
    Deploy api → Run workflow with that image: it copies the same digest to the prod registries (crane)
    and checks out the commit it was built from. Set `CLOUD_RUN_DEPLOY=false` once Cloud Run prod is deleted.
- `.github/workflows/deploy-curator.yml`: runs after CI passes on a push to `main`. It builds the curator
  image (Claude CLI pinned by `CURATOR_CLAUDE_CODE_VERSION` in `deploy/prod.env`) and pushes it to GHCR
  (`VPS_CURATOR_IMAGE_REPO:<commit>`); after the `production` environment's approval it copies the curator
  files in `deploy/vps/` to the VPS and runs `curator-deploy.sh <commit>`, so the next nightly run uses it
  (Curator: nightly job on the VPS). Run it by hand to rebuild after bumping the Claude CLI version.
- **Branches:** open PRs against `staging`; Dependabot does too. Merge `staging` into `main` (a PR, with a
  merge commit) when you want a prod release. `main` is the only branch that deploys prod; protect both.
- `.github/workflows/android-release.yml`
  - **Staging:** pushes to `staging` that touch `mobile/` build the signed staging APK and send it to
    Firebase App Distribution. Its `versionName` is the last release plus the build, e.g.
    `1.2.3-staging.45+abc1234` (run 45, commit abc1234; `0.0.0-staging…` before the first release).
  - **Prod:** a release tag (after approval) builds the signed prod AAB with the tag's `versionName` and
    uploads it to Play: `v1.2.3-alpha.4` to closed testing (the default closed track, `alpha`), `v1.2.3`
    to production. `PLAY_RELEASE_STATUS` (default `completed`) applies to both; `draft` leaves the
    release for you to roll out in Play Console.
  - `versionCode` is the workflow's run number plus the optional repo variable
    `ANDROID_VERSION_CODE_OFFSET`. Set the offset above any `versionCode` you uploaded by hand.
- **Cutting a release:** `make release` lists the commits since the last `v*` tag and proposes the next
  version from their conventional-commit types (`feat` → minor, `fix`/`perf` → patch, `type!:` or
  `BREAKING CHANGE:` → major). After you confirm, it tags `main` and pushes the tag, which starts the prod
  release above. It refuses unless `main` is clean and matches `origin/main`. `make release DRY_RUN=1` only
  shows the proposal; `make release VERSION=1.4.0` picks the number yourself (it must be above the last).
  `make release PRE=alpha` tags a closed-testing build instead: `v1.4.0-alpha.1`, then `-alpha.2` and so on
  for the same version. A later `make release` releases that version (`v1.4.0`) to production, even from
  the same commit. First alpha: `make release VERSION=1.0.0 PRE=alpha`.

The workflows reach GCP through Workload Identity Federation, so there are no service account keys.
**One-time setup, per env.** These commands change IAM and create resources; read them, then run them
yourself. `P`, `R` and `SA` are as above, `REPO=saniuzzaman-robin/changeloom`, and `GH_ENV` is the
GitHub environment: `staging`, or `production` for prod.

1. **APIs and the image repository.**
   ```sh
   gcloud services enable iamcredentials.googleapis.com sts.googleapis.com --project "$P"
   gcloud artifacts repositories create changeloom --project "$P" --location "$R" --repository-format docker
   ```
   Staging also needs `firebaseappdistribution.googleapis.com`; prod needs `androidpublisher.googleapis.com`.
2. **Workload Identity pool.** It accepts tokens from this repository only, and each binding below
   is limited to jobs running in `GH_ENV`.
   ```sh
   gcloud iam workload-identity-pools create github --project "$P" --location global \
     --display-name "GitHub Actions"
   gcloud iam workload-identity-pools providers create-oidc github --project "$P" --location global \
     --workload-identity-pool github --issuer-uri https://token.actions.githubusercontent.com \
     --attribute-mapping google.subject=assertion.sub,attribute.repository=assertion.repository \
     --attribute-condition "assertion.repository == '$REPO'"
   NUM=$(gcloud projects describe "$P" --format 'value(projectNumber)')
   WIF_PROVIDER="projects/$NUM/locations/global/workloadIdentityPools/github/providers/github"
   # The job's OIDC subject. This repo uses GitHub's immutable subject, which adds the owner and repo ids:
   # `gh api repos/$REPO/actions/oidc/customization/sub` shows the prefix (sub_claim_prefix).
   SUBJECT="repo:saniuzzaman-robin@32646654/changeloom@1400225189:environment:$GH_ENV"
   PRINCIPAL="principal://iam.googleapis.com/projects/$NUM/locations/global/workloadIdentityPools/github/subject/$SUBJECT"
   ```
3. **Deployer service account. IAM:** it can deploy Cloud Run revisions as `$SA`, push images and
   read the migration URL. It can't change the service's IAM policy.
   ```sh
   gcloud iam service-accounts create changeloom-deployer --project "$P"
   DEPLOYER="changeloom-deployer@$P.iam.gserviceaccount.com"
   gcloud iam service-accounts add-iam-policy-binding "$DEPLOYER" --project "$P" \
     --role roles/iam.workloadIdentityUser --member "$PRINCIPAL"
   gcloud projects add-iam-policy-binding "$P" --member "serviceAccount:$DEPLOYER" --role roles/run.developer
   gcloud iam service-accounts add-iam-policy-binding "$SA" --project "$P" \
     --member "serviceAccount:$DEPLOYER" --role roles/iam.serviceAccountUser
   gcloud artifacts repositories add-iam-policy-binding changeloom --project "$P" --location "$R" \
     --member "serviceAccount:$DEPLOYER" --role roles/artifactregistry.writer

   # The direct (non-pooled) Neon URL, the same one as REMOTE_DATABASE_URL_<ENV> in curator/.env.
   printf '%s' "<direct Neon URL>" | gcloud secrets create changeloom-migrate-database-url --project "$P" --data-file=-
   gcloud secrets add-iam-policy-binding changeloom-migrate-database-url --project "$P" \
     --member "serviceAccount:$DEPLOYER" --role roles/secretmanager.secretAccessor
   ```
   Prod only: the prod deployer copies images out of staging's repository, so it can read it.
   This is a **cross-project IAM grant**.
   ```sh
   gcloud artifacts repositories add-iam-policy-binding changeloom --project <staging project> \
     --location <staging region> --role roles/artifactregistry.reader \
     --member "serviceAccount:changeloom-deployer@<prod project>.iam.gserviceaccount.com"
   ```
4. **Android release service account.**
   ```sh
   gcloud iam service-accounts create changeloom-android-release --project "$P"
   ANDROID_SA="changeloom-android-release@$P.iam.gserviceaccount.com"
   gcloud iam service-accounts add-iam-policy-binding "$ANDROID_SA" --project "$P" \
     --role roles/iam.workloadIdentityUser --member "$PRINCIPAL"
   ```
   - Staging, **IAM:** `gcloud projects add-iam-policy-binding "$P" --member "serviceAccount:$ANDROID_SA" --role roles/firebaseappdistro.admin`.
   - Prod: in Play Console → Users and permissions, invite `$ANDROID_SA` with release permissions
     for this app only. Play's API can upload only after you have uploaded the first AAB by hand.
     While the app is still a draft in Play Console, set `PLAY_RELEASE_STATUS=draft`.
5. **GitHub environments.** In the repository's Settings → Environments, create `staging` and
   `production`. On `production`, add yourself as a required reviewer and limit deployments to `main`
   and tags matching `v*`. Then set these on each environment:

   | Name | Kind | Value |
   |---|---|---|
   | `WIF_PROVIDER` | variable | `$WIF_PROVIDER` |
   | `DEPLOYER_SA` | variable | `$DEPLOYER` |
   | `ANDROID_RELEASE_SA` | variable | `$ANDROID_SA` |
   | `ANDROID_API_BASE_URL` | variable | staging's Cloud Run URL; prod: `https://changeloom-api.saniuzzaman.dev` (VPS) |
   | `FIREBASE_ANDROID_APP_ID` | variable, staging | the Firebase Android app id (`1:...:android:...`) |
   | `APP_DISTRIBUTION_GROUPS` | variable, staging | tester group aliases, comma-separated |
   | `PLAY_RELEASE_STATUS` | variable, prod, optional | `draft` until the app is out of draft; default `completed` |
   | `ANDROID_ADMOB_APP_ID`, `ANDROID_ADMOB_NATIVE_AD_UNIT_ID` | variables, prod | the AdMob app and native ad unit ids |
   | `GOOGLE_SERVICES_JSON` | secret | the env's `google-services.json` |
   | `ANDROID_KEYSTORE_BASE64` | secret | `base64 -i ~/keys/changeloom-upload.jks` |
   | `ANDROID_KEYSTORE_PASSWORD`, `ANDROID_KEY_ALIAS`, `ANDROID_KEY_PASSWORD` | secrets | as in `mobile/keystore.properties` |

   The deploy workflow reads `deploy/<env>.env` from the commit it deploys.
6. **First deploy** of the env by hand (Backend above). After that, the workflows take over.

## Release order

1. Staging: merge to `staging`. The workflows deploy the api and send the app to testers. Run the
   curator's `sync --env staging` and test.
2. Prod: merge `staging` into `main` (deploys the api and the nightly curator after approval), then `make release PRE=alpha` for a
   closed-testing build of the app, and `make release` to ship that version to production.
