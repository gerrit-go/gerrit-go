#!/usr/bin/env bash
# gerrit-go backup: dump the PostgreSQL database and archive the git
# repositories, keeping the most recent archives. Intended to run on the NAS
# host via cron, e.g.:
#   0 3 * * * /vol1/1000/dockers/gerrit-go/deploy/backup.sh >> /var/log/gerrit-go-backup.log 2>&1
set -euo pipefail

COMPOSE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BACKUP_DIR="$COMPOSE_DIR/data/backups"
KEEP=5
mkdir -p "$BACKUP_DIR"

TS="$(date -u +%Y%m%d-%H%M%S)"

# 1. Database dump (PostgreSQL runs in the gerrit-go-db container).
docker exec gerrit-go-db pg_dump -U gerrit -d gerrit \
  | gzip > "$BACKUP_DIR/db-$TS.sql.gz"

# 2. Git repositories archive (same layout the /admin/backup endpoint uses).
docker exec gerrit-go tar -czf "/data/backups/repos-$TS.tar.gz" -C /data git

# 3. Retention: keep the newest $KEEP of each kind.
ls -1t "$BACKUP_DIR"/db-*.sql.gz 2>/dev/null | tail -n +$((KEEP + 1)) | xargs -r rm -f
ls -1t "$BACKUP_DIR"/repos-*.tar.gz 2>/dev/null | tail -n +$((KEEP + 1)) | xargs -r rm -f

echo "backup complete: db-$TS.sql.gz repos-$TS.tar.gz"
