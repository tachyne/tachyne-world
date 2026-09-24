#!/usr/bin/env python3
"""Generate internal/server/villager_trades_gen.go — the merchant trade tables.

Since 26.3 vanilla's trades are data: every offer is a villager_trade JSON
(wants / additional_wants / gives, max_uses, xp, reputation_discount, an
optional merchant_predicate and given_item_modifier, and
double_trade_price_enchantments), and each profession level — and each of
the wandering trader's three pools — is a trade_set naming a villager_trade
tag and how many offers to draw from it. This reads those files straight out
of the canonical server jar and emits the tables the engine rolls from:

  villagerTrades[profession][level]   the listings a level draws from
  villagerTradeAmount[profession][level]  how many offers it draws
  traderTradeSets                     the wandering trader's pools, in the
                                      order WanderingTrader.updateTrades adds them

A listing whose result is rolled when the offer is made carries a kind (and,
where it needs one, an index into a side table): enchant_with_levels gear,
enchant_randomly books, exploration maps, random dyes, a stew effect, a
potion. The discard-on-missing `filtered` step that follows each of them is
the roll's own "no offer" answer (a map with nothing in reach, gear the
enchanter left bare), so it is checked for shape and not emitted. A
merchant_predicate on villager/variant becomes a villager-type bitmask.
Anything the engine cannot express is skipped, loudly.

Items are written by NAME through itemByName; the generator refuses a name
itemnames_gen.go does not know. Stdlib only.

    python3 scripts/gen_villager_trades.py [--dump rows.json]
"""
import json
import os
import re
import sys

import canon

HERE = os.path.dirname(__file__)
NAMES = os.path.join(HERE, "..", "internal", "server", "itemnames_gen.go")
OUT = os.path.join(HERE, "..", "internal", "server", "villager_trades_gen.go")

# Profession → tachyne index. The order is PERSISTED (a saved villager stores
# its index), so it never changes; new professions append.
PROFS = ["farmer", "fisherman", "shepherd", "fletcher", "librarian", "cartographer",
         "cleric", "armorer", "weaponsmith", "toolsmith", "mason", "butcher", "leatherworker"]

# Villager types in registry order (villagerdata.go's villagerType*); a
# merchant_predicate becomes a bitmask over these.
VILLAGER_TYPES = ["desert", "jungle", "plains", "savanna", "snow", "swamp", "taiga"]

# WanderingTrader.updateTrades adds its pools in this order.
TRADER_SETS = ["buying", "uncommon", "common"]

# MobEffect.isInstantaneous: SetStewEffectFunction keeps these durations in
# ticks and multiplies every other one by 20.
INSTANT_EFFECTS = {"instant_health", "instant_damage", "saturation"}


def strip(n):
    return n.split(":", 1)[1] if ":" in n else n


def load_item_names():
    return set(re.findall(r'^\t"([a-z0-9_]+)":\s*\d+', open(NAMES).read(), re.M))


class Data:
    """The jar's data/minecraft tree, by path."""

    def __init__(self):
        z = canon.inner_jar()
        self.files = {}
        for n in z.namelist():
            if n.startswith("data/minecraft/") and n.endswith(".json"):
                self.files[n[len("data/minecraft/"):-len(".json")]] = z
        self.z = z

    def get(self, path):
        if path not in self.files:
            raise KeyError(path)
        return json.loads(self.z.read("data/minecraft/" + path + ".json"))

    def tag(self, kind, name, seen=None):
        """A tag's entries in load order, nested tags expanded in place."""
        seen = seen or set()
        out = []
        for v in self.get(f"tags/{kind}/{strip(name)}")["values"]:
            if isinstance(v, dict):
                v = v["id"]
            if v.startswith("#"):
                if v not in seen:
                    seen.add(v)
                    out += [x for x in self.tag(kind, v[1:], seen) if x not in out]
            elif strip(v) not in out:
                out.append(strip(v))
        return out


class Skip(Exception):
    pass


def const_int(v, what):
    """A ContextIntProvider the engine bakes in: a plain number."""
    if isinstance(v, (int, float)) and int(v) == v:
        return int(v)
    if isinstance(v, dict) and v.get("type") == "minecraft:constant":
        return int(v["value"])
    raise Skip(f"{what} is a {v!r} number provider")


def flatten(mods):
    if mods is None:
        return []
    if isinstance(mods, dict):
        mods = [mods]
    out = []
    for m in mods:
        if m.get("type") == "minecraft:sequence":
            out += flatten(m["functions"])
        else:
            out.append(m)
    return out


def types_mask(pred):
    """A merchant_predicate → villager-type bitmask (0 = any villager)."""
    if pred is None:
        return 0
    try:
        assert pred["type"] == "minecraft:entity_properties" and pred["entity"] == "this"
        inner = pred["predicate"]
        assert list(inner) == ["minecraft:predicates"]
        preds = inner["minecraft:predicates"]
        assert list(preds) == ["minecraft:villager/variant"]
    except (AssertionError, KeyError):
        raise Skip(f"merchant_predicate {json.dumps(pred)} is not a villager/variant test")
    want = preds["minecraft:villager/variant"]
    mask = 0
    for t in ([want] if isinstance(want, str) else want):
        if strip(t) not in VILLAGER_TYPES:
            raise Skip(f"unknown villager type {t}")
        mask |= 1 << VILLAGER_TYPES.index(strip(t))
    return mask


class Gen:
    def __init__(self):
        self.d = Data()
        self.items = load_item_names()
        regs = json.load(open(canon.report("registries.json")))
        self.decor = {strip(k): v["protocol_id"]
                      for k, v in regs["minecraft:map_decoration_type"]["entries"].items()}
        self.maps = []     # (dest structure, decoration id, search radius in chunks)
        self.stews = []    # [(effect, ticks), ...] per stew listing
        self.potions = []  # [potion name, ...] per potion listing
        self.dyes = None   # (base, trials, p) of set_random_dyes' number_of_dyes
        self.skipped = []

    def item(self, name, where):
        n = strip(name)
        if n not in self.items:
            raise Skip(f"{where}: item {n} is not in itemnames_gen.go")
        return n

    def cost(self, c, where):
        if c is None:
            return None, 0
        if c.get("components"):
            raise Skip(f"{where}: an ItemCost with components {json.dumps(c['components'])}")
        return self.item(c["id"], where), const_int(c.get("count", 1), where + ".count")

    def side(self, table, row):
        if row not in table:
            table.append(row)
        return table.index(row) + 1

    def listing(self, key):
        """One villager_trade → a row dict, or Skip."""
        t = self.d.get("villager_trade/" + key)
        known = {"wants", "additional_wants", "gives", "max_uses", "xp", "reputation_discount",
                 "merchant_predicate", "given_item_modifier", "double_trade_price_enchantments"}
        if set(t) - known:
            raise Skip(f"{key}: unknown fields {sorted(set(t) - known)}")
        wi, wn = self.cost(t["wants"], key + ".wants")
        ci, cn = self.cost(t.get("additional_wants"), key + ".additional_wants")
        g = t["gives"]
        if set(g) - {"id", "count"}:
            raise Skip(f"{key}: gives carries {sorted(set(g) - {'id', 'count'})}")
        gi, gn = self.item(g["id"], key + ".gives"), const_int(g.get("count", 1), key + ".gives.count")
        mult = t.get("reputation_discount", 0.0)
        if not isinstance(mult, (int, float)):
            raise Skip(f"{key}: reputation_discount provider {mult!r}")
        mult100 = int(round(mult * 100))
        if mult100 <= 0:
            # mult100 0 means "the old default 0.05" in the engine's store.
            raise Skip(f"{key}: a zero reputation_discount has no engine encoding")
        row = dict(key=key, wants=(wi, wn), extra=(ci, cn), gives=(gi, gn),
                   max_uses=max(const_int(t.get("max_uses", 4), key + ".max_uses"), 1),
                   xp=max(const_int(t.get("xp", 1), key + ".xp"), 0),
                   mult100=mult100, kind="vTradeFixed", aux=0,
                   types=types_mask(t.get("merchant_predicate")), detail=None)

        dtpe = t.get("double_trade_price_enchantments")
        mods = flatten(t.get("given_item_modifier"))
        rolled = [m for m in mods if m["type"] != "minecraft:filtered"]
        for m in mods:
            if m["type"] == "minecraft:filtered":
                if m.get("on_fail", {}).get("type") != "minecraft:discard" or "on_pass" in m:
                    raise Skip(f"{key}: a filtered step that is not discard-on-fail")
        if len(rolled) > 1:
            raise Skip(f"{key}: {len(rolled)} result modifiers")
        if dtpe is not None and (not rolled or rolled[0]["type"] != "minecraft:enchant_randomly"):
            raise Skip(f"{key}: double_trade_price_enchantments without a stored enchantment roll")
        if not rolled:
            if len(mods):
                raise Skip(f"{key}: a filter with nothing to filter")
            return row
        m = rolled[0]
        ty = strip(m["type"])
        if ty == "enchant_with_levels":
            lv = m.get("levels")
            if (m.get("options") != "#minecraft:on_traded_equipment" or not isinstance(lv, dict)
                    or lv.get("type") != "minecraft:uniform" or (lv["min"], lv["max"]) != (5, 19)
                    or not m.get("include_additional_cost_component")):
                raise Skip(f"{key}: enchant_with_levels {json.dumps(m)} differs from rollGearOffer")
            row["kind"] = "vTradeEnchantedGear"
        elif ty == "enchant_randomly":
            if (m.get("options") != "#minecraft:tradeable" or m.get("only_compatible", True)
                    or not m.get("include_additional_cost_component") or gi != "enchanted_book"):
                raise Skip(f"{key}: enchant_randomly {json.dumps(m)} differs from rollBookOffer")
            if dtpe not in (None, "#minecraft:double_trade_price"):
                raise Skip(f"{key}: double_trade_price_enchantments {dtpe!r}")
            row["kind"] = "vTradeEnchantedBook"
            row["aux"] = 1 if dtpe else 0  # 1 = the double-price tag applies
            row["detail"] = "double" if dtpe else None
        elif ty == "exploration_map":
            dest = m["destination"]
            if not dest.startswith("#"):
                raise Skip(f"{key}: exploration_map destination {dest} is not a tag")
            structs = self.d.tag("worldgen/structure", dest[1:])
            if len(structs) != 1:
                raise Skip(f"{key}: {dest} names {len(structs)} structures")
            if m.get("zoom", 2) != 2:
                raise Skip(f"{key}: exploration_map zoom {m['zoom']}")
            decor = self.decor[strip(m.get("decoration", "mansion"))]
            listing = (structs[0], decor, int(m.get("search_radius", 50)))
            row["kind"], row["aux"] = "vTradeTreasureMap", self.side(self.maps, listing)
            row["detail"] = structs[0]
        elif ty == "set_random_dyes":
            n = m["number_of_dyes"]
            try:
                assert n["type"] == "minecraft:add" and len(n["inputs"]) == 2
                base, b = n["inputs"]
                assert isinstance(base, int) and b["type"] == "minecraft:binomial"
                shape = (base, int(b["n"]), float(b["p"]))
            except (AssertionError, KeyError, TypeError):
                raise Skip(f"{key}: number_of_dyes {json.dumps(n)}")
            if self.dyes not in (None, shape):
                raise Skip(f"{key}: a second number_of_dyes shape {shape}")
            self.dyes = shape
            row["kind"] = "vTradeDyedArmor"
        elif ty == "set_stew_effect":
            effs = []
            for e in m["effects"]:
                name = strip(e["type"])
                dur = const_int(e["duration"], key + ".duration")
                effs.append((name, dur if name in INSTANT_EFFECTS else dur * 20))
            row["kind"], row["aux"] = "vTradeStew", self.side(self.stews, effs)
            row["detail"] = effs
        elif ty in ("set_random_potion", "set_potion"):
            if ty == "set_potion":
                pool = [strip(m["id"])]
            elif m.get("options", "").startswith("#"):
                pool = self.d.tag("potion", m["options"][1:])
            else:
                raise Skip(f"{key}: set_random_potion over the whole registry")
            row["kind"], row["aux"] = "vTradePotion", self.side(self.potions, pool)
            row["detail"] = pool if len(pool) == 1 else f"{len(pool)} potions"
        else:
            raise Skip(f"{key}: modifier {ty}")
        return row

    def trade_set(self, path):
        s = self.d.get("trade_set/" + path)
        if s.get("allow_duplicates"):
            raise SystemExit(f"{path}: allow_duplicates sets are not supported")
        trades = s["trades"]
        keys = self.d.tag("villager_trade", trades[1:]) if trades.startswith("#") else [strip(trades)]
        rows = []
        for k in keys:
            try:
                rows.append(self.listing(k))
            except Skip as e:
                self.skipped.append(str(e))
        return const_int(s["amount"], path + ".amount"), rows


def go_trade(r):
    wi, wn = r["wants"]
    ci, cn = r["extra"]
    gi, gn = r["gives"]
    c2 = f'itemByName["{ci}"]' if ci else "0"
    return (f'{{itemByName["{wi}"], {wn}, itemByName["{gi}"], {gn}, {r["max_uses"]}, {r["xp"]}, '
            f'{r["kind"]}, {r["aux"]}, {c2}, {cn}, {r["mult100"]}, {r["types"]}}},')


def main():
    g = Gen()
    table, amounts = {}, {}
    for pi, p in enumerate(PROFS):
        table[pi], amounts[pi] = {}, {}
        for lvl in range(1, 6):
            try:
                n, rows = g.trade_set(f"{p}/level_{lvl}")
            except KeyError:
                continue
            table[pi][lvl], amounts[pi][lvl] = rows, n
    trader = [g.trade_set(f"wandering_trader/{s}") for s in TRADER_SETS]
    if g.dyes is None:
        raise SystemExit("no set_random_dyes listing: the dyed-armour constants have no source")

    if "--dump" in sys.argv:
        rows = []
        for pi in table:
            for lvl, rs in table[pi].items():
                rows += [dict(r, prof=PROFS[pi], tier=lvl) for r in rs]
        for s, (_, rs) in zip(TRADER_SETS, trader):
            rows += [dict(r, prof="wandering_trader", tier=s) for r in rs]
        json.dump(dict(rows=rows, amounts={PROFS[p]: a for p, a in amounts.items()},
                       trader_amounts={s: n for s, (n, _) in zip(TRADER_SETS, trader)}),
                  open(sys.argv[sys.argv.index("--dump") + 1], "w"), indent=1)

    w = []
    w.append("// Code generated by scripts/gen_villager_trades.py. DO NOT EDIT.")
    w.append(f"// Merchant trades read from the vanilla {canon.VERSION} data: villager_trade,")
    w.append("// trade_set and the villager_trade tags.")
    w.append("")
    w.append("package server")
    w.append("")
    w.append("// vTrade is one listing: the wanted item×count (the cost that demand and")
    w.append("// reputation move) → the given item×count, with a use limit before it locks")
    w.append("// and the trade-XP it grants the villager.")
    w.append("type vTrade struct {")
    w.append("\tinItem, inCount, outItem, outCount, maxUses, xp int32")
    w.append("\t// kind is vTradeFixed for a plain offer, or one of the rolled kinds")
    w.append("\t// below, whose result is built when the villager unlocks the level.")
    w.append("\tkind int32")
    w.append("\t// aux is 1 + the row's index into the side table its kind uses (0 =")
    w.append("\t// none): villagerMapListings for a map, villagerStewListings for a stew,")
    w.append("\t// villagerPotionPools for a potion. For an enchanted book it is 1 when")
    w.append("\t// the listing's double_trade_price_enchantments applies.")
    w.append("\taux int32")
    w.append("\t// The listing's additional_wants (0 = none): the second cost, never")
    w.append("\t// adjusted by demand or reputation.")
    w.append("\tc2Item, c2Count int32")
    w.append("\t// mult100 is the listing's reputation_discount — vanilla")
    w.append("\t// MerchantOffer.priceMultiplier — in hundredths. It scales both the")
    w.append("\t// demand markup and the reputation discount.")
    w.append("\tmult100 int32")
    w.append("\t// forTypes is the merchant_predicate: a bitmask of the villager types")
    w.append("\t// (villagerType*) the listing is offered to, 0 = any.")
    w.append("\tforTypes int32")
    w.append("}")
    w.append("")
    w.append("const (")
    w.append("\tvTradeFixed         int32 = 0 // no given_item_modifier")
    w.append("\tvTradeEnchantedGear int32 = 1 // enchant_with_levels 5..19 from #on_traded_equipment")
    w.append("\tvTradeTreasureMap   int32 = 2 // exploration_map")
    w.append("\tvTradeDyedArmor     int32 = 3 // set_random_dyes")
    w.append("\tvTradeStew          int32 = 4 // set_stew_effect")
    w.append("\tvTradePotion        int32 = 5 // set_potion / set_random_potion")
    w.append("\tvTradeEnchantedBook int32 = 6 // enchant_randomly from #tradeable")
    w.append(")")
    w.append("")
    w.append("// vMapListing is an exploration_map's destination: the structure the map")
    w.append("// points at, the decoration that marks it, and the search radius in chunks.")
    w.append("type vMapListing struct {")
    w.append("\tdest          string")
    w.append("\tdecor, radius int32")
    w.append("}")
    w.append("")
    w.append("var villagerMapListings = []vMapListing{")
    for dest, decor, radius in g.maps:
        w.append(f'\t{{"{dest}", {decor}, {radius}}},')
    w.append("}")
    w.append("")
    w.append("// vStewEffect is one set_stew_effect entry: the vanilla effect name and its")
    w.append("// duration in ticks.")
    w.append("type vStewEffect struct {")
    w.append("\teffect string")
    w.append("\tticks  int")
    w.append("}")
    w.append("")
    w.append("// villagerStewListings is each stew listing's effects; the roll picks one.")
    w.append("var villagerStewListings = [][]vStewEffect{")
    for effs in g.stews:
        w.append("\t{" + ", ".join(f'{{"{e}", {t}}}' for e, t in effs) + "},")
    w.append("}")
    w.append("")
    w.append("// villagerPotionPools is each potion listing's candidates by vanilla name")
    w.append("// (set_potion: one; set_random_potion: the options tag); the roll picks one.")
    w.append("var villagerPotionPools = [][]string{")
    for pool in g.potions:
        w.append("\t{" + ", ".join(f'"{p}"' for p in pool) + "},")
    w.append("}")
    w.append("")
    base, trials, p = g.dyes
    w.append("// set_random_dyes' number_of_dyes: base + binomial(trials, chance).")
    w.append("const (")
    w.append(f"\tdyedArmorBaseDyes   = {base}")
    w.append(f"\tdyedArmorDyeTrials  = {trials}")
    w.append(f"\tdyedArmorDyeChance  = {p}")
    w.append(")")
    w.append("")
    w.append("// profession names by tachyne index (persisted: order is fixed).")
    w.append("var professionNames = []string{")
    for pn in PROFS:
        w.append(f'\t"{pn}",')
    w.append("}")
    w.append("")
    w.append("// villagerTradeAmount[profession][level] is the trade_set's amount: how")
    w.append("// many offers a villager draws when it reaches that level.")
    w.append("var villagerTradeAmount = map[int]map[int]int{")
    for pi in table:
        k = f"{pi}:".ljust(len(f"{len(PROFS) - 1}:") + 1)  # gofmt's key column
        w.append(f"\t{k}{{" + ", ".join(f"{l}: {amounts[pi][l]}" for l in sorted(amounts[pi])) + f"}}, // {PROFS[pi]}")
    w.append("}")
    w.append("")
    w.append("// villagerTrades[profession][level 1..5] = the listings that level draws from.")
    w.append("var villagerTrades = map[int]map[int][]vTrade{")
    for pi in table:
        w.append(f"\t{pi}: {{ // {PROFS[pi]}")
        for lvl in sorted(table[pi]):
            w.append(f"\t\t{lvl}: {{")
            for r in table[pi][lvl]:
                w.append("\t\t\t" + go_trade(r))
            w.append("\t\t},")
        w.append("\t},")
    w.append("}")
    w.append("")
    w.append("// vTradeSet is a trade_set: draw amount offers from trades.")
    w.append("type vTradeSet struct {")
    w.append("\tamount int")
    w.append("\ttrades []vTrade")
    w.append("}")
    w.append("")
    w.append("// traderTradeSets is the wandering trader's pools, in the order")
    w.append("// WanderingTrader.updateTrades adds them: " + ", ".join(TRADER_SETS) + ".")
    w.append("var traderTradeSets = []vTradeSet{")
    for n, rows in trader:
        w.append(f"\t{{{n}, []vTrade{{")
        for r in rows:
            w.append("\t\t" + go_trade(r))
        w.append("\t}},")
    w.append("}")
    src = "\n".join(w) + "\n"
    # gofmt aligns the const block's '=' columns; write them pre-aligned.
    src = src.replace("\tdyedArmorBaseDyes   =", "\tdyedArmorBaseDyes  =").replace(
        "\tdyedArmorDyeTrials  =", "\tdyedArmorDyeTrials =").replace(
        "\tdyedArmorDyeChance  =", "\tdyedArmorDyeChance =")
    open(OUT, "w").write(src)

    n = sum(len(v) for pt in table.values() for v in pt.values())
    print(f"wrote {n} villager listings across {len(table)} professions and "
          f"{sum(len(r) for _, r in trader)} wandering-trader listings to {OUT}")
    for s in g.skipped:
        print("skipped:", s)


if __name__ == "__main__":
    main()
