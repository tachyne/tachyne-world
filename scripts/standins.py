"""Stand-ins: what a client is shown for a block its version does not have.

A shift range cannot say "absent": the canonical id would land on whatever
that client has at the same number (a 26.2 client saw a white wool slab as
fire). So every absent block gets a stand-in the client does have, chosen to
keep the SHAPE (a slab for a slab, stairs for stairs — a wrong shape desyncs
collision) and roughly the colour. A block with no stand-in is shown as air
(Java) or the fallback block (Bedrock). Used by gen_translation.py and
gen_bedrock.py.

Stdlib only.
"""

# wool/concrete colour -> a slab+stairs family of about that colour
_COLOUR_FAMILY = {
    "white": "quartz", "orange": "acacia", "magenta": "crimson", "light_blue": "prismarine_brick",
    "yellow": "bamboo", "lime": "mossy_cobblestone", "pink": "cherry", "gray": "stone_brick",
    "light_gray": "andesite", "cyan": "prismarine", "purple": "purpur", "blue": "dark_prismarine",
    "brown": "spruce", "green": "mossy_stone_brick", "red": "red_nether_brick", "black": "blackstone",
}

# block -> its stand-in
BLOCKS = {
    "straw_bed": "yellow_bed", "red_shrub": "dead_bush",
    "red_poplar_leaves": "birch_leaves", "orange_poplar_leaves": "birch_leaves",
    "yellow_poplar_leaves": "birch_leaves",
    "stripped_poplar_log": "stripped_birch_log", "stripped_poplar_wood": "stripped_birch_wood",
    "potted_poplar_sapling": "potted_birch_sapling",
}
for _c, _fam in _COLOUR_FAMILY.items():
    for _m in ("wool", "concrete"):
        BLOCKS[f"{_c}_{_m}_slab"] = f"{_fam}_slab"
        BLOCKS[f"{_c}_{_m}_stairs"] = f"{_fam}_stairs"
for _p in ("log", "wood", "planks", "slab", "stairs", "fence", "fence_gate", "door", "trapdoor", "button",
           "pressure_plate", "sign", "wall_sign", "hanging_sign", "wall_hanging_sign", "shelf", "sapling"):
    BLOCKS[f"poplar_{_p}"] = f"birch_{_p}"


def props(have, stand_states, stand_default):
    """The stand-in state for a state with properties `have`: every property
    value the stand-in also has, and the stand-in's default for the rest."""
    want = dict(stand_default)
    want.update({k: v for k, v in have.items() if k in want and any(p.get(k) == v for p in stand_states)})
    return want


# biome -> its stand-in, for a client that lacks the biome (the Java side
# keeps the same pairing in tachyne-common's protocol.biomesAdded).
BIOMES = {
    "dappled_forest": "forest",
}
