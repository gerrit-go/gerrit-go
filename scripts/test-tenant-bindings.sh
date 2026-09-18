#!/usr/bin/env bash
# End-to-end check of the default-deny model + apply-tenant-bindings.sh:
# boots a throwaway Gerrit-Go server (SQLite, no SSH), asserts that a fresh
# signed-in user sees nothing, applies a namespace role binding, and asserts
# the grant takes effect (and that the script is idempotent).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PORT="${PORT:-8099}"
BASE="http://127.0.0.1:$PORT"
DATA="$(mktemp -d /tmp/gerrit-e2e-tenant.XXXX)"
LOG="$DATA/server.log"
BIN="$DATA/gerrit-server"

cleanup() { [[ -n "${SRV_PID:-}" ]] && kill "$SRV_PID" 2>/dev/null || true; rm -rf "$DATA"; }
trap cleanup EXIT

# strip the anti-XSSI ")]}'" prefix every JSON response carries
strip() { sed "1{/^)\]}/d;}"; }
api() { curl -sS -u "$1:$2" -H 'Content-Type: application/json' "${@:3}" | strip; }
get() { curl -sS "$1" | strip; }
fail() { echo "FAIL: $*" >&2; exit 1; }
pass() { echo "PASS: $*"; }

echo "== build server"
(cd "$ROOT/backend" && go build -o "$BIN" ./cmd/server)

echo "== boot server on $BASE (data=$DATA)"
"$BIN" -addr "127.0.0.1:$PORT" -data "$DATA" -ssh-addr '' >"$LOG" 2>&1 &
SRV_PID=$!
for _ in $(seq 1 50); do
  curl -fsS "$BASE/healthz" >/dev/null 2>&1 && break
  sleep 0.2
done
curl -fsS "$BASE/healthz" >/dev/null || fail "server did not start: $(cat "$LOG")"

ADMIN_PW=$(grep -oE 'password=[^ ]+' "$LOG" | head -1 | cut -d= -f2)
[[ -n "$ADMIN_PW" ]] || fail "bootstrap admin password not found in log"

echo "== seed fixtures (group, account, two namespaced projects)"
api admin "$ADMIN_PW" -X POST "$BASE/groups/" -d '{"name":"e2e-team"}' >/dev/null
api admin "$ADMIN_PW" -X POST "$BASE/accounts/" \
  -d '{"username":"alice","password":"alice-pass-123","name":"Alice","email":"alice@e2e.test"}' >/dev/null
api admin "$ADMIN_PW" -X POST "$BASE/projects/" -d '{"name":"e2e/ns1/repo-a"}' >/dev/null
api admin "$ADMIN_PW" -X POST "$BASE/projects/" -d '{"name":"other/repo-b"}' >/dev/null
GID=$(api admin "$ADMIN_PW" "$BASE/groups/" | jq -r '."e2e-team".id')
api admin "$ADMIN_PW" -X PUT "$BASE/groups/$GID/members/alice" >/dev/null

echo "== default-deny: alice and anonymous see no projects"
visible=$(api alice "alice-pass-123" "$BASE/projects/" | jq 'length')
[[ "$visible" == "0" ]] || fail "expected 0 visible projects for fresh user, got $visible"
visible=$(get "$BASE/projects/" | jq 'length')
[[ "$visible" == "0" ]] || fail "expected 0 visible projects for anonymous, got $visible"
pass "default-deny holds before any binding"

echo "== admin still sees everything"
visible=$(api admin "$ADMIN_PW" "$BASE/projects/" | jq 'length')
[[ "$visible" == "2" ]] || fail "expected 2 visible projects for admin, got $visible"
pass "admin bypass works"

echo "== apply manifest (first run creates role + binding)"
MANIFEST="$DATA/manifest.json"
cat >"$MANIFEST" <<'JSON'
{
  "roles": [
    { "name": "ns1-viewer", "display_name": "NS1 viewer", "permissions": ["read", "comment"] }
  ],
  "bindings": [
    { "role": "ns1-viewer", "subject_type": "group", "subject": "e2e-team", "scope": "e2e/ns1/*" }
  ]
}
JSON
GERRIT_URL="$BASE" GERRIT_USER=admin GERRIT_PASSWORD="$ADMIN_PW" \
  "$ROOT/scripts/apply-tenant-bindings.sh" "$MANIFEST"

echo "== grant took effect for alice"
visible=$(api alice "alice-pass-123" "$BASE/projects/" | jq -r 'keys | join(",")')
[[ "$visible" == "e2e/ns1/repo-a" ]] || fail "alice should see only e2e/ns1/repo-a, sees: $visible"
code=$(curl -sS -o /dev/null -w '%{http_code}' -u alice:alice-pass-123 "$BASE/a/projects/e2e%2Fns1%2Frepo-a")
[[ "$code" == "200" ]] || fail "alice GET own project via compat = $code, want 200"
code=$(curl -sS -o /dev/null -w '%{http_code}' -u alice:alice-pass-123 "$BASE/a/projects/other%2Frepo-b")
[[ "$code" == "404" ]] || fail "alice must not read other/repo-b via compat (got $code)"
pass "namespace binding scoped access enforced"

echo "== second run is idempotent"
out=$(GERRIT_URL="$BASE" GERRIT_USER=admin GERRIT_PASSWORD="$ADMIN_PW" \
  "$ROOT/scripts/apply-tenant-bindings.sh" "$MANIFEST")
grep -q "exists (skip)" <<<"$out" || fail "expected skip lines, got: $out"
n=$(api admin "$ADMIN_PW" "$BASE/roles/" | jq '[.[] | select(.name=="ns1-viewer")] | length')
[[ "$n" == "1" ]] || fail "role duplicated (n=$n)"
pass "re-run skipped without duplicating"

echo "== migration marker exposed via /config"
model=$(get "$BASE/config" | jq -r '.permission_model')
[[ "$model" == "default-deny-v1" ]] || fail "permission_model = $model"
pass "/config reports default-deny-v1"

echo "== audit trail recorded the mutations"
actions=$(api admin "$ADMIN_PW" "$BASE/admin/audit?n=100" | jq -r '.entries[].action' | sort -u)
grep -q "role-create" <<<"$actions" || fail "missing role-create audit entry"
grep -q "binding-create" <<<"$actions" || fail "missing binding-create audit entry"
pass "audit entries present"

echo "ALL E2E CHECKS PASSED"
