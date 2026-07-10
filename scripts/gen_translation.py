#!/usr/bin/env python3
"""Generate internal/protocol/translation_gen.go: the unified multi-version registry
translation tables (ViaVersion-style MappingData).

Newer versions insert blocks/entities/items earlier in their registries, shifting
the IDs of everything after them. We send the world in canonical 1.21.5 (770) IDs;
a translated client decodes them against its own registry. This emits, per registry
and per client protocol version, the ID delta for each run of canonical IDs that
moved — so the translation layer can rewrite IDs in either direction with one API.

Every registry is reduced to the same shape: a canonical-ID → delta array, run-
length-encoded into (min, max, delta) ranges. "Ranged" registries (block states)
get their delta from each block's state-range shift; "flat" registries (entities,
items, biomes) get it per ID by resource name.

Run outside the sandbox (needs network):  python3 scripts/gen_translation.py
"""
import json, urllib.request, os

BASE = "https://raw.githubusercontent.com/PrismarineJS/minecraft-data/master/data/pc/{}/{}.json"
OUT = os.path.join(os.path.dirname(__file__), "..", "..", "tachyne-common", "protocol", "translation_gen.go")

CANON = "1.21.11"  # engine content is 1.21.11 (proto 774); wire LAYOUT stays 770
# version -> (source dir/version, source kind). "md" = minecraft-data (one JSON per
# registry); "via" = ViaVersion Mappings (one JSON, list-index = ID) for the 26.x
# versions minecraft-data doesn't cover yet. The ViaVersion files are factual
# ID↔name dumps (used to derive our own delta tables, not their code).
TARGETS = {
    770: ("1.21.5", "md"), 771: ("1.21.6", "md"), 772: ("1.21.8", "md"),
    773: ("1.21.9", "md"), 775: ("26.1", "via"), 776: ("26.2", "via"),
    # 774 = 1.21.11 = canonical id version → identity, no table.
}
VIA = "https://raw.githubusercontent.com/ViaVersion/Mappings/main/mappings/mapping-{}.json"

# registry Go const name -> (minecraft-data file, ViaVersion key, kind)
REGISTRIES = [
    ("RegBlockState", "blocks", "blockstates", "stateRange"),
    ("RegEntity", "entities", "entities", "flat"),
    ("RegItem", "items", "items", "flat"),
    ("RegBiome", "biomes", None, "flat"),  # ViaVersion mappings carry no biomes
]


def fetch(ver, name):
    with urllib.request.urlopen(BASE.format(ver, name)) as r:
        return json.load(r)


_via_cache = {}


def fetch_via(ver):
    if ver not in _via_cache:
        with urllib.request.urlopen(VIA.format(ver)) as r:
            _via_cache[ver] = json.load(r)
    return _via_cache[ver]


def normalize(ver, source, mdfile, viakey, kind):
    """Return the version's registry in a uniform shape: stateRange -> list of
    {name,minStateId,maxStateId}; flat -> list of {name,id}. None if unavailable."""
    if source == "md":
        data = fetch(ver, mdfile)
        return data  # minecraft-data already has the right fields
    # ViaVersion: list where index = ID, value = name (block states are "name[props]").
    if viakey is None:
        return None
    lst = fetch_via(ver).get(viakey)
    if lst is None:
        return None
    if kind == "stateRange":
        rng = {}
        for i, entry in enumerate(lst):
            name = entry.split("[")[0]
            if name in rng:
                rng[name]["maxStateId"] = i
            else:
                rng[name] = {"name": name, "minStateId": i, "maxStateId": i}
        return list(rng.values())
    return [{"name": name, "id": i} for i, name in enumerate(lst)]


def delta_array_flat(canon, version):
    """canon/version: list of {name,id}. Returns {canon_id: delta}."""
    cby = {e["name"]: e["id"] for e in canon}
    vby = {e["name"]: e["id"] for e in version}
    out = {}
    for name, cid in cby.items():
        vid = vby.get(name)
        if vid is not None and vid != cid:
            out[cid] = vid - cid
    return out


def delta_array_states(canon, version):
    """blocks.json: each block has min/maxStateId. Every state in a block shifts by
    the same delta = version.min - canon.min. Returns {canon_state: delta}."""
    vby = {b["name"]: b for b in version}
    out = {}
    for b in canon:
        vb = vby.get(b["name"])
        if vb is None:
            continue
        # Constant-delta only holds when the block's state count is unchanged; skip
        # the rare block that gained/lost a property (it would map imperfectly).
        if (b["maxStateId"] - b["minStateId"]) != (vb["maxStateId"] - vb["minStateId"]):
            continue
        d = vb["minStateId"] - b["minStateId"]
        if d != 0:
            for s in range(b["minStateId"], b["maxStateId"] + 1):
                out[s] = d
    return out


def rle(deltas):
    """{id: delta} -> sorted [(min, max, delta)] merging consecutive ids w/ same delta."""
    ranges = []
    for cid in sorted(deltas):
        d = deltas[cid]
        if ranges and ranges[-1][2] == d and ranges[-1][1] == cid - 1:
            ranges[-1][1] = cid
        else:
            ranges.append([cid, cid, d])
    return ranges


# registry const -> version -> ranges
data = {}
for const, mdfile, viakey, kind in REGISTRIES:
    canon = fetch(CANON, mdfile)
    per = {}
    for ver, (dir, source) in sorted(TARGETS.items()):
        version = normalize(dir, source, mdfile, viakey, kind)
        if version is None:
            per[ver] = []
            continue
        deltas = delta_array_states(canon, version) if kind == "stateRange" else delta_array_flat(canon, version)
        per[ver] = rle(deltas)
        print(f"{const} 1.21.11->{ver}: {len(deltas)} ids shifted -> {len(per[ver])} ranges")
    data[const] = per

L = [
    "// Code generated by scripts/gen_translation.py; DO NOT EDIT.",
    "",
    "package protocol",
    "",
    "// IDSpace identifiers for multi-version ID translation.",
    "const (",
]
for i, (const, _, _, _) in enumerate(REGISTRIES):
    L.append(f"\t{const} IDSpace = {i}")
L += [
    "\tnumRegistries = " + str(len(REGISTRIES)),
    ")",
    "",
    "// translationTables[registry][clientProtocol] = forward ID-shift ranges (canonical",
    "// 1.21.11(774) -> client version). Sorted by Min for binary search.",
    "var translationTables = map[IDSpace]map[int32][]idRange{",
]
for const, _, _, _ in REGISTRIES:
    L.append(f"\t{const}: {{")
    for ver in sorted(data[const]):
        ranges = data[const][ver]
        if not ranges:
            continue
        inner = ", ".join(f"{{{a}, {b}, {d}}}" for a, b, d in ranges)
        L.append(f"\t\t{ver}: {{{inner}}},")
    L.append("\t},")
L += ["}", ""]

with open(OUT, "w") as f:
    f.write("\n".join(L))
print(f"wrote {OUT}")
