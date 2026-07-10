#!/usr/bin/env bash
# run-local.sh — launch the two-pod sharding testbed on localhost, no k8s.
#
# Two tachyne-world pods (SID 0 = west tile, SID 1 = east tile, seam at block
# x=0) plus one gw-java-770, all local, built from the working tree (each repo
# already has a `replace … => ../tachyne-common` for the in-progress sharding
# code). Connect a 1.21.5 client to localhost:25565, spawn in the west tile, and
# walk EAST past x=0 to cross the seam.
#
# Port map (single-%d patterns so -peer-pattern / TACHYNE_WORLD_PATTERN work):
#   pod 0: attach :25500  peer :25510
#   pod 1: attach :25501  peer :25511
#   attach pattern  127.0.0.1:2550%d  (sid 0 -> 25500, sid 1 -> 25501)
#   peer   pattern  127.0.0.1:2551%d  (sid 0 -> 25510, sid 1 -> 25511)
set -euo pipefail
export 
HERE="$(cd "$(dirname "$0")" && pwd)"
WORLD_REPO="$(cd "$HERE/../.." && pwd)"          # tachyne-world
GW_REPO="$WORLD_REPO/../tachyne-gw-java-770"
TOPO="$HERE/topology.json"
BIN="$(mktemp -d)"
TOKEN="dev"
SEED=42                                           # same seed on both pods → continuous terrain across the seam

echo "building world + gateway (local replace)…"
( cd "$WORLD_REPO" && go build -o "$BIN/world" ./cmd/server )
( cd "$GW_REPO"    && go build -o "$BIN/gw"    ./cmd/gw )

pids=()
cleanup() { echo; echo "stopping…"; kill "${pids[@]}" 2>/dev/null || true; wait 2>/dev/null || true; rm -rf "$BIN"; }
trap cleanup EXIT INT TERM

echo "starting pod 0 (west) attach :25500 peer :25510"
ATTACH_TOKEN=$TOKEN "$BIN/world" -sid 0 -seed $SEED -world "" \
  -attach :25500 -peer-addr :25510 -peer-pattern "127.0.0.1:2551%d" \
  -topology "$TOPO" -debug-borders -spawn "-103,-31" 2>&1 | sed 's/^/[w0] /' &
pids+=($!)

echo "starting pod 1 (east) attach :25501 peer :25511"
ATTACH_TOKEN=$TOKEN "$BIN/world" -sid 1 -seed $SEED -world "" \
  -attach :25501 -peer-addr :25511 -peer-pattern "127.0.0.1:2551%d" \
  -topology "$TOPO" -debug-borders 2>&1 | sed 's/^/[w1] /' &
pids+=($!)

sleep 1  # let the pods bind + form the peer mesh

echo "starting gateway :25565 (login → pod 0; resume pattern → 2550%d)"
TACHYNE_ATTACH_TOKEN=$TOKEN \
TACHYNE_BACKEND=127.0.0.1:25500 \
TACHYNE_WORLD_PATTERN="127.0.0.1:2550%d" \
TACHYNE_LISTEN=:25565 \
  "$BIN/gw" 2>&1 | sed 's/^/[gw] /' &
pids+=($!)

echo
echo "READY. Connect a 1.21.5 client to  localhost:25565"
echo "  • you spawn in the WEST tile (SID 0); a white particle wall marks the seam at x=0"
echo "  • walk EAST across x=0 → handover to pod 1 (watch the [w0]/[w1] logs)"
echo "Ctrl-C to stop."
wait
