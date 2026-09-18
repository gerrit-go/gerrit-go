#!/usr/bin/env bash
# Reorg-rk-structure: rename projects per mapping.tsv (DB rows + bare repo dirs).
# Run ON THE NAS (needs sudo). Modes:
#   precheck     - verify mapping vs DB/filesystem, no changes
#   sql          - apply only the DB part (shadow-DB test: PGDB=reorg_test ./x.sh m.tsv sql)
#   fs           - mv repo dirs only (journal: moved.lst)
#   apply        - stop app, pg_dump, sql, fs, verify, start app
#   rollback-fs  - reverse the mv loop using the journal
# Env: DEPLOY_DIR GITDIR DBC APP PGDB PGUSER JOURNAL SQLFILE
set -euo pipefail
MAPPING=${1:?usage: $0 mapping.tsv <precheck|sql|fs|apply|rollback-fs>}
MODE=${2:-precheck}
DEPLOY_DIR=${DEPLOY_DIR:-/vol1/1000/dockers/gerrit-go}
GITDIR=${GITDIR:-$DEPLOY_DIR/data/git}
DBC=${DBC:-gerrit-go-db}
APP=${APP:-gerrit-go}
PGDB=${PGDB:-gerrit}
PGUSER=${PGUSER:-gerrit}
JOURNAL=${JOURNAL:-$DEPLOY_DIR/reorg-moved.lst}
SQLFILE=${SQLFILE:-/tmp/reorg.sql}

grep -q "'" "$MAPPING" && { echo "FATAL: single quote in mapping"; exit 1; }
grep -qP '^\S+\t\S+$' "$MAPPING" || { [ -s "$MAPPING" ] || { echo "FATAL: empty mapping"; exit 1; }; }

psql_q() { docker exec "$DBC" psql -U "$PGUSER" -d "$PGDB" -v ON_ERROR_STOP=1 -tAc "$1"; }
psql_f() { docker exec -i "$DBC" psql -U "$PGUSER" -d "$PGDB" -v ON_ERROR_STOP=1 -q -f -; }

gen_sql() {
  # Emit: drop FKs to projects -> full rename in one txn -> re-add FKs (validates).
  local fks
  fks=$(psql_q "SELECT conrelid::regclass||'|'||conname||'|'||pg_get_constraintdef(oid) FROM pg_constraint WHERE contype='f' AND confrelid='projects'::regclass")
  {
    echo "BEGIN;"
    while IFS='|' read -r tbl con def; do
      [ -n "$tbl" ] && echo "ALTER TABLE $tbl DROP CONSTRAINT $con;"
    done <<<"$fks"
    while IFS=$'\t' read -r old new; do
      [ -z "$old" ] && continue
      echo "UPDATE projects SET name='$new' WHERE name='$old';"
      for t in access_rules changes project_labels submit_requirements watched_projects webhooks; do
        echo "UPDATE $t SET project='$new' WHERE project='$old';"
      done
      echo "UPDATE projects SET parent='$new' WHERE parent='$old';"
    done < "$MAPPING"
    while IFS='|' read -r tbl con def; do
      [ -n "$tbl" ] && echo "ALTER TABLE $tbl ADD CONSTRAINT $con $def;"
    done <<<"$fks"
    echo "COMMIT;"
  } > "$SQLFILE"
}

precheck() {
  echo "== mapping rows: $(grep -c '' "$MAPPING")"
  local errs=0
  # DB side: every old must exist; no new may exist
  docker exec -i "$DBC" psql -U "$PGUSER" -d "$PGDB" -v ON_ERROR_STOP=1 -q <<'EOSQL'
DROP TABLE IF EXISTS reorg_map_tmp;
CREATE TABLE reorg_map_tmp(old text, new text);
EOSQL
  # stream mapping into the temp-ish table
  awk -F'\t' '{printf("INSERT INTO reorg_map_tmp VALUES (\x27%s\x27,\x27%s\x27);\n", $1, $2)}' "$MAPPING" \
    | docker exec -i "$DBC" psql -U "$PGUSER" -d "$PGDB" -v ON_ERROR_STOP=1 -q -f -
  local missing extra dup
  missing=$(psql_q "SELECT count(*) FROM reorg_map_tmp m LEFT JOIN projects p ON p.name=m.old WHERE p.name IS NULL")
  extra=$(psql_q "SELECT count(*) FROM reorg_map_tmp m JOIN projects p ON p.name=m.new")
  dup=$(psql_q "SELECT count(*) FROM (SELECT new FROM reorg_map_tmp GROUP BY 1 HAVING count(*)>1) x")
  echo "old names missing in DB: $missing | targets already present: $extra | duplicate targets: $dup"
  [ "$missing" = 0 ] && [ "$extra" = 0 ] && [ "$dup" = 0 ] || errs=1
  psql_q "DROP TABLE reorg_map_tmp" >/dev/null
  # FS side: src dir must exist, target must not
  while IFS=$'\t' read -r old new; do
    [ -z "$old" ] && continue
    [ -d "$GITDIR/$old.git" ] || { echo "MISSING repo dir: $old.git"; errs=1; }
    [ -e "$GITDIR/$new.git" ] && { echo "TARGET exists: $new.git"; errs=1; }
  done < "$MAPPING"
  [ "$errs" = 0 ] && echo "PRECHECK OK" || { echo "PRECHECK FAILED"; exit 1; }
}

do_sql() {
  gen_sql
  echo "SQL: $(grep -c '' "$SQLFILE") statements -> $SQLFILE (piping into $DBC/$PGDB)"
  psql_f < "$SQLFILE"
  echo "SQL applied"
}

do_fs() {
  : > "$JOURNAL"
  local n=0
  while IFS=$'\t' read -r old new; do
    [ -z "$old" ] && continue
    mkdir -p "$GITDIR/$(dirname "$new")"
    mv "$GITDIR/$old.git" "$GITDIR/$new.git"
    printf '%s\t%s\n' "$old" "$new" >> "$JOURNAL"
    n=$((n+1))
  done < "$MAPPING"
  echo "moved $n repos (journal: $JOURNAL)"
}

rollback_fs() {
  local n=0
  while IFS=$'\t' read -r old new; do
    [ -z "$new" ] && continue
    mv "$GITDIR/$new.git" "$GITDIR/$old.git"
    n=$((n+1))
  done < <(tac "$JOURNAL")
  echo "rolled back $n repos"
}

verify() {
  echo "projects: $(psql_q "SELECT count(*) FROM projects") (expect 1303)"
  echo "  rk/Linux:    $(psql_q "SELECT count(*) FROM projects WHERE name LIKE 'rk/Linux/%'")"
  echo "  rk/Android:  $(psql_q "SELECT count(*) FROM projects WHERE name LIKE 'rk/Android/%'")"
  psql_q "SELECT name FROM projects ORDER BY 1" > /tmp/reorg-dbnames.txt
  local orphan=0
  while read -r p; do
    [ -d "$GITDIR/$p.git" ] || { echo "DB name without dir: $p"; orphan=$((orphan+1)); }
  done < /tmp/reorg-dbnames.txt
  [ $orphan -eq 0 ] && echo "all DB names have repo dirs"
  for p in rk/Linux/mpp rk/Linux/rkbin; do
    git -C "$GITDIR/$p.git" rev-parse --git-dir >/dev/null 2>&1 && echo "  git OK $p" || echo "  git MISSING $p"
  done
}

case "$MODE" in
  precheck) precheck ;;
  sql)      do_sql ;;
  fs)       do_fs; verify ;;
  apply)
    precheck
    mkdir -p "$DEPLOY_DIR/reorg-backups"
    TS=$(date +%Y%m%d-%H%M%S)
    echo "stopping $APP"; docker stop -t 20 "$APP" >/dev/null
    echo "pg_dump -> reorg-backups/pre-reorg-$TS.sql.gz"
    docker exec "$DBC" pg_dump -U "$PGUSER" -d "$PGDB" | gzip > "$DEPLOY_DIR/reorg-backups/pre-reorg-$TS.sql.gz"
    do_sql
    do_fs
    verify
    echo "starting $APP"; docker start "$APP" >/dev/null
    echo "APPLY DONE"
    ;;
  rollback-fs) rollback_fs ;;
  *) echo "unknown mode $MODE"; exit 1 ;;
esac
