#!/usr/bin/env bash
# Runs the whole curator pipeline on the local Ollama model: starts Ollama if it is not running,
# pulls the model if missing, runs `curator run --full`, then frees the model's memory and stops
# the Ollama server if this script started it. Needs `make db-up` and `make searxng-up` (the
# Makefile target does both). Settings come from curator/.env.
set -euo pipefail
cd "$(dirname "$0")/.."

OLLAMA_URL="${OLLAMA_URL:-http://127.0.0.1:11434}"
OLLAMA_MODEL="${OLLAMA_MODEL:-qwen2.5:14b}"
SEARXNG_URL="${SEARXNG_URL:-http://127.0.0.1:8080}"
WAIT_SECONDS=60

command -v ollama >/dev/null || { echo "ollama is not installed (brew install ollama)" >&2; exit 1; }

wait_for() { # wait_for <name> <url>
	for _ in $(seq "$WAIT_SECONDS"); do
		curl -sf -m 5 "$2" >/dev/null && return 0
		sleep 1
	done
	echo "$1 did not come up at $2 within ${WAIT_SECONDS}s" >&2
	return 1
}

ollama_pid=""
cleanup() {
	ollama stop "$OLLAMA_MODEL" >/dev/null 2>&1 || true
	if [ -n "$ollama_pid" ]; then kill "$ollama_pid" 2>/dev/null || true; fi
}
trap cleanup EXIT

if ! curl -sf -m 5 "$OLLAMA_URL/api/tags" >/dev/null; then
	echo "starting ollama serve"
	ollama serve >"${TMPDIR:-/tmp}/ollama-serve.log" 2>&1 &
	ollama_pid=$!
	wait_for ollama "$OLLAMA_URL/api/tags"
fi
ollama list | awk 'NR>1 {print $1}' | grep -qx "$OLLAMA_MODEL" || ollama pull "$OLLAMA_MODEL"
wait_for searxng "$SEARXNG_URL/search?q=test&format=json"

CURATOR_AI_PROVIDER=ollama CURATOR_CONCURRENCY=1 go run ./cmd/curator run --full
