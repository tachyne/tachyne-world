#!/usr/bin/env python3
"""Regenerate internal/server/recipeunlocks_gen.go — how each recipe enters a
player's recipe book, from the canonical version's recipe-unlock
advancements.

Every recipe-book entry vanilla hands out comes from an advancement under
data/minecraft/advancement/recipes/: its reward is the recipe, and it is
done when any one of its criteria is (the requirements are a single OR
group). The criteria are:

  - recipe_unlocked ("has_the_recipe"): the recipe is already known — no
    unlock of its own, so it is left out here;
  - inventory_changed with one item predicate: the player holds one of the
    predicate's items (an item, a list, or a tag);
  - inventory_changed with slots.occupied.min: that many inventory slots in
    use (the chest: ten);
  - tick: every player, straight away (the crafting table);
  - enter_block minecraft:water: the player in water (the boats).

A reward names a vanilla recipe. A crafting recipe is keyed in the engine's
book by that name; the engine keeps cooking recipes per input item, so a
smelting / blasting / smoking / campfire recipe becomes one key per input,
"cook/<station>/<item>" (recipebook_progress.go cookRecipeKey). A key the
engine's book does not have is dropped when the table is read.

Sources (local, no network): the server jar's data/minecraft/advancement/
recipes/**, data/minecraft/recipe/*.json and data/minecraft/tags/item/**;
item ids from internal/server/itemnames_gen.go.

Run: python3 scripts/gen_recipeunlocks.py [path-to-server.jar]
"""
import json
import os
import re
import sys

import canon

HERE = os.path.dirname(os.path.abspath(__file__))
OUT = os.path.join(HERE, "..", "internal", "server", "recipeunlocks_gen.go")
ITEMS = os.path.join(HERE, "..", "internal", "server", "itemnames_gen.go")

STATIONS = {
    "minecraft:smelting": "furnace",
    "minecraft:blasting": "blast_furnace",
    "minecraft:smoking": "smoker",
    "minecraft:campfire_cooking": "campfire",
}


def strip_ns(name):
    return name.split(":", 1)[1] if ":" in name else name


def main():
    if len(sys.argv) > 1:
        import io
        import zipfile
        outer = zipfile.ZipFile(sys.argv[1])
        inner = [n for n in outer.namelist() if n.startswith("META-INF/versions/") and n.endswith(".jar")]
        z = zipfile.ZipFile(io.BytesIO(outer.read(inner[0]))) if inner else outer
        source = os.path.basename(sys.argv[1])
    else:
        z = canon.inner_jar()
        source = canon.VERSION + " server jar"

    src = open(ITEMS).read()
    item_id = {m.group(1): int(m.group(2)) for m in re.finditer(r'"([a-z0-9_]+)":\s+(\d+),', src)}

    tag_cache = {}

    def tag_items(tag):
        tag = strip_ns(tag.lstrip("#"))
        if tag in tag_cache:
            return tag_cache[tag]
        out = []
        try:
            d = json.loads(z.read(f"data/minecraft/tags/item/{tag}.json"))
        except KeyError:
            tag_cache[tag] = out
            return out
        for v in d["values"]:
            v = v["id"] if isinstance(v, dict) else v
            if v.startswith("#"):
                out += tag_items(v)
            else:
                out.append(strip_ns(v))
        tag_cache[tag] = out
        return out

    def names(val):
        vals = val if isinstance(val, list) else [val]
        out = []
        for v in vals:
            if v.startswith("#"):
                out += tag_items(v)
            else:
                out.append(strip_ns(v))
        seen, uniq = set(), []
        for n in out:
            if n not in seen:
                seen.add(n)
                uniq.append(n)
        return uniq

    def recipe_keys(recipe):
        """The engine book keys a vanilla recipe name stands for."""
        try:
            r = json.loads(z.read(f"data/minecraft/recipe/{recipe}.json"))
        except KeyError:
            return [recipe]
        station = STATIONS.get(r.get("type", ""))
        if station is None:
            return [recipe]
        ing = r.get("ingredient")
        if isinstance(ing, dict):  # an old-style {"item": ...} / {"tag": ...}
            ing = ing.get("item") or ("#" + ing["tag"] if "tag" in ing else None)
        if ing is None:
            return []
        return ["cook/%s/%s" % (station, n) for n in names(ing)]

    prefix = "data/minecraft/advancement/recipes/"
    rules = []
    unknown = set()
    for path in sorted(n for n in z.namelist() if n.startswith(prefix) and n.endswith(".json")):
        adv = json.loads(z.read(path))
        rewards = (adv.get("rewards") or {}).get("recipes") or []
        if not rewards:
            continue  # recipes/root: the tree's impossible root
        if len(adv.get("requirements", [])) != 1:
            raise SystemExit(f"{path}: expected one OR group of requirements")
        keys = []
        for rec in rewards:
            keys += recipe_keys(strip_ns(rec))
        items, tick, water, occupied = [], False, False, 0
        for cname in adv["requirements"][0]:
            c = adv["criteria"][cname]
            trig = strip_ns(c["trigger"])
            cond = c.get("conditions") or {}
            if trig == "recipe_unlocked":
                continue
            if trig == "tick":
                tick = True
            elif trig == "enter_block":
                if names(cond.get("blocks", [])) != ["water"] or "state" in cond:
                    raise SystemExit(f"{path}: enter_block other than water: {cond}")
                water = True
            elif trig == "inventory_changed":
                if "slots" in cond:
                    occ = (cond["slots"].get("occupied") or {}).get("min")
                    if occ is None or set(cond["slots"]) != {"occupied"}:
                        raise SystemExit(f"{path}: slots criterion {cond}")
                    occupied = occ
                preds = cond.get("items") or []
                if len(preds) > 1:
                    raise SystemExit(f"{path}: inventory_changed with {len(preds)} predicates")
                for p in preds:
                    if set(p) != {"items"}:
                        raise SystemExit(f"{path}: item predicate beyond items: {p}")
                    ids = []
                    for n in names(p["items"]):
                        if n in item_id:
                            ids.append(item_id[n])
                        else:
                            unknown.add(n)
                    items.append(sorted(set(ids)))
            else:
                raise SystemExit(f"{path}: trigger {trig} not handled")
        rules.append((path[len(prefix):-5], keys, items, tick, water, occupied))

    lines = [
        "// Code generated by scripts/gen_recipeunlocks.py. DO NOT EDIT.",
        f"// Source: {source} data/minecraft/advancement/recipes/** (+ recipe/, tags/item/).",
        f"// {len(rules)} recipe-unlock advancements.",
        "",
        "package server",
        "",
        "// recipeUnlockTable is every recipe-unlock advancement: the engine book",
        "// keys its reward stands for, and the criteria any one of which unlocks",
        "// it (has_the_recipe aside).",
        "var recipeUnlockTable = []recipeUnlockRule{",
    ]
    for name, keys, items, tick, water, occupied in rules:
        parts = [f"adv: {json.dumps(name)}", "keys: []string{" + ", ".join(json.dumps(k) for k in keys) + "}"]
        if items:
            parts.append("items: [][]int32{" + ", ".join("{" + ", ".join(str(i) for i in s) + "}" for s in items) + "}")
        if tick:
            parts.append("tick: true")
        if water:
            parts.append("water: true")
        if occupied:
            parts.append(f"occupied: {occupied}")
        lines.append("\t{" + ", ".join(parts) + "},")
    lines.append("}")
    with open(OUT, "w") as f:
        f.write("\n".join(lines) + "\n")
    print(f"wrote {OUT}: {len(rules)} rules; items outside the registry dropped: {sorted(unknown)}")


if __name__ == "__main__":
    main()
