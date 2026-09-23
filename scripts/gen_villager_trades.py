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

import canon

SRC = os.environ.get("VANILLA_SRC", os.path.join(  # behaviour data: canon.py
    canon.src(canon.DATA), "net/minecraft/world/entity/npc/villager/VillagerTrades.java"))
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

    def mult100(text, default=5):
        """A listing's priceMultiplier as hundredths (vanilla uses 0.05 and 0.2)."""
        return default if not text else int(round(float(text) * 100))

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
    eife = re.compile(r"new EnchantedItemForEmeralds\(" + cast + r"(?:Items|Blocks)\.([A-Z_]+),"
                      r"\s*(\d+),\s*(\d+),\s*(\d+)(?:,\s*([0-9.]+)[fF])?\)")
    # TreasureMapForEmeralds(emeraldCost, StructureTags.X, "name",
    # MapDecorationTypes.Y, maxUses, villagerXp) — optionally wrapped in
    # TypeSpecificTrade.oneTradeInBiomes(..., VillagerType.A, VillagerType.B).
    tmfe = re.compile(r"new TreasureMapForEmeralds\((\d+),\s*(?:\(TagKey<Structure>\)\s*)?"
                      r"StructureTags\.([A-Z_]+),\s*\"[^\"]*\",\s*"
                      r"(?:\(Holder<MapDecorationType>\)\s*)?MapDecorationTypes\.[A-Z_]+,"
                      r"\s*(\d+),\s*(\d+)\)")
    # ItemsAndEmeraldsToItems(from, fromCount, emeraldCost, to, toCount,
    # maxUses, xp, mult): a second item cost beside the emeralds.
    iaeti = re.compile(r"new ItemsAndEmeraldsToItems\(" + cast + r"(?:Items|Blocks)\.([A-Z_]+),\s*(\d+),\s*(\d+),\s*"
                       + cast + r"(?:Items|Blocks)\.([A-Z_]+),\s*(\d+),\s*(\d+),\s*(\d+)(?:,\s*([0-9.]+)[fF])?")
    # DyedArmorForEmeralds(item, cost[, maxUses, xp]) — maxUses 12, xp 1 by default.
    dafe = re.compile(r"new DyedArmorForEmeralds\(" + cast + r"Items\.([A-Z_]+),\s*(\d+)((?:,\s*\d+){0,2})\)")
    # SuspiciousStewForEmerald(MobEffects.X, durationTicks, xp).
    ssfe = re.compile(r"new SuspiciousStewForEmerald\((?:\(Holder<MobEffect>\)\s*)?MobEffects\.([A-Z_]+),\s*(\d+),\s*(\d+)\)")
    # TippedArrowForItemsAndEmeralds(from, fromCount, to, toCount, cost, maxUses, xp).
    tafie = re.compile(r"new TippedArrowForItemsAndEmeralds\(" + cast + r"Items\.([A-Z_]+),\s*(\d+),\s*"
                       + cast + r"Items\.([A-Z_]+),\s*(\d+),\s*(\d+),\s*(\d+),\s*(\d+)\)")
    # EnchantBookForEmeralds(villagerXp, EnchantmentTags.TRADEABLE): an
    # ordinary listing in the librarian's pool, competing for the tier's slots.
    ebfe = re.compile(r"new EnchantBookForEmeralds\((\d+),\s*(?:\(TagKey<Enchantment>\)\s*)?EnchantmentTags\.TRADEABLE\)")
    # EmeraldsForVillagerTypeItem(cost, maxUses, xp, {type: item, ...}).
    efvti = re.compile(r"new EmeraldsForVillagerTypeItem\((\d+),\s*(\d+),\s*(\d+),")
    vtype_item = re.compile(r"put\(VillagerType\.([A-Z_]+),\s*(?:\(Object\)\s*)?Items\.([A-Z_]+)\)")
    tier_re = re.compile(r"\(Object\)(\d),\s*\(Object\)new ItemListing\[\]\{")

    # profession-index → tier(1..5) → list of trades
    table = {}
    stews = []      # (effect name, duration ticks) per SuspiciousStewForEmerald row
    typeitems = []  # the per-villager-type item of an EmeraldsForVillagerTypeItem row
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
                trades.append((m.start(), (iid, price, emerald, 1, maxu, xp, 0, 0, 0, 0, 5)))
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
                mult = mult100(str(vals[4])) if len(vals) >= 5 else 5
                trades.append((m.start(), (emerald, cost, iid, count, maxu, xp, 0, 0, 0, 0, mult)))
            for m in eife.finditer(seg):
                iid = item_id(m.group(1))
                if iid is None:
                    continue
                base, maxu, xp = int(m.group(2)), int(m.group(3)), int(m.group(4))
                # villager SELLS ONE, enchanted at roll time. inCount carries the
                # BASE emerald cost; rollGearOffer replaces it with base+level.
                trades.append((m.start(), (emerald, base, iid, 1, maxu, xp, 1, 0, 0, 0,
                                           mult100(m.group(5)))))
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
                trades.append((m.start(), (emerald, cost, ids["filled_map"], 1, maxu, xp,
                                           "map", (dest, mask), ids["compass"], 1, 20)))
            for m in iaeti.finditer(seg):
                fid, tid = item_id(m.group(1)), item_id(m.group(4))
                if fid is None or tid is None:
                    continue
                # in = emerald×cost PLUS from×fromCount → out = to×toCount.
                trades.append((m.start(), (emerald, int(m.group(3)), tid, int(m.group(5)),
                                           int(m.group(6)), int(m.group(7)), 0, 0,
                                           fid, int(m.group(2)), mult100(m.group(8)))))
            for m in dafe.finditer(seg):
                iid = item_id(m.group(1))
                if iid is None:
                    continue
                extra = [int(n) for n in re.findall(r"\d+", m.group(3))]
                maxu, xp = (extra + [12, 1])[:2] if extra else (12, 1)
                trades.append((m.start(), (emerald, int(m.group(2)), iid, 1, maxu, xp,
                                           "dyed", 0, 0, 0, 20)))
            for m in ssfe.finditer(seg):
                stews.append((m.group(1).lower(), int(m.group(2))))
                trades.append((m.start(), (emerald, 1, ids["suspicious_stew"], 1, 12,
                                           int(m.group(3)), "stew", len(stews), 0, 0, 5)))
            for m in tafie.finditer(seg):
                fid, tid = item_id(m.group(1)), item_id(m.group(3))
                if fid is None or tid is None:
                    continue
                trades.append((m.start(), (emerald, int(m.group(5)), tid, int(m.group(4)),
                                           int(m.group(6)), int(m.group(7)), "arrow", 0,
                                           fid, int(m.group(2)), 5)))
            for m in ebfe.finditer(seg):
                # The price and the enchantment are rolled; inCount is a
                # placeholder rollBookOffer replaces.
                trades.append((m.start(), (emerald, 0, ids["enchanted_book"], 1, 12,
                                           int(m.group(1)), "book", 0, ids["book"], 1, 20)))
            for m in efvti.finditer(seg):
                per = {}
                for t in vtype_item.finditer(seg[m.end():m.end() + 1200]):
                    if t.group(1) in VILLAGER_TYPES:
                        per[VILLAGER_TYPES.index(t.group(1))] = item_id(t.group(2))
                if len(per) != len(VILLAGER_TYPES) or None in per.values():
                    continue
                typeitems.append(tuple(per[i] for i in range(len(VILLAGER_TYPES))))
                # villager BUYS the type's item: in = item×cost → out = emerald×1.
                trades.append((m.start(), (0, int(m.group(1)), emerald, 1, int(m.group(2)),
                                           int(m.group(3)), "typeitem", len(typeitems), 0, 0, 5)))
            trades.sort(key=lambda t: t[0])
            trades = [t for _, t in trades]
            # count the exotic listings we skipped in this segment
            skipped += (seg.count("new ") - len(efi.findall(seg)) - len(ife.findall(seg))
                        - len(eife.findall(seg)) - len(tmfe.findall(seg))
                        - len(iaeti.findall(seg)) - len(dafe.findall(seg))
                        - len(ssfe.findall(seg)) - len(tafie.findall(seg))
                        - len(efvti.findall(seg)) - len(ebfe.findall(seg)))
            if trades:
                table[pi][tier] = trades

    # The distinct treasure-map listings, in first-seen order.
    maps = []
    for pi in sorted(table):
        for tier in sorted(table[pi]):
            for row in table[pi][tier]:
                if row[6] == "map" and row[7] not in maps:
                    maps.append(row[7])

    with open(OUT, "w") as f:
        f.write("// Code generated by scripts/gen_villager_trades.py. DO NOT EDIT.\n")
        f.write("// Villager trades transcribed from the vanilla VillagerTrades.TRADES\n")
        f.write("// (every listing type in the trade map, in source order per tier).\n\n")
        f.write("package server\n\n")
        f.write("// vTrade is one merchant offer: input item×count → output item×count, with a\n")
        f.write("// use limit before it locks and the trade-XP it grants the villager.\n")
        f.write("type vTrade struct {\n")
        f.write("\tinItem, inCount, outItem, outCount, maxUses, xp int32\n")
        f.write("\t// kind is vTradeFixed for a plain offer, or one of the rolled kinds\n")
        f.write("\t// below, whose output is built when the villager unlocks the tier.\n")
        f.write("\tkind int32\n")
        f.write("\t// aux is 1 + the row's index into the side table its kind uses (0 =\n")
        f.write("\t// none): villagerMapListings for a treasure map, villagerStewListings\n")
        f.write("\t// for a stew, villagerTypeItems for a per-villager-type buy. vTrade\n")
        f.write("\t// stays a comparable row of plain ints, so those extras live beside the\n")
        f.write("\t// table rather than in it.\n")
        f.write("\taux int32\n")
        f.write("\t// An optional SECOND item cost beside the emeralds (0 = none):\n")
        f.write("\t// ItemsAndEmeraldsToItems' input, a treasure map's compass, the arrows a\n")
        f.write("\t// fletcher tips.\n")
        f.write("\tc2Item, c2Count int32\n")
        f.write("\t// mult100 is vanilla MerchantOffer.priceMultiplier in hundredths — 5 for\n")
        f.write("\t// most listings, 20 for armour, bells, shields, saddles, explorer maps,\n")
        f.write("\t// dyed armour and most enchanted gear. It scales both the demand markup\n")
        f.write("\t// and the reputation discount.\n")
        f.write("\tmult100 int32\n")
        f.write("}\n\n")
        f.write("const (\n")
        f.write("\tvTradeFixed         int32 = 0 // EmeraldForItems / ItemsForEmeralds\n")
        f.write("\tvTradeEnchantedGear int32 = 1 // EnchantedItemForEmeralds\n")
        f.write("\tvTradeTreasureMap   int32 = 2 // TreasureMapForEmeralds\n")
        f.write("\tvTradeDyedArmor     int32 = 3 // DyedArmorForEmeralds\n")
        f.write("\tvTradeStew          int32 = 4 // SuspiciousStewForEmerald\n")
        f.write("\tvTradeTippedArrow   int32 = 5 // TippedArrowForItemsAndEmeralds\n")
        f.write("\tvTradeTypeItem      int32 = 6 // EmeraldsForVillagerTypeItem\n")
        f.write("\tvTradeEnchantedBook int32 = 7 // EnchantBookForEmeralds\n")
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
        f.write("// villagerStewListings is the effect a SuspiciousStewForEmerald row sells:\n")
        f.write("// the vanilla effect name and its duration in ticks. stewCodeFor resolves\n")
        f.write("// each to a suspicious-stew table row at startup.\n")
        f.write("var villagerStewListings = []struct {\n\teffect string\n\tticks  int\n}{\n")
        for name, ticks in stews:
            f.write(f'\t{{"{name}", {ticks}}},\n')
        f.write("}\n\n")
        f.write("// villagerTypeItems is one EmeraldsForVillagerTypeItem row: the item the\n")
        f.write("// villager buys, indexed by its villager type (the boats a fisherman takes).\n")
        f.write("var villagerTypeItems = [][%d]int32{\n" % len(VILLAGER_TYPES))
        for row in typeitems:
            f.write("\t{" + ", ".join(str(v) for v in row) + "},\n")
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
                for (ii, ic, oi, oc, mu, xp, kind, aux, c2i, c2n, mult) in table[pi][tier]:
                    if kind == "map":
                        kind, aux = "vTradeTreasureMap", maps.index(aux) + 1
                    elif kind == "dyed":
                        kind = "vTradeDyedArmor"
                    elif kind == "stew":
                        kind = "vTradeStew"
                    elif kind == "arrow":
                        kind = "vTradeTippedArrow"
                    elif kind == "typeitem":
                        kind = "vTradeTypeItem"
                    elif kind == "book":
                        kind = "vTradeEnchantedBook"
                    f.write(f"{{{ii}, {ic}, {oi}, {oc}, {mu}, {xp}, {kind}, {aux}, {c2i}, {c2n}, {mult}}}, ")
                f.write("},\n")
            f.write("\t},\n")
        f.write("}\n")
    n = sum(len(v) for pt in table.values() for v in pt.values())
    print(f"wrote {n} trades across {len(table)} professions to {OUT} (skipped ~{skipped} exotic listings)")


if __name__ == "__main__":
    main()
