#!/usr/bin/env python3
"""Regenerate ../tachyne-gw-bedrock/internal/gw/bedrock_*_gen.go — the
Java-canonical → Bedrock mapping tables for the Bedrock gateway.

Sources:
  - GeyserMC/mappings @ the pin for the canonical Java version (MIT data,
    consumed as facts): blocks.nbt is a gzipped big-endian NBT whose
    "bedrock_mappings" list is POSITIONAL by Java block-state ID — index i
    maps canonical state i to a Bedrock {name, states} pair (empty entry =
    same-named stateless block) — so the pin must be a commit for exactly
    the canonical version. items.json / biomes.json / particles.json are
    keyed by Java identifier.
  - GeyserMC/Geyser master bedrock/ resources for the Bedrock side, at
    BEDROCK (the version the gateway's gophertunnel speaks):
    runtime_item_states.<BEDROCK>.json (item registry for StartGame),
    block_palette.<BEDROCK>.nbt (every block state that version knows, with
    its hashed network id — every id we emit is checked against it),
    entity_identifiers.dat (AvailableActorIdentifiers payload, sent verbatim).
  - The Java side is vanilla's own, for the canonical version: the reports
    (~/vanilla/reports/<version>, via scripts/vanillareport.py) give each
    block's state range (for blocks.nbt entries that omit
    bedrock_identifier, and the banner ranges) and the item, entity-type and
    particle registries in network-id order — the engine's id spaces — and
    the server jar's en_us.json gives the advancement text.

Block runtime IDs are HASHED network IDs (StartGame UseBlockNetworkIDHashes):
fnv1a-32 over the canonical little-endian NBT of {name, states} with state
keys sorted — the exact dragonfly/vanilla algorithm (network_block_hash.go).
This decouples us from Bedrock palette order across versions.

Entity identifiers default to "minecraft:<java name>"; the exception table
below is derived from Geyser's EntityDefinitions.java. Identifiers absent
from entity_identifiers.dat are emitted as "" (gateway skips rendering them).

Run from the repo root (fetches the Geyser side):

    python3 scripts/gen_bedrock.py [canonical-version]     # default: scripts/canon.py

Stdlib only.
"""
import gzip
import io
import json
import os
import struct
import subprocess
import sys
import urllib.request
import zipfile

import canon
import standins
import vanillareport

CANON = sys.argv[1] if len(sys.argv) > 1 else canon.VERSION
# canonical Java version -> (GeyserMC/mappings commit, the Java version that
# commit maps). blocks.nbt is positional in the pin's own Java state ids, so
# it is re-indexed onto the canonical states by (block, properties); a pin
# one version behind still maps every block the two share, and a block new
# in the canonical version renders as the fallback until Geyser maps it.
GEYSER_PINS = {
    # Geyser's BE 26.50 update, which maps Java 26.2: the Bedrock side has to
    # be the version the gateway speaks, and every 1.21.11 state is in 26.2.
    # (2f0a8da2, the last Java 1.21.11 commit, is Bedrock 26.30-era: 511 of
    # its block states no longer exist in 26.50.)
    "1.21.11": ("0fd435d3", "26.2"),
    # No Java 26.3 mappings yet: 26.2's, less 90 blocks new in 26.3 (wool
    # and concrete stairs/slabs, the poplar set). Re-pin when Geyser has 26.3.
    "26.3": ("0fd435d3", "26.2"),
}
# The Bedrock version the tables are for: must be the one the gateway's
# gophertunnel speaks (protocol.CurrentVersion), and the pin's Bedrock side.
BEDROCK = "26_50"
if CANON not in GEYSER_PINS:
    raise SystemExit(f"no GeyserMC/mappings pin for Java {CANON}: add one to GEYSER_PINS")
PIN, GEYSER_JAVA = GEYSER_PINS[CANON]
MAPPINGS = f"https://raw.githubusercontent.com/GeyserMC/mappings/{PIN}"
GEYSER = "https://raw.githubusercontent.com/GeyserMC/Geyser/master/core/src/main/resources/bedrock"

REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SERVER_JAR = os.path.expanduser(f"~/vanilla/server-{CANON}.jar")  # en_us.json lives in the bundled inner jar
OUT = os.path.join(REPO, "..", "tachyne-gw-bedrock", "internal", "gw")

# Geyser EntityDefinitions.java: Java entity-type name → Bedrock identifier
# where it differs from the default "minecraft:<same>". (Display/interaction
# entities are faked as armor stands, exactly like Geyser does.)
ENTITY_EXCEPTIONS = {
    "end_crystal": "ender_crystal",
    "experience_orb": "xp_orb",
    "evoker_fangs": "evocation_fang",
    "eye_of_ender": "eye_of_ender_signal",
    "firework_rocket": "fireworks_rocket",
    "fishing_bobber": "fishing_hook",
    "text_display": "armor_stand",
    "interaction": "armor_stand",
    "experience_bottle": "xp_bottle",
    "lingering_potion": "splash_potion",
    "breeze_wind_charge": "breeze_wind_charge_projectile",
    "wind_charge": "wind_charge_projectile",
    "spectral_arrow": "arrow",
    "trident": "thrown_trident",
    "furnace_minecart": "minecart",
    "spawner_minecart": "minecart",
    "giant": "zombie",
    "zombie_villager": "zombie_villager_v2",
    "zombified_piglin": "zombie_pigman",
    "tropical_fish": "tropicalfish",
    "evoker": "evocation_illager",
    "illusioner": "evocation_illager",
    "villager": "villager_v2",
    "trader_llama": "llama",
}

# Java has one entity type per boat wood; Bedrock has a single boat (+ chest
# boat) with the wood as a variant (cosmetic metadata, not the identifier).
for _wood in ("oak", "spruce", "birch", "jungle", "acacia", "dark_oak",
              "mangrove", "cherry", "pale_oak", "poplar"):
    ENTITY_EXCEPTIONS[f"{_wood}_boat"] = "boat"
    ENTITY_EXCEPTIONS[f"{_wood}_chest_boat"] = "chest_boat"
ENTITY_EXCEPTIONS["bamboo_raft"] = "boat"
ENTITY_EXCEPTIONS["bamboo_chest_raft"] = "chest_boat"


def fetch(url):
    req = urllib.request.Request(url, headers={"User-Agent": "tachyne-gen"})
    with urllib.request.urlopen(req) as r:
        return r.read()


# ---- big-endian (Java) NBT parser, preserving tag types --------------------

def be_parse(data):
    """Parse one named big-endian NBT tag; values are (tagType, payload)."""
    buf = io.BytesIO(data)

    def rstr():
        (n,) = struct.unpack(">H", buf.read(2))
        return buf.read(n).decode()

    def payload(t):
        if t == 1:
            return struct.unpack(">b", buf.read(1))[0]
        if t == 2:
            return struct.unpack(">h", buf.read(2))[0]
        if t == 3:
            return struct.unpack(">i", buf.read(4))[0]
        if t == 4:
            return struct.unpack(">q", buf.read(8))[0]
        if t == 5:
            return struct.unpack(">f", buf.read(4))[0]
        if t == 6:
            return struct.unpack(">d", buf.read(8))[0]
        if t == 7:
            (n,) = struct.unpack(">i", buf.read(4))
            return buf.read(n)
        if t == 8:
            return rstr()
        if t == 9:
            et = buf.read(1)[0]
            (n,) = struct.unpack(">i", buf.read(4))
            return [(et, payload(et)) for _ in range(n)]
        if t == 10:
            d = {}
            while True:
                ct = buf.read(1)[0]
                if ct == 0:
                    return d
                name = rstr()
                d[name] = (ct, payload(ct))
        if t == 11:
            (n,) = struct.unpack(">i", buf.read(4))
            return [struct.unpack(">i", buf.read(4))[0] for _ in range(n)]
        raise ValueError(f"tag {t}")

    t = buf.read(1)[0]
    rstr()  # root name
    return t, payload(t)


# ---- canonical LE NBT block hash (dragonfly network_block_hash.go) ---------

def fnv1a32(data):
    h = 0x811C9DC5
    for b in data:
        h = ((h ^ b) * 0x01000193) & 0xFFFFFFFF
    return h


def block_hash(name, states):
    """states: dict key → (tagType, value) with Java-NBT tag types."""
    if name == "minecraft:unknown":
        return 0xFFFFFFFE
    out = bytearray()

    def s(x):
        b = x.encode()
        out.extend(struct.pack("<H", len(b)))
        out.extend(b)

    out.extend(b"\x0a\x00\x00")  # root compound, empty name
    out.append(8)
    s("name")
    s(name)
    out.append(10)
    s("states")
    for k in sorted(states):
        t, v = states[k]
        if t == 1:
            out.append(1)
            s(k)
            out.extend(struct.pack("<b", v))
        elif t == 2:
            out.append(2)
            s(k)
            out.extend(struct.pack("<h", v))
        elif t == 3:
            out.append(3)
            s(k)
            out.extend(struct.pack("<i", v))
        elif t == 8:
            out.append(8)
            s(k)
            s(v)
        else:
            raise ValueError(f"state tag {t} for {name}[{k}]")
    out.append(0)  # end states
    out.append(0)  # end root
    return fnv1a32(bytes(out))


# ---- network little-endian NBT (entity_identifiers.dat idlist check) -------

def net_parse(data):
    buf = io.BytesIO(data)

    def varu():
        v = sh = 0
        while True:
            b = buf.read(1)[0]
            v |= (b & 0x7F) << sh
            if not b & 0x80:
                return v
            sh += 7

    def zig():
        v = varu()
        return (v >> 1) ^ -(v & 1)

    def rstr():
        return buf.read(varu()).decode()

    def payload(t):
        if t == 1:
            return struct.unpack("<b", buf.read(1))[0]
        if t == 2:
            return struct.unpack("<h", buf.read(2))[0]
        if t == 3:
            return zig()
        if t == 8:
            return rstr()
        if t == 9:
            et = buf.read(1)[0]
            return [payload(et) for _ in range(zig())]
        if t == 10:
            d = {}
            while True:
                ct = buf.read(1)[0]
                if ct == 0:
                    return d
                k = rstr()  # name MUST be read before the payload
                d[k] = payload(ct)
        raise ValueError(f"net tag {t}")

    t = buf.read(1)[0]
    rstr()
    return payload(t)


def header(w, script):
    w(f"// Code generated by scripts/{script}; DO NOT EDIT.\n")
    w(f"// Sources: GeyserMC/mappings @{PIN} (Java {GEYSER_JAVA}) + Geyser core\n")
    w(f"// bedrock/ resources (Bedrock 1.{BEDROCK.replace('_', '.')}), MIT; vanilla {CANON} reports.\n\n")
    w("package gw\n\n")


def state_keys(ver):
    """[state id] -> (block name, sorted properties) from the blocks report."""
    keys = {}
    for name, b in vanillareport._load(ver, "blocks.json").items():
        for st in b["states"]:
            keys[st["id"]] = (name.removeprefix("minecraft:"), tuple(sorted(st.get("properties", {}).items())))
    if sorted(keys) != list(range(len(keys))):
        raise SystemExit(f"Java {ver} state ids are not dense from 0")
    return [keys[i] for i in range(len(keys))]


def main():
    os.makedirs(OUT, exist_ok=True)

    # ---- blocks -------------------------------------------------------------
    print("fetching blocks.nbt …")
    _, root = be_parse(gzip.decompress(fetch(f"{MAPPINGS}/blocks.nbt")))
    entries = root["bedrock_mappings"][1]
    mcblocks = vanillareport.blocks(CANON)
    state_owner = {}
    for b in mcblocks:
        for sid in range(b["minStateId"], b["maxStateId"] + 1):
            state_owner[sid] = b["name"]
    pin_keys = state_keys(GEYSER_JAVA)
    if len(entries) != len(pin_keys):
        raise SystemExit(f"state count mismatch: geyser {len(entries)} vs Java {GEYSER_JAVA} {len(pin_keys)}")
    by_key = {k: e for k, (_, e) in zip(pin_keys, entries)}
    canon_keys = state_keys(CANON)
    n = len(canon_keys)
    if n != max(state_owner) + 1:
        raise SystemExit(f"state count mismatch: blocks report {n} vs Java {CANON} {max(state_owner)+1}")
    print(f"  {n} block states (Geyser maps Java {GEYSER_JAVA}: {len(entries)})")

    air = block_hash("minecraft:air", {})
    assert air == 3690217760, air  # cross-check vs dragonfly's known value
    fallback = block_hash("minecraft:info_update", {})
    # A block Geyser does not map yet is shown as its stand-in (scripts/standins.py),
    # which keeps its shape, before the fallback block.
    states_of, default_of = {}, {}
    for name, b in vanillareport._load(CANON, "blocks.json").items():
        name = name.removeprefix("minecraft:")
        states_of[name] = [st.get("properties", {}) for st in b["states"]]
        default_of[name] = next(st.get("properties", {}) for st in b["states"] if st.get("default"))
    rids = []
    unmapped = {}
    stood_in = 0
    for sid, key in enumerate(canon_keys):
        e = by_key.get(key)
        owner = state_owner[sid]
        stand = standins.BLOCKS.get(owner)
        if e is None and stand is not None:
            want = standins.props(dict(key[1]), states_of[stand], default_of[stand])
            e = by_key.get((stand, tuple(sorted(want.items()))))
            if e is not None:
                stood_in += 1
                owner = stand
        if e is None:
            unmapped[key[0]] = unmapped.get(key[0], 0) + 1
            rids.append(fallback)
            continue
        ident = e.get("bedrock_identifier", (8, owner))[1]
        states = e.get("state", (10, {}))[1]
        rids.append(block_hash("minecraft:" + ident, states))
    assert rids[0] == air  # canonical state 0 is air
    # Every id must be a state this Bedrock version knows, or the client shows
    # an unknown block: the check that a Geyser pin and the gateway's Bedrock
    # version have drifted apart.
    _, pal = be_parse(gzip.decompress(fetch(f"{GEYSER}/block_palette.{BEDROCK}.nbt")))
    known = {e["network_id"][1] & 0xFFFFFFFF for _, e in pal["blocks"][1]}
    stale = sorted({r for r in rids if r not in known})
    if stale:
        raise SystemExit(f"{len(stale)} Bedrock block states are not in the {BEDROCK} palette "
                         f"({sum(r in stale for r in rids)} Java states): re-pin GEYSER_PINS")
    print(f"  {stood_in} states shown as a stand-in")
    if unmapped:
        print(f"  {sum(unmapped.values())} states of {len(unmapped)} blocks have no Geyser mapping -> fallback: {sorted(unmapped)}")

    with open(os.path.join(OUT, "bedrock_blocks_gen.go"), "w") as f:
        header(f.write, "gen_bedrock.py")
        f.write("// bedrockBlockRIDs[canonical Java state ID] = Bedrock hashed block\n")
        f.write("// network ID (fnv1a-32 of the {name,states} LE NBT; StartGame\n")
        f.write("// UseBlockNetworkIDHashes=true).\n")
        f.write(f"const bedrockAirRID uint32 = {air:#x}\n\n")
        f.write(f"const bedrockFallbackRID uint32 = {fallback:#x} // minecraft:info_update\n\n")
        f.write("var bedrockBlockRIDs = []uint32{\n")
        for i in range(0, n, 8):
            f.write("\t" + ", ".join(f"{v:#x}" for v in rids[i : i + 8]) + ",\n")
        f.write("}\n")
    print(f"  bedrock_blocks_gen.go: {n} states, air {air:#x}")

    # ---- biomes -------------------------------------------------------------
    biomes = json.loads(fetch(f"{MAPPINGS}/biomes.json"))
    with open(os.path.join(OUT, "bedrock_biomes_gen.go"), "w") as f:
        header(f.write, "gen_bedrock.py")
        f.write("// bedrockBiomeIDs: Java biome identifier → Bedrock numeric biome ID.\n")
        f.write("var bedrockBiomeIDs = map[string]uint32{\n")
        for k in sorted(biomes):
            f.write(f"\t{json.dumps(k)}: {biomes[k]['bedrock_id']},\n")
        f.write("}\n")
    print(f"  bedrock_biomes_gen.go: {len(biomes)} biomes")

    # ---- items --------------------------------------------------------------
    # Canonical order from the registries report (network ids, the engine's
    # item ID space).
    print("fetching item registries …")
    mcitems = vanillareport.registry(CANON, "item")
    if [i["id"] for i in mcitems] != list(range(len(mcitems))):
        raise SystemExit("item ids are not dense from 0")
    ritems = json.loads(fetch(f"{GEYSER}/runtime_item_states.{BEDROCK}.json"))
    jitems = json.loads(fetch(f"{MAPPINGS}/items.json"))
    with open(os.path.join(OUT, "bedrock_items_gen.go"), "w") as f:
        header(f.write, "gen_bedrock.py")
        f.write('import "github.com/sandertv/gophertunnel/minecraft/protocol"\n\n')
        f.write(f"// bedrockItemEntries is the FULL Bedrock 1.{BEDROCK.replace('_', '.')} item registry for the\n")
        f.write("// StartGame ItemRegistry packet (omitting it crashes mobile clients).\n")
        f.write("var bedrockItemEntries = []protocol.ItemEntry{\n")
        for it in ritems:
            cb = "true" if it.get("componentBased") else "false"
            f.write(f'\t{{Name: {json.dumps(it["name"])}, RuntimeID: {it["id"]}, ComponentBased: {cb}, Version: {it.get("version", 2)}}},\n')
        f.write("}\n\n")
        f.write("// bedrockItemRef is one Java item's Bedrock rendering: identifier + aux.\n")
        f.write("type bedrockItemRef struct {\n\tName string\n\tData int16\n}\n\n")
        f.write("// javaItemBedrock[canonical Java item ID] — Name==\"\" means unmapped.\n")
        f.write("var javaItemBedrock = []bedrockItemRef{\n")
        missing, absent = 0, []
        registry = {it["name"] for it in ritems}
        for it in mcitems:
            j = jitems.get("minecraft:" + it["name"])
            if j is None:
                missing += 1
                f.write("\t{},\n")
                continue
            ident = j["bedrock_identifier"]
            if not ident.startswith("minecraft:"):
                ident = "minecraft:" + ident
            if ident not in registry:
                absent.append(ident)
            f.write(f'\t{{Name: {json.dumps(ident)}, Data: {j.get("bedrock_data", 0)}}},\n')
        f.write("}\n")
    if absent:  # an item the client's registry lacks cannot be shown
        raise SystemExit(f"{len(absent)} mapped Bedrock items are not in the {BEDROCK} registry: {sorted(set(absent))[:10]}")
    print(f"  bedrock_items_gen.go: {len(ritems)} registry entries, {len(mcitems)} java items ({missing} unmapped)")

    # ---- banner base colours -------------------------------------------------
    # A banner's own colour is its block (red_banner…); Bedrock keeps it on
    # the block entity ("Base"), so the gateway needs the colour by state.
    dyes = ["white", "orange", "magenta", "light_blue", "yellow", "lime", "pink", "gray",
            "light_gray", "cyan", "purple", "blue", "brown", "green", "red", "black"]
    with open(os.path.join(OUT, "bedrock_banners_gen.go"), "w") as f:
        header(f.write, "gen_bedrock.py")
        f.write("// bannerBaseRanges: block-state ranges of the banner blocks and their dye\n")
        f.write("// colour (Java dye id: white 0 … black 15).\n")
        f.write("var bannerBaseRanges = []struct {\n\tMin, Max uint32\n\tColor   int32\n}{\n")
        nb = 0
        for b in sorted(mcblocks, key=lambda b: b["minStateId"]):
            name = b["name"]
            for i, d in enumerate(dyes):
                if name == d + "_banner" or name == d + "_wall_banner":
                    f.write(f"\t{{{b['minStateId']}, {b['maxStateId']}, {i}}}, // {name}\n")
                    nb += 1
        f.write("}\n")
    print(f"  bedrock_banners_gen.go: {nb} banner blocks")

    # ---- entities -----------------------------------------------------------
    mcents = vanillareport.registry(CANON, "entity_type")
    if [e["id"] for e in mcents] != list(range(len(mcents))):
        raise SystemExit("entity ids are not dense from 0")
    if mcents[-2]["name"] != "player":
        raise SystemExit(f"expected player second-to-last, got {mcents[-2]['name']}")
    dat = fetch(f"{GEYSER}/entity_identifiers.dat")
    idlist = {e["id"] for e in net_parse(dat)["idlist"]}
    with open(os.path.join(OUT, "entity_identifiers.dat"), "wb") as f:
        f.write(dat)
    unmapped = []
    with open(os.path.join(OUT, "bedrock_entities_gen.go"), "w") as f:
        header(f.write, "gen_bedrock.py")
        f.write("// bedrockEntityIDs[canonical Java entity-type ID] = Bedrock actor\n")
        f.write("// identifier (Geyser's mapping; \"\" = no Bedrock equivalent, skip).\n")
        f.write("// Index order is NETWORK ids (player second-to-last).\n")
        f.write("var bedrockEntityIDs = []string{\n")
        for e in mcents:
            name = e["name"]
            ident = "minecraft:" + ENTITY_EXCEPTIONS.get(name, name)
            if ident not in idlist:
                unmapped.append(name)
                ident = ""
            f.write(f"\t{json.dumps(ident)}, // {name}\n")
        f.write("}\n\n")
        f.write("// javaEntityNames[canonical Java entity-type ID] = the Java name, for the\n")
        f.write("// looks that hang off the type alone (a boat's wood).\n")
        f.write("var javaEntityNames = []string{\n")
        for e in mcents:
            f.write(f"\t{json.dumps(e['name'])},\n")
        f.write("}\n")
    print(f"  bedrock_entities_gen.go: {len(mcents)} types, unmapped: {unmapped}")

    # ---- particles -----------------------------------------------------------
    # Geyser's particle mappings name a Bedrock particle (a named particle
    # effect) and/or a level event for each Java particle; the named ones are
    # what the gateway spawns. "geyseropt:" ids need Geyser's optional pack.
    mcparts = vanillareport.registry(CANON, "particle_type")
    if [e["id"] for e in mcparts] != list(range(len(mcparts))):
        raise SystemExit("particle ids are not dense from 0")
    pmap = json.loads(fetch(f"{MAPPINGS}/particles.json"))
    named = 0
    with open(os.path.join(OUT, "bedrock_particles_gen.go"), "w") as f:
        header(f.write, "gen_bedrock.py")
        f.write("// bedrockParticleNames[canonical Java particle id] = the Bedrock named\n")
        f.write("// particle Geyser maps it to (\"\" = none).\n")
        f.write("var bedrockParticleNames = []string{\n")
        for e in mcparts:
            entry = pmap.get(e["name"].upper(), {})
            bid = entry.get("bedrockId", "")
            if bid.startswith("geyseropt:"):
                bid = ""
            if bid:
                named += 1
            f.write(f"\t{json.dumps(bid)}, // {e['name']}\n")
        f.write("}\n")
    print(f"  bedrock_particles_gen.go: {len(mcparts)} particles, {named} named")

    # ---- language: advancement titles/descriptions ---------------------------
    # Java clients translate advancement keys themselves; Bedrock has no Java
    # language table, so the gateway carries the English strings for the
    # advancement toast (from the vanilla server jar's en_us.json).
    jar = zipfile.ZipFile(SERVER_JAR)
    inner = [n for n in jar.namelist() if n.startswith("META-INF/versions/") and n.endswith(".jar")]
    if inner:
        jar = zipfile.ZipFile(io.BytesIO(jar.read(inner[0])))
    lang = json.loads(jar.read("assets/minecraft/lang/en_us.json"))
    keys = sorted(k for k in lang if k.startswith("advancements.") and
                  (k.endswith(".title") or k.endswith(".description") or k.startswith("advancements.toast.")))
    with open(os.path.join(OUT, "bedrock_lang_gen.go"), "w") as f:
        header(f.write, "gen_bedrock.py")
        f.write("// advLangEN: English text for the advancement translate keys (vanilla\n")
        f.write(f"// {CANON} en_us.json), for the Bedrock toast.\n")
        f.write("var advLangEN = map[string]string{\n")
        for k in keys:
            f.write(f"\t{json.dumps(k)}: {json.dumps(lang[k])},\n")
        f.write("}\n")
    print(f"  bedrock_lang_gen.go: {len(keys)} strings")
    print(f"  entity_identifiers.dat: {len(dat)} bytes ({len(idlist)} bedrock ids)")

    subprocess.run(["gofmt", "-w", OUT], check=True)


if __name__ == "__main__":
    main()
