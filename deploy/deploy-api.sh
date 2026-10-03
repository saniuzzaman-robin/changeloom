#!/usr/bin/env bash
# Builds backend/ with Cloud Build and deploys it to Cloud Run for one environment.
# Usage: deploy/deploy-api.sh staging|prod   (or `make deploy-api DEPLOY_ENV=...`)
# Settings come from deploy/<env>.env (copy deploy/<env>.env.example). See deploy/README.md for the
# one-time setup and for migrating the database first.
set -euo pipefail

env="${1:-}"
case "$env" in
staging | prod) ;;
*)
	echo "usage: $0 staging|prod" >&2
	exit 2
	;;
esac

root="$(cd "$(dirname "$0")/.." && pwd)"
conf="$root/deploy/$env.env"
if [[ ! -f $conf ]]; then
	echo "$conf is missing: copy deploy/$env.env.example to it and fill it in" >&2
	exit 1
fi
set -a
# shellcheck source=/dev/null
source "$conf"
set +a

missing=()
for key in GCP_PROJECT GCP_REGION CLOUD_RUN_SERVICE SERVICE_ACCOUNT MIN_INSTANCES MAX_INSTANCES \
	DATABASE_URL_SECRET FIREBASE_PROJECT_ID FCM_ENABLED LOG_LEVEL TIMELINE_WINDOW_DAYS TOPIC_REQUEST_MAX_PENDING; do
	[[ -n ${!key:-} ]] || missing+=("$key")
done
if ((${#missing[@]} > 0)); then
	echo "set these in $conf: ${missing[*]}" >&2
	exit 1
fi

commit="$(git -C "$root" rev-parse --short HEAD)"
if [[ -n $(git -C "$root" status --porcelain -- backend) ]]; then
	if [[ $env == prod ]]; then
		echo "backend/ has uncommitted changes: prod deploys only committed code (commit, or deploy to staging)" >&2
		exit 1
	fi
	commit="$commit-dirty"
fi

env_vars="ENV=$env,FIREBASE_PROJECT_ID=$FIREBASE_PROJECT_ID,FCM_ENABLED=$FCM_ENABLED,LOG_LEVEL=$LOG_LEVEL,TIMELINE_WINDOW_DAYS=$TIMELINE_WINDOW_DAYS,TOPIC_REQUEST_MAX_PENDING=$TOPIC_REQUEST_MAX_PENDING"
if [[ ${OTEL_ENABLED:-false} == true ]]; then
	env_vars+=",OTEL_ENABLED=true,OTEL_SAMPLE_RATIO=${OTEL_SAMPLE_RATIO:-0.1},OTEL_EXPORTER_OTLP_ENDPOINT=${OTEL_EXPORTER_OTLP_ENDPOINT:-}"
fi

secrets="DATABASE_URL=$DATABASE_URL_SECRET:latest"
if [[ -n ${NOTIFY_SECRET_SECRET:-} ]]; then
	secrets+=",NOTIFY_SECRET=$NOTIFY_SECRET_SECRET:latest"
fi

cmd=(
	gcloud run deploy "$CLOUD_RUN_SERVICE"
	--project "$GCP_PROJECT"
	--region "$GCP_REGION"
	--source "$root/backend"
	--service-account "$SERVICE_ACCOUNT"
	--min-instances "$MIN_INSTANCES"
	--max-instances "$MAX_INSTANCES"
	--allow-unauthenticated
	--set-env-vars "$env_vars"
	--set-secrets "$secrets"
	--labels "env=$env,commit=$commit"
)

echo "Deploying commit $commit to $env ($GCP_PROJECT, $GCP_REGION/$CLOUD_RUN_SERVICE):"
echo " $(printf ' %q' "${cmd[@]}")"
if [[ $env == prod ]]; then
	read -r -p "Type 'prod' to deploy to production: " answer
	if [[ $answer != prod ]]; then
		echo "aborted" >&2
		exit 1
	fi
fi
"${cmd[@]}"
