#!/usr/bin/env python3
"""Print a `go test -run` regex covering the tests a change can plausibly break.

internal/server is one package of ~870s under -race, which is 84% of a full
run — so scoping by package does nothing and scoping by TEST is the only
lever. Blast radius here means, for every changed .go file:

  * the tests in its own _test.go, and
  * every test that mentions a top-level identifier the file declares.

The second is what makes it a blast radius rather than a file pairing: change
`connectWire` and you get every test that touches it, wherever it lives.

This is a FAST-LOOP tool, not the gate. It cannot see a test that breaks for a
reason not spelled in its source — the RNG-draw-order class, where adding a
random call moves a seed-dependent fixture somewhere else, is exactly that.
The full -race suite stays the gate before anything is pushed.

Run: python3 scripts/affected_tests.py [git-rev, default origin/main]
"""
import os, re, subprocess, sys

ROOT = os.path.join(os.path.dirname(__file__), "..")
base = sys.argv[1] if len(sys.argv) > 1 else "origin/main"

changed = subprocess.run(["git", "diff", "--name-only", base, "--"],
                         cwd=ROOT, capture_output=True, text=True).stdout.split()
changed += subprocess.run(["git", "status", "--porcelain"],
                          cwd=ROOT, capture_output=True, text=True).stdout
changed = [c for c in (changed if isinstance(changed, list) else []) if c.endswith(".go")]
# untracked/modified from `git status --porcelain` (lines are "XY path")
for line in subprocess.run(["git", "status", "--porcelain"], cwd=ROOT,
                           capture_output=True, text=True).stdout.splitlines():
    p = line[3:].strip()
    if p.endswith(".go"):
        changed.append(p)
changed = sorted(set(changed))
if not changed:
    print("")
    sys.exit(0)

# Top-level declarations in the changed non-test files.
DECL = re.compile(r"^(?:func\s+(?:\([^)]*\)\s*)?([A-Za-z_]\w*)|"
                  r"(?:type|const|var)\s+([A-Za-z_]\w*))", re.M)
symbols, pkgs, own_tests = set(), set(), set()
for path in changed:
    full = os.path.join(ROOT, path)
    if not os.path.exists(full):
        continue
    pkgs.add(os.path.dirname(path))
    src = open(full).read()
    if path.endswith("_test.go"):
        own_tests.update(re.findall(r"^func (Test\w+)", src, re.M))
        continue
    for m in DECL.finditer(src):
        name = m.group(1) or m.group(2)
        if name and len(name) > 3:  # skip i, ok, err-ish noise
            symbols.add(name)
    # the file's own test counterpart
    twin = full[:-3] + "_test.go"
    if os.path.exists(twin):
        own_tests.update(re.findall(r"^func (Test\w+)", open(twin).read(), re.M))

# Every test that mentions one of those identifiers — but a symbol that turns
# up in most tests (hub, setBlock, the shared fixtures) says nothing about
# blast radius, it just says the package is one package. Score each symbol by
# how many tests name it and throw away the ones that are merely structural.
bodies = {}
for pkg in pkgs:
    d = os.path.join(ROOT, pkg)
    if not os.path.isdir(d):
        continue
    for f in os.listdir(d):
        if not f.endswith("_test.go"):
            continue
        parts = re.split(r"^func (Test\w+)", open(os.path.join(d, f)).read(), flags=re.M)
        for i in range(1, len(parts), 2):
            bodies[parts[i]] = parts[i + 1]

total = max(len(bodies), 1)
SPREAD = 0.10  # a symbol naming more than a tenth of the suite is not a signal
hits, kept = set(own_tests), []
for sym in symbols:
    pat = re.compile(r"\b%s\b" % re.escape(sym))
    named = [t for t, b in bodies.items() if pat.search(b)]
    if not named or len(named) / total > SPREAD:
        continue
    kept.append(sym)
    hits.update(named)

print("^(" + "|".join(sorted(hits)) + ")$" if hits else "")
sys.stderr.write(f"{len(changed)} changed file(s), {len(kept)}/{len(symbols)} discriminating symbol(s)"
                 f" -> {len(hits)}/{total} test(s)\n")
