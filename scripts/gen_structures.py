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
SHIPWRECK = ["shipwreck/%s%s" % (v, d) for v in (
    "with_mast", "sideways_full", "rightsideup_full",
    "rightsideup_fronthalf", "rightsideup_backhalf",
    "sideways_fronthalf", "sideways_backhalf",
    "upsidedown_full", "upsidedown_fronthalf", "upsidedown_backhalf",
) for d in ("", "_degraded")]
RUINED_PORTAL = ["ruined_portal/portal_%d" % i for i in range(1, 11)] + \
    ["ruined_portal/giant_portal_%d" % i for i in range(1, 4)]
import zipfile as _zf, io as _io, os as _os
def _mansion_names():
    jar=_os.path.expanduser("~/vanilla/server-1.21.11.jar")
    z=_zf.ZipFile(jar); inner=[n for n in z.namelist() if n.endswith("server-1.21.11.jar") and "versions" in n]
    zz=_zf.ZipFile(_io.BytesIO(z.read(inner[0]))) if inner else z
    return sorted(n.split("/structure/")[1][:-4] for n in zz.namelist()
                  if "/structure/woodland_mansion/" in n and n.endswith(".nbt"))
MANSION = _mansion_names()
END_CITY = ["end_city/" + n for n in (
    "base_floor", "base_roof", "bridge_end", "bridge_gentle_stairs", "bridge_piece", "bridge_steep_stairs",
    "fat_tower_base", "fat_tower_middle", "fat_tower_top", "second_floor_1", "second_floor_2", "second_roof",
    "ship", "third_floor_1", "third_floor_2", "third_roof", "tower_base", "tower_floor", "tower_piece", "tower_top")]
TEMPLATES = [
    "igloo/top",
    "igloo/middle",
    "igloo/bottom",
] + SHIPWRECK + RUINED_PORTAL + MANSION + END_CITY

# structure_block DATA-marker metadata → the vanilla loot table for the chest
# one block below it (shipwreck supply/map/treasure chests).
MOB_MARKER = {"Mage": 0, "Warrior": 1, "Group of Allays": 2}
# End city DATA markers → entities the server seeds ("Sentry" = a shulker,
# "Elytra" = the ship's item frame holding an elytra); baked into "mobs".
ENTITY_MARKER = {"Sentry": "shulker", "Elytra": "elytra_frame"}
MARKER_LOOT = {
    "supply_chest": "chests/shipwreck_supply",
    "map_chest": "chests/shipwreck_map",
    "treasure_chest": "chests/shipwreck_treasure",
    "ChestWest": "chests/woodland_mansion",
    "ChestEast": "chests/woodland_mansion",
    "ChestSouth": "chests/woodland_mansion",
    "ChestNorth": "chests/woodland_mansion",
    "Chest": "chests/end_city_treasure",
}

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
    "trial_chambers/chamber/end",
    "bastion/starts",
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
    # A template carries either one "palette" or several random-variant "palettes"
    # (degraded block swaps); take the first — the degraded forms are separate
    # templates, so palette[0] is the canonical set.
    pal_src = d.get("palette") or d["palettes"][0]
    palette = []
    for p in pal_src:
        entry = {"name": p["Name"]}
        if "Properties" in p:
            entry["props"] = p["Properties"]
        palette.append(entry)
    # structure_block DATA markers set the loot table of the chest ONE BELOW them
    # (vanilla handleDataMarker on blockPos.below): "supply_chest"/"map_chest"/
    # "treasure_chest" → the shipwreck tables.
    markers = {}
    for b in d["blocks"]:
        nbt = b.get("nbt")
        if nbt and nbt.get("id") == "minecraft:structure_block" and nbt.get("metadata"):
            markers[tuple(b["pos"])] = nbt["metadata"]
    blocks = []
    chests = []
    chestloot = []
    mobspawns = []  # [x,y,z,type] illager markers (mansion): 0=evoker 1=vindicator 2=allay
    entity_markers = []  # DATA markers that stand for an entity (end city sentries, the elytra frame)
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
            # Loot table: the chest's own LootTable (ruined portal), else the
            # DATA marker one above it (shipwreck supply/map/treasure).
            lt = nbt.get("LootTable", "")
            if lt:
                chestloot.append(lt.split(":", 1)[-1])
            else:
                chestloot.append(MARKER_LOOT.get(markers.get((x, y + 1, z), ""), ""))
        elif bid == "minecraft:structure_block" and nbt.get("metadata", "").startswith("Chest"):
            # Woodland mansion: a "Chest*" DATA marker BECOMES a chest at its own
            # position (vanilla handleDataMarker), with the mansion loot table.
            chests.append([x, y, z])
            chestloot.append(MARKER_LOOT.get(nbt["metadata"], "chests/woodland_mansion"))
        elif bid == "minecraft:structure_block" and nbt.get("metadata", "") in MOB_MARKER:
            # Woodland mansion illager markers: the server seeds the mob here.
            mobspawns.append([x, y, z, MOB_MARKER[nbt["metadata"]]])
        elif bid == "minecraft:structure_block" and nbt.get("metadata", "") in ENTITY_MARKER:
            entity_markers.append({"pos": [x, y, z], "type": ENTITY_MARKER[nbt["metadata"]]})
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
    # Entities embedded in the template (the bastion "mobs" pieces are one
    # entity each): the server seeds them when a player first arrives.
    mobs = list(entity_markers)
    for e in d.get("entities", []):
        eid = (e.get("nbt") or {}).get("id", "")
        bp = e.get("blockPos")
        if eid and bp:
            mobs.append({"pos": [bp[0], bp[1], bp[2]], "type": eid.split(":", 1)[-1]})
    t = {"size": d["size"], "palette": palette, "blocks": blocks}
    if mobs:
        t["mobs"] = mobs
    if chests:
        t["chests"] = chests
        if any(chestloot):
            t["chestloot"] = chestloot
    if mobspawns:
        t["mobspawns"] = mobspawns
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
        entry = {
            "location": loc.split(":", 1)[-1],
            "weight": e.get("weight", 1),
            "projection": el.get("projection", "rigid"),
        }
        if isinstance(el.get("processors"), str):
            entry["processors"] = strip_ns(el["processors"])
        out["elements"].append(entry)
    return out


# Processor pieces the baker does not model (reported once per run). The
# bastion pools use only what IS modelled; trial chambers and villages were
# assembled without processors before and still are.
skipped = set()


def load_processors(inner, name):
    """Parse a processor_list into flat rules the Go side applies per block:
    {in: block name ("" = any), p: probability, out: {name, props}, pos: {...}}.
    Only the rule processor with random_block_match / always_true inputs is
    modelled — that is every processor the bastion pools use."""
    j = json.loads(inner.read("data/minecraft/worldgen/processor_list/%s.json" % name))
    rules = []
    for p in j.get("processors", []):
        if p.get("processor_type") != "minecraft:rule":
            skipped.add("%s: %s" % (name, p.get("processor_type")))
            continue
        for r in p.get("rules", []):
            ip = r["input_predicate"]
            rule = {}
            if ip["predicate_type"] == "minecraft:random_block_match":
                rule["in"] = strip_ns(ip["block"])
                rule["p"] = ip.get("probability", 1.0)
            elif ip["predicate_type"] == "minecraft:block_match":
                rule["in"] = strip_ns(ip["block"])
                rule["p"] = 1.0
            elif ip["predicate_type"] == "minecraft:always_true":
                rule["in"] = ""
                rule["p"] = 1.0
            else:
                skipped.add("%s: input %s" % (name, ip["predicate_type"]))
                continue
            if r["location_predicate"]["predicate_type"] != "minecraft:always_true":
                skipped.add("%s: location %s" % (name, r["location_predicate"]["predicate_type"]))
                continue
            out = {"name": r["output_state"]["Name"]}
            if r["output_state"].get("Properties"):
                out["props"] = r["output_state"]["Properties"]
            rule["out"] = out
            pp = r.get("position_predicate")
            if pp:
                if pp["predicate_type"] != "minecraft:axis_aligned_linear_pos":
                    skipped.add("%s: position %s" % (name, pp["predicate_type"]))
                    continue
                rule["pos"] = {"axis": pp.get("axis", "y"), "min_chance": pp.get("min_chance", 0.0),
                               "max_chance": pp.get("max_chance", 0.0), "min_dist": pp.get("min_dist", 0),
                               "max_dist": pp.get("max_dist", 0)}
            rules.append(rule)
    return rules


def strip_ns(s):
    return s.split(":", 1)[-1] if s else s


# Structures whose jigsaws use POOL ALIASES. A trial chamber's spawner
# connectors deliberately name pools that do not exist as files
# (trial_chambers/spawner/contents/melee and friends); the structure remaps
# each alias to a real pool ONCE PER INSTANCE, which is what makes one chamber
# consistently zombie-themed and the next husk-themed. Without this the
# connectors resolve to nothing and a chamber generates no spawners at all.
ALIAS_STRUCTURES = ["trial_chambers"]


def load_aliases(inner, name):
    """Parse a structure's pool_aliases into a flat list the Go side can walk.

    Two shapes, both reduced to "pick one option, then apply its bindings":
      random        -> one alias, weighted targets
      random_group  -> weighted groups, each binding SEVERAL aliases together
    A group is what keeps a chamber's ranged and slow_ranged spawners matching.
    """
    try:
        j = json.loads(inner.read("data/minecraft/worldgen/structure/%s.json" % name))
    except KeyError:
        return []
    out = []
    for entry in j.get("pool_aliases", []):
        et = strip_ns(entry.get("type", ""))
        if et == "random":
            out.append({"options": [
                {"weight": t.get("weight", 1),
                 "bind": {strip_ns(entry["alias"]): strip_ns(t["data"])}}
                for t in entry.get("targets", [])
            ]})
        elif et == "random_group":
            opts = []
            for g in entry.get("groups", []):
                bind = {}
                for d in g.get("data", []):
                    if strip_ns(d.get("type", "")) == "direct":
                        bind[strip_ns(d["alias"])] = strip_ns(d["target"])
                if bind:
                    opts.append({"weight": g.get("weight", 1), "bind": bind})
            if opts:
                out.append({"options": opts})
        elif et == "direct":
            out.append({"options": [
                {"weight": 1, "bind": {strip_ns(entry["alias"]): strip_ns(entry["target"])}}
            ]})
    return out


def collect(inner):
    """Bake POOL_ROOTS: their pools + every template reachable through jigsaws."""
    pools, templates = {}, {}
    pool_queue = list(POOL_ROOTS)
    # Alias TARGETS are never reached by walking jigsaws (the jigsaw names the
    # alias, not the target), so they have to be seeded explicitly or their
    # templates — the actual spawner pieces — are simply never baked.
    aliases = {}
    for sname in ALIAS_STRUCTURES:
        aliases[sname] = load_aliases(inner, sname)
        for entry in aliases[sname]:
            for opt in entry["options"]:
                pool_queue.extend(opt["bind"].values())
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
    processors = {}
    for pool in pools.values():
        for el in pool["elements"]:
            pr = el.get("processors")
            if pr and pr not in processors:
                processors[pr] = load_processors(inner, pr)
    return pools, templates, aliases, processors


def main():
    outer = zipfile.ZipFile(JAR)
    inner = zipfile.ZipFile(io.BytesIO(outer.read("META-INF/versions/1.21.11/server-1.21.11.jar")))
    out = {name: bake(inner, name) for name in TEMPLATES}
    pools, jig_templates, aliases, processors = collect(inner)
    out.update(jig_templates)
    os.makedirs(os.path.dirname(OUT), exist_ok=True)
    with open(OUT, "w") as f:
        json.dump({"templates": out, "pools": pools, "aliases": aliases, "processors": processors}, f, separators=(",", ":"))
    total = sum(len(t["blocks"]) for t in out.values())
    nal = sum(len(v) for v in aliases.values())
    for sk in sorted(skipped):
        print("  skipped processor piece:", sk)
    print("baked %d templates (%d blocks), %d pools, %d alias entries, %d processor lists -> %s" % (
        len(out), total, len(pools), nal, len(processors), os.path.relpath(OUT)))


if __name__ == "__main__":
    main()
