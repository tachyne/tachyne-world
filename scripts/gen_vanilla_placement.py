#!/usr/bin/env python3
"""Bake 26.3's placement data for the vanilla world generator.

Writes internal/worldgen/vanilladata/placement.json (go:embed by
vanillaplace.go): the structure sets and structures (biome tags resolved to
biome lists), every biome's per-step placed-feature lists, the placed
features (feature + placement modifiers, as the data has them), the
configured features, the block and fluid tags the placements and ore
targets name (resolved), the ore-vein noises, and the order of each
dimension's possible biomes.

The possible-biome orders are not in the data: they are the order the
dimension's biome source first lists each biome (MultiNoiseBiomeSource's
parameter list, as OverworldBiomeBuilder emits it; TheEndBiomeSource's
fixed list), which decides the feature index every decoration seed is
salted with. They were read off a running 26.3 (the Place1 oracle) and are
kept here as constants.

    python3 scripts/gen_vanilla_placement.py

Stdlib only.
"""
import gzip
import io
import json
import os
import struct
import sys

sys.path.insert(0, os.path.dirname(__file__))
import canon  # noqa: E402

VERSION = "26.3"
OUT = os.path.join(os.path.dirname(__file__), "..", "internal", "worldgen", "vanilladata", "placement.json")

OVERWORLD_BIOMES = """mushroom_fields deep_frozen_ocean frozen_ocean deep_cold_ocean cold_ocean deep_ocean ocean
deep_lukewarm_ocean lukewarm_ocean warm_ocean stony_shore swamp mangrove_swamp snowy_slopes
snowy_plains snowy_beach windswept_gravelly_hills grove windswept_hills snowy_taiga
windswept_forest taiga plains meadow beach forest old_growth_spruce_taiga flower_forest
birch_forest dark_forest pale_garden savanna_plateau savanna jungle badlands desert
wooded_badlands jagged_peaks stony_peaks frozen_river river ice_spikes dappled_forest
old_growth_pine_taiga sunflower_plains old_growth_birch_forest sparse_jungle bamboo_jungle
eroded_badlands windswept_savanna cherry_grove frozen_peaks dripstone_caves lush_caves
sulfur_caves deep_dark""".split()

NETHER_BIOMES = "nether_wastes soul_sand_valley crimson_forest warped_forest basalt_deltas".split()
END_BIOMES = "the_end end_highlands end_midlands small_end_islands end_barrens".split()

ORE_NOISES = ["ore_veininess", "ore_vein_a", "ore_vein_b", "ore_gap"]

# Block tags the code asks directly (would_survive's canSurvive rules), on
# top of those the data names.
CODE_BLOCK_TAGS = ["supports_vegetation", "supports_mangrove_propagule", "supports_cactus",
                   "supports_sugar_cane", "supports_sugar_cane_adjacently"]

# Biome tags the structure code asks directly.
CODE_BIOME_TAGS = ["required_ocean_monument_surrounding"]


def read_nbt(data):
    """A gzipped NBT compound as Python values (enough of the format for
    structure templates)."""
    f = io.BytesIO(gzip.decompress(data))

    def rd(fmt):
        return struct.unpack(">" + fmt, f.read(struct.calcsize(">" + fmt)))[0]

    def string():
        n = rd("H")
        return f.read(n).decode("utf-8", "replace")

    def payload(t):
        if t == 1:
            return rd("b")
        if t == 2:
            return rd("h")
        if t == 3:
            return rd("i")
        if t == 4:
            return rd("q")
        if t == 5:
            return rd("f")
        if t == 6:
            return rd("d")
        if t == 7:
            return f.read(rd("i"))
        if t == 8:
            return string()
        if t == 9:
            et, n = rd("b"), rd("i")
            return [payload(et) for _ in range(n)]
        if t == 10:
            out = {}
            while True:
                tt = rd("b")
                if tt == 0:
                    return out
                k = string()
                out[k] = payload(tt)
        if t == 11:
            return [rd("i") for _ in range(rd("i"))]
        if t == 12:
            return [rd("q") for _ in range(rd("i"))]
        raise ValueError(f"nbt tag {t}")
    t = rd("b")
    string()
    return payload(t)


def short(name):
    return name[len("minecraft:"):] if name.startswith("minecraft:") else name


def main():
    z = canon.inner_jar(VERSION)
    names = z.namelist()

    def load(kind):
        pre = f"data/minecraft/worldgen/{kind}/"
        out = {}
        for n in names:
            if n.startswith(pre) and n.endswith(".json"):
                out[n[len(pre):-5]] = json.loads(z.read(n))
        return out

    def tagfiles(kind):
        pre = f"data/minecraft/tags/{kind}/"
        out = {}
        for n in names:
            if n.startswith(pre) and n.endswith(".json"):
                out[n[len(pre):-5]] = json.loads(z.read(n))["values"]
        return out

    def resolver(tags):
        memo = {}

        def resolve(tag):
            if tag in memo:
                return memo[tag]
            vals = []
            for v in tags[tag]:
                if isinstance(v, dict):
                    v = v["id"]
                if v.startswith("#"):
                    vals += resolve(short(v[1:]))
                else:
                    vals.append(short(v))
            memo[tag] = list(dict.fromkeys(vals))
            return memo[tag]
        return resolve

    btags = resolver(tagfiles("worldgen/biome"))
    blocktags = resolver(tagfiles("block"))
    fluidtags = resolver(tagfiles("fluid"))

    def biome_list(v):
        if isinstance(v, str):
            return btags(short(v[1:])) if v.startswith("#") else [short(v)]
        return [short(x) for x in v]

    used_block_tags, used_fluid_tags = set(CODE_BLOCK_TAGS), set()

    def scan_tags(o):
        if isinstance(o, dict):
            t = o.get("type") or o.get("predicate_type")
            if t in ("minecraft:matching_block_tag", "minecraft:tag_match") and "tag" in o:
                used_block_tags.add(short(o["tag"]))
            if t == "minecraft:matching_fluids" and isinstance(o.get("fluids"), str) and o["fluids"].startswith("#"):
                used_fluid_tags.add(short(o["fluids"][1:]))
            for k, v in o.items():
                if k in ("blocks", "can_place_on", "can_replace", "replaceable") and isinstance(v, str) and v.startswith("#"):
                    used_block_tags.add(short(v[1:]))
                scan_tags(v)
        elif isinstance(o, list):
            for v in o:
                scan_tags(v)

    sets = {}
    for name, s in sorted(load("structure_set").items()):
        p = dict(s["placement"])
        if "preferred_biomes" in p:
            p["preferred_biomes"] = biome_list(p["preferred_biomes"])
        if "exclusion_zone" in p:
            p["exclusion_zone"] = {"other_set": short(p["exclusion_zone"]["other_set"]),
                                   "chunk_count": p["exclusion_zone"]["chunk_count"]}
        p["type"] = short(p["type"])
        sets[name] = {"placement": p, "structures": [[short(e["structure"]), e["weight"]] for e in s["structures"]]}

    structs = {}
    for name, s in sorted(load("structure").items()):
        d = {k: v for k, v in s.items() if k not in ("spawn_overrides",)}
        d["type"] = short(d["type"])
        d["biomes"] = biome_list(s["biomes"])
        structs[name] = d

    biomes = {}
    for name, b in sorted(load("biome").items()):
        biomes[name] = [[short(f) for f in step] for step in b.get("features", [])]

    placed = {}
    for name, p in sorted(load("placed_feature").items()):
        f = p["feature"]
        placed[name] = {"feature": short(f) if isinstance(f, str) else f, "placement": p["placement"]}
        scan_tags(p["placement"])

    features = load("feature")
    for f in features.values():
        scan_tags(f)
    providers = load("block_state_provider")
    for f in providers.values():
        scan_tags(f)

    noises = {n: load("noise")[n] for n in ORE_NOISES}

    # Jigsaw starts: each start pool's elements (weight, size of the
    # template and where its named start jigsaws sit), which is all a
    # structure's start position needs (JigsawPlacement.addPieces).
    pools = load("template_pool")
    start_pools = {}
    for name, st in structs.items():
        if st["type"] != "jigsaw":
            continue
        pool = short(st["start_pool"])
        anchor = short(st.get("start_jigsaw_name", ""))
        elems = []
        for e in pools[pool]["elements"]:
            el = e["element"]
            et = short(el["element_type"])
            out = {"type": et, "weight": e["weight"]}
            if et in ("single_pool_element", "legacy_single_pool_element"):
                loc = short(el["location"])
                nbt = read_nbt(z.read(f"data/minecraft/structure/{loc}.nbt"))
                out["location"] = loc
                out["size"] = nbt["size"]
                if anchor:
                    out["anchors"] = [b["pos"] for b in nbt["blocks"]
                                      if "nbt" in b and short(b["nbt"].get("name", "")) == anchor
                                      and nbt["palette"][b["state"]].get("id", nbt["palette"][b["state"]].get("Name")) == "minecraft:jigsaw"]
            elif et != "empty_pool_element":
                raise SystemExit(f"start pool {pool}: element type {et} not handled")
            elems.append(out)
        start_pools[pool] = elems

    # Template sizes code-placed structures pick between before their start
    # position is known (RuinedPortalStructure's portals).
    sized = [f"ruined_portal/portal_{i}" for i in range(1, 11)] + [f"ruined_portal/giant_portal_{i}" for i in range(1, 4)]
    template_sizes = {loc: read_nbt(z.read(f"data/minecraft/structure/{loc}.nbt"))["size"] for loc in sized}

    for b in OVERWORLD_BIOMES + NETHER_BIOMES + END_BIOMES:
        if b not in biomes:
            raise SystemExit(f"possible biome {b} has no biome file")

    out = {
        "version": VERSION,
        "structure_sets": sets,
        "structures": structs,
        "biomes": biomes,
        "placed_features": placed,
        "features": dict(sorted(features.items())),
        "block_tags": {t: blocktags(t) for t in sorted(used_block_tags)},
        "fluid_tags": {t: fluidtags(t) for t in sorted(used_fluid_tags)},
        "biome_tags": {t: btags(t) for t in CODE_BIOME_TAGS},
        "noises": noises,
        "start_pools": start_pools,
        "template_sizes": template_sizes,
        "state_providers": dict(sorted(providers.items())),
        "possible_biomes": {"overworld": OVERWORLD_BIOMES, "nether": NETHER_BIOMES, "end": END_BIOMES},
    }
    os.makedirs(os.path.dirname(OUT), exist_ok=True)
    with open(OUT, "w") as fh:
        json.dump(out, fh, separators=(",", ":"), sort_keys=False)
        fh.write("\n")
    print(f"{OUT}: {len(sets)} structure sets, {len(structs)} structures, {len(biomes)} biomes, "
          f"{len(placed)} placed features, {len(features)} features, {len(used_block_tags)} block tags")


if __name__ == "__main__":
    main()
