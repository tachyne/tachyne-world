#!/usr/bin/env python3
"""Generate internal/worldgen/blockmeta_gen.go.

This bakes the per-block facts the server is authoritative about:

  - hardness     mining time basis (-1.0 = unbreakable)
  - resistance   blast resistance (parked: no explosions yet)
  - stackSize    max stack for the block's item (64/16/1)
  - diggable     false => cannot be broken by normal mining (bedrock/barrier/…)
  - boundingBox  "empty" (pass-through) vs "block" (collides) -> Box 0/1,
                 PER STATE: an open fence gate does not collide and a closed
                 one does, snow collides from two layers up
  - harvestTools set of tool item-ids that yield drops (absent => any/hand drops)

Block states are contiguous per block (minStateId..maxStateId), so the union of
ranges covers every valid state with no gaps. Scalars go in one table sorted by
Min for binary search; harvestTools is a second, smaller table (only ~380 blocks
require a tool).

boundingBox comes from the local extract (see scripts/extract), which
asks the game per STATE. minecraft-data's schema carries ONE per block, so
every open fence gate collided (192 states) and deep snow did not (7) — a mob
would not walk through a gate it had just opened, and a drift was walked
through rather than over.

Every field comes from the game itself, through the local extract (see
scripts/extract), read by running the server rather than taken from a
third-party summary:

  hardness, resistance   Block.defaultDestroyTime / getExplosionResistance
  diggable               breakable (hardness >= 0) and not air — air has
                         hardness 0.0 and is not dug
  stackSize              the stack size of the block's ITEM as the game maps
                         it (Block.asItem): a wall sign is the sign item, 16
  harvestTools           for a block that needs the right tool
                         (requiresCorrectToolForDrops), every tool whose TOOL
                         component calls it correct — evaluated the way
                         Tool.isCorrectForDrops does, first rule with a verdict
                         that holds the block, tags resolved from the jar

A block that needs a tool no tool satisfies (command blocks, the jigsaw, the
structure block — all unbreakable) gets no harvest row, as before.

    python3 scripts/gen_blockmeta.py [version]
"""
import io, json, os, subprocess, sys, zipfile

VER = sys.argv[1] if len(sys.argv) > 1 else "1.21.11"
SRC = os.path.expanduser("~/vanilla/extract/%s.json" % VER)
JAR = os.path.expanduser("~/vanilla/server-%s.jar" % VER)
OUT = os.path.join(os.path.dirname(__file__), "..", "internal", "worldgen", "blockmeta_gen.go")


def block_tags():
    """Resolve a block tag (nested tags included) from the jar's datapack."""
    outer = zipfile.ZipFile(JAR)
    inner = zipfile.ZipFile(io.BytesIO(outer.read(next(
        n for n in outer.namelist() if n.startswith("META-INF/versions/") and n.endswith(".jar")))))
    pre = "data/minecraft/tags/block/"
    raw = {n[len(pre):-len(".json")]: json.loads(inner.read(n))["values"]
           for n in inner.namelist() if n.startswith(pre) and n.endswith(".json")}
    memo = {}

    def resolve(tag):
        if tag in memo:
            return memo[tag]
        memo[tag] = set()  # guards a cycle
        out = set()
        for v in raw.get(tag, []):
            ref = v["id"] if isinstance(v, dict) else v
            name = ref.lstrip("#").removeprefix("minecraft:")
            out |= resolve(name) if ref.startswith("#") else {name}
        memo[tag] = out
        return out
    return resolve


extract = json.load(open(SRC))
blocks = extract["blocks"]
item_stack = {i["name"]: i["stackSize"] for i in extract["items"]}
tags = block_tags()
tools = [i for i in extract["items"] if i.get("toolRules")]


def harvest_tools(block):
    ok = []
    for t in tools:
        for r in t["toolRules"]:
            if block in (tags(r["tag"]) if "tag" in r else set(r["blocks"])):
                if r["correct"]:
                    ok.append(t["id"])
                break
    return sorted(ok)


scalar = []   # (min, max, hardness, resistance, stack, diggable, box)
harvest = []  # (min, max, [tool item ids])
tall = []     # (min, max) fences/walls/fence gates — 1.5-block collision height
for b in blocks:
    mn, mx = b["minStateId"], b["maxStateId"]
    hardness = b["hardness"]
    diggable = hardness >= 0 and not b["isAir"]
    stack = item_stack[b["item"]]
    boxes = [1 if x == "block" else 0 for x in b["boundingBox"]]
    if mx - mn + 1 != len(boxes):
        raise SystemExit("%s: state count disagrees with boundingBox count" % b["name"])
    # One row per RUN of equal collision within the block: the other four
    # fields are per-block, so a block only splits where its box changes.
    start = 0
    for i in range(1, len(boxes) + 1):
        if i == len(boxes) or boxes[i] != boxes[start]:
            scalar.append((mn + start, mn + i - 1, float(hardness), float(b["resistance"]),
                           int(stack), bool(diggable), boxes[start]))
            start = i
    if b["requiresTool"]:
        ht = harvest_tools(b["name"])
        if ht:
            harvest.append((mn, mx, ht))
    # Fences, fence gates and walls have a 1.5-block-tall collision box in vanilla,
    # so mobs (and players) can't step or jump over a single one. Detect by name
    # suffix: barrier "*_wall" blocks all end in _wall (wall_torch/_sign/_banner
    # start with wall_, so they're excluded), fences end in _fence(_gate).
    n = b["name"]
    if n.endswith("_fence") or n.endswith("_fence_gate") or n.endswith("_wall"):
        tall.append((mn, mx))
scalar.sort()
harvest.sort()
tall.sort()


def gofloat(x):
    # Render a float that Go parses exactly; keep it simple/readable.
    s = repr(x)
    return s


L = [
    "// Code generated by scripts/gen_blockmeta.py; DO NOT EDIT.",
    "",
    "package worldgen",
    "",
    "// blockMeta carries the per-block facts, taken from the game itself, that",
    "// the server enforces: hardness (mining-time basis; -1 = unbreakable),",
    "// resistance (blast; currently unused), Stack (max stack of the block item),",
    "// Diggable (false => unbreakable by normal mining), and Box (1 = collides,",
    "// 0 = pass-through). Ranges cover every state; sorted by Min for binary search.",
    "var blockMeta = []struct {",
    "\tMin, Max            uint32",
    "\tHardness, Resistance float32",
    "\tStack               uint8",
    "\tDiggable            bool",
    "\tBox                 uint8",
    "}{",
]
for mn, mx, hd, rs, st, dg, bx in scalar:
    L.append(f"\t{{{mn}, {mx}, {gofloat(hd)}, {gofloat(rs)}, {st}, {str(dg).lower()}, {bx}}},")
L += [
    "}",
    "",
    "// harvestRanges lists, for blocks that require a tool to drop loot, the set of",
    "// tool item-ids that succeed. A block absent here drops by hand/any tool.",
    "var harvestRanges = []struct {",
    "\tMin, Max uint32",
    "\tTools    []uint16",
    "}{",
]
for mn, mx, tools in harvest:
    ids = ", ".join(str(t) for t in tools)
    L.append(f"\t{{{mn}, {mx}, []uint16{{{ids}}}}},")
L += [
    "}",
    "",
    "// tallCollision lists the state ranges of fences, fence gates and walls, which",
    "// have a 1.5-block-tall collision box in vanilla — a single one blocks a mob or",
    "// player from stepping/jumping over. Sorted by Min for binary search.",
    "var tallCollision = []struct{ Min, Max uint32 }{",
]
for mn, mx in tall:
    L.append(f"\t{{{mn}, {mx}}},")
L += [
    "}",
    "",
    "// IsTallCollision reports whether a block is a fence, fence gate or wall — a",
    "// 1.5-block-tall obstacle non-flying entities cannot step or jump over.",
    "func IsTallCollision(state uint32) bool {",
    "\tlo, hi := 0, len(tallCollision)-1",
    "\tfor lo <= hi {",
    "\t\tmid := (lo + hi) / 2",
    "\t\tr := tallCollision[mid]",
    "\t\tswitch {",
    "\t\tcase state < r.Min:",
    "\t\t\thi = mid - 1",
    "\t\tcase state > r.Max:",
    "\t\t\tlo = mid + 1",
    "\t\tdefault:",
    "\t\t\treturn true",
    "\t\t}",
    "\t}",
    "\treturn false",
    "}",
    "",
    "// metaFor binary-searches the scalar block-meta table for a state.",
    "func metaFor(state uint32) (hardness, resistance float32, stack uint8, diggable bool, box uint8, ok bool) {",
    "\tlo, hi := 0, len(blockMeta)-1",
    "\tfor lo <= hi {",
    "\t\tmid := (lo + hi) / 2",
    "\t\tr := blockMeta[mid]",
    "\t\tswitch {",
    "\t\tcase state < r.Min:",
    "\t\t\thi = mid - 1",
    "\t\tcase state > r.Max:",
    "\t\t\tlo = mid + 1",
    "\t\tdefault:",
    "\t\t\treturn r.Hardness, r.Resistance, r.Stack, r.Diggable, r.Box, true",
    "\t\t}",
    "\t}",
    "\treturn 0, 0, 64, true, 0, false",
    "}",
    "",
    "// Hardness returns the block's mining-time basis (-1 = unbreakable, 0 = instant).",
    "func Hardness(state uint32) float32 { h, _, _, _, _, _ := metaFor(state); return h }",
    "",
    "// Resistance returns blast resistance (currently unused; no explosions yet).",
    "func Resistance(state uint32) float32 { _, r, _, _, _, _ := metaFor(state); return r }",
    "",
    "// Diggable reports whether the block can be broken by normal mining.",
    "func Diggable(state uint32) bool { _, _, _, d, _, _ := metaFor(state); return d }",
    "",
    "// StackSizeState returns the max stack of the block's item (64/16/1).",
    "func StackSizeState(state uint32) int { _, _, s, _, _, _ := metaFor(state); return int(s) }",
    "",
    "// Collides reports whether the block has a solid (non-empty) bounding box, i.e.",
    "// an entity collides with it. Pass-through blocks (air, plants, torches) are false.",
    "// NOTE: 'block' includes non-full shapes (stairs/slabs/fences); for the full-cube",
    "// test fences use to connect, use IsSolidFull instead.",
    "func Collides(state uint32) bool { _, _, _, _, b, _ := metaFor(state); return b == 1 }",
    "",
    "// HarvestableBy reports whether breaking the block with the given held item id",
    "// yields its drops. Blocks with no tool requirement drop for any item (incl. an",
    "// empty hand, item 0). Blocks that require a tool drop only when held is in the set.",
    "func HarvestableBy(state uint32, held uint16) bool {",
    "\tlo, hi := 0, len(harvestRanges)-1",
    "\tfor lo <= hi {",
    "\t\tmid := (lo + hi) / 2",
    "\t\tr := harvestRanges[mid]",
    "\t\tswitch {",
    "\t\tcase state < r.Min:",
    "\t\t\thi = mid - 1",
    "\t\tcase state > r.Max:",
    "\t\t\tlo = mid + 1",
    "\t\tdefault:",
    "\t\t\tfor _, t := range r.Tools {",
    "\t\t\t\tif t == held {",
    "\t\t\t\t\treturn true",
    "\t\t\t\t}",
    "\t\t\t}",
    "\t\t\treturn false",
    "\t\t}",
    "\t}",
    "\treturn true // no tool requirement",
    "}",
    "",
]
with open(OUT, "w") as f:
    f.write("\n".join(L))
subprocess.run(["gofmt", "-w", OUT], check=True)  # keep the generated file gofmt-clean
print(f"wrote {OUT}: {len(scalar)} scalar rows, {len(harvest)} harvest rows, {len(tall)} tall-collision rows")
