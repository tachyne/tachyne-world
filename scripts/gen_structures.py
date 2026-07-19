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

# Standalone templates to bake (non-jigsaw, placed by code).
TEMPLATES = [
    "igloo/top",
    "igloo/middle",
    "igloo/bottom",
]

# Jigsaw structures to bake: their template pools (+ every template the pools
# reference, collected transitively). Phase 2 proves the assembler on the
# pillager outpost (small); ancient_city / trial_chambers / village follow.
POOL_ROOTS = [
    "pillager_outpost/base_plates",
    "village/plains/town_centers",
    "village/desert/town_centers",
    "village/savanna/town_centers",
    "village/snowy/town_centers",
    "village/taiga/town_centers",
    "ancient_city/city_center",
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


# Village job-site block → tachyne profession index (matches professionNames).
JOBSITE_PROF = {
    "composter": 0, "barrel": 1, "loom": 2, "fletching_table": 3, "lectern": 4,
    "cartography_table": 5, "brewing_stand": 6, "blast_furnace": 7, "grindstone": 8,
    "smithing_table": 9, "stonecutter": 10, "smoker": 11, "cauldron": 12,
}


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
    jigsaws = []
    beds = []      # bed HEAD positions → one villager home each
    jobsites = []  # [x,y,z,profession] job-site blocks → villager professions
    bells = []     # meeting-point bell positions
    for b in d["blocks"]:
        x, y, z = b["pos"]
        blocks.append([x, y, z, b["state"]])
        pname = palette[b["state"]]["name"].split(":", 1)[-1]
        props = palette[b["state"]].get("props", {})
        if pname.endswith("_bed") and props.get("part") == "head":
            beds.append([x, y, z])
        elif pname in JOBSITE_PROF:
            jobsites.append([x, y, z, JOBSITE_PROF[pname]])
        elif pname == "bell":
            bells.append([x, y, z])
        nbt = b.get("nbt")
        if not nbt:
            continue
        bid = nbt.get("id", "")
        if bid == "minecraft:chest":
            chests.append([x, y, z])
        elif bid == "minecraft:mob_spawner":
            sd = nbt.get("SpawnData", {}).get("entity", {})
            spawners.append([x, y, z, sd.get("id", "") if isinstance(sd, dict) else ""])
        elif bid == "minecraft:jigsaw":
            # orientation "{front}_{top}" (FrontAndTop); front/top are directions.
            orient = palette[b["state"]].get("props", {}).get("orientation", "north_up")
            front, top = orient.split("_", 1)
            jigsaws.append({
                "pos": [x, y, z], "front": front, "top": top,
                "joint": nbt.get("joint", "rollable"),
                "name": nbt.get("name", "").split(":", 1)[-1],
                "pool": nbt.get("pool", "empty").split(":", 1)[-1],
                "target": nbt.get("target", "").split(":", 1)[-1],
                "final": nbt.get("final_state", "minecraft:air"),
            })
    t = {"size": d["size"], "palette": palette, "blocks": blocks}
    if chests:
        t["chests"] = chests
    if spawners:
        t["spawners"] = spawners
    if jigsaws:
        t["jigsaws"] = jigsaws
    if beds:
        t["beds"] = beds
    if jobsites:
        t["jobsites"] = jobsites
    if bells:
        t["bells"] = bells
    return t


def elem_location(el):
    """Resolve a pool element to its base template location (or None for
    feature/empty elements). list_pool_element → its first real location."""
    et = el.get("element_type", "")
    if et in ("minecraft:legacy_single_pool_element", "minecraft:single_pool_element"):
        return el.get("location")
    if et == "minecraft:list_pool_element":
        for s in el.get("elements", []):
            loc = elem_location(s)
            if loc:
                return loc
    return None


def load_pool(inner, name):
    """Parse a template_pool JSON → {elements:[{location,weight,projection}], fallback}."""
    j = json.loads(inner.read("data/minecraft/worldgen/template_pool/%s.json" % name))
    out = {"elements": [], "fallback": j.get("fallback", "minecraft:empty").split(":", 1)[-1]}
    for e in j.get("elements", []):
        el = e["element"]
        loc = elem_location(el)
        if not loc:  # feature/empty pool elements — skip for now
            continue
        out["elements"].append({
            "location": loc.split(":", 1)[-1],
            "weight": e.get("weight", 1),
            "projection": el.get("projection", "rigid"),
        })
    return out


def collect(inner):
    """Bake POOL_ROOTS: their pools + every template reachable through jigsaws."""
    pools, templates = {}, {}
    pool_queue = list(POOL_ROOTS)
    seen_pools = set()
    while pool_queue:
        pn = pool_queue.pop()
        if pn in seen_pools or pn == "empty":
            continue
        seen_pools.add(pn)
        try:
            pool = load_pool(inner, pn)
        except KeyError:
            continue
        kept = []
        for el in pool["elements"]:
            loc = el["location"]
            if loc not in templates:
                try:
                    templates[loc] = bake(inner, loc)
                except KeyError:
                    continue  # pool references a template absent from this version — drop it
                for j in templates[loc].get("jigsaws", []):
                    if j["pool"] and j["pool"] != "empty":
                        pool_queue.append(j["pool"])
            kept.append(el)
        pool["elements"] = kept
        pools[pn] = pool
    return pools, templates


def main():
    outer = zipfile.ZipFile(JAR)
    inner = zipfile.ZipFile(io.BytesIO(outer.read("META-INF/versions/1.21.11/server-1.21.11.jar")))
    out = {name: bake(inner, name) for name in TEMPLATES}
    pools, jig_templates = collect(inner)
    out.update(jig_templates)
    os.makedirs(os.path.dirname(OUT), exist_ok=True)
    with open(OUT, "w") as f:
        json.dump({"templates": out, "pools": pools}, f, separators=(",", ":"))
    total = sum(len(t["blocks"]) for t in out.values())
    print("baked %d templates (%d blocks), %d pools -> %s" % (len(out), total, len(pools), os.path.relpath(OUT)))


if __name__ == "__main__":
    main()
