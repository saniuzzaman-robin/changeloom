#!/bin/sh
# Entrypoint of the curator Cloud Run Job: migrate and seed the job's own schema, then pull demand
# and fetch the top-ranked topics with Claude. Never syncs or prunes (run `make curator-sync` and
# `make curator-prune` yourself). Settings come from the job's environment; see deploy/README.md.
set -eu

for key in LOCAL_DATABASE_URL REMOTE_DATABASE_URL_PROD; do
	eval "value=\${$key:-}"
	[ -n "$value" ] || { echo "$key is not set" >&2; exit 1; }
done
if [ -z "${CLAUDE_CODE_OAUTH_TOKEN:-}" ] && [ -z "${ANTHROPIC_API_KEY:-}" ]; then
	echo "set CLAUDE_CODE_OAUTH_TOKEN or ANTHROPIC_API_KEY" >&2
	exit 1
fi

export CURATOR_AI_PROVIDER=claude
export CURATOR_RUN_ENVS="${CURATOR_RUN_ENVS:-prod}"
top="${CURATOR_CLAUDE_TOP:-100}"

/app/curator migrate
/app/curator seed
exec /app/curator run --full --rank "1-$top"
