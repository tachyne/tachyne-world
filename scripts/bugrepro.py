#!/usr/bin/env python3
"""Render an in-game /bug report as slices and a Go test fixture.

A report carries the cuboid around the reporter, run-length encoded. Printing
it as horizontal slices shows the build at a glance; the fixture rebuilds it
exactly, so a reproduction starts from what the player had rather than from
what you imagined they had.

Run: scripts/bugrepro.py <id> [bugs.json]      (default: stdin)
"""
import json, sys

def load(src):
    if src and src != "-":
        with open(src) as f:
            return json.load(f)
    return json.load(sys.stdin)

def expand(report):
    cells = []
    for run in report["blocks"]:
        n, _, name = run.partition("x")
        cells += [name] * int(n)
    return cells

def main():
    want = int(sys.argv[1]) if len(sys.argv) > 1 else None
    data = load(sys.argv[2] if len(sys.argv) > 2 else None)
    items = data.get("items", data) if isinstance(data, dict) else data
    rep = next((r for r in items if want is None or r["id"] == want), None)
    if rep is None:
        sys.exit(f"no report #{want}")

    ox, oy, oz = rep["origin"]
    sx, sy, sz = rep["size"]
    cells = expand(rep)
    if len(cells) != sx * sy * sz:
        sys.exit(f"region is {len(cells)} cells, header says {sx*sy*sz}")

    print(f"#{rep['id']} {rep['player']} @ {rep['at']}")
    print(f"  {rep['text']}")
    print(f"  dim {rep['dim']} at ({rep['x']:.1f},{rep['y']:.1f},{rep['z']:.1f}) "
          f"yaw {rep['yaw']:.0f} pitch {rep['pitch']:.0f} gamemode {rep['gamemode']}")
    if rep.get("held"):
        print(f"  holding {rep['held']}" + (f" / {rep['offhand']}" if rep.get("offhand") else ""))
    for n in rep.get("nearby", []):
        print(f"  nearby: {n}")

    # A legend keeps the slices readable: one glyph per distinct block.
    glyphs = {}
    for c in cells:
        if c != "air" and c not in glyphs:
            glyphs[c] = "0123456789abcdefghijklmnopqrstuvwxyz"[len(glyphs) % 36]
    print("\n  legend: " + ", ".join(f"{g}={n}" for n, g in glyphs.items()))

    for dy in range(sy):
        layer = [[cells[(dy * sz + dz) * sx + dx] for dx in range(sx)] for dz in range(sz)]
        if all(c == "air" for row in layer for c in row):
            continue
        print(f"\n  y={oy+dy}   (x {ox}..{ox+sx-1} across, z {oz}..{oz+sz-1} down)")
        for row in layer:
            print("    " + "".join("." if c == "air" else glyphs[c] for c in row))

    print("\n--- Go fixture ---")
    print(f"// Rebuilt from bug #{rep['id']}: {rep['text']}")
    print(f"const bugOX, bugOY, bugOZ = {ox}, {oy}, {oz}")
    print("for _, b := range []struct {")
    print("\tdx, dy, dz int\n\tname       string\n}{")
    for dy in range(sy):
        for dz in range(sz):
            for dx in range(sx):
                c = cells[(dy * sz + dz) * sx + dx]
                if c != "air":
                    print(f"\t{{{dx}, {dy}, {dz}, {json.dumps(c)}}},")
    print("} {")
    print("\tw.SetBlock(bugOX+b.dx, bugOY+b.dy, bugOZ+b.dz, stateFromDescription(b.name))")
    print("}")

if __name__ == "__main__":
    main()
