#!/usr/bin/env bash
# Deploys one curator image on the VPS: pulls it and records its tag in curator-tag, which the next nightly
# run of curator.sh uses. It does not run the job. Runs on the server as the deploy user, from /srv/changeloom;
# the CI deploy-curator workflow calls it over SSH.
# Usage: curator-deploy.sh <tag>   (the image tag, normally the commit SHA)
# See deploy/README.md, section VPS.
set -euo pipefail

APP_DIR="${APP_DIR:-/srv/changeloom}"
IMAGE_REPO="${IMAGE_REPO:-ghcr.io/saniuzzaman-robin/changeloom-curator}"

log() { echo "==> $*"; }
die() {
	echo "curator-deploy: $*" >&2
	exit 1
}

(($# == 1)) || die "usage: $0 <tag>"
tag="$1"
[[ $tag =~ ^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$ ]] || die "invalid image tag: $tag"

cd "$APP_DIR"
for f in curator.compose.yaml curator.sh curator.env curator-db.env; do
	[[ -r $f ]] || die "missing $APP_DIR/$f (see deploy/README.md, section VPS)"
done
for f in curator.env curator-db.env; do
	[[ $(stat -c %a "$f") == 600 ]] || die "$APP_DIR/$f must be mode 600: chmod 600 $APP_DIR/$f"
done

# One deploy at a time; a running job keeps the tag it started with.
exec 9>"$APP_DIR/.curator-deploy.lock"
flock -n 9 || die "another curator deploy is running"

previous=""
[[ -s curator-tag ]] && previous="$(<curator-tag)"

log "Pull $IMAGE_REPO:$tag"
docker pull -q "$IMAGE_REPO:$tag"
echo "$tag" >curator-tag

# Keep this and the previous image; drop older curator ones (an image a running job uses is kept by Docker).
keep="$tag ${previous:-}"
docker image ls "$IMAGE_REPO" --format '{{.Tag}}' | while read -r t; do
	[[ " $keep " == *" $t "* ]] || docker image rm "$IMAGE_REPO:$t" >/dev/null ||
		echo "curator-deploy: could not remove old image $IMAGE_REPO:$t" >&2
done

log "Deployed $tag; the next nightly run uses it"
