#!/usr/bin/env python3
"""Bake vanilla structure NBT templates into a compact JSON the engine embeds.

Reads the structure templates from the (bundled) 1.21.11 server jar and emits
internal/worldgen/structdata/structures.json. The palette is kept as
{name, properties} pairs — the Go loader resolves each to a canonical state id
and rotates directional properties at stamp time, where tachyne's block-state
tables live. Block-entity chests keep their LootTable ref for loot routing.

Run outside nothing special (all inputs are local):
    python3 scripts/gen_structures.py
"""
import zipfile, io, gzip, os, struct, json

JAR = os.path.expanduser("~/vanilla/server-1.21.11.jar")
OUT = os.path.join(os.path.dirname(__file__), "..", "internal", "worldgen", "structdata", "structures.json")

# Curated set to bake (Phase 1: igloos — the simplest real templates).
TEMPLATES = [
    "igloo/top",
    "igloo/middle",
    "igloo/bottom",
]


class R:
    def __init__(s, b): s.b = b; s.i = 0
    def u1(s): v = s.b[s.i]; s.i += 1; return v
    def i2(s): v = struct.unpack_from(">h", s.b, s.i)[0]; s.i += 2; return v
    def i4(s): v = struct.unpack_from(">i", s.b, s.i)[0]; s.i += 4; return v
    def i8(s): v = struct.unpack_from(">q", s.b, s.i)[0]; s.i += 8; return v
    def f4(s): v = struct.unpack_from(">f", s.b, s.i)[0]; s.i += 4; return v
    def f8(s): v = struct.unpack_from(">d", s.b, s.i)[0]; s.i += 8; return v
    def st(s): n = s.i2(); v = s.b[s.i:s.i + n].decode("utf8"); s.i += n; return v


def payload(r, t):
    if t == 1: return r.u1()
    if t == 2: return r.i2()
    if t == 3: return r.i4()
    if t == 4: return r.i8()
    if t == 5: return r.f4()
    if t == 6: return r.f8()
    if t == 7: n = r.i4(); v = r.b[r.i:r.i + n]; r.i += n; return list(v)
    if t == 8: return r.st()
    if t == 9:
        it = r.u1(); n = r.i4(); return [payload(r, it) for _ in range(n)]
    if t == 10:
        d = {}
        while True:
            tt = r.u1()
            if tt == 0: break
            nm = r.st(); d[nm] = payload(r, tt)
        return d
    if t == 11: n = r.i4(); return [r.i4() for _ in range(n)]
    if t == 12: n = r.i4(); return [r.i8() for _ in range(n)]
    raise Exception("unknown tag %d" % t)


def parse_nbt(raw):
    if raw[:2] == b"\x1f\x8b":
        raw = gzip.decompress(raw)
    r = R(raw); t = r.u1(); r.st()  # root name
    return payload(r, t)


def bake(inner, name):
    d = parse_nbt(inner.read("data/minecraft/structure/%s.nbt" % name))
    palette = []
    for p in d["palette"]:
        entry = {"name": p["Name"]}
        if "Properties" in p:
            entry["props"] = p["Properties"]
        palette.append(entry)
    blocks = []
    chests = []
    spawners = []
    for b in d["blocks"]:
        x, y, z = b["pos"]
        blocks.append([x, y, z, b["state"]])
        nbt = b.get("nbt")
        if not nbt:
            continue
        bid = nbt.get("id", "")
        if bid == "minecraft:chest":
            # The loot table is assigned per-structure in Go (vanilla sets it in
            # piece code, not the template); record the position only.
            chests.append([x, y, z])
        elif bid == "minecraft:mob_spawner":
            ent = ""
            sd = nbt.get("SpawnData", {}).get("entity", {})
            if isinstance(sd, dict):
                ent = sd.get("id", "")
            spawners.append([x, y, z, ent])
    return {
        "size": d["size"],
        "palette": palette,
        "blocks": blocks,
        "chests": chests,
        "spawners": spawners,
    }


def main():
    outer = zipfile.ZipFile(JAR)
    inner = zipfile.ZipFile(io.BytesIO(outer.read("META-INF/versions/1.21.11/server-1.21.11.jar")))
    out = {name: bake(inner, name) for name in TEMPLATES}
    os.makedirs(os.path.dirname(OUT), exist_ok=True)
    with open(OUT, "w") as f:
        json.dump(out, f, separators=(",", ":"))
    total = sum(len(t["blocks"]) for t in out.values())
    print("baked %d templates, %d blocks -> %s" % (len(out), total, os.path.relpath(OUT)))


if __name__ == "__main__":
    main()
