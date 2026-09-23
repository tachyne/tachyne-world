#!/usr/bin/env python3
"""Generate internal/server/smelting_gen.go: cooker recipes + fuel burn times.

Recipes come from the canonical version's server jar datapack (recipe/*.json) —
four cooker types: minecraft:smelting (furnace), minecraft:blasting (blast
furnace), minecraft:smoking (smoker), minecraft:campfire_cooking (campfire).
Ingredients may be item names or #tags; tags resolve transitively via the
jar's tags/item/*.json. Result counts are always 1; each recipe's cook time
(`cookingtime`, required since 26.x) is kept as written — blasting and smoking
recipes say 200 like smelting, and the fast cookers' 2x speed comes from the
fuel.

Fuel is an item component since 26.x (`minecraft:cooking_fuel`, in the items'
component reports): a burn time and a speed multiplier, each a context
provider resolved against the cooker block — a blast furnace or smoker
(the `block/fast_cooking` predicate) halves the burn time and doubles the
speed. Both are resolved here for each of the three furnace blocks.

Run: python3 scripts/gen_smelting.py [path-to-server.jar]
(item ids from vanilla's registries report, via scripts/vanillareport.py)
"""
import canon
import io, json, sys, os, subprocess, zipfile
import vanillareport

JAR = sys.argv[1] if len(sys.argv) > 1 else canon.jar()
OUT = os.path.join(os.path.dirname(__file__), "..", "internal", "server", "smelting_gen.go")

item_id = {i["name"]: i["id"] for i in vanillareport.registry(canon.VERSION, "item")}

# The bundler jar nests the real server jar.
outer = zipfile.ZipFile(JAR)
inner_name = [n for n in outer.namelist() if n.startswith("META-INF/versions/") and n.endswith(".jar")]
z = zipfile.ZipFile(io.BytesIO(outer.read(inner_name[0]))) if inner_name else outer

recipes, tags = {}, {}
for path in z.namelist():
    if path.startswith("data/minecraft/recipe/") and path.endswith(".json"):
        recipes[path] = json.loads(z.read(path))
    elif path.startswith("data/minecraft/tags/item/") and path.endswith(".json"):
        key = path.removeprefix("data/minecraft/tags/item/").removesuffix(".json")
        tags[key] = json.loads(z.read(path))

tag_cache = {}


def resolve_tag(name, depth=0):
    """#minecraft:logs_that_burn -> flat list of item names."""
    if depth > 8:
        return []
    key = name.lstrip("#").removeprefix("minecraft:")
    if key in tag_cache:
        return tag_cache[key]
    if key not in tags:
        return []
    out = []
    for v in tags[key]["values"]:
        v = v["id"] if isinstance(v, dict) else v
        if v.startswith("#"):
            out += resolve_tag(v, depth + 1)
        else:
            out.append(v.removeprefix("minecraft:"))
    tag_cache[key] = out
    return out


COOKERS = [  # (recipe type, go map name, unused, doc)
    ("minecraft:smelting", "smeltResult", None, "furnace"),
    ("minecraft:blasting", "blastResult", None, "blast furnace"),
    ("minecraft:smoking", "smokeResult", None, "smoker"),
    ("minecraft:campfire_cooking", "campfireResult", None, "campfire"),
]

# The recipe book tab an entry lands on. Ids are the recipe_book_category
# registry's registration order (crafting_building_blocks 0 … campfire 12);
# the mapping per cooker is the recipeBookCategory() each cooking recipe
# class returns, keyed by the recipe json's own "category" field.
BOOK_CATEGORY = {
    "minecraft:smelting": {"blocks": 5, "food": 4, "misc": 6},
    "minecraft:blasting": {"blocks": 7, "food": 8, "misc": 8},
    "minecraft:smoking": {"blocks": 9, "food": 9, "misc": 9},
    "minecraft:campfire_cooking": {"blocks": 12, "food": 12, "misc": 12},
}
tables = {t: [] for t, _, _, _ in COOKERS}
for path, r in sorted(recipes.items()):
    for rtype, _, default, _ in COOKERS:
        if r.get("type") != rtype:
            continue
        result = r["result"]["id"].removeprefix("minecraft:")
        if result not in item_id:
            continue
        cook = r["cookingtime"]
        # Each recipe carries its own experience; the engine used to guess
        # 0.7 (0.35 for food), which paid the same for cactus green and
        # ancient debris.
        xp = float(r.get("experience", 0.0))
        cat = BOOK_CATEGORY[rtype][r.get("category", "misc")]
        ing = r["ingredient"]
        ings = [ing] if isinstance(ing, str) else ing
        names = []
        for i in ings:
            if i.startswith("#"):
                names += resolve_tag(i)
            else:
                names.append(i.removeprefix("minecraft:"))
        for n in names:
            if n in item_id:
                tables[rtype].append((item_id[n], item_id[result], cook, xp, cat))
for rows in tables.values():
    rows.sort()

# Fuel: each item's cooking_fuel component, resolved per furnace block.
# The order is the engine's cooker kinds (cookFurnace, cookBlast, cookSmoker).
FURNACES = ["furnace", "blast_furnace", "smoker"]


def data(kind, key):
    ns, _, path = key.partition(":")
    return json.loads(z.read(f"data/{ns}/{kind}/{path}.json"))


def condition(c, block):
    if isinstance(c, str):
        c = data("predicate", c)
    if c.get("type") != "minecraft:match_block":
        raise SystemExit(f"cooking fuel: unhandled condition {c}")
    blocks = c["blocks"]
    blocks = [blocks] if isinstance(blocks, str) else blocks
    if any(b.startswith("#") for b in blocks):
        raise SystemExit(f"cooking fuel: block tag in {c}")
    return "minecraft:" + block in blocks


def provide(p, kind, block):
    """A context_int/float_provider's value with the cooker block as context."""
    if isinstance(p, (int, float)):
        return p
    if isinstance(p, str):
        return provide(data(kind, p), kind, block)
    t = p.get("type")
    if t == "minecraft:constant":
        return p["value"]
    if t == "minecraft:div":
        l, r = provide(p["left"], kind, block), provide(p["right"], kind, block)
        return int(l / r) if kind == "context_int_provider" else l / r  # Java int division
    if t == "minecraft:conditional":
        return provide(p["on_true" if condition(p["condition"], block) else "on_false"], kind, block)
    raise SystemExit(f"cooking fuel: unhandled provider {p}")


COMPONENTS = canon.report("reports/minecraft/components/item")
if not os.path.isdir(COMPONENTS):
    raise SystemExit(f"{COMPONENTS}: no item component reports (run the server's --reports)")
fuel_rows = []
for name, iid in item_id.items():
    comps = json.load(open(os.path.join(COMPONENTS, name + ".json")))["components"]
    fuel = comps.get("minecraft:cooking_fuel")
    if fuel is None:
        continue
    burn = [provide(fuel["burn_time"], "context_int_provider", b) for b in FURNACES]
    speed = [provide(fuel["speed_multiplier"], "context_float_provider", b) for b in FURNACES]
    fuel_rows.append((iid, burn, speed))
fuel_rows.sort()

L = [
    "// Code generated by scripts/gen_smelting.py; DO NOT EDIT.",
    "",
    "package server",
    "",
    "// cookEntry is one cooker recipe: output item, cook time in ticks, the",
    "// experience it banks (the recipe's own `experience` field) and the recipe",
    "// book tab it is filed under (a recipe_book_category registry index).",
    "type cookEntry struct {",
    "\tOut  int32",
    "\tCook int",
    "\tXP   float64",
    "\tCat  int32",
    "}",
]
for rtype, goname, _, doc in COOKERS:
    L += [
        "",
        f"// {goname} maps a {doc} input item to its cooked output + cook ticks.",
        f"var {goname} = map[int32]cookEntry{{",
    ]
    for inp, out, cook, xp, cat in tables[rtype]:
        L.append(f"\t{inp}: {{{out}, {cook}, {xp:g}, {cat}}},")
    L.append("}")
L += [
    "",
    "// cookingFuel is a fuel item's cooking_fuel component, resolved for each",
    "// furnace block (indexed by cooker kind: furnace, blast furnace, smoker):",
    "// how long one burns (ticks) and how much faster it cooks.",
    "type cookingFuel struct {",
    "\tBurn  [3]int",
    "\tSpeed [3]float32",
    "}",
    "",
    "// fuels maps a fuel item to its cooking_fuel component.",
    "var fuels = map[int32]cookingFuel{",
]
for iid, burn, speed in fuel_rows:
    L.append("\t%d: {[3]int{%s}, [3]float32{%s}}," % (iid, ", ".join(map(str, burn)), ", ".join("%g" % s for s in speed)))
L += ["}", ""]
with open(OUT, "w") as f:
    f.write("\n".join(L))
subprocess.run(["gofmt", "-w", OUT], check=True)
print(f"wrote {OUT}: " + ", ".join(f"{len(tables[t])} {d}" for t, _, _, d in COOKERS) + f", {len(fuel_rows)} fuels")
