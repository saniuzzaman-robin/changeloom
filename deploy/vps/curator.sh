#!/usr/bin/env bash
# Runs the nightly prod curator job on the VPS. changeloom-curator.service starts it as the deploy user from
# /srv/changeloom, next to curator.compose.yaml. It starts curator-db, runs the image tag recorded in
# curator-tag by curator-deploy.sh (job.sh: fetch every due topic with Claude, then sync and prune prod),
# and stops curator-db again, also when the job fails or is stopped. Exits with the job's code.
# See deploy/README.md, section VPS.
set -euo pipefail

APP_DIR="${APP_DIR:-/srv/changeloom}"
IMAGE_REPO="${IMAGE_REPO:-ghcr.io/saniuzzaman-robin/changeloom-curator}"
# Seconds compose waits for curator-db's healthcheck to pass.
DB_READY_TIMEOUT="${DB_READY_TIMEOUT:-60}"
JOB_CONTAINER=changeloom-curator-job

# journald timestamps each line.
log() { echo "==> $*"; }
die() {
	echo "curator: $*" >&2
	exit 1
}

cd "$APP_DIR"
for f in curator.compose.yaml curator.env curator-db.env curator-tag; do
	[[ -r $f ]] || die "missing $APP_DIR/$f (see deploy/README.md, section VPS)"
done
for f in curator.env curator-db.env; do
	[[ $(stat -c %a "$f") == 600 ]] || die "$APP_DIR/$f must be mode 600: chmod 600 $APP_DIR/$f"
done

tag="$(<curator-tag)"
[[ $tag =~ ^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$ ]] || die "invalid image tag in $APP_DIR/curator-tag: $tag"

# One job at a time.
exec 9>"$APP_DIR/.curator.lock"
flock -n 9 || die "another curator job is running"

CURATOR_IMAGE="$IMAGE_REPO:$tag"
export CURATOR_IMAGE
compose() { docker compose -f "$APP_DIR/curator.compose.yaml" "$@"; }

cleanup() {
	if [[ -n $(docker ps -q --filter "name=^${JOB_CONTAINER}\$") ]]; then
		log "Stopping $JOB_CONTAINER"
		docker stop "$JOB_CONTAINER" >/dev/null || echo "curator: could not stop $JOB_CONTAINER" >&2
	fi
	compose stop curator-db >/dev/null || echo "curator: could not stop curator-db" >&2
}
trap cleanup EXIT

started=$SECONDS
log "Start $CURATOR_IMAGE"
compose up -d --wait --wait-timeout "$DB_READY_TIMEOUT" curator-db ||
	die "curator-db did not become healthy; see: docker compose -f $APP_DIR/curator.compose.yaml logs curator-db"

status=0
compose run --rm -T --name "$JOB_CONTAINER" curator || status=$?
log "End $CURATOR_IMAGE: exit $status after $(((SECONDS - started) / 60))m"
exit "$status"
