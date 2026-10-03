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

Files holding secrets or per-env values are gitignored and never committed: `deploy/<env>.env`,
`curator/.env`, `mobile/keystore.properties`, the keystore itself, and `google-services.json`.

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
5. **Deploy settings.** `cp deploy/<env>.env.example deploy/<env>.env` and fill it in
   (`GCP_PROJECT`, `SERVICE_ACCOUNT=$SA`, `FIREBASE_PROJECT_ID`, ...). Secrets are pinned to a
   version: after `gcloud secrets versions add`, bump the matching `*_VERSION` and redeploy.
6. **Curator.** In `curator/.env`, set `REMOTE_DATABASE_URL_<ENV>`, `NOTIFY_SECRET_<ENV>`, and after
   the first deploy `API_BASE_URL_<ENV>` (the Cloud Run URL).

## Backend (api on Cloud Run)

Normally the deploy workflow releases the api (see Continuous deployment): every push to `main` that
passes CI goes to staging, and you promote the same image to prod by hand. The first deploy of each
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
   - no `notify received` log line for 12h, which means the curator stopped. This policy needs its
     log-based metric, so create the metric first.
   ```sh
   gcloud logging metrics create notify_received --project "$P" \
     --description "POST /internal/notify calls from the curator" \
     --log-filter "resource.type=\"cloud_run_revision\" AND resource.labels.service_name=\"$CLOUD_RUN_SERVICE\" AND jsonPayload.message=\"notify received\""
   mkdir -p /tmp/changeloom-monitoring
   for f in deploy/monitoring/*.yaml; do
     out=/tmp/changeloom-monitoring/$(basename "$f")
     sed -e "s/__SERVICE__/$CLOUD_RUN_SERVICE/g" -e "s/__MAX_INSTANCES__/$MAX_INSTANCES/g" "$f" > "$out"
     gcloud monitoring policies create --project "$P" --notification-channels "$CHANNEL" --policy-from-file "$out"
   done
   ```
   The 12h absence alert also fires while the curator is intentionally off; snooze it then.
5. **Error Reporting.** The api's error logs show up there automatically. Turn on notifications in the
   console: Error Reporting → Configure notifications → `$CHANNEL`.
6. **Billing budget.** Alerts the billing account's admins by email at 50%, 90% and 100% of the
   amount. **Billing:** pick an amount that fits; the free tiers should keep normal use near 0.
   ```sh
   gcloud billing budgets create --billing-account <BILLING_ACCOUNT_ID> \
     --display-name "changeloom $P" --budget-amount 10USD --filter-projects "projects/$P" \
     --threshold-rule percent=0.5 --threshold-rule percent=0.9 --threshold-rule percent=1.0
   ```

When `curator run` fails on the Mac, it also shows a macOS notification and exits non-zero. Details
are in `~/Library/Logs/changeloom-curator.log`.

## Android app

Per-env api URLs go in `~/.gradle/gradle.properties` (or `-P` on the command line):

```properties
changeloom.staging.apiBaseUrl=https://<staging Cloud Run url>
changeloom.prod.apiBaseUrl=https://<prod Cloud Run url>
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
  - **Staging:** runs after CI passes on a push to `main`. It builds the image once, pushes it to
    staging's Artifact Registry, runs the backend migrations with the image's `goose` (same
    `goose_db_version` table as `curator migrate --remote`), deploys with no traffic, checks `/ready`,
    then shifts traffic. The run summary prints the image (`...api@sha256:...`).
  - **Prod:** Actions → Deploy api → Run workflow, with that image. After the `production`
    environment's reviewers approve, it copies the same digest to prod's registry (crane), checks out
    the commit the image was built from, then migrates, deploys, checks and promotes the same way.
- `.github/workflows/android-release.yml`
  - **Staging:** pushes to `main` that touch `mobile/` build the signed staging APK and send it to
    Firebase App Distribution.
  - **Prod:** a `v1.2.3` tag (after approval) builds the signed prod AAB with `versionName` 1.2.3 and
    uploads it to Play's internal track. Promote it in Play Console.
  - `versionCode` is the workflow's run number plus the optional repo variable
    `ANDROID_VERSION_CODE_OFFSET`. Set the offset above any `versionCode` you uploaded by hand.

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
   PRINCIPAL="principal://iam.googleapis.com/projects/$NUM/locations/global/workloadIdentityPools/github/subject/repo:$REPO:environment:$GH_ENV"
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
   | `DEPLOY_ENV_FILE` | variable | the whole of `deploy/<env>.env` (no secret values in it) |
   | `ANDROID_RELEASE_SA` | variable | `$ANDROID_SA` |
   | `ANDROID_API_BASE_URL` | variable | the env's Cloud Run URL |
   | `FIREBASE_ANDROID_APP_ID` | variable, staging | the Firebase Android app id (`1:...:android:...`) |
   | `APP_DISTRIBUTION_GROUPS` | variable, staging | tester group aliases, comma-separated |
   | `PLAY_RELEASE_STATUS` | variable, prod, optional | `draft` until the app is out of draft; default `completed` |
   | `ANDROID_ADMOB_APP_ID`, `ANDROID_ADMOB_NATIVE_AD_UNIT_ID` | variables, prod | the AdMob app and native ad unit ids |
   | `GOOGLE_SERVICES_JSON` | secret | the env's `google-services.json` |
   | `ANDROID_KEYSTORE_BASE64` | secret | `base64 -i ~/keys/changeloom-upload.jks` |
   | `ANDROID_KEYSTORE_PASSWORD`, `ANDROID_KEY_ALIAS`, `ANDROID_KEY_PASSWORD` | secrets | as in `mobile/keystore.properties` |

   With the GitHub CLI: `gh variable set DEPLOY_ENV_FILE --env staging < deploy/staging.env`.
   **Whenever `deploy/<env>.env` changes, update `DEPLOY_ENV_FILE` too.**
6. **First deploy** of the env by hand (Backend above). After that, the workflows take over.

## Release order

1. Staging: merge to `main`. The workflows deploy the api and send the app to testers. Run the
   curator's `sync --env staging` and test.
2. Prod: run Deploy api with the staging image, then push a `v<x.y.z>` tag for the app, and promote
   the internal release in Play Console.
