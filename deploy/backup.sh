#!/usr/bin/env bash
# Dump the production database to $BACKUP_DIR and keep the newest $BACKUP_KEEP dumps.
# Run from cron on the host, e.g.: 15 3 * * * /opt/changeloom/deploy/backup.sh
# Restore: gunzip -c FILE | docker compose exec -T postgres psql -U USER -d DB
set -euo pipefail

cd "$(dirname "$0")"
set -a
# shellcheck disable=SC1091
source .env
set +a

: "${BACKUP_DIR:?set BACKUP_DIR in .env}"
: "${BACKUP_KEEP:?set BACKUP_KEEP in .env}"

mkdir -p "$BACKUP_DIR"
out="$BACKUP_DIR/changeloom-$(date -u +%Y%m%dT%H%M%SZ).sql.gz"
tmp="$out.partial"

if ! docker compose exec -T postgres pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" --no-owner | gzip > "$tmp"; then
  rm -f "$tmp"
  echo "backup failed: pg_dump exited non-zero (is the postgres service running?)" >&2
  exit 1
fi
mv "$tmp" "$out"
echo "wrote $out"

# Prune: list newest first, drop everything past BACKUP_KEEP
ls -1t "$BACKUP_DIR"/changeloom-*.sql.gz | tail -n +"$((BACKUP_KEEP + 1))" | xargs -r rm -f --
