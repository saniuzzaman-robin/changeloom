#!/usr/bin/env bash
# Smoke-tests the revision tagged "candidate" (deployed by `deploy-api.sh <env> --image ...`) on
# /ready, then sends all traffic to the latest revision.
# Usage: deploy/promote-api.sh staging|prod   (settings from deploy/<env>.env)
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

where=(--project "$GCP_PROJECT" --region "$GCP_REGION")
service="$(gcloud run services describe "$CLOUD_RUN_SERVICE" "${where[@]}" --format json)"
url="$(jq -r '.status.traffic[] | select(.tag == "candidate") | .url' <<<"$service")"
revision="$(jq -r '.status.traffic[] | select(.tag == "candidate") | .revisionName' <<<"$service")"
latest="$(jq -r '.status.latestReadyRevisionName' <<<"$service")"
if [[ -z $url || $url == null ]]; then
	echo "no revision is tagged candidate on $CLOUD_RUN_SERVICE: deploy one with deploy-api.sh $env --image ..." >&2
	exit 1
fi
if [[ $revision != "$latest" ]]; then
	echo "the candidate ($revision) is not the latest ready revision ($latest); not promoting" >&2
	exit 1
fi

echo "Smoke-testing $revision at $url/ready"
if ! curl -fsS --max-time 10 --retry 5 --retry-delay 3 --retry-all-errors "$url/ready"; then
	echo "$revision failed /ready; traffic is unchanged" >&2
	exit 1
fi
echo

echo "Sending all traffic to $revision"
gcloud run services update-traffic "$CLOUD_RUN_SERVICE" "${where[@]}" --to-latest
