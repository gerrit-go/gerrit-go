#!/usr/bin/env python3
"""Generate old->new project name mapping for the RK namespace reorg.

Usage: reorg-mapping.py names.txt mapping.tsv
Rules (confirmed 2026-09-18):
  A = rk/Android/35xx_Android14   L = rk/Linux
  - dotted families: strip family segment, dots->slashes
      linux.X -> L/X   rk.X -> L/X   rtos.X -> L/rtos-family path   android.X -> A/X
  - slash names: platform/ device/ kernel/ external/ toolchain/ build/ tools/ -> A/<full>
      rk/<android seg> -> A/rk/<rest>   other rk/<rest> -> L/rk/<rest>
  - docs/android -> A/docs/android, docs/common -> L/docs/common
  - RKTools -> L/RKTools ; manifests kept unchanged
Exit non-zero if any name is unmatched or mapping collides.
"""
import sys

A = "rk/Android/35xx_Android14"
L = "rk/Linux"

ANDROID_RK_SEGS = {"hardware", "platform", "device", "android", "packages",
                   "rkCamera2", "eink-paint", "uvc-gadget", "hwc_proxy_service",
                   "vman_module"}
ANDROID_TOP_SEGS = {"platform", "device", "kernel", "external", "toolchain",
                    "build", "tools", "android"}
# Special cases: same component exists under two legacy spellings (dotted and
# slash); keep them in sibling dirs so neither target collides.
OVERRIDES = {
    "android.rk.platform.system.rk_tee_user": f"{A}/rk-tee-user",
    "linux.linux-rga": f"{L}/rga",
    "linux/linux-rga": f"{L}/rk/rga",
}

def slashify(rest: str) -> str:
    # Convert hierarchy dots to slashes, but keep dots that belong to
    # version-bearing names (13.2, gcc-arm-10.3-2021.07, ...): a dot is a
    # level separator only when neither neighbour segment touches a digit.
    segs = rest.split(".")
    out = segs[0]
    for i, s in enumerate(segs[1:], start=1):
        if segs[i - 1][-1:].isdigit() or s[:1].isdigit():
            out += "." + s
        else:
            out += "/" + s
    return out

def classify(name: str):
    """Return (new_name, bucket) or (None, 'keep') or (None, 'UNMATCHED')."""
    if name in OVERRIDES:
        return OVERRIDES[name], "android" if name.startswith("android") else "linux"
    if name == "manifests":
        return None, "keep"
    # dotted families (no leading-slash dotted root)
    if "/" not in name and "." in name:
        fam, _, rest = name.partition(".")
        if fam == "linux":
            return f"{L}/{slashify(rest)}", "linux"
        if fam == "rk":
            return f"{L}/{slashify(rest)}", "linux"
        if fam == "rtos":
            return f"{L}/{slashify(name)}", "linux"  # keep rtos segment: rk/Linux/rtos/rt-thread/...
        if fam == "android":
            return f"{A}/{slashify(rest)}", "android"
        return f"{L}/{name}", "linux-other"
    # slash names
    if name in ("RKTools",):
        return f"{L}/RKTools", "linux"
    if name.startswith("docs/"):
        if name == "docs/android" or name.startswith("docs/android/"):
            return f"{A}/{name}", "android"
        return f"{L}/{name}", "linux"
    if name.startswith("linux/"):
        return f"{L}/{name[len('linux/'):]}", "linux"
    top = name.split("/", 1)[0]
    if top in ANDROID_TOP_SEGS:
        return f"{A}/{name}", "android"
    if top == "rk":
        rest = name.split("/", 1)[1]
        seg = rest.split("/", 1)[0]
        if seg in ANDROID_RK_SEGS:
            return f"{A}/rk/{rest}", "android"
        return f"{L}/rk/{rest}", "linux"
    return f"{A}/{name}", "UNMATCHED"

def main():
    names = [l.strip() for l in open(sys.argv[1]) if l.strip()]
    out_path = sys.argv[2]
    mapping = {}
    buckets = {}
    unmatched = []
    for n in names:
        new, bucket = classify(n)
        if bucket == "UNMATCHED":
            unmatched.append(n)
            continue
        buckets[bucket] = buckets.get(bucket, 0) + 1
        if new is None:
            continue
        if "'" in n or "'" in new:
            sys.exit(f"FATAL: quote in name {n}")
        mapping[n] = new
    # collision checks
    olds = set(names)
    seen_new = {}
    errs = []
    for old, new in mapping.items():
        if new in olds:
            errs.append(f"new {new} collides with existing project {new}")
        if new in seen_new:
            errs.append(f"duplicate target {new}: {seen_new[new]} and {old}")
        seen_new[new] = old
    # a repo dir X.git and a parent dir X legitimately coexist on the
    # filesystem, so ancestor overlap is fine; only exact target equality
    # (checked above) and new==old are real collisions.
    with open(out_path, "w") as f:
        for old in sorted(mapping):
            f.write(f"{old}\t{mapping[old]}\n")
    total_renamed = len(mapping)
    print(f"renamed: {total_renamed}  kept: {len(names) - total_renamed}")
    print("buckets:", {k: v for k, v in sorted(buckets.items())})
    if unmatched:
        print("UNMATCHED (fix rules!):")
        for u in unmatched:
            print("  ", u)
    if errs:
        print("COLLISIONS (fix rules!):")
        for e in sorted(set(errs))[:40]:
            print("  ", e)
        sys.exit(1)
    if unmatched:
        sys.exit(1)
    print("OK: mapping written to", out_path)

if __name__ == "__main__":
    main()
