#!/usr/bin/env python3
"""Regenerates internal/server/stepsounds_gen.go from vanilla's block sound
types.

Three facts come out of the reference sources:

  * SoundType's constants — each one's volume, pitch and the five sound
    events (break/step/place/hit/fall). We keep step and place.
  * SoundEvents' registrations — the constant name to its resource name.
  * Blocks' registrations — which sound type each block asks for. A block
    that asks for none is STONE, and one built with ofFullCopy inherits the
    type of the block it copies.

Usage: gen_blocksounds.py [<reference server source root>]
"""
import canon
import re
import sys
import os

SRC = sys.argv[1] if len(sys.argv) > 1 else canon.src()


def statements(path):
    """Yields each `public static final …;` statement as one flat line."""
    buf = None
    for line in open(os.path.join(SRC, path)):
        line = line.strip()
        if buf is None:
            if not line.startswith("public static final"):
                continue
            buf = line
        else:
            buf += " " + line
        if buf.endswith(";"):
            yield buf
            buf = None


# SoundEvents: constant -> resource name.
events = {}
for s in statements("net/minecraft/sounds/SoundEvents.java"):
    m = re.match(r'public static final SoundEvent (\w+) = (?:SoundEvents\.)?register\("([^"]+)"', s)  # 26.x: unqualified
    if m:
        events[m.group(1)] = m.group(2)

# SoundType: constant -> (volume, pitch, step event, place event).
types = {}
for s in statements("net/minecraft/world/level/block/SoundType.java"):
    m = re.match(r"public static final SoundType (\w+) = new SoundType\((.+?)\);", s)
    if not m:
        continue
    args = [a.strip() for a in m.group(2).split(",")]
    vol, pitch = float(args[0].rstrip("fF")), float(args[1].rstrip("fF"))
    step, place = args[3].split(".")[-1], args[4].split(".")[-1]
    types[m.group(1)] = (vol, pitch, events[step], events[place])

# Blocks: block name -> sound type, resolving ofFullCopy inheritance.
direct, copies, order = {}, {}, []
for s in statements("net/minecraft/world/level/block/Blocks.java"):
    m = re.search(r'register\(\s*"([a-z_]+)"', s)
    if not m:
        continue
    name = m.group(1)
    order.append(name)
    snd = re.search(r"\.sound\(SoundType\.(\w+)\)", s)
    if snd:
        direct[name] = snd.group(1)
        continue
    cp = re.search(r"of(?:Full|Legacy)Copy\(Blocks\.(\w+)\)", s)
    if cp:
        copies[name] = cp.group(1).lower()

blocks = {}
for name in order:
    seen, cur = set(), name
    while cur in copies and cur not in seen:
        seen.add(cur)
        cur = copies[cur]
    if cur in direct:
        blocks[name] = direct[cur]


def num(v):
    return ("%g" % v)


out = ["// Code generated from vanilla's block sound types (Blocks/SoundType). DO NOT EDIT.",
       "", "package server", "",
       "// blockSoundType names every block's sound type; a block absent here is stone.",
       "var blockSoundType = map[string]string{"]
w = max(len(n) for n in blocks) + 3
for name in sorted(blocks):
    out.append('\t%-*s "%s",' % (w, '"%s":' % name, blocks[name]))
out += ["}", "",
        "// soundTypeSteps is each sound type's volume, pitch and step sound event.",
        "var soundTypeSteps = map[string]stepSound{"]
w = max(len(t) for t in types) + 3
for t in sorted(types):
    vol, pitch, step, _ = types[t]
    out.append('\t%-*s {%s, %s, "minecraft:%s"},' % (w, '"%s":' % t, num(vol), num(pitch), step))
out += ["}", "",
        "// soundTypePlaces is each sound type's place sound event. Its volume and",
        "// pitch are the same pair soundTypeSteps carries — BlockItem.place scales",
        "// them, it does not keep its own.",
        "var soundTypePlaces = map[string]string{"]
for t in sorted(types):
    out.append('\t%-*s "minecraft:%s",' % (w, '"%s":' % t, types[t][3]))
out += ["}", ""]

dest = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "internal/server/stepsounds_gen.go")
open(dest, "w").write("\n".join(out))
print("%d blocks, %d sound types -> %s" % (len(blocks), len(types), os.path.normpath(dest)))
