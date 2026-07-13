#!/usr/bin/env python3
"""Generate internal/server/lootdata/entities.json — the vanilla BLOCK loot
tables baked into a compact IR the engine's data-driven loot evaluator reads,
keyed by block-state range (binary-searched in Go, like loot_gen.go).

Source: the 1.21.11 server jar datapack (data/minecraft/loot_table/blocks/
*.json); block names → state ranges and item names → ids via minecraft-data.
Only the node types the evaluator supports are kept; a table using anything
else is omitted so the engine falls back to its legacy drop for that block.

Run: python3 scripts/gen_blockloot.py [path-to-server.jar]  (needs network)
"""
import io, json, sys, os, urllib.request, zipfile

MD = "https://raw.githubusercontent.com/PrismarineJS/minecraft-data/master/data/pc/1.21.11"
JAR = sys.argv[1] if len(sys.argv) > 1 else os.path.expanduser("~/vanilla/server-1.21.11.jar")
OUTDIR = os.path.join(os.path.dirname(__file__), "..", "internal", "server", "lootdata")
OUT = os.path.join(OUTDIR, "entities.json")

item_id = {i["name"]: i["id"] for i in json.load(urllib.request.urlopen(MD + "/items.json"))}
ents = json.load(urllib.request.urlopen(MD + "/entities.json"))
ent_id = {e["name"]: e["id"] for e in ents}

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
    if t == "survives_explosion":
        return {"c": "survives"}
    if t == "random_chance":
        ch = c["chance"]
        return {"c": "chance", "p": float(ch if isinstance(ch, (int, float)) else ch["value"])}
    if t == "table_bonus":
        return {"c": "table_bonus", "ench": c["enchantment"].removeprefix("minecraft:"),
                "chances": [float(x) for x in c["chances"]]}
    if t == "block_state_property":
        return {"c": "state", "props": {k: str(v) for k, v in c.get("properties", {}).items()}}
    if t == "match_tool":
        pred = c.get("predicate", {})
        for e in pred.get("predicates", {}).get("minecraft:enchantments", []):
            if e.get("enchantments") == "minecraft:silk_touch":
                return {"c": "tool", "silk": True}
        items = pred.get("items")
        if isinstance(items, str):
            if items.startswith("#"):
                return {"c": "tool", "tag": items.lstrip("#").removeprefix("minecraft:")}
            return {"c": "tool", "item": items.removeprefix("minecraft:")}
        raise Unsupported("match_tool shape")
    if t == "killed_by_player":
        return {"c": "killed_by_player"}
    if t == "random_chance_with_enchanted_bonus":
        ec = c["enchanted_chance"]
        return {"c": "ench_chance", "ench": c["enchantment"].removeprefix("minecraft:"),
                "base_chance": float(c["unenchanted_chance"]),
                "linear_base": float(ec["base"]), "per_level": float(ec["per_level_above_first"])}
    if t == "entity_properties":
        pred = c.get("predicate", {})
        who = c.get("entity", "this")
        out = {"c": "entity", "who": who}
        flags = pred.get("flags", {})
        if "is_on_fire" in flags:
            out["on_fire"] = bool(flags["is_on_fire"])
        ty = pred.get("type")
        if isinstance(ty, str):
            out["etype"] = ty.removeprefix("minecraft:")
        return out
    if t == "inverted":
        return {"c": "not", "term": cond(c["term"])}
    if t == "any_of":
        return {"c": "any", "terms": [cond(x) for x in c["terms"]]}
    if t == "all_of":
        return {"c": "all", "terms": [cond(x) for x in c["terms"]]}
    raise Unsupported("cond " + t)


def func(f):
    t = f["function"].removeprefix("minecraft:")
    if t == "set_count":
        return {"f": "set_count", "np": num(f["count"]), "add": bool(f.get("add", False))}
    if t == "explosion_decay":
        return {"f": "explosion_decay"}
    if t == "limit_count":
        lim, out = f["limit"], {"f": "limit"}
        if isinstance(lim, dict):
            for k in ("min", "max"):
                if k in lim:
                    out[k] = int(lim[k] if isinstance(lim[k], (int, float)) else lim[k]["value"])
        return out
    if t == "apply_bonus":
        formula = f["formula"].removeprefix("minecraft:")
        out = {"f": "bonus", "ench": f["enchantment"].removeprefix("minecraft:"), "formula": formula}
        p = f.get("parameters", {})
        if formula == "uniform_bonus_count":
            out["mult"] = int(p.get("bonusMultiplier", 1))
        elif formula == "binomial_with_bonus_count":
            out["extra"] = int(p["extra"])
            out["prob"] = float(p["probability"])
        elif formula != "ore_drops":
            raise Unsupported("bonus " + formula)
        return out
    if t == "enchanted_count_increase":
        return {"f": "looting", "ench": f["enchantment"].removeprefix("minecraft:"),
                "np": num(f["count"]), "limit": int(f.get("limit", 0))}
    if t == "furnace_smelt":
        return {"f": "smelt"}
    if t in ("copy_components", "copy_state", "copy_name", "copy_custom_data",
             "set_contents", "set_potion", "set_ominous_bottle_amplifier"):
        return None
    raise Unsupported("func " + t)


def entry(e):
    t = e["type"].removeprefix("minecraft:")
    conds = [cond(c) for c in e.get("conditions", [])]
    funcs = [x for x in (func(f) for f in e.get("functions", [])) if x is not None]
    if t == "item":
        return {"type": "item", "id": iid(e["name"]), "conditions": conds, "functions": funcs}
    if t in ("alternatives", "group", "sequence"):
        return {"type": t, "children": [entry(c) for c in e["children"]], "conditions": conds}
    if t == "empty":
        return {"type": "empty", "conditions": conds}
    raise Unsupported("entry " + t)


def table(t):
    pools = []
    for pool in t.get("pools", []):
        pools.append({
            "rolls": num(pool["rolls"]),
            "conditions": [cond(c) for c in pool.get("conditions", [])],
            "functions": [x for x in (func(f) for f in pool.get("functions", [])) if x is not None],
            "entries": [entry(e) for e in pool["entries"]],
        })
    return {"pools": pools}


rows, kept, skipped = {}, 0, 0
for n in sorted(z.namelist()):
    if not (n.startswith("data/minecraft/loot_table/entities/") and n.endswith(".json")):
        continue
    name = n[len("data/minecraft/loot_table/entities/"):-len(".json")]
    if name not in ent_id:
        skipped += 1
        continue
    try:
        rows[str(ent_id[name])] = table(json.loads(z.read(n)))
        kept += 1
    except Unsupported:
        skipped += 1

os.makedirs(OUTDIR, exist_ok=True)
with open(OUT, "w") as f:
    json.dump(rows, f, separators=(",", ":"))
print(f"wrote {OUT}: {kept} entity tables kept, {skipped} skipped (engine falls back)")
