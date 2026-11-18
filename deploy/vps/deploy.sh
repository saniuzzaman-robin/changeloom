#!/usr/bin/env bash
# Deploys one changeloom api image on the VPS. Runs on the server as the deploy user, from /srv/changeloom
# next to compose.yaml and changeloom.caddy; the CI deploy job calls it over SSH.
# Usage: deploy.sh <tag>   (the image tag, normally the commit SHA)
# Steps: install the Caddy site file if it changed, pull the image, run goose migrations with
# MIGRATE_DATABASE_URL from migrate.env, start the api and wait until /ready passes. If the api does not
# become healthy it restarts the previous tag (from current-tag). Migrations are not rolled back, so they must
# stay compatible with the previous release (Cloud Run prod also runs against the same database).
# See deploy/README.md, section VPS.
set -euo pipefail

APP_DIR="${APP_DIR:-/srv/changeloom}"
IMAGE_REPO="${IMAGE_REPO:-ghcr.io/saniuzzaman-robin/changeloom-api}"
SITE_FILE="${SITE_FILE:-/etc/caddy/sites/changeloom.caddy}"
# Seconds compose waits for the api's healthcheck to pass.
READY_TIMEOUT="${READY_TIMEOUT:-90}"

log() { echo "==> $*"; }
die() {
	echo "deploy: $*" >&2
	exit 1
}

(($# == 1)) || die "usage: $0 <tag>"
tag="$1"
[[ $tag =~ ^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$ ]] || die "invalid image tag: $tag"

cd "$APP_DIR"
for f in compose.yaml changeloom.caddy api.env migrate.env fcm.json; do
	[[ -r $f ]] || die "missing $APP_DIR/$f (see deploy/README.md, section VPS)"
done
for f in api.env migrate.env fcm.json; do
	[[ $(stat -c %a "$f") == 600 ]] || die "$APP_DIR/$f must be mode 600: chmod 600 $APP_DIR/$f"
done

# One deploy at a time.
exec 9>"$APP_DIR/.deploy.lock"
flock -n 9 || die "another deploy is running"

RUN_AS="$(id -u):$(id -g)"
export RUN_AS
compose() { docker compose -f "$APP_DIR/compose.yaml" "$@"; }

# up <tag>: starts that tag and waits for the healthcheck.
up() {
	API_IMAGE="$IMAGE_REPO:$1" compose up -d --wait --wait-timeout "$READY_TIMEOUT" api
}

previous=""
[[ -s current-tag ]] && previous="$(<current-tag)"

if ! cmp -s changeloom.caddy "$SITE_FILE"; then
	log "Caddy site file"
	backup=""
	if [[ -f $SITE_FILE ]]; then
		backup="$(mktemp)"
		cp "$SITE_FILE" "$backup"
	fi
	cp changeloom.caddy "$SITE_FILE"
	# Caddy keeps its running config when the new one fails to load, so restore the file to match it.
	if ! sudo systemctl reload caddy; then
		if [[ -n $backup ]]; then cp "$backup" "$SITE_FILE"; else rm -f "$SITE_FILE"; fi
		die "Caddy rejected changeloom.caddy; see journalctl -u caddy -n 50"
	fi
	[[ -z $backup ]] || rm -f "$backup"
fi

log "Pull $IMAGE_REPO:$tag"
docker pull -q "$IMAGE_REPO:$tag"

log "Migrations"
(
	set -a
	# shellcheck source=/dev/null
	. "$APP_DIR/migrate.env"
	set +a
	[[ -n ${MIGRATE_DATABASE_URL:-} ]] || die "MIGRATE_DATABASE_URL is empty in $APP_DIR/migrate.env"
	GOOSE_DRIVER=postgres GOOSE_DBSTRING="$MIGRATE_DATABASE_URL" docker run --rm --user "$RUN_AS" \
		-e GOOSE_DRIVER -e GOOSE_DBSTRING "$IMAGE_REPO:$tag" /app/goose -dir /app/migrations up
)

log "Start $tag"
if ! up "$tag"; then
	compose logs --tail 50 api >&2 || true
	if [[ -n $previous && $previous != "$tag" ]]; then
		log "Rolling back to $previous"
		up "$previous" || die "$tag failed and rollback to $previous failed too; the api may be down"
		die "$tag did not become healthy; rolled back to $previous"
	fi
	die "$tag did not become healthy and there is no previous tag to roll back to"
fi
echo "$tag" >current-tag

# Keep the running and previous images; drop older changeloom ones.
keep="$tag ${previous:-}"
docker image ls "$IMAGE_REPO" --format '{{.Tag}}' | while read -r t; do
	[[ " $keep " == *" $t "* ]] || docker image rm "$IMAGE_REPO:$t" >/dev/null ||
		echo "deploy: could not remove old image $IMAGE_REPO:$t" >&2
done

log "Deployed $tag"
