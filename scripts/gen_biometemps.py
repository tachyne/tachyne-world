#!/usr/bin/env python3
"""Generate internal/worldgen/biometemps_gen.go — each biome's base
temperature and whether it carries the FROZEN temperature modifier, from the
canonical version's worldgen/biome/*.json (scripts/canon.py).

    python3 scripts/gen_biometemps.py
"""
import json
import os
import subprocess

import canon

OUT = os.path.join(os.path.dirname(__file__), "..", "internal", "worldgen", "biometemps_gen.go")
z = canon.inner_jar()
pre = "data/minecraft/worldgen/biome/"
temps, frozen = {}, []
for n in sorted(z.namelist()):
    if not n.startswith(pre) or not n.endswith(".json"):
        continue
    b = json.loads(z.read(n))
    name = "minecraft:" + n[len(pre):-5]
    temps[name] = b["temperature"]
    if b.get("temperature_modifier", "none") == "frozen":
        frozen.append(name)

L = ["package worldgen", "",
     "// Code generated from the vanilla biome reports (worldgen/biome/*.json",
     "// \"temperature\" and \"temperature_modifier\"); DO NOT EDIT.", "",
     "// biomeTemperature is each biome's base temperature (Biome.climateSettings).",
     "var biomeTemperature = map[string]float64{"]
L += ['\t"%s": %s,' % (k, repr(float(v))) for k, v in sorted(temps.items())]
L += ["}", "", "// frozenModifier marks the biomes with the FROZEN temperature modifier.",
      "var frozenModifier = map[string]bool{"]
L += ['\t"%s": true,' % k for k in frozen]
L += ["}", ""]
with open(OUT, "w") as f:
    f.write("\n".join(L))
subprocess.run(["gofmt", "-w", OUT], check=True)
print(f"wrote {os.path.normpath(OUT)}: {len(temps)} biomes, {len(frozen)} frozen")
