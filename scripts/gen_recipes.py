#!/usr/bin/env python3
"""Generate internal/server/recipes_gen.go from vanilla's own recipe files.

Crafting recipes come straight from the server jar's data/minecraft/recipe/
(crafting_shaped, crafting_shapeless and crafting_transmute), each keeping its
vanilla NAME — the key vanilla's recipe book stores.

Each grid cell or shapeless ingredient is an INGREDIENT: the set of items that
may fill it, with tags such as #minecraft:planks expanded. A crafting table is
one recipe whose four cells each accept any planks, so oak and spruce may be
mixed, as in vanilla. This used to be built from a dataset that pre-expanded
tags into uniform per-material recipes (twelve crafting tables, one per wood),
and since the matcher compared each cell against one exact id, mixing
materials crafted nothing.

Identical sets are stored once, in ingredientSets; recipes refer to them by
index, 0 being the empty set (an empty cell in a shaped pattern).

The crafting_special_* recipes (map cloning, firework stars, …) and the
decorated pot are code, not data, and are handled in craftspecial.go.
crafting_transmute recipes are listed here for the recipe book, but crafting
one is resolved by transmuteMatch first, which carries the contents across.

    python3 scripts/gen_recipes.py [version]     # default: scripts/canon.py
"""
import canon
import io
import json
import os
import subprocess
import sys
import zipfile

import vanillareport

VER = sys.argv[1] if len(sys.argv) > 1 else canon.VERSION  # recipes read 26.x as is (only defaults are omitted)
JAR = os.path.expanduser("~/vanilla/server-%s.jar" % VER)
OUT = os.path.join(os.path.dirname(__file__), "..", "internal", "server", "recipes_gen.go")

outer = zipfile.ZipFile(JAR)
inner = zipfile.ZipFile(io.BytesIO(outer.read(
    next(n for n in outer.namelist() if n.startswith("META-INF/versions/") and n.endswith(".jar")))))

item_id = {e["name"]: e["id"] for e in vanillareport.registry(canon.VERSION, "item")}


def ns(ref):
    return ref.removeprefix("minecraft:")


_tags = {}


def tag_items(tag):
    """Every registered item in an item tag, nested tags included."""
    if tag in _tags:
        return _tags[tag]
    _tags[tag] = set()  # guards a cycle
    path = "data/minecraft/tags/item/%s.json" % ns(tag)
    out = set()
    for v in json.loads(inner.read(path))["values"]:
        ref = v["id"] if isinstance(v, dict) else v
        if ref.startswith("#"):
            out |= tag_items(ref[1:])
        elif ns(ref) in item_id:
            out.add(item_id[ns(ref)])
    _tags[tag] = out
    return out


def ingredient(ing):
    """An ingredient as a sorted tuple of item ids. Items outside the registry
    (feature-flagged extras the jar carries) are dropped."""
    refs = ing if isinstance(ing, list) else [ing]
    out = set()
    for r in refs:
        if r.startswith("#"):
            out |= tag_items(r[1:])
        elif ns(r) in item_id:
            out.add(item_id[ns(r)])
    return tuple(sorted(out))


sets = {(): 0}  # ingredient tuple -> index; index 0 is the empty set


def set_index(t):
    if t not in sets:
        sets[t] = len(sets)
    return sets[t]


def shrink(rows):
    """ShapedRecipePattern.shrink: drop the all-blank rows at the top and bottom
    and trim every row to the columns something occupies. Vanilla pads patterns
    with spaces (a spyglass is " # ", " X ", " X ") and matches the trimmed one;
    keeping the padding would make the recipe unmatchable, since a grid is
    matched on its own trimmed bounding box."""
    first = min((len(r) - len(r.lstrip(" ")) for r in rows), default=0)
    last = max((len(r.rstrip(" ")) - 1 for r in rows), default=-1)
    used = [i for i, r in enumerate(rows) if r.strip(" ")]
    if not used:
        return []
    return [r[first:last + 1] for r in rows[used[0]:used[-1] + 1]]


def result_of(r):
    res = r["result"]
    if isinstance(res, str):
        return ns(res), 1
    return ns(res["id"]), res.get("count", 1)


shaped, shapeless, skipped = [], [], []
prefix = "data/minecraft/recipe/"
for path in sorted(n for n in inner.namelist() if n.startswith(prefix) and n.endswith(".json")):
    name = path[len(prefix):-len(".json")]
    r = json.loads(inner.read(path))
    kind = r["type"].removeprefix("minecraft:")
    if kind not in ("crafting_shaped", "crafting_shapeless", "crafting_transmute"):
        continue
    if kind == "crafting_transmute" and not r.get("result"):
        # 26.3's map cloning: a transmute whose result is a copy of its input.
        # The engine crafts it as code (craftspecial.go), as it did the
        # special recipe it replaced.
        skipped.append((name, "result copies the input (craftspecial.go)"))
        continue
    res, count = result_of(r)
    if res not in item_id:
        skipped.append((name, "result " + res))
        continue
    if kind == "crafting_shaped":
        rows = shrink(r["pattern"])
        w, h = max(len(row) for row in rows), len(rows)
        cells = []
        for row in rows:
            for ch in row.ljust(w):
                cells.append(() if ch == " " else ingredient(r["key"][ch]))
        if any(ch != " " and not cells[i] for i, ch in enumerate("".join(x.ljust(w) for x in rows))):
            skipped.append((name, "an ingredient with no registered item"))
            continue
        shaped.append((name, w, h, [set_index(c) for c in cells], item_id[res], count))
    else:
        ings = ([r["input"], r["material"]] if kind == "crafting_transmute" else r["ingredients"])
        ings = [ingredient(i) for i in ings]
        if not all(ings):
            skipped.append((name, "an ingredient with no registered item"))
            continue
        shapeless.append((name, sorted(set_index(i) for i in ings), item_id[res], count,
                          kind == "crafting_transmute"))

ordered_sets = [t for t, _ in sorted(sets.items(), key=lambda kv: kv[1])]

L = [
    "// Code generated by scripts/gen_recipes.py; DO NOT EDIT.",
    "",
    "package server",
    "",
    "// ingredientSets are the distinct sets of items a recipe slot accepts, each",
    "// sorted; recipes refer to them by index. Index 0 is the empty set — an empty",
    "// cell in a shaped pattern.",
    "var ingredientSets = [][]int32{",
]
for t in ordered_sets:
    L.append("\t{%s}," % ", ".join(map(str, t)))
L += [
    "}",
    "",
    "// shapedRecipes: vanilla's crafting_shaped recipes, by name. Cells is the",
    "// pattern row-major as ingredientSets indices. A grid matches when its",
    "// non-empty bounding box has the pattern's size and every cell holds an item",
    "// its set accepts, in either orientation (vanilla allows the mirror).",
    "var shapedRecipes = []struct {",
    "\tName   string",
    "\tW, H   uint8",
    "\tCells  []uint16",
    "\tResult int32",
    "\tCount  uint8",
    "}{",
]
for name, w, h, cells, res, cnt in shaped:
    L.append('\t{"%s", %d, %d, []uint16{%s}, %d, %d},' % (name, w, h, ", ".join(map(str, cells)), res, cnt))
L += [
    "}",
    "",
    "// shapelessRecipes: vanilla's crafting_shapeless and crafting_transmute",
    "// recipes, by name. A grid matches when its non-empty cells can be paired",
    "// off one-to-one with these ingredients, each item going to a set that",
    "// accepts it.",
    "//",
    "// Transmute recipes are here for the recipe book only and are never matched:",
    "// vanilla's TransmuteRecipe refuses an input that is already the result and",
    "// carries the input's contents across, neither of which a plain shapeless",
    "// recipe can say — transmuteMatch does both.",
    "var shapelessRecipes = []struct {",
    "\tName        string",
    "\tIngredients []uint16 // ingredientSets indices, sorted",
    "\tResult      int32",
    "\tCount       uint8",
    "\tTransmute   bool",
    "}{",
]
for name, ings, res, cnt, tm in shapeless:
    L.append('\t{"%s", []uint16{%s}, %d, %d, %s},' % (name, ", ".join(map(str, ings)), res, cnt, "true" if tm else "false"))
L += ["}", ""]

with open(OUT, "w") as f:
    f.write("\n".join(L))
subprocess.run(["gofmt", "-w", OUT], check=True)
print("%s: %d shaped + %d shapeless recipes, %d ingredient sets, %d skipped -> %s"
      % (VER, len(shaped), len(shapeless), len(ordered_sets), len(skipped), os.path.normpath(OUT)))
for name, why in skipped:
    print("   skipped %-40s %s" % (name, why))
