#!/bin/sh
# Entrypoint of the nightly curator job on the VPS: migrate and seed the job's own database, pull prod's
# demand and fetch every due topic with Claude, then sync to prod and prune it. The fetch writes only to
# the job's own database, so the hosted one is touched by the demand pull and then by sync and prune.
# If the fetch fails partway (e.g. a subscription limit), what it stored is still synced and the job
# exits non-zero. Settings come from the job's environment; see deploy/README.md.
set -eu

for key in LOCAL_DATABASE_URL REMOTE_DATABASE_URL_PROD API_BASE_URL_PROD NOTIFY_SECRET_PROD; do
	eval "value=\${$key:-}"
	[ -n "$value" ] || { echo "$key is not set" >&2; exit 1; }
done
if [ -z "${CLAUDE_CODE_OAUTH_TOKEN:-}" ] && [ -z "${ANTHROPIC_API_KEY:-}" ]; then
	echo "set CLAUDE_CODE_OAUTH_TOKEN or ANTHROPIC_API_KEY" >&2
	exit 1
fi

export CURATOR_AI_PROVIDER=claude
export CURATOR_RUN_ENVS=prod

/app/curator migrate
/app/curator seed

status=0
/app/curator run --full || status=$?
[ "$status" -eq 0 ] || echo "fetch failed (exit $status); syncing what it stored" >&2

/app/curator sync --env prod
/app/curator prune --env prod
exit "$status"
