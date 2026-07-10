#!/usr/bin/env python3
"""Generate internal/worldgen/flammable_gen.go — the fire flammability table.

Facts matching vanilla 1.21.5 behavior (FireBlock.bootStrap's
setFlammable(block, igniteOdds, burnOdds) registrations; formulas reimplemented
in Go, table transcribed — the odds are unchanged through 1.21.11) and mapped to
tachyne block-state id ranges via minecraft-data (canonical version's blocks.json).

Emits a sorted, non-overlapping range table + Flammability(state) lookup,
mirroring light_emission_gen.py. Run OUTSIDE the sandbox (needs network)."""
import json
import os
import urllib.request

URL = "https://raw.githubusercontent.com/PrismarineJS/minecraft-data/master/data/pc/1.21.11/blocks.json"
OUT = os.path.join(os.path.dirname(__file__), "..", "internal", "worldgen", "flammable_gen.go")

# name -> (igniteOdds, burnOdds), matching vanilla 1.21.5 FireBlock.
FLAMMABLE = {
"oak_planks":(5,20),"spruce_planks":(5,20),"birch_planks":(5,20),"jungle_planks":(5,20),
"acacia_planks":(5,20),"cherry_planks":(5,20),"dark_oak_planks":(5,20),"pale_oak_planks":(5,20),
"mangrove_planks":(5,20),"bamboo_planks":(5,20),"bamboo_mosaic":(5,20),
"oak_slab":(5,20),"spruce_slab":(5,20),"birch_slab":(5,20),"jungle_slab":(5,20),"acacia_slab":(5,20),
"cherry_slab":(5,20),"dark_oak_slab":(5,20),"pale_oak_slab":(5,20),"mangrove_slab":(5,20),
"bamboo_slab":(5,20),"bamboo_mosaic_slab":(5,20),
"oak_fence_gate":(5,20),"spruce_fence_gate":(5,20),"birch_fence_gate":(5,20),"jungle_fence_gate":(5,20),
"acacia_fence_gate":(5,20),"cherry_fence_gate":(5,20),"dark_oak_fence_gate":(5,20),"pale_oak_fence_gate":(5,20),
"mangrove_fence_gate":(5,20),"bamboo_fence_gate":(5,20),
"oak_fence":(5,20),"spruce_fence":(5,20),"birch_fence":(5,20),"jungle_fence":(5,20),"acacia_fence":(5,20),
"cherry_fence":(5,20),"dark_oak_fence":(5,20),"pale_oak_fence":(5,20),"mangrove_fence":(5,20),"bamboo_fence":(5,20),
"oak_stairs":(5,20),"birch_stairs":(5,20),"spruce_stairs":(5,20),"jungle_stairs":(5,20),"acacia_stairs":(5,20),
"cherry_stairs":(5,20),"dark_oak_stairs":(5,20),"pale_oak_stairs":(5,20),"mangrove_stairs":(5,20),
"bamboo_stairs":(5,20),"bamboo_mosaic_stairs":(5,20),
"oak_log":(5,5),"spruce_log":(5,5),"birch_log":(5,5),"jungle_log":(5,5),"acacia_log":(5,5),"cherry_log":(5,5),
"pale_oak_log":(5,5),"dark_oak_log":(5,5),"mangrove_log":(5,5),"bamboo_block":(5,5),
"stripped_oak_log":(5,5),"stripped_spruce_log":(5,5),"stripped_birch_log":(5,5),"stripped_jungle_log":(5,5),
"stripped_acacia_log":(5,5),"stripped_cherry_log":(5,5),"stripped_dark_oak_log":(5,5),"stripped_pale_oak_log":(5,5),
"stripped_mangrove_log":(5,5),"stripped_bamboo_block":(5,5),
"stripped_oak_wood":(5,5),"stripped_spruce_wood":(5,5),"stripped_birch_wood":(5,5),"stripped_jungle_wood":(5,5),
"stripped_acacia_wood":(5,5),"stripped_cherry_wood":(5,5),"stripped_dark_oak_wood":(5,5),"stripped_pale_oak_wood":(5,5),
"stripped_mangrove_wood":(5,5),
"oak_wood":(5,5),"spruce_wood":(5,5),"birch_wood":(5,5),"jungle_wood":(5,5),"acacia_wood":(5,5),"cherry_wood":(5,5),
"pale_oak_wood":(5,5),"dark_oak_wood":(5,5),"mangrove_wood":(5,5),"mangrove_roots":(5,20),
"oak_leaves":(30,60),"spruce_leaves":(30,60),"birch_leaves":(30,60),"jungle_leaves":(30,60),"acacia_leaves":(30,60),
"cherry_leaves":(30,60),"dark_oak_leaves":(30,60),"pale_oak_leaves":(30,60),"mangrove_leaves":(30,60),
"bookshelf":(30,20),"tnt":(15,100),
"short_grass":(60,100),"fern":(60,100),"dead_bush":(60,100),"short_dry_grass":(60,100),"tall_dry_grass":(60,100),
"sunflower":(60,100),"lilac":(60,100),"rose_bush":(60,100),"peony":(60,100),"tall_grass":(60,100),"large_fern":(60,100),
"dandelion":(60,100),"poppy":(60,100),"open_eyeblossom":(60,100),"closed_eyeblossom":(60,100),"blue_orchid":(60,100),
"allium":(60,100),"azure_bluet":(60,100),"red_tulip":(60,100),"orange_tulip":(60,100),"white_tulip":(60,100),
"pink_tulip":(60,100),"oxeye_daisy":(60,100),"cornflower":(60,100),"lily_of_the_valley":(60,100),"torchflower":(60,100),
"pitcher_plant":(60,100),"wither_rose":(60,100),"pink_petals":(60,100),"wildflowers":(60,100),"leaf_litter":(60,100),
"cactus_flower":(60,100),
"white_wool":(30,60),"orange_wool":(30,60),"magenta_wool":(30,60),"light_blue_wool":(30,60),"yellow_wool":(30,60),
"lime_wool":(30,60),"pink_wool":(30,60),"gray_wool":(30,60),"light_gray_wool":(30,60),"cyan_wool":(30,60),
"purple_wool":(30,60),"blue_wool":(30,60),"brown_wool":(30,60),"green_wool":(30,60),"red_wool":(30,60),"black_wool":(30,60),
"vine":(15,100),"coal_block":(5,5),"hay_block":(60,20),"target":(15,20),
"white_carpet":(60,20),"orange_carpet":(60,20),"magenta_carpet":(60,20),"light_blue_carpet":(60,20),"yellow_carpet":(60,20),
"lime_carpet":(60,20),"pink_carpet":(60,20),"gray_carpet":(60,20),"light_gray_carpet":(60,20),"cyan_carpet":(60,20),
"purple_carpet":(60,20),"blue_carpet":(60,20),"brown_carpet":(60,20),"green_carpet":(60,20),"red_carpet":(60,20),
"black_carpet":(60,20),
"pale_moss_block":(5,100),"pale_moss_carpet":(5,100),"pale_hanging_moss":(5,100),"dried_kelp_block":(30,60),
"bamboo":(60,60),"scaffolding":(60,60),"lectern":(30,20),"composter":(5,20),"sweet_berry_bush":(60,100),
"beehive":(5,20),"bee_nest":(30,20),"azalea_leaves":(30,60),"flowering_azalea_leaves":(30,60),
"cave_vines":(15,60),"cave_vines_plant":(15,60),"spore_blossom":(60,100),"azalea":(30,60),"flowering_azalea":(30,60),
"big_dripleaf":(60,100),"big_dripleaf_stem":(60,100),"small_dripleaf":(60,100),"hanging_roots":(30,60),
"glow_lichen":(15,100),"firefly_bush":(60,100),"bush":(60,100),
}


def main():
    # minecraft-data blocks.json is a list; index by name → (minStateId, maxStateId).
    blocks = {b["name"]: b for b in json.load(urllib.request.urlopen(URL))}
    rows = []  # (minState, maxState, ignite, burn, name)
    missing = []
    for name, (ig, bu) in FLAMMABLE.items():
        b = blocks.get(name)
        if not b:
            missing.append(name)
            continue
        rows.append((b["minStateId"], b["maxStateId"], ig, bu, name))
    rows.sort()
    # sanity: no overlaps
    for i in range(1, len(rows)):
        assert rows[i][0] > rows[i - 1][1], f"overlap {rows[i-1]} {rows[i]}"
    with open(OUT, "w") as f:
        f.write("// Code generated by scripts/gen_flammable.py. DO NOT EDIT.\n")
        f.write("// Fire flammability (ignite/burn odds) transcribed from vanilla's\n")
        f.write("// 1.21.5 FireBlock.setFlammable registrations, mapped to block-state id\n")
        f.write("// ranges via Mojang's datagen block report.\n\n")
        f.write("package worldgen\n\n")
        f.write("// flammable is one contiguous block-state id range with its fire odds.\n")
        f.write("type flammable struct{ lo, hi, ignite, burn uint32 }\n\n")
        f.write("// flammableRanges is sorted by lo for binary search (see Flammability).\n")
        f.write("var flammableRanges = []flammable{\n")
        for lo, hi, ig, bu, name in rows:
            f.write(f"\t{{{lo}, {hi}, {ig}, {bu}}}, // {name}\n")
        f.write("}\n\n")
        f.write("""// Flammability returns a block state's fire ignite odds (chance the block
// catches from a neighbouring fire) and burn odds (chance an adjacent fire
// consumes it), both 0 for non-flammable blocks. Mirrors FireBlock's
// igniteOdds/burnOdds maps.
func Flammability(state uint32) (ignite, burn uint32) {
	lo, hi := 0, len(flammableRanges)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		r := flammableRanges[mid]
		switch {
		case state < r.lo:
			hi = mid - 1
		case state > r.hi:
			lo = mid + 1
		default:
			return r.ignite, r.burn
		}
	}
	return 0, 0
}
""")
    print(f"wrote {len(rows)} ranges to {OUT}")
    if missing:
        print("MISSING from report (skipped):", missing)


if __name__ == "__main__":
    main()
