#!/usr/bin/env bash
# Idempotently apply a tenant binding manifest (roles + role bindings) to a
# Gerrit-Go instance over its REST API using HTTP Basic auth.
#
#   GERRIT_URL=https://host:port GERRIT_USER=admin GERRIT_PASSWORD=<http-password> \
#     ./apply-tenant-bindings.sh manifest.json
#
# Manifest shape:
# {
#   "roles": [
#     { "name": "bsp-dev", "display_name": "BSP 开发", "permissions": ["read", "push", "comment"] }
#   ],
#   "bindings": [
#     { "role": "bsp-dev", "subject_type": "group", "subject": "rk3308-team", "scope": "rk3308_linux/*" },
#     { "role": "bsp-dev", "subject_type": "account", "subject": "alice", "scope": "docs/*" }
#   ]
# }
set -euo pipefail

: "${GERRIT_URL:?set GERRIT_URL, e.g. https://gerrit.example:8443}"
: "${GERRIT_USER:?set GERRIT_USER (admin username)}"
: "${GERRIT_PASSWORD:?set GERRIT_PASSWORD (admin HTTP password)}"
MANIFEST="${1:?usage: $0 <manifest.json>}"

command -v jq >/dev/null || { echo "jq is required" >&2; exit 1; }
command -v curl >/dev/null || { echo "curl is required" >&2; exit 1; }
[[ -f "$MANIFEST" ]] || { echo "manifest not found: $MANIFEST" >&2; exit 1; }

BASE="${GERRIT_URL%/}"
# Gerrit-style responses are prefixed with an anti-XSSI ")]}'" line.
api() { curl -fsS -u "$GERRIT_USER:$GERRIT_PASSWORD" -H 'Content-Type: application/json' "$@" | sed "1{/^)\]}/d;}"; }

echo "== fetch current state"
roles_now=$(api "$BASE/roles/")
groups_now=$(api "$BASE/groups/")
accounts_now=$(api "$BASE/accounts/")

echo "== ensure roles"
jq -c '.roles // [] | .[]' "$MANIFEST" | while read -r role; do
  name=$(jq -r '.name' <<<"$role")
  if [[ -n "$(jq -r --arg n "$name" '(. // [])[] | select(.name==$n) | .id' <<<"$roles_now")" ]]; then
    echo "  role $name exists (skip)"
    continue
  fi
  created=$(api -X POST "$BASE/roles/" -d "$role")
  echo "  role $name created id=$(jq -r '.id' <<<"$created")"
done

roles_now=$(api "$BASE/roles/")

role_id() { jq -r --arg n "$1" '(. // [])[] | select(.name==$n) | .id' <<<"$roles_now"; }
subject_id() { # type name -> numeric id
  local type="$1" name="$2"
  if [[ "$type" == "group" ]]; then
    jq -r --arg n "$name" '.[$n] // empty | .id // empty' <<<"$groups_now"
  else
    jq -r --arg n "$name" '(. // [])[] | select(.username==$n) | ._account_id' <<<"$accounts_now"
  fi
}

echo "== ensure bindings"
jq -c '.bindings // [] | .[]' "$MANIFEST" | while read -r b; do
  role=$(jq -r '.role' <<<"$b")
  stype=$(jq -r '.subject_type // "group"' <<<"$b")
  sname=$(jq -r '.subject' <<<"$b")
  scope=$(jq -r '.scope // "*"' <<<"$b")

  rid=$(role_id "$role")
  [[ -n "$rid" ]] || { echo "  ERROR: role $role not found" >&2; exit 1; }
  sid=$(subject_id "$stype" "$sname")
  [[ -n "$sid" && "$sid" != "null" ]] || { echo "  ERROR: $stype $sname not found" >&2; exit 1; }

  existing=$(api "$BASE/roles/$rid/bindings")
  if [[ -n "$(jq -r --argjson sid "$sid" --arg st "$stype" --arg sc "$scope" \
      '(. // [])[] | select(.subject_type==$st and .subject_id==$sid and .scope==$sc) | .id' <<<"$existing")" ]]; then
    echo "  binding $role <- $stype:$sname @ $scope exists (skip)"
    continue
  fi
  api -X POST "$BASE/roles/$rid/bindings" \
    -d "{\"subject_type\":\"$stype\",\"subject_id\":$sid,\"scope\":\"$scope\"}" >/dev/null
  echo "  binding $role <- $stype:$sname @ $scope created"
done

echo "== done"
