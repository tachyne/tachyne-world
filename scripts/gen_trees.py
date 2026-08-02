#!/usr/bin/env python3
"""Generate internal/worldgen/trees_gen.go — every vanilla tree feature's
parameters, straight out of the jar's worldgen/configured_feature JSONs.

The ALGORITHMS live in treeplacer.go / treefoliage.go as ports of the placer
classes; this bakes only the numbers those placers are configured with, so a
species' height range, canopy radius and branch odds are vanilla's rather than
somebody's estimate. Names are the feature names, so `oak`, `fancy_oak`,
`mega_spruce` and so on.

Variants that differ ONLY in decorators (the bees_002 / leaf_litter family)
collapse onto their base feature: they place the same tree.

No network — reads the local server jar.

Run: python3 scripts/gen_trees.py [path-to-server.jar]
"""
import io, json, os, re, sys, zipfile

JAR = sys.argv[1] if len(sys.argv) > 1 else os.path.expanduser("~/vanilla/server-1.21.11.jar")
HERE = os.path.dirname(__file__)
OUT = os.path.join(HERE, "..", "internal", "worldgen", "trees_gen.go")

outer = zipfile.ZipFile(JAR)
inner = [n for n in outer.namelist() if n.startswith("META-INF/versions/") and n.endswith(".jar")]
z = zipfile.ZipFile(io.BytesIO(outer.read(inner[0]))) if inner else outer

TRUNK = {
    "straight_trunk_placer": "TrunkStraight",
    "forking_trunk_placer": "TrunkForking",
    "dark_oak_trunk_placer": "TrunkDarkOak",
    "giant_trunk_placer": "TrunkGiant",
    "mega_jungle_trunk_placer": "TrunkMegaJungle",
    "bending_trunk_placer": "TrunkBending",
    "upwards_branching_trunk_placer": "TrunkUpwardsBranching",
    "cherry_trunk_placer": "TrunkCherry",
    "fancy_trunk_placer": "TrunkFancy",
}
FOLIAGE = {
    "blob_foliage_placer": "FoliageBlob",
    "fancy_foliage_placer": "FoliageFancy",
    "dark_oak_foliage_placer": "FoliageDarkOak",
    "acacia_foliage_placer": "FoliageAcacia",
    "bush_foliage_placer": "FoliageBush",
    "spruce_foliage_placer": "FoliageSpruce",
    "pine_foliage_placer": "FoliagePine",
    "mega_pine_foliage_placer": "FoliageMegaPine",
    "mega_jungle_foliage_placer": "FoliageMegaJungle",
    "jungle_foliage_placer": "FoliageMegaJungle",  # same class in vanilla
    "random_spread_foliage_placer": "FoliageRandomSpread",
    "cherry_foliage_placer": "FoliageCherry",
}


def rng_range(v, default=(0, 0)):
    """An IntProvider is either a bare int or {min_inclusive, max_inclusive}."""
    if v is None:
        return default
    if isinstance(v, int):
        return (v, v)
    if isinstance(v, dict):
        if "min_inclusive" in v:
            return (v["min_inclusive"], v["max_inclusive"])
        if v.get("type") == "minecraft:weighted_list":
            data = [e["data"] for e in v["distribution"]]
            return (min(data), max(data))
        if "value" in v:
            return (v["value"], v["value"])
    raise SystemExit(f"unhandled int provider: {v}")


# name+properties -> state id, from the datagen block report. Vanilla's
# providers name an EXACT state, not a block: oak leaves are placed at
# distance=7 persistent=false, and using the block's base state instead gives a
# state no client renders as ordinary leaves and no decay rule recognises.
REPORT = os.path.expanduser("~/vanilla/reports/1.21.11/blocks.json")
report = json.load(open(REPORT))


def state_of(prov):
    """A simple_state_provider's exact state id; None if unsupported."""
    if prov.get("type") != "minecraft:simple_state_provider":
        return None, None
    st = prov["state"]
    name = st["Name"].removeprefix("minecraft:")
    props = st.get("Properties", {})
    entry = report.get("minecraft:" + name)
    if entry is None:
        return name, None
    for cand in entry["states"]:
        cp = cand.get("properties", {})
        if all(cp.get(k) == v for k, v in props.items()):
            return name, cand["id"]
    return name, None


pre = "data/minecraft/worldgen/configured_feature/"
feats = {}
for n in sorted(z.namelist()):
    if not n.startswith(pre) or not n.endswith(".json"):
        continue
    d = json.loads(z.read(n))
    if d.get("type") != "minecraft:tree":
        continue
    feats[n[len(pre):-len(".json")]] = d["config"]

# Block name -> MIN state id, from the engine's own generated table.
#
# blockids_gen.go holds THREE maps (default, base, max) in that order, so a
# regex over the whole file keeps the LAST match — the MAX state. That silently
# gave every trunk axis=z and laid the whole forest on its side. Slice out
# blockStateBase specifically: the placers add the axis themselves, so they need
# the base, and a log's base state is axis=x.
src = open(os.path.join(HERE, "..", "internal", "worldgen", "blockids_gen.go")).read()
seg = src[src.index("var blockStateBase"):]
seg = seg[:seg.index("\n}")]
base_id = {m.group(1): int(m.group(2)) for m in re.finditer(r'"([a-z0-9_]+)":\s+(\d+),', seg)}

def weighted_leaves(prov):
    """A two-entry weighted foliage provider (azalea's 3:1 mix), heavier
    entry first: [(name, state, weight), ...] or None."""
    if prov.get("type") != "minecraft:weighted_state_provider":
        return None
    ents = prov.get("entries", [])
    if len(ents) != 2:
        return None
    ents = sorted(ents, key=lambda e: -e.get("weight", 1))
    out = []
    for e in ents:
        nm, sid = state_of({"type": "minecraft:simple_state_provider", "state": e["data"]})
        if sid is None:
            return None
        out.append((nm, sid, e.get("weight", 1)))
    return out


rows, skipped = {}, []
for name, c in feats.items():
    log, _ = state_of(c["trunk_provider"])
    leaves, leaf_state = state_of(c["foliage_provider"])
    leaf2_state, leaf2_chance = None, None
    if leaves is None or leaf_state is None:
        mix = weighted_leaves(c["foliage_provider"])
        if mix is not None:
            (leaves, leaf_state, w1), (_, leaf2_state, w2) = mix
            leaf2_chance = w2 / (w1 + w2)
    if log is None or leaves is None or log not in base_id or leaf_state is None:
        skipped.append((name, "non-simple provider" if log is None or leaves is None else "unresolved state"))
        continue
    tp, fp = c["trunk_placer"], c["foliage_placer"]
    tk = TRUNK.get(tp["type"].removeprefix("minecraft:"))
    fk = FOLIAGE.get(fp["type"].removeprefix("minecraft:"))
    if tk is None or fk is None:
        skipped.append((name, "unknown placer"))
        continue

    rmin, rmax = rng_range(fp.get("radius"))
    omin, omax = rng_range(fp.get("offset"))
    f = {
        "Log": base_id[log], "Leaves": leaf_state,
        "Trunk": tk, "BaseHeight": tp["base_height"],
        "HeightRandA": tp["height_rand_a"], "HeightB": tp["height_rand_b"],
        "Foliage": fk, "RadiusMin": rmin, "RadiusMax": rmax,
        "OffsetMin": omin, "OffsetMax": omax,
    }
    if leaf2_state is not None:
        f["Leaves2"] = leaf2_state
        f["Leaves2Chance"] = leaf2_chance
    if isinstance(fp.get("height"), int):
        f["FoliageH"] = fp["height"]
    elif "height" in fp:
        f["FoliageHMin"], f["FoliageHMax"] = rng_range(fp["height"])
    if "foliage_height" in fp:
        f["FoliageHMin"], f["FoliageHMax"] = rng_range(fp["foliage_height"])
    if "crown_height" in fp:
        f["FoliageHMin"], f["FoliageHMax"] = rng_range(fp["crown_height"])
    if "trunk_height" in fp:
        f["TrunkHeightMin"], f["TrunkHeightMax"] = rng_range(fp["trunk_height"])
    if "leaf_placement_attempts" in fp:
        f["LeafPlacementAttempts"] = fp["leaf_placement_attempts"]
    for jkey, gkey in (("hanging_leaves_chance", "HangingLeavesChance"),
                       ("hanging_leaves_extension_chance", "HangingExtChance"),
                       ("wide_bottom_layer_hole_chance", "WideBottomHoleChance"),
                       ("corner_hole_chance", "CornerHoleChance")):
        if jkey in fp:
            f[gkey] = fp[jkey]
    # trunk-placer extras
    if "min_height_for_leaves" in tp:
        f["MinHeightForLeaves"] = tp["min_height_for_leaves"]
    if "bend_length" in tp:
        f["BendLengthMin"], f["BendLengthMax"] = rng_range(tp["bend_length"])
    if "place_branch_per_log_probability" in tp:
        f["BranchProbability"] = tp["place_branch_per_log_probability"]
    if "extra_branch_steps" in tp:
        f["ExtraBranchStepsMin"], f["ExtraBranchStepsMax"] = rng_range(tp["extra_branch_steps"])
    if "extra_branch_length" in tp:
        f["ExtraBranchLenMin"], f["ExtraBranchLenMax"] = rng_range(tp["extra_branch_length"])
    if "branch_count" in tp:
        f["BranchCountMin"], f["BranchCountMax"] = rng_range(tp["branch_count"])
    if "branch_horizontal_length" in tp:
        f["BranchHorizMin"], f["BranchHorizMax"] = rng_range(tp["branch_horizontal_length"])
    if "branch_start_offset_from_top" in tp:
        f["BranchStartMin"], f["BranchStartMax"] = rng_range(tp["branch_start_offset_from_top"])
    if "branch_end_offset_from_top" in tp:
        f["BranchEndMin"], f["BranchEndMax"] = rng_range(tp["branch_end_offset_from_top"])

    # minimum_size — the clearance gate. Without it a tree grows straight
    # through a roof, so this is not optional decoration.
    ms = c["minimum_size"]
    f["SizeLimit"] = ms.get("limit", 0)
    f["SizeLower"] = ms.get("lower_size", 0)
    f["SizeUpper"] = ms.get("upper_size", 0)
    if ms["type"] == "minecraft:three_layers_feature_size":
        f["SizeThreeLayer"] = "true"
        f["SizeMiddle"] = ms.get("middle_size", 0)
        f["SizeUpperLimit"] = ms.get("upper_limit", 0)
    if "min_clipped_height" in ms:
        f["MinClippedHeight"] = ms["min_clipped_height"]
    # dirt_provider / force_dirt — what setDirtAt writes under the trunk.
    dirt_name, dirt_state = state_of(c["dirt_provider"])
    if dirt_state is None:
        raise SystemExit(f"{name}: dirt provider not a resolvable simple state: {c['dirt_provider']}")
    f["DirtState"] = dirt_state
    if c.get("force_dirt"):
        f["ForceDirt"] = "true"
    # Ported decorators ride the feature — jungle_tree and jungle_tree_no_vine
    # place the same trunk and differ ONLY here, so these fields are part of
    # the identity below.
    for dec in c.get("decorators", []):
        dt = dec["type"].removeprefix("minecraft:")
        if dt == "trunk_vine":
            f["TrunkVine"] = "true"
        elif dt == "leave_vine":
            f["LeaveVineProb"] = dec["probability"]
        elif dt == "cocoa":
            f["CocoaProb"] = dec["probability"]
        elif dt == "creaking_heart":
            f["HeartProb"] = dec["probability"]
        elif dt == "place_on_ground":
            # Leaf litter: the provider must be the uniform facing x segment
            # weighted list the Go side draws with a single roll.
            entries = dec["block_state_provider"]["entries"]
            segs = set()
            for e in entries:
                if e["data"]["Name"] != "minecraft:leaf_litter" or e.get("weight", 1) != 1:
                    raise SystemExit(f"{name}: place_on_ground provider changed: {e}")
                segs.add(int(e["data"]["Properties"]["segment_amount"]))
            if segs != set(range(1, max(segs) + 1)) or len(entries) != 4 * max(segs):
                raise SystemExit(f"{name}: litter entries are not uniform facings x 1..{max(segs)}")
            passes = f.setdefault("_litter", [])
            passes.append((dec.get("tries", 128), dec.get("radius", 2),
                           dec.get("height", 1), max(segs)))
        elif dt == "pale_moss":
            f["PaleMossLeaves"] = dec["leaves_probability"]
            f["PaleMossTrunk"] = dec["trunk_probability"]
            f["PaleMossGround"] = dec["ground_probability"]
        elif dt == "alter_ground":
            # 1.21.11's decorator hardcodes the #dirt ground check in code and
            # carries a plain podzol provider in data. Anything else here means
            # the data moved under the port — fail loudly, don't guess.
            p = dec["provider"]
            if p["type"] != "minecraft:simple_state_provider" or \
                    p["state"]["Name"] != "minecraft:podzol":
                raise SystemExit(f"{name}: alter_ground provider changed: {p}")
            f["AlterGround"] = "true"
        elif dt == "attached_to_leaves":
            # Only the mangroves use this in 1.21.11, and always in the same
            # shape: downward, hanging propagule, age uniform 0-4. The Go side
            # bakes that shape in, so refuse any other.
            bp = dec["block_provider"]
            if dec["directions"] != ["down"] or \
                    bp["type"] != "minecraft:randomized_int_state_provider" or \
                    bp["property"] != "age" or \
                    bp["source"]["state"]["Name"] != "minecraft:mangrove_propagule" or \
                    bp["values"] != {"type": "minecraft:uniform",
                                     "min_inclusive": 0, "max_inclusive": 4}:
                raise SystemExit(f"{name}: attached_to_leaves shape changed: {dec}")
            f["PropaguleProb"] = dec["probability"]
            f["PropaguleExclXZ"] = dec["exclusion_radius_xz"]
            f["PropaguleExclY"] = dec["exclusion_radius_y"]
            f["PropaguleEmpty"] = dec["required_empty_blocks"]
    # root_placer — the mangrove's stilted roots.
    rp = c.get("root_placer")
    if rp is not None:
        if rp["type"] != "minecraft:mangrove_root_placer":
            raise SystemExit(f"{name}: unknown root placer {rp['type']}")
        mrp = rp["mangrove_root_placement"]
        if mrp["muddy_roots_in"] != ["minecraft:mud", "minecraft:muddy_mangrove_roots"] or \
                mrp["can_grow_through"] != "#minecraft:mangrove_roots_can_grow_through":
            raise SystemExit(f"{name}: mangrove root placement changed: {mrp}")
        _, root_state = state_of(rp["root_provider"])
        _, muddy_state = state_of(mrp["muddy_roots_provider"])
        arp = rp["above_root_placement"]
        _, above_state = state_of(arp["above_root_provider"])
        if None in (root_state, muddy_state, above_state):
            raise SystemExit(f"{name}: root provider states unresolved")
        f["RootTrunkOffMin"], f["RootTrunkOffMax"] = rng_range(rp["trunk_offset_y"])
        f["RootMaxLength"] = mrp["max_root_length"]
        f["RootMaxWidth"] = mrp["max_root_width"]
        f["RootSkewChance"] = mrp["random_skew_chance"]
        f["RootState"] = root_state
        f["RootMuddyState"] = muddy_state
        f["AboveRootChance"] = arp["above_root_placement_chance"]
        f["AboveRootState"] = above_state
    if "_litter" in f:
        inner = ", ".join("{%d, %d, %d, %d}" % p for p in f.pop("_litter"))
        f["LitterPasses"] = f"[]LitterPass{{{inner}}}"
    rows[name] = f

# Collapse variants that place the identical tree INCLUDING its ported
# decorators; bees/leaf-litter variants still collapse (their decorators are
# not ported yet), but the vine/cocoa-bearing jungle tree stays distinct from
# its bare sapling-grown twin.
canon = {}
# Prefer the bees-free name as the canonical one — the biome wiring selects
# "oak_leaf_litter", and the bees variants are the derived twins until the
# beehive decorator exists.
for name in sorted(rows, key=lambda n: ("bees" in n, n)):
    key = json.dumps(rows[name], sort_keys=True)
    canon.setdefault(key, name)
aliases = {n: canon[json.dumps(rows[n], sort_keys=True)] for n in rows}
base_rows = {n: r for n, r in rows.items() if aliases[n] == n}

ORDER = ["Log", "Leaves", "Leaves2", "Leaves2Chance",
         "Trunk", "BaseHeight", "HeightRandA", "HeightB",
         "Foliage", "RadiusMin", "RadiusMax", "OffsetMin", "OffsetMax",
         "FoliageH", "FoliageHMin", "FoliageHMax", "MinHeightForLeaves",
         "BendLengthMin", "BendLengthMax", "BranchProbability",
         "ExtraBranchStepsMin", "ExtraBranchStepsMax", "ExtraBranchLenMin",
         "ExtraBranchLenMax", "BranchCountMin", "BranchCountMax",
         "BranchHorizMin", "BranchHorizMax", "BranchStartMin", "BranchStartMax",
         "BranchEndMin", "BranchEndMax", "HangingLeavesChance",
         "HangingExtChance", "WideBottomHoleChance", "CornerHoleChance",
         "TrunkHeightMin", "TrunkHeightMax", "LeafPlacementAttempts",
         "SizeThreeLayer", "SizeLimit", "SizeLower", "SizeMiddle",
         "SizeUpperLimit", "SizeUpper", "MinClippedHeight",
         "TrunkVine", "LeaveVineProb", "CocoaProb", "HeartProb", "AlterGround",
         "PropaguleProb", "PropaguleExclXZ", "PropaguleExclY", "PropaguleEmpty",
         "PaleMossLeaves", "PaleMossTrunk", "PaleMossGround",
         "DirtState", "ForceDirt", "LitterPasses",
         "RootTrunkOffMin", "RootTrunkOffMax", "RootMaxLength", "RootMaxWidth",
         "RootSkewChance", "RootState", "RootMuddyState",
         "AboveRootChance", "AboveRootState"]

L = ["// Code generated by scripts/gen_trees.py. DO NOT EDIT.",
     "",
     "package worldgen",
     "",
     "// TreeFeatures is every vanilla tree, by its feature name, carrying the",
     "// parameters its trunk and foliage placers are configured with. The",
     "// algorithms are in treeplacer.go / treefoliage.go; only the numbers are",
     "// generated, so a species' proportions are vanilla's and not an estimate.",
     "var TreeFeatures = map[string]*TreeConfig{"]
for name in sorted(base_rows):
    f = base_rows[name]
    fields = ", ".join(f"{k}: {f[k]}" for k in ORDER if k in f)
    L.append(f'\t"{name}": {{{fields}}},')
L += ["}", ""]
if len(aliases) > len(base_rows):
    L += ["// treeAliases are features that place an identical tree and differ only in",
          "// decorators (bee nests, leaf litter).",
          "var treeAliases = map[string]string{"]
    for n in sorted(aliases):
        if aliases[n] != n:
            L.append(f'\t"{n}": "{aliases[n]}",')
    L += ["}", ""]

with open(OUT, "w") as fh:
    fh.write("\n".join(L))
print(f"wrote {OUT}: {len(base_rows)} trees ({len(aliases)} features), {len(skipped)} skipped")
for n, why in skipped:
    print(f"  skip {n}: {why}")
