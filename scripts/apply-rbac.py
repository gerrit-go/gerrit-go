#!/usr/bin/env python3
"""Apply an RBAC manifest (groups + roles + bindings) to Gerrit-Go over REST.

Usage: GERRIT_URL=http://host:port GERRIT_USER=u GERRIT_PASSWORD=p \
         apply-rbac.py manifest.json

Idempotent: groups/roles are created (roles additionally updated via PUT so the
manifest stays the source of truth for permissions); bindings are skipped when
an identical (role, subject, scope) row already exists. No jq/curl needed.
"""
import base64, json, os, sys, urllib.error, urllib.request

BASE = os.environ.get("GERRIT_URL", "http://127.0.0.1:8443").rstrip("/")
AUTH = base64.b64encode(f'{os.environ["GERRIT_USER"]}:{os.environ["GERRIT_PASSWORD"]}'.encode()).decode()


def api(path, method="GET", body=None):
    req = urllib.request.Request(BASE + path, method=method)
    req.add_header("Authorization", "Basic " + AUTH)
    data = None
    if body is not None:
        data = json.dumps(body).encode()
        req.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(req, data) as r:
            text = r.read().decode()
    except urllib.error.HTTPError as e:
        print(f"  ! {method} {path} -> {e.code} {e.read().decode()[:200]}")
        raise SystemExit(1)
    if text.startswith(")]}'"):
        text = text.split("\n", 1)[1] if "\n" in text else ""
    return json.loads(text) if text.strip() else None


def main():
    manifest = json.load(open(sys.argv[1]))

    groups = api("/groups/") or {}
    for g in manifest.get("groups", []):
        if g["name"] in groups:
            print(f"group {g['name']}: exists")
            continue
        created = api("/groups/", "POST", g)
        groups[g["name"]] = created
        print(f"group {g['name']}: created id={created.get('id')}")

    roles = api("/roles/") or []
    by_name = {r["name"]: r for r in roles}
    for role in manifest.get("roles", []):
        want = sorted(role.get("permissions", []))
        cur = by_name.get(role["name"])
        if cur is None:
            created = api("/roles/", "POST", role)
            by_name[role["name"]] = created
            print(f"role {role['name']}: created id={created.get('id')}")
        elif sorted(cur.get("permissions", [])) != want:
            updated = api(f"/roles/{cur['id']}", "PUT", role)
            by_name[role["name"]] = updated
            print(f"role {role['name']}: updated permissions -> {want}")
        else:
            print(f"role {role['name']}: up to date")

    for b in manifest.get("bindings", []):
        role = by_name.get(b["role"])
        if role is None:
            print(f"ERROR: role {b['role']} not defined/created")
            raise SystemExit(1)
        stype = b.get("subject_type", "group")
        if stype == "group":
            sid = groups.get(b["subject"], {}).get("id") or (api("/groups/").get(b["subject"]) or {}).get("id")
        else:
            acct = next((a for a in api("/accounts/") if a.get("username") == b["subject"]), None)
            sid = acct and acct.get("_account_id")
        if sid is None:
            print(f"ERROR: {stype} {b['subject']} not found")
            raise SystemExit(1)
        sid = int(sid)  # REST may serialize ids as strings
        scope = b.get("scope", "*")
        existing = api(f"/roles/{role['id']}/bindings") or []
        if any(x.get("subject_type") == stype and int(x.get("subject_id", -1)) == sid and x.get("scope") == scope
               for x in existing):
            print(f"binding {b['role']} <- {stype}:{b['subject']} @ {scope}: exists")
            continue
        api(f"/roles/{role['id']}/bindings", "POST",
            {"subject_type": stype, "subject_id": sid, "scope": scope})
        print(f"binding {b['role']} <- {stype}:{b['subject']} @ {scope}: created")

    print("RBAC manifest applied")


if __name__ == "__main__":
    main()
