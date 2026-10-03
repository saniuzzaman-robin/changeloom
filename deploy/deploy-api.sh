#!/usr/bin/env bash
# Deploys the api to Cloud Run for one environment.
# Usage: deploy/deploy-api.sh staging|prod [--image <image@sha256:digest>]
#   (or `make deploy-api DEPLOY_ENV=...`)
# Without --image it builds backend/ with Cloud Build and sends all traffic to the new revision.
# With --image (the deploy workflow) it deploys that image with no traffic under the tag
# "candidate"; deploy/promote-api.sh then smoke-tests it and shifts traffic.
# Settings come from deploy/<env>.env (copy deploy/<env>.env.example). See deploy/README.md for the
# one-time setup and for migrating the database first.
set -euo pipefail

usage() {
	echo "usage: $0 staging|prod [--image <image@sha256:digest>]" >&2
	exit 2
}

env="${1:-}"
case "$env" in
staging | prod) ;;
*) usage ;;
esac
shift
image=""
while (($# > 0)); do
	case "$1" in
	--image)
		[[ -n ${2:-} ]] || usage
		image="$2"
		shift 2
		;;
	*) usage ;;
	esac
done
if [[ -n $image && $image != *@sha256:* ]]; then
	echo "--image must be pinned by digest (<image>@sha256:...), got $image" >&2
	exit 2
fi

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
	CONCURRENCY CPU MEMORY TIMEOUT CPU_BOOST STARTUP_PROBE LIVENESS_PROBE \
	DATABASE_URL_SECRET DATABASE_URL_SECRET_VERSION FIREBASE_PROJECT_ID FCM_ENABLED LOG_LEVEL \
	TIMELINE_WINDOW_DAYS TOPIC_REQUEST_MAX_PENDING DB_MAX_CONNS REQUEST_TIMEOUT \
	RATE_LIMIT_IP_PER_MIN RATE_LIMIT_USER_PER_MIN; do
	[[ -n ${!key:-} ]] || missing+=("$key")
done
if [[ -n ${NOTIFY_SECRET_SECRET:-} && -z ${NOTIFY_SECRET_SECRET_VERSION:-} ]]; then
	missing+=(NOTIFY_SECRET_SECRET_VERSION)
fi
if ((${#missing[@]} > 0)); then
	echo "set these in $conf: ${missing[*]}" >&2
	exit 1
fi
case "$CPU_BOOST" in
true) boost_flag=--cpu-boost ;;
false) boost_flag=--no-cpu-boost ;;
*)
	echo "CPU_BOOST in $conf must be true or false, got $CPU_BOOST" >&2
	exit 1
	;;
esac

commit="$(git -C "$root" rev-parse --short HEAD)"
if [[ -n $(git -C "$root" status --porcelain -- backend) ]]; then
	if [[ $env == prod || -n $image ]]; then
		echo "backend/ has uncommitted changes: prod and image deploys use committed code only" >&2
		exit 1
	fi
	commit="$commit-dirty"
fi

env_vars="ENV=$env,FIREBASE_PROJECT_ID=$FIREBASE_PROJECT_ID,FCM_ENABLED=$FCM_ENABLED,LOG_LEVEL=$LOG_LEVEL,TIMELINE_WINDOW_DAYS=$TIMELINE_WINDOW_DAYS,TOPIC_REQUEST_MAX_PENDING=$TOPIC_REQUEST_MAX_PENDING"
env_vars+=",DB_MAX_CONNS=$DB_MAX_CONNS,REQUEST_TIMEOUT=$REQUEST_TIMEOUT,RATE_LIMIT_IP_PER_MIN=$RATE_LIMIT_IP_PER_MIN,RATE_LIMIT_USER_PER_MIN=$RATE_LIMIT_USER_PER_MIN"
env_vars+=",APPCHECK_ENFORCE=${APPCHECK_ENFORCE:-false}"
if [[ ${OTEL_ENABLED:-false} == true ]]; then
	env_vars+=",OTEL_ENABLED=true,OTEL_SAMPLE_RATIO=${OTEL_SAMPLE_RATIO:-0.1},OTEL_EXPORTER_OTLP_ENDPOINT=${OTEL_EXPORTER_OTLP_ENDPOINT:-}"
fi

secrets="DATABASE_URL=$DATABASE_URL_SECRET:$DATABASE_URL_SECRET_VERSION"
if [[ -n ${NOTIFY_SECRET_SECRET:-} ]]; then
	secrets+=",NOTIFY_SECRET=$NOTIFY_SECRET_SECRET:$NOTIFY_SECRET_SECRET_VERSION"
fi

cmd=(
	gcloud run deploy "$CLOUD_RUN_SERVICE"
	--project "$GCP_PROJECT"
	--region "$GCP_REGION"
	--service-account "$SERVICE_ACCOUNT"
	--min-instances "$MIN_INSTANCES"
	--max-instances "$MAX_INSTANCES"
	--concurrency "$CONCURRENCY"
	--cpu "$CPU"
	--memory "$MEMORY"
	--timeout "$TIMEOUT"
	"$boost_flag"
	--startup-probe "$STARTUP_PROBE"
	--liveness-probe "$LIVENESS_PROBE"
	--set-env-vars "$env_vars"
	--set-secrets "$secrets"
	--labels "env=$env,commit=$commit"
)
if [[ -n $image ]]; then
	# The service already exists and is public; the deployer's role can't change its IAM policy.
	cmd+=(--image "$image" --no-traffic --tag candidate)
else
	cmd+=(--source "$root/backend" --allow-unauthenticated)
fi

echo "Deploying commit $commit to $env ($GCP_PROJECT, $GCP_REGION/$CLOUD_RUN_SERVICE):"
echo " $(printf ' %q' "${cmd[@]}")"
# In GitHub Actions, the production environment's required reviewers are the confirmation.
if [[ $env == prod && ${GITHUB_ACTIONS:-} != true ]]; then
	read -r -p "Type 'prod' to deploy to production: " answer
	if [[ $answer != prod ]]; then
		echo "aborted" >&2
		exit 1
	fi
fi
"${cmd[@]}"
