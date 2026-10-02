# Environments, builds and deploys

Changeloom has a local dev setup plus two hosted environments, **staging** and **prod**. Each hosted
env is fully separate: its own GCP project (which is also its Firebase project), Neon database,
Cloud Run service and Android app.

| | dev (local) | staging | prod |
|---|---|---|---|
| api `ENV` | `dev` (dev auth: `Bearer dev:<name>`) | `staging` (Firebase auth) | `prod` (Firebase auth) |
| api settings | root `.env` | `deploy/staging.env` | `deploy/prod.env` |
| Database | dev Postgres (`make db-up`) | Neon, staging | Neon, prod |
| Android flavor | `staging`/`prod` debug, local api by default | `staging`: `dev.changeloom.android.staging`, "Changeloom Staging" | `prod`: `dev.changeloom.android` |
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
   Download `google-services.json` to `mobile/androidApp/src/<env>/google-services.json`.
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
   (`GCP_PROJECT`, `SERVICE_ACCOUNT=$SA`, `FIREBASE_PROJECT_ID`, ...).
6. **Curator.** In `curator/.env`, set `REMOTE_DATABASE_URL_<ENV>`, `NOTIFY_SECRET_<ENV>`, and after
   the first deploy `API_BASE_URL_<ENV>` (the Cloud Run URL).

## Backend (api on Cloud Run)

Release to staging, check it, then release the same commit to prod:

```sh
make migrate-remote                      # staging DB: pending backend migrations
make deploy-api                          # Cloud Build from backend/, deploy to staging Cloud Run
curl -fsS https://<staging url>/healthz

make migrate-remote DEPLOY_ENV=prod
make deploy-api DEPLOY_ENV=prod          # committed code only; asks you to type 'prod'
```

- `deploy/deploy-api.sh` prints the full `gcloud run deploy` command before running it and labels the
  revision with `env` and `commit` (`<sha>-dirty` for uncommitted staging deploys).
- The first `--source` deploy in a project asks to create an Artifact Registry repository; accept it.
- Migrations are forward-only and run before the new code, so keep them compatible with the code that
  is still serving (add columns first, drop them in a later release).
- Roll back by sending traffic to an earlier revision:
  ```sh
  gcloud run revisions list --service changeloom-api --project "$P" --region "$R"
  gcloud run services update-traffic changeloom-api --to-revisions <revision>=100 --project "$P" --region "$R"
  ```

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

**Builds.** A release build stops before compiling unless its env has an https api URL, its
`google-services.json` and the signing key (`:androidApp:verify<Env>ReleaseConfig`).

| What | Command | Output (under `mobile/androidApp/build/outputs/`) |
|---|---|---|
| Debug, both flavors | `cd mobile && ./gradlew :androidApp:assembleDebug` | `apk/<env>/debug/` |
| Staging release APK | `make android-apk` | `apk/staging/release/androidApp-staging-release.apk` |
| Prod release AAB | `make android-bundle DEPLOY_ENV=prod` | `bundle/prodRelease/androidApp-prod-release.aab` |

Add `GRADLE=gradle` to the make targets to use the system Gradle instead of the wrapper.
Before each release, bump `versionCode` (and `versionName`) in `mobile/androidApp/build.gradle.kts`;
Play rejects a `versionCode` it has seen before.

**Distribution.**
- Staging: upload the APK to Firebase App Distribution in the staging Firebase project (console, or
  `firebase appdistribution:distribute <apk> --app <staging Firebase Android app id> --groups <group>`).
- Prod: upload the AAB in Play Console, to the internal testing track first, then promote it to production.

## Release order

1. Staging: `make migrate-remote`, `make deploy-api`, the curator's `sync --env staging` (once
   Phase 6 lands), `make android-apk`, test.
2. Prod: the same commit with `DEPLOY_ENV=prod`, then `make android-bundle DEPLOY_ENV=prod` and Play Console.
