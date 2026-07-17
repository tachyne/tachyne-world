#!/usr/bin/env python3
"""Generate internal/server/lootdata/chests.json — the vanilla CHEST/gameplay
loot tables baked into the same compact IR the engine's loot evaluator reads,
keyed by table name ("simple_dungeon", "village/village_plains_house", …).

Unlike the block/entity bakers this one needs NO network: item names → ids come
from the engine's own internal/server/itemnames_gen.go (the canonical id space),
and the tables come from the local 1.21.11 server jar. A function the evaluator
does not support is DROPPED (graceful degradation → a plainer item) rather than
omitting the whole table; an entry whose item id is unknown is skipped.

Run: python3 scripts/gen_chestloot.py [path-to-server.jar]
"""
import io, json, os, re, sys, zipfile

JAR = sys.argv[1] if len(sys.argv) > 1 else os.path.expanduser("~/vanilla/server-1.21.11.jar")
HERE = os.path.dirname(__file__)
OUTDIR = os.path.join(HERE, "..", "internal", "server", "lootdata")
OUT = os.path.join(OUTDIR, "chests.json")

# Item ids from the engine's generated name→id table (no network).
src = open(os.path.join(HERE, "..", "internal", "server", "itemnames_gen.go")).read()
item_id = {m.group(1): int(m.group(2)) for m in re.finditer(r'"([a-z0-9_]+)":\s+(\d+),', src)}

outer = zipfile.ZipFile(JAR)
inner = [n for n in outer.namelist() if n.startswith("META-INF/versions/") and n.endswith(".jar")]
z = zipfile.ZipFile(io.BytesIO(outer.read(inner[0]))) if inner else outer


class Unsupported(Exception):
    pass


def iid(name):
    n = name.removeprefix("minecraft:")
    if n not in item_id:
        raise Unsupported("item " + n)
    return item_id[n]


def num(v):
    if isinstance(v, (int, float)):
        return {"t": "const", "v": float(v)}
    t = v.get("type", "").removeprefix("minecraft:")
    if t in ("", "constant"):
        return {"t": "const", "v": float(v["value"])}
    if t == "uniform":
        return {"t": "uniform", "min": num(v["min"]), "max": num(v["max"])}
    if t == "binomial":
        return {"t": "binomial", "n": num(v["n"]), "p": num(v["p"])}
    raise Unsupported("np " + t)


def cond(c):
    t = c["condition"].removeprefix("minecraft:")
    if t == "random_chance":
        ch = c["chance"]
        return {"c": "chance", "p": float(ch if isinstance(ch, (int, float)) else ch["value"])}
    raise Unsupported("cond " + t)


def func(f):
    t = f["function"].removeprefix("minecraft:")
    if t == "set_count":
        return {"f": "set_count", "np": num(f["count"]), "add": bool(f.get("add", False))}
    if t == "enchant_randomly":
        return {"f": "ench_random"}
    if t == "enchant_with_levels":
        return {"f": "ench_levels", "np": num(f["levels"])}
    if t == "set_damage":
        dmg = f["damage"]
        return {"f": "set_damage", "np": num(dmg), "add": bool(f.get("add", False))}
    # Cosmetic / component functions the engine cannot represent yet — drop the
    # function (the item still appears, just plainer).
    if t in ("exploration_map", "set_name", "set_enchantments", "set_instrument",
             "set_potion", "set_stew_effect", "set_ominous_bottle_amplifier",
             "set_nbt", "set_components", "set_custom_data", "enchanted_count_increase",
             "set_written_book_pages", "set_book_cover", "reference", "furnace_smelt"):
        return None
    raise Unsupported("func " + t)


def conds(node):
    out = []
    for c in node.get("conditions", []):
        out.append(cond(c))  # a table-referencing chance rides here; may raise
    return out


def funcs(node):
    return [x for x in (func(f) for f in node.get("functions", [])) if x is not None]


def entry(e):
    t = e["type"].removeprefix("minecraft:")
    if t == "item":
        return {"type": "item", "id": iid(e["name"]), "w": e.get("weight", 1),
                "q": e.get("quality", 0), "conditions": conds(e), "functions": funcs(e)}
    if t == "empty":
        return {"type": "empty", "w": e.get("weight", 1), "q": e.get("quality", 0),
                "conditions": conds(e)}
    if t == "loot_table":
        # A nested table reference: keep the referenced name; the evaluator
        # rolls it as a sub-table. Inline value objects are unsupported.
        v = e["value"]
        if not isinstance(v, str):
            raise Unsupported("inline loot_table")
        return {"type": "ref", "ref": v.removeprefix("minecraft:"), "w": e.get("weight", 1),
                "q": e.get("quality", 0), "conditions": conds(e), "functions": funcs(e)}
    if t in ("alternatives", "group", "sequence"):
        return {"type": t, "children": [entry(c) for c in e["children"]],
                "w": e.get("weight", 1), "q": e.get("quality", 0), "conditions": conds(e)}
    raise Unsupported("entry " + t)


def entry_opt(e):
    """Bake an entry, or None if its item id is unknown (skip just the entry)."""
    try:
        return entry(e)
    except Unsupported as ex:
        if str(ex).startswith("item "):
            return None
        raise


def table(t):
    pools = []
    for pool in t.get("pools", []):
        es = [x for x in (entry_opt(e) for e in pool["entries"]) if x is not None]
        pools.append({
            "rolls": num(pool["rolls"]),
            "bonus": num(pool["bonus_rolls"]) if "bonus_rolls" in pool else None,
            "conditions": conds(pool),
            "functions": funcs(pool),
            "entries": es,
        })
    return {"pools": pools}


PREFIX = "data/minecraft/loot_table/chests/"
out, kept, skipped = {}, 0, 0
for n in sorted(z.namelist()):
    if not (n.startswith(PREFIX) and n.endswith(".json")):
        continue
    name = n[len(PREFIX):-len(".json")]
    try:
        out["chests/" + name] = table(json.loads(z.read(n)))
        kept += 1
    except Unsupported as ex:
        skipped += 1
        print(f"  skip chests/{name}: {ex}", file=sys.stderr)

os.makedirs(OUTDIR, exist_ok=True)
with open(OUT, "w") as f:
    json.dump(out, f, separators=(",", ":"), sort_keys=True)
print(f"wrote {OUT}: {kept} chest tables kept, {skipped} skipped")
