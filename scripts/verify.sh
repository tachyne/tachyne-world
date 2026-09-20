#!/bin/bash
# Tiered verification. internal/server is ~870s of a ~1040s -race run — 84% of
# it — so the only lever that matters is which TESTS run, not which packages.
#
#   ./scripts/verify.sh            fast: build, vet, and the affected tests
#   ./scripts/verify.sh race       the same set, under -race
#   ./scripts/verify.sh full       the whole suite under -race (the push gate)
#
# "Affected" is scripts/affected_tests.py: the changed files' own tests plus
# every test naming a symbol they declare, minus symbols so common they name a
# tenth of the suite. It is a fast loop, not a proof — it cannot see a test
# that breaks for a reason not written in its source, so `full` stays the gate
# before anything is pushed.
set -uo pipefail
cd "$(dirname "$0")/.."
mode="${1:-fast}"
base="${2:-HEAD}"

go build ./... || exit 1
go vet ./... || exit 1
[ "$mode" = "vet" ] && exit 0

if [ "$mode" = "full" ]; then
    exec go test -race -short -p 1 -timeout 2400s ./...
fi

run=$(python3 scripts/affected_tests.py "$base")
if [ -z "$run" ]; then
    echo "nothing changed since $base — build and vet only"
    exit 0
fi
pkgs=$(python3 - "$base" <<'PY'
import os, subprocess, sys
root = os.getcwd()
out = subprocess.run(["git", "diff", "--name-only", sys.argv[1], "--"],
                     capture_output=True, text=True).stdout.split()
out += [l[3:].strip() for l in subprocess.run(["git", "status", "--porcelain"],
        capture_output=True, text=True).stdout.splitlines()]
dirs = {os.path.dirname(p) for p in out if p.endswith(".go")}
print(" ".join("./" + d for d in sorted(dirs) if os.path.isdir(d)) or "./...")
PY
)
flags=(-short -count=1)
[ "$mode" = "race" ] && flags=(-race -short -count=1)
echo "==> go test ${flags[*]} -run <affected> $pkgs"
exec go test "${flags[@]}" -run "$run" $pkgs
