# The curator plus the Claude CLI, for the daily Cloud Run Job (fetch only; see deploy/README.md).
# Build from the repo root: docker build -f curator/Dockerfile --build-arg CLAUDE_CODE_VERSION=<x.y.z> .
FROM golang:1.27.2-alpine AS build
WORKDIR /src
COPY curator/go.mod curator/go.sum ./
RUN go mod download
COPY curator/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/curator ./cmd/curator

FROM node:22-bookworm-slim
ARG CLAUDE_CODE_VERSION
RUN test -n "$CLAUDE_CODE_VERSION" || { echo "set --build-arg CLAUDE_CODE_VERSION=<x.y.z>" >&2; exit 1; } \
 && apt-get update && apt-get install -y --no-install-recommends ca-certificates tzdata \
 && rm -rf /var/lib/apt/lists/* \
 && npm install -g "@anthropic-ai/claude-code@${CLAUDE_CODE_VERSION}" \
 && useradd --create-home --uid 10001 app
COPY --from=build /out/curator /app/curator
COPY backend/migrations /app/backend-migrations
COPY curator/deploy/job.sh /app/job.sh
ENV BACKEND_MIGRATIONS_DIR=/app/backend-migrations \
    CURATOR_CLAUDE_CONFIG_DIR=/home/app/.claude \
    CLAUDE_BIN=claude
USER app
WORKDIR /app
CMD ["/app/job.sh"]
