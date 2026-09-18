#!/usr/bin/env python3
"""Split the legacy `manifests` repo into rk/Linux/manifests and
rk/Android/35xx_Android14/manifests.

Usage: reorg-manifests.py <mapping.tsv> <manifests.git> <dry-run|apply>
Apply also needs env: A_URL / L_URL (push targets, e.g. file:///abs/path.git)

Each branch goes wholesale to the repo owning the MAJORITY of its project
references (Linux SDK branches -> rk/Linux, Android SDK branches -> rk/Android),
because both SDKs legitimately reference shared BSP repos (u-boot, kernel,
rkbin): cross-side references are KEPT, only names are rewritten to the new
namespace. Dangling references to never-migrated repos are rewritten with the
same classification rules. Each target branch is a fresh single commit; the
original history remains in the archived `manifests` repo.
"""
import os, re, subprocess, sys, shutil, tempfile, importlib.util

MAPFILE, SRCGIT, MODE = sys.argv[1], sys.argv[2], sys.argv[3]
A_REPO = "rk/Android/35xx_Android14/manifests"
L_REPO = "rk/Linux/manifests"
A_PREFIX, L_PREFIX = "rk/Android/", "rk/Linux/"
SELF = "__SELF_MANIFEST_REPO__"

mapping = {}
for line in open(MAPFILE):
    p = line.rstrip("\n").split("\t")
    if len(p) == 2:
        mapping[p[0]] = p[1]

_spec = importlib.util.spec_from_file_location(
    "reorg_map", os.path.join(os.path.dirname(os.path.abspath(__file__)), "reorg-rk-mapping.py"))
_reorg_map = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(_reorg_map)

def inferred_dest(old):
    new, bucket = _reorg_map.classify(old)
    return new if new and bucket != "UNMATCHED" else None

TAG_RE = re.compile(r'<(extend-project|project)(\s[^>]*)/?>')
NAME_RE = re.compile(r'name="([^"]+)"')

def sh(*a, cwd=None, env=None, check=True):
    r = subprocess.run(a, cwd=cwd, env=env, capture_output=True, text=True)
    if check and r.returncode != 0:
        raise SystemExit(f"failed: {' '.join(a)}\n{r.stdout}\n{r.stderr}")
    return r.stdout

def dest_of(old_name):
    if old_name == "manifests":
        return SELF
    if old_name in mapping:
        return mapping[old_name]
    if old_name.startswith(A_PREFIX) or old_name.startswith(L_PREFIX):
        return old_name
    return inferred_dest(old_name)

def rewrite_file(text, self_repo):
    out, sides = [], {"A": 0, "L": 0}
    for ln in text.split("\n"):
        m = TAG_RE.search(ln)
        if m:
            base = m.start(2)
            nm = NAME_RE.search(m.group(2))
            if nm:
                d = dest_of(nm.group(1))
                if d is None:
                    raise RuntimeError(f"unmapped project {nm.group(1)!r}")
                if d == SELF:
                    d = self_repo
                else:
                    sides["A" if d.startswith(A_PREFIX) else "L"] += 1
                s, e = base + nm.start(1), base + nm.end(1)
                ln = ln[:s] + d + ln[e:]
        out.append(ln)
    return "\n".join(out), sides

def collect(clone_dir):
    heads = sh("git", "branch", "-r", cwd=clone_dir)
    branches = sorted(l.strip().split("origin/", 1)[1] for l in heads.splitlines()
                      if "origin/" in l and "->" not in l)
    plan = {}
    for br in branches:
        sh("git", "checkout", "-q", "-B", br, "origin/" + br, cwd=clone_dir)
        files = {}
        for f in filter(None, sh("git", "ls-files", "-z", cwd=clone_dir).split("\0")):
            p = os.path.join(clone_dir, f)
            if f.endswith(".xml") and os.path.isfile(p):
                files[f] = open(p).read()
        plan[br] = files
    return plan

def main():
    work = tempfile.mkdtemp(prefix="mani-reorg-")
    clone = os.path.join(work, "src")
    sh("git", "clone", "-q", SRCGIT, clone)
    plan = collect(clone)
    sides = {}
    for br, files in plan.items():
        tA = tL = 0
        rendered = {}
        for f, t in files.items():
            rA, sA = rewrite_file(t, A_REPO)
            rL, sL = rewrite_file(t, L_REPO)
            tA += sA["A"] + sL["A"]
            tL += sA["L"] + sL["L"]
            rendered[f] = t
        side = "A" if tA >= tL else "L"
        sides[br] = (side, tA, tL)
        print(f"branch {br}: xml={len(files)} android_refs={tA} linux_refs={tL} -> {side}")
    if MODE != "apply":
        print("dry-run complete (no pushes)")
        return
    targets = {"A": (os.environ["A_URL"], A_REPO), "L": (os.environ["L_URL"], L_REPO)}
    for side in ("A", "L"):
        url, self_repo = targets[side]
        seed = os.path.join(work, f"seed-{side}")
        os.makedirs(seed)
        sh("git", "init", "-q", seed)
        sh("git", "commit", "-q", "--allow-empty", "-m", "init", cwd=seed, env=gitenv())
        pushed = 0
        for br, files in plan.items():
            if sides[br][0] != side:
                continue
            clean_tree(seed)
            for f, t in files.items():
                text, _ = rewrite_file(t, self_repo)
                fp = os.path.join(seed, f)
                if os.path.dirname(fp):
                    os.makedirs(os.path.dirname(fp), exist_ok=True)
                open(fp, "w").write(text)
            sh("git", "checkout", "-q", "--orphan", br, cwd=seed)
            sh("git", "add", "-A", cwd=seed)
            sh("git", "commit", "-q", "-m", f"split from manifests branch {br}", cwd=seed, env=gitenv())
            sh("git", "push", "-f", "-q", url, f"HEAD:refs/heads/{br}", cwd=seed)
            pushed += 1
        print(f"{url}: pushed {pushed} branches")
    shutil.rmtree(work, ignore_errors=True)

def gitenv():
    return {**os.environ, "GIT_AUTHOR_NAME": "reorg", "GIT_AUTHOR_EMAIL": "reorg@gerrit-go",
            "GIT_COMMITTER_NAME": "reorg", "GIT_COMMITTER_EMAIL": "reorg@gerrit-go"}

def clean_tree(seed):
    for root, dirs, fs in os.walk(seed):
        if "/.git" in root:
            continue
        for fn in fs:
            os.remove(os.path.join(root, fn))

if __name__ == "__main__":
    main()
