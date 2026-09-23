#!/usr/bin/env python3
"""Generate tachyne-common/protocol/translation_gen.go: the multi-version
registry translation tables (ViaVersion-style MappingData).

The engine's ids are its canonical content version's (CANON). A client on
another version numbers the same things differently — newer versions insert
entries earlier in a registry, shifting everything after them — so for each
registry and each served protocol this emits the id delta for every run of
canonical ids that moved, run-length-encoded into (min, max, delta) ranges,
and the translation layer rewrites ids in either direction with one API.

Every registry, for every version, comes from vanilla's own data reports
(~/vanilla/reports/<version>/, the server's --reports output), read through
scripts/vanillareport.py. Block states are "ranged" — a block's whole state
range shifts by one delta — and everything else is "flat", matched per id by
name. An entry that exists on only one side cannot be shifted: it is listed
in absentIDs (canonical ids a client lacks) or addedIDs (client ids the
canonical registry lacks), so it is dropped rather than turned into something
else.

    python3 scripts/gen_translation.py [canonical-version]     # default: scripts/canon.py
"""
import canon
import json, os, sys

import standins
import vanillareport

OUT = os.path.join(os.path.dirname(__file__), "..", "..", "tachyne-common", "protocol", "translation_gen.go")

# client protocol -> the version whose reports describe it.
PROTOCOLS = {
    770: "1.21.5", 771: "1.21.6", 772: "1.21.8", 773: "1.21.9",
    774: "1.21.11", 775: "26.1", 776: "26.2", 777: "26.3",
}
CANON = sys.argv[1] if len(sys.argv) > 1 else canon.VERSION  # content ids; the wire LAYOUT stays 770
CANON_PROTO = next(p for p, v in PROTOCOLS.items() if v == CANON)
# The canonical version is the identity and gets no table.
TARGETS = {p: v for p, v in PROTOCOLS.items() if v != CANON}

# (Go const, kind, report registry). The order fixes the IDSpace numbering.
REGISTRIES = [
    ("RegBlockState", "stateRange", "block"),
    ("RegEntity", "flat", "entity_type"),
    ("RegItem", "flat", "item"),
    # Biome ids never need translating: a client learns them from the biome
    # registry the server itself sends at configuration, so they mean the same
    # thing on every version by construction. The IDSpace stays so the
    # numbering does not move; its table is always empty.
    ("RegBiome", "flat", None),
    # award_stats key registries (see render770/stats.go):
    ("RegBlock", "flat", "block"),
    ("RegCustomStat", "flat", "custom_stat"),
    # update_attributes carries attribute registry ids, and the registry grew
    # between served versions (32 on 770, 35 canonical, 40 on 776) — so the ids
    # shift and the packet is meaningless to a client without this table.
    ("RegAttribute", "flat", "attribute"),
]


def load(ver, kind, registry):
    if registry is None:
        return []
    if kind == "stateRange":
        return vanillareport.blocks(ver)
    return vanillareport.registry(ver, registry)


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


def absent_flat(canon, version):
    """Canonical ids with NO counterpart on the target version.

    A shift range cannot express "this does not exist here": it would map the
    id onto whatever now occupies that slot, which is a DIFFERENT registry
    entry. The sender has to drop these instead, so they are emitted
    separately."""
    vby = {e["name"] for e in version}
    return sorted(e["id"] for e in canon if e["name"] not in vby)


def added_flat(canon, version):
    """Version ids with NO counterpart in canonical — the SERVERBOUND mirror
    of absent_flat.

    The reverse shift table has the same blind spot in the other direction: a
    client id the engine's registry never had still falls inside some range
    and comes back as whatever canonical entry sits there. That is how a 26.3
    client putting poplar planks in its hotbar had the world store redstone
    ore. The receiver has to drop these rather than shift them."""
    cby = {e["name"] for e in canon}
    return sorted(e["id"] for e in version if e["name"] not in cby)


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


def report_states(ver):
    """{block name: ([(state id, {prop: value})], default state id)} from blocks.json."""
    raw = json.load(open(canon.report("blocks.json", ver)))
    out = {}
    for key, b in raw.items():
        sts = [(st["id"], st.get("properties", {})) for st in b["states"]]
        out[key.removeprefix("minecraft:")] = (sts, next(st["id"] for st in b["states"] if st.get("default")))
    return out


def absent_states(canon_states, version_blocks):
    """For each canonical block the version lacks: its states and what each is
    shown as — a canonical state of the stand-in (it then shifts like any other
    state), or None for air. The stand-in takes every property value it shares
    and its own default for the rest. Returns [(lo, hi, delta or None)] runs,
    where a state s is shown as s + delta."""
    have = {b["name"] for b in version_blocks}
    subs = {}
    for name, (sts, _) in canon_states.items():
        if name in have:
            continue
        stand = standins.BLOCKS.get(name)
        if stand is not None and stand not in have:
            stand = None  # an older client lacks the stand-in too: air
        for sid, props in sts:
            if stand is None:
                subs[sid] = None
                continue
            ssts, sdef = canon_states[stand]
            want = standins.props(props, [p for _, p in ssts], next(p for i, p in ssts if i == sdef))
            subs[sid] = next(i for i, p in ssts if p == want) - sid
    runs = []
    for sid in sorted(subs):
        d = subs[sid]
        if runs and runs[-1][1] == sid - 1 and runs[-1][2] == d:
            runs[-1][1] = sid
        else:
            runs.append([sid, sid, d])
    return runs


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
absent = {}
added = {}
absent_state_runs = {}  # version -> [(lo, hi, delta or None)]
item_stand_ins = {}     # version -> {canonical absent item id: canonical stand-in id}
canon_states = report_states(CANON)
for const, kind, registry in REGISTRIES:
    canon = load(CANON, kind, registry)
    per = {}
    for ver, dir in sorted(TARGETS.items()):
        version = load(dir, kind, registry)
        deltas = delta_array_states(canon, version) if kind == "stateRange" else delta_array_flat(canon, version)
        per[ver] = rle(deltas)
        if kind == "stateRange":
            runs = absent_states(canon_states, version)
            if runs:
                absent_state_runs[ver] = runs
                n = sum(b - a + 1 for a, b, _ in runs)
                air = sum(b - a + 1 for a, b, d in runs if d is None)
                print(f"{const} {CANON}->{ver}: {n} states ABSENT ({n - air} shown as a stand-in, {air} as air)")
        if const == "RegItem":
            cid = {e["name"]: e["id"] for e in canon}
            have = {e["name"] for e in version}
            subs = {}  # registry names are bare ("stone")
            for a, b in standins.items_for(set(cid)).items():
                if a in cid and a not in have and b in have:
                    subs[cid[a]] = cid[b]
            if subs:
                item_stand_ins[ver] = subs
                print(f"RegItem {CANON}->{ver}: {len(subs)} absent items shown as a stand-in")
        if kind != "stateRange":
            gone = absent_flat(canon, version)
            if gone:
                absent.setdefault(const, {})[ver] = gone
                print(f"{const} {CANON}->{ver}: {len(gone)} ids ABSENT (dropped, not shifted)")
            new_ids = added_flat(canon, version)
            if new_ids:
                added.setdefault(const, {})[ver] = new_ids
                print(f"{const} {ver}->{CANON}: {len(new_ids)} ids ADDED (dropped serverbound)")
        print(f"{const} {CANON}->{ver}: {len(deltas)} ids shifted -> {len(per[ver])} ranges")
    data[const] = per

L = [
    "// Code generated by scripts/gen_translation.py; DO NOT EDIT.",
    "",
    "package protocol",
    "",
    "// IDSpace identifiers for multi-version ID translation.",
    "const (",
]
for i, (const, _, _) in enumerate(REGISTRIES):
    L.append(f"\t{const} IDSpace = {i}")
L += [
    "\tnumRegistries = " + str(len(REGISTRIES)),
    ")",
    "",
    "// translationTables[registry][clientProtocol] = forward ID-shift ranges (canonical",
    f"// {CANON}({CANON_PROTO}) -> client version). Sorted by Min for binary search.",
    "var translationTables = map[IDSpace]map[int32][]idRange{",
]
for const, _, _ in REGISTRIES:
    L.append(f"\t{const}: {{")
    for ver in sorted(data[const]):
        ranges = data[const][ver]
        if not ranges:
            continue
        inner = ", ".join(f"{{{a}, {b}, {d}}}" for a, b, d in ranges)
        L.append(f"\t\t{ver}: {{{inner}}},")
    L.append("\t},")
L += ["}", ""]

L += [
    "// absentIDs[registry][clientProtocol] = canonical IDs that DO NOT EXIST on that",
    "// version. A shift range cannot say \"missing\" — it would map the id onto",
    "// whatever occupies the slot there, silently meaning something else — so these",
    "// must be dropped by the sender instead. Sorted for binary search.",
    "var absentIDs = map[IDSpace]map[int32][]int32{",
]
for const in sorted(absent):
    L.append(f"\t{const}: {{")
    for ver in sorted(absent[const]):
        ids = ", ".join(str(i) for i in absent[const][ver])
        L.append(f"\t\t{ver}: {{{ids}}},")
    L.append("\t},")
L += ["}", ""]

L += [
    "// absentStates[clientProtocol] = runs of canonical block states that version",
    "// does not have, with what each is shown as instead: state s becomes the",
    "// canonical state s+Delta (a stand-in of the same shape, which then shifts",
    "// like any other), or air when Air is set. Sorted by Lo.",
    "var absentStates = map[int32][]absentStateRun{",
]
for ver in sorted(absent_state_runs):
    inner = ", ".join(f"{{{a}, {b}, {0 if d is None else d}, {'true' if d is None else 'false'}}}"
                      for a, b, d in absent_state_runs[ver])
    L.append(f"\t{ver}: {{{inner}}},")
L += ["}", ""]

L += [
    "// absentItemStandIns[clientProtocol] = for an item that version lacks, the",
    "// canonical item shown in its place (an explorer map as a filled map, a",
    "// poplar plank as a birch one); RemapID shifts it like any other.",
    "var absentItemStandIns = map[int32]map[int32]int32{",
]
for ver in sorted(item_stand_ins):
    inner = ", ".join(f"{a}: {b}" for a, b in sorted(item_stand_ins[ver].items()))
    L.append(f"\t{ver}: {{{inner}}},")
L += ["}", ""]

L += [
    "// addedIDs[registry][clientProtocol] = IDs that exist on that version and have",
    "// NO canonical counterpart. The serverbound mirror of absentIDs: the reverse",
    "// shift table can only MOVE an id, so one the engine never had lands on",
    "// whatever canonical entry now occupies the slot — a 26.3 client's poplar",
    "// planks arriving as redstone ore. The receiver drops these instead. Sorted",
    "// for binary search.",
    "var addedIDs = map[IDSpace]map[int32][]int32{",
]
for const in sorted(added):
    L.append(f"\t{const}: {{")
    for ver in sorted(added[const]):
        ids = ", ".join(str(i) for i in added[const][ver])
        L.append(f"\t\t{ver}: {{{ids}}},")
    L.append("\t},")
L += ["}", ""]

with open(OUT, "w") as f:
    f.write("\n".join(L))
print(f"wrote {OUT}")
