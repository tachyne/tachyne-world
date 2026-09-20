#!/usr/bin/env python3
"""Generate internal/server/villager_trades_gen.go — the villager trade tables.

Facts matching vanilla's VillagerTrades.TRADES. Three ItemListing types are
transcribed: EmeraldForItems (the villager buys N of an item for 1 emerald),
ItemsForEmeralds (it sells N items for C emeralds) and EnchantedItemForEmeralds
(it sells one piece of gear, enchanted at roll time, for a base price the roll
tops up). Rows keep their source order within a tier, which is the order a
villager unlocks them in. Still skipped: treasure maps, suspicious stew, tipped
arrows, dyed armour and the villager-type/biome-specific wrappers. Item names
mapped to tachyne ids via the generated itemByName table.

Run locally (vanilla reference source + itemnames_gen.go on disk; set VANILLA_SRC)."""
import os
import re

SRC = os.environ.get("VANILLA_SRC", "")  # path to a vanilla VillagerTrades.java reference
NAMES = os.path.join(os.path.dirname(__file__), "..", "internal", "server", "itemnames_gen.go")
OUT = os.path.join(os.path.dirname(__file__), "..", "internal", "server", "villager_trades_gen.go")

# Profession → tachyne index (order defines the generated enum).
# A vanilla structure tag a treasure map points at → (tachyne locator id,
# map decoration type id, the en_us name of the map).
MAP_DESTS = {
    "ON_TAIGA_VILLAGE_MAPS":     ("village_taiga", 31, "Taiga Village Map"),
    "ON_SWAMP_EXPLORER_MAPS":    ("swamp_hut", 33, "Swamp Explorer Map"),
    "ON_SNOWY_VILLAGE_MAPS":     ("village_snowy", 30, "Snowy Village Map"),
    "ON_SAVANNA_VILLAGE_MAPS":   ("village_savanna", 29, "Savanna Village Map"),
    "ON_PLAINS_VILLAGE_MAPS":    ("village_plains", 28, "Plains Village Map"),
    "ON_JUNGLE_EXPLORER_MAPS":   ("jungle_pyramid", 32, "Jungle Explorer Map"),
    "ON_DESERT_VILLAGE_MAPS":    ("village_desert", 27, "Desert Village Map"),
    "ON_OCEAN_EXPLORER_MAPS":    ("monument", 9, "Ocean Explorer Map"),
    "ON_TRIAL_CHAMBERS_MAPS":    ("trial_chambers", 34, "Trial Explorer Map"),
    "ON_WOODLAND_EXPLORER_MAPS": ("mansion", 8, "Woodland Explorer Map"),
}

# Villager type (biome) → tachyne index; the generated forTypes bitmask.
VILLAGER_TYPES = ["DESERT", "JUNGLE", "PLAINS", "SAVANNA", "SNOW", "SWAMP", "TAIGA"]

PROFS = ["FARMER", "FISHERMAN", "SHEPHERD", "FLETCHER", "LIBRARIAN", "CARTOGRAPHER",
         "CLERIC", "ARMORER", "WEAPONSMITH", "TOOLSMITH", "MASON", "BUTCHER", "LEATHERWORKER"]


def load_item_ids():
    ids = {}
    for m in re.finditer(r'"([a-z0-9_]+)":\s*(\d+)', open(NAMES).read()):
        ids[m.group(1)] = int(m.group(2))
    return ids


def main():
    ids = load_item_ids()
    emerald = ids["emerald"]
    src = open(SRC).read()

    def item_id(enum):  # Items.WHEAT / Blocks.PUMPKIN → id
        name = enum.lower()
        return ids.get(name)

    # Per profession, isolate its put(...) region up to the next put or EOF.
    prof_re = {p: None for p in PROFS}
    puts = list(re.finditer(r"hashMap\.put\(VillagerProfession\.([A-Z_]+),", src))
    # The LAST profession's region must stop at the end of the trade map, not
    # run to EOF: what follows is the wandering trader's list and the
    # trade-rebalance EXPERIMENTAL_TRADES, and swallowing those gave the mason
    # the experimental armorer's and librarian's offers.
    tail = len(src)
    for marker in ("WANDERING_TRADER_TRADES", "EXPERIMENTAL_TRADES"):
        i = src.find(marker)
        if i != -1:
            tail = min(tail, i)
    for i, m in enumerate(puts):
        p = m.group(1)
        if p not in prof_re:
            continue
        start = m.end()
        end = puts[i + 1].start() if i + 1 < len(puts) else tail
        prof_re[p] = src[start:end]

    # Decompilers differ on casts: CFR writes `new EmeraldForItems((ItemLike)
    # Items.WHEAT, 20, 16, 2)` where others drop the cast. Tolerate both, or
    # every "villager buys X" row silently disappears from the table.
    cast = r"(?:\((?:ItemLike|Object)\)\s*)?"
    efi = re.compile(r"new EmeraldForItems\(" + cast + r"(?:Items|Blocks)\.([A-Z_]+),\s*(\d+),\s*(\d+),\s*(\d+)\)")
    ife = re.compile(r"new ItemsForEmeralds\((?:new ItemStack\()?" + cast + r"(?:Items|Blocks)\.([A-Z_]+)\)?,\s*([0-9.,fF ]+)\)")
    # EnchantedItemForEmeralds(item, baseEmeraldCost, maxUses, villagerXp[, mult]):
    # the villager sells ONE of the item, enchanted when the offer is rolled.
    eife = re.compile(r"new EnchantedItemForEmeralds\(" + cast + r"(?:Items|Blocks)\.([A-Z_]+),\s*(\d+),\s*(\d+),\s*(\d+)")
    # TreasureMapForEmeralds(emeraldCost, StructureTags.X, "name",
    # MapDecorationTypes.Y, maxUses, villagerXp) — optionally wrapped in
    # TypeSpecificTrade.oneTradeInBiomes(..., VillagerType.A, VillagerType.B).
    tmfe = re.compile(r"new TreasureMapForEmeralds\((\d+),\s*(?:\(TagKey<Structure>\)\s*)?"
                      r"StructureTags\.([A-Z_]+),\s*\"[^\"]*\",\s*"
                      r"(?:\(Holder<MapDecorationType>\)\s*)?MapDecorationTypes\.[A-Z_]+,"
                      r"\s*(\d+),\s*(\d+)\)")
    tier_re = re.compile(r"\(Object\)(\d),\s*\(Object\)new ItemListing\[\]\{")

    # profession-index → tier(1..5) → list of trades
    table = {}
    skipped = 0
    for p in PROFS:
        body = prof_re.get(p)
        if not body:
            continue
        pi = PROFS.index(p)
        table[pi] = {}
        # Split the body into tier segments by the "(Object)N, (Object)new ItemListing[]{" markers.
        marks = list(tier_re.finditer(body))
        for j, tm in enumerate(marks):
            tier = int(tm.group(1))
            seg_start = tm.end()
            seg_end = marks[j + 1].start() if j + 1 < len(marks) else len(body)
            seg = body[seg_start:seg_end]
            # (source offset, trade) so a tier's rows keep vanilla's order —
            # the order decides which two a villager unlocks at that tier.
            trades = []
            for m in efi.finditer(seg):
                iid = item_id(m.group(1))
                if iid is None:
                    continue
                price, maxu, xp = int(m.group(2)), int(m.group(3)), int(m.group(4))
                # villager BUYS: in = item×price → out = emerald×1
                trades.append((m.start(), (iid, price, emerald, 1, maxu, xp, 0)))
            for m in ife.finditer(seg):
                iid = item_id(m.group(1))
                if iid is None:
                    continue
                nums = [n for n in re.split(r"[,\s]+", m.group(2).replace("f", "").replace("F", "")) if n]
                vals = [float(n) for n in nums]
                if len(vals) < 3:
                    continue
                cost, count = int(vals[0]), int(vals[1])
                if len(vals) == 3:      # cost, count, xp — maxUses defaults 12
                    maxu, xp = 12, int(vals[2])
                else:                    # cost, count, maxUses, xp[, priceMult]
                    maxu, xp = int(vals[2]), int(vals[3])
                # villager SELLS: in = emerald×cost → out = item×count
                trades.append((m.start(), (emerald, cost, iid, count, maxu, xp, 0)))
            for m in eife.finditer(seg):
                iid = item_id(m.group(1))
                if iid is None:
                    continue
                base, maxu, xp = int(m.group(2)), int(m.group(3)), int(m.group(4))
                # villager SELLS ONE, enchanted at roll time. inCount carries the
                # BASE emerald cost; rollGearOffer replaces it with base+level.
                trades.append((m.start(), (emerald, base, iid, 1, maxu, xp, 1)))
            for m in tmfe.finditer(seg):
                dest = MAP_DESTS.get(m.group(2))
                if dest is None:
                    continue
                cost, maxu, xp = int(m.group(1)), int(m.group(3)), int(m.group(4))
                # oneTradeInBiomes(listing, VillagerType...) restricts the
                # listing to villagers of those types — a bitmask, 0 = any.
                mask, tail = 0, seg[m.end():m.end() + 400]
                if tail.startswith(", VillagerType."):
                    for t in re.finditer(r"VillagerType\.([A-Z_]+)", tail[:tail.index(")")]):
                        if t.group(1) in VILLAGER_TYPES:
                            mask |= 1 << VILLAGER_TYPES.index(t.group(1))
                trades.append((m.start(), ("map", emerald, cost, ids["filled_map"],
                                           maxu, xp, dest, mask)))
            trades.sort(key=lambda t: t[0])
            trades = [t for _, t in trades]
            # count the exotic listings we skipped in this segment
            skipped += (seg.count("new ") - len(efi.findall(seg)) - len(ife.findall(seg))
                        - len(eife.findall(seg)) - len(tmfe.findall(seg)))
            if trades:
                table[pi][tier] = trades

    # The distinct treasure-map listings, in first-seen order.
    maps = []
    for pi in sorted(table):
        for tier in sorted(table[pi]):
            for row in table[pi][tier]:
                if row[0] == "map" and (row[6], row[7]) not in maps:
                    maps.append((row[6], row[7]))

    with open(OUT, "w") as f:
        f.write("// Code generated by scripts/gen_villager_trades.py. DO NOT EDIT.\n")
        f.write("// Villager trades transcribed from the vanilla VillagerTrades.TRADES\n")
        f.write("// (EmeraldForItems, ItemsForEmeralds, EnchantedItemForEmeralds and\n")
        f.write("// TreasureMapForEmeralds listings, in source order per tier; the remaining\n")
        f.write("// exotic types are omitted).\n\n")
        f.write("package server\n\n")
        f.write("// vTrade is one merchant offer: input item×count → output item×count, with a\n")
        f.write("// use limit before it locks and the trade-XP it grants the villager.\n")
        f.write("type vTrade struct {\n")
        f.write("\tinItem, inCount, outItem, outCount, maxUses, xp int32\n")
        f.write("\t// kind is vTradeFixed for a plain offer, or one of the rolled kinds\n")
        f.write("\t// below, whose output is built when the villager unlocks the tier.\n")
        f.write("\tkind int32\n")
        f.write("\t// For a treasure map, 1 + its index into villagerMapListings (0 = none);\n")
        f.write("\t// vTrade stays a comparable row of plain ints, so the map-only extras\n")
        f.write("\t// live beside the table.\n")
        f.write("\tmapIdx int32\n")
        f.write("}\n\n")
        f.write("const (\n")
        f.write("\tvTradeFixed         int32 = 0 // EmeraldForItems / ItemsForEmeralds\n")
        f.write("\tvTradeEnchantedGear int32 = 1 // EnchantedItemForEmeralds\n")
        f.write("\tvTradeTreasureMap   int32 = 2 // TreasureMapForEmeralds\n")
        f.write(")\n\n")
        f.write("// vMapListing is the rest of a TreasureMapForEmeralds row: the structure\n")
        f.write("// the map points at, the decoration that marks it, the name the map\n")
        f.write("// carries, and a bitmask of the villager types the listing is offered to\n")
        f.write("// (0 = any).\n")
        f.write("type vMapListing struct {\n")
        f.write("\tdest, label string\n")
        f.write("\tdecor       int32\n")
        f.write("\tforTypes    uint8\n")
        f.write("}\n\n")
        f.write("var villagerMapListings = []vMapListing{\n")
        for (dest, decor, label), mask in maps:
            f.write(f'\t{{"{dest}", "{label}", {decor}, {mask}}},\n')
        f.write("}\n\n")
        f.write("// profession names by tachyne index (order matches the generator).\n")
        f.write("var professionNames = []string{\n")
        for p in PROFS:
            f.write(f'\t"{p.lower()}",\n')
        f.write("}\n\n")
        f.write("// villagerTrades[profession][tier 1..5] = the offers unlocked at that tier.\n")
        f.write("var villagerTrades = map[int]map[int][]vTrade{\n")
        for pi in sorted(table):
            f.write(f"\t{pi}: {{ // {PROFS[pi].lower()}\n")
            for tier in sorted(table[pi]):
                f.write(f"\t\t{tier}: {{")
                for row in table[pi][tier]:
                    if row[0] == "map":
                        _, ii, ic, oi, mu, xp, dest, mask = row
                        f.write(f"{{{ii}, {ic}, {oi}, 1, {mu}, {xp}, vTradeTreasureMap, "
                                f"{maps.index((dest, mask)) + 1}}}, ")
                        continue
                    (ii, ic, oi, oc, mu, xp, kind) = row
                    f.write(f"{{{ii}, {ic}, {oi}, {oc}, {mu}, {xp}, {kind}, 0}}, ")
                f.write("},\n")
            f.write("\t},\n")
        f.write("}\n")
    n = sum(len(v) for pt in table.values() for v in pt.values())
    print(f"wrote {n} trades across {len(table)} professions to {OUT} (skipped ~{skipped} exotic listings)")


if __name__ == "__main__":
    main()
