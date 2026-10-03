#!/usr/bin/env python3
"""Generate internal/worldgen/vanillaoffset_gen.go — the overworld offset
splines (density_function/overworld/offset.json and its amplified twin) that
the vanilla biome source's depth climate value reads, from the canonical
version's data (scripts/canon.py).

The numbers are kept as the JSON spells them so Go rounds each one straight
to float32, as the game's codec does.

    python3 scripts/gen_vanillaoffset.py
"""
import json
import os
import subprocess

import canon

OUT = os.path.join(os.path.dirname(__file__), "..", "internal", "worldgen", "vanillaoffset_gen.go")
z = canon.inner_jar()

COORDS = {
    "minecraft:overworld/continents": "vbcContinents",
    "minecraft:overworld/erosion": "vbcErosion",
    "minecraft:overworld/ridges_folded": "vbcRidgesFolded",
}


def spline(name):
    d = json.loads(z.read(f"data/minecraft/worldgen/density_function/{name}.json"), parse_float=str, parse_int=str)
    # cache(lerp(blend_alpha, blend_offset, add(-0.50375, spline)))
    add = d["input"]["second"]
    assert d["type"] == "minecraft:cache" and add["type"] == "minecraft:add", name
    assert add["right"]["type"] == "minecraft:spline", name
    return add["left"], add["right"]["spline"]


def emit(s, ind):
    if not isinstance(s, dict):
        return f"vbK({s})"
    pad = "\t" * (ind + 1)
    locs = ", ".join(p["location"] for p in s["points"])
    ders = ", ".join(p["derivative"] for p in s["points"])
    vals = (",\n" + pad + "\t").join(emit(p["value"], ind + 2) for p in s["points"])
    return (f"&vbSpline{{coord: {COORDS[s['coordinate']]},\n{pad}locs: []float32{{{locs}}},\n"
            f"{pad}ders: []float32{{{ders}}},\n{pad}vals: []*vbSpline{{\n{pad}\t{vals},\n{pad}}}}}")


L = ["package worldgen", "",
     "// Code generated from the vanilla density functions overworld/offset and",
     "// overworld_amplified/offset by scripts/gen_vanillaoffset.py; DO NOT EDIT.", ""]
for var, name in (("vbOffsetNormal", "overworld/offset"), ("vbOffsetAmplified", "overworld_amplified/offset")):
    base, s = spline(name)
    L += [f"// {var} is {name} without blending: {base} plus the spline.",
          f"var {var} = vbOffset{{base: {base}, spline: {emit(s, 0)}}}", ""]
with open(OUT, "w") as f:
    f.write("\n".join(L))
subprocess.run(["gofmt", "-w", OUT], check=True)
print(f"wrote {os.path.normpath(OUT)}")
