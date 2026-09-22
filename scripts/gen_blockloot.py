#!/usr/bin/env python3
"""Generate internal/server/lootdata/blocks.json — the vanilla BLOCK loot
tables baked into a compact IR the engine's data-driven loot evaluator reads,
keyed by block-state range (binary-searched in Go).

Source: the 1.21.11 server jar datapack (data/minecraft/loot_table/blocks/
*.json); block names → state ranges and item names → ids via vanilla's own reports
(scripts/vanillareport.py).
Only the node types the evaluator supports are kept; a table using anything
else is omitted so the engine falls back to its legacy drop for that block.

Run: python3 scripts/gen_blockloot.py [path-to-server.jar]  (needs network)
"""
import io, json, sys, os, zipfile
import vanillareport

JAR = sys.argv[1] if len(sys.argv) > 1 else os.path.expanduser("~/vanilla/server-1.21.11.jar")
OUTDIR = os.path.join(os.path.dirname(__file__), "..", "internal", "server", "lootdata")
OUT = os.path.join(OUTDIR, "blocks.json")

item_id = {i["name"]: i["id"] for i in vanillareport.registry("1.21.11", "item")}
blocks = vanillareport.blocks("1.21.11")
brange = {b["name"]: (b["minStateId"], b["maxStateId"]) for b in blocks}

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


def expand_item_tag(ref, seen=None):
    """#minecraft:foo -> the concrete item names it contains (recursively)."""
    seen = seen or set()
    name = ref.lstrip("#")
    if ":" not in name:
        name = "minecraft:" + name
    if name in seen:
        return []
    seen.add(name)
    ns, path = name.split(":", 1)
    entry = f"data/{ns}/tags/item/{path}.json"
    try:
        data = json.loads(z.read(entry))
    except KeyError:
        return []
    out = []
    for v in data.get("values", []):
        v = v if isinstance(v, str) else v.get("id", "")
        if v.startswith("#"):
            out += expand_item_tag(v, seen)
        elif v:
            out.append(v.removeprefix("minecraft:"))
    return out


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
                # Expand the tag here: the evaluator used to understand only
                # "shears" by name, so #cluster_max_harvestables silently
                # failed and amethyst clusters never gave their four shards.
                names = expand_item_tag(items)
                if names:
                    return {"c": "tool", "items": names}
                return {"c": "tool", "tag": items.lstrip("#").removeprefix("minecraft:")}
            return {"c": "tool", "item": items.removeprefix("minecraft:")}
        raise Unsupported("match_tool shape")
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
    if t in ("copy_components", "copy_state", "copy_name", "copy_custom_data", "set_contents"):
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


# Each block drops from the loot table IT names, which the game reports (see
# scripts/extract). Usually that is blocks/<its own name>, but a wall torch,
# wall sign, wall banner or wall head borrows the standing block's table
# (dropsLike), and pairing tables to blocks by file name left all 61 of those
# with no table at all — they fell through to a flat one-item fallback.
# Blocks with no loot table get no row.
names = set(z.namelist())
loot_of = {b["name"]: b.get("lootTable")
           for b in json.load(open(os.path.expanduser("~/vanilla/extract/1.21.11.json")))["blocks"]}
rows, kept, skipped = [], 0, 0
for b in blocks:
    key = loot_of.get(b["name"])
    path = "data/minecraft/loot_table/%s.json" % key
    if not key or path not in names:
        continue
    try:
        tbl = table(json.loads(z.read(path)))
        rows.append({"lo": b["minStateId"], "hi": b["maxStateId"], "table": tbl})
        kept += 1
    except Unsupported:
        skipped += 1
rows.sort(key=lambda r: r["lo"])

os.makedirs(OUTDIR, exist_ok=True)
with open(OUT, "w") as f:
    json.dump(rows, f, separators=(",", ":"))
print(f"wrote {OUT}: {kept} block tables kept, {skipped} skipped (engine falls back)")
