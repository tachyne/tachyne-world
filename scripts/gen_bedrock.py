#!/usr/bin/env python3
"""Regenerate ../tachyne-gw-bedrock/internal/gw/bedrock_*_gen.go — the
Java-canonical → Bedrock mapping tables for the Bedrock gateway.

Sources (all fetched; MIT-licensed GeyserMC data consumed as facts):
  - GeyserMC/mappings @ PIN (last Java 1.21.11 commit): blocks.nbt is a
    gzipped big-endian NBT whose "bedrock_mappings" list is POSITIONAL by
    Java block-state ID — index i maps canonical state i to a Bedrock
    {name, states} pair (empty entry = same-named stateless block).
    items.json / biomes.json are keyed by Java identifier.
  - GeyserMC/Geyser master bedrock/ resources for the Bedrock 1.26.30 side:
    runtime_item_states.26_30.json (item registry for StartGame),
    entity_identifiers.dat (AvailableActorIdentifiers payload, sent verbatim).
  - PrismarineJS/minecraft-data 1.21.11 blocks.json (state-ID → Java block
    name, for blocks.nbt entries that omit bedrock_identifier), items.json +
    entities.json (canonical NETWORK id order — the engine's ID space; NOTE
    mcmeta's registries list is ALPHABETICAL, which is NOT network order for
    entities: player/fishing_bobber sit at the END of the real registry).

Block runtime IDs are HASHED network IDs (StartGame UseBlockNetworkIDHashes):
fnv1a-32 over the canonical little-endian NBT of {name, states} with state
keys sorted — the exact dragonfly/vanilla algorithm (network_block_hash.go).
This decouples us from Bedrock palette order across versions.

Entity identifiers default to "minecraft:<java name>"; the exception table
below is derived from Geyser's EntityDefinitions.java. Identifiers absent
from entity_identifiers.dat are emitted as "" (gateway skips rendering them).

Run from the repo root, OUTSIDE the sandbox (needs network):

    python3 scripts/gen_bedrock.py

Stdlib only.
"""
import gzip
import io
import json
import os
import struct
import subprocess
import urllib.request

PIN = "2f0a8da2"  # GeyserMC/mappings: last Java 1.21.11 commit
MAPPINGS = f"https://raw.githubusercontent.com/GeyserMC/mappings/{PIN}"
GEYSER = "https://raw.githubusercontent.com/GeyserMC/Geyser/master/core/src/main/resources/bedrock"
MCDATA = "https://raw.githubusercontent.com/PrismarineJS/minecraft-data/master/data/pc/1.21.11"

REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
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
              "mangrove", "cherry", "pale_oak"):
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
    w("// Sources: GeyserMC/mappings @" + PIN + " (Java 1.21.11) + Geyser core\n")
    w("// bedrock/ resources (Bedrock 1.26.30) + mcmeta 1.21.11 registries. MIT.\n\n")
    w("package gw\n\n")


def main():
    os.makedirs(OUT, exist_ok=True)

    # ---- blocks -------------------------------------------------------------
    print("fetching blocks.nbt + minecraft-data blocks.json …")
    _, root = be_parse(gzip.decompress(fetch(f"{MAPPINGS}/blocks.nbt")))
    entries = root["bedrock_mappings"][1]
    mcblocks = json.loads(fetch(f"{MCDATA}/blocks.json"))
    state_owner = {}
    for b in mcblocks:
        for sid in range(b["minStateId"], b["maxStateId"] + 1):
            state_owner[sid] = b["name"]
    n = len(entries)
    print(f"  {n} block states (minecraft-data max {max(state_owner)})")
    if n != max(state_owner) + 1:
        raise SystemExit(f"state count mismatch: geyser {n} vs minecraft-data {max(state_owner)+1}")

    rids = []
    for sid, (_, e) in enumerate(entries):
        ident = e.get("bedrock_identifier", (8, state_owner[sid]))[1]
        states = e.get("state", (10, {}))[1]
        rids.append(block_hash("minecraft:" + ident, states))

    air = block_hash("minecraft:air", {})
    assert air == 3690217760, air  # cross-check vs dragonfly's known value
    fallback = block_hash("minecraft:info_update", {})
    assert rids[0] == air  # canonical state 0 is air

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
    # Canonical order from minecraft-data (id field = network id, the engine's
    # item ID space).
    print("fetching item registries …")
    mcitems = sorted(json.loads(fetch(f"{MCDATA}/items.json")), key=lambda i: i["id"])
    if [i["id"] for i in mcitems] != list(range(len(mcitems))):
        raise SystemExit("minecraft-data item ids are not dense from 0")
    ritems = json.loads(fetch(f"{GEYSER}/runtime_item_states.26_30.json"))
    jitems = json.loads(fetch(f"{MAPPINGS}/items.json"))
    with open(os.path.join(OUT, "bedrock_items_gen.go"), "w") as f:
        header(f.write, "gen_bedrock.py")
        f.write('import "github.com/sandertv/gophertunnel/minecraft/protocol"\n\n')
        f.write("// bedrockItemEntries is the FULL Bedrock 1.26.30 item registry for the\n")
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
        missing = 0
        for it in mcitems:
            j = jitems.get("minecraft:" + it["name"])
            if j is None:
                missing += 1
                f.write("\t{},\n")
                continue
            ident = j["bedrock_identifier"]
            if not ident.startswith("minecraft:"):
                ident = "minecraft:" + ident
            f.write(f'\t{{Name: {json.dumps(ident)}, Data: {j.get("bedrock_data", 0)}}},\n')
        f.write("}\n")
    print(f"  bedrock_items_gen.go: {len(ritems)} registry entries, {len(mcitems)} java items ({missing} unmapped)")

    # ---- entities -----------------------------------------------------------
    mcents = sorted(json.loads(fetch(f"{MCDATA}/entities.json")), key=lambda e: e["id"])
    if [e["id"] for e in mcents] != list(range(len(mcents))):
        raise SystemExit("minecraft-data entity ids are not dense from 0")
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
        f.write("// Index order is minecraft-data NETWORK ids (player second-to-last).\n")
        f.write("var bedrockEntityIDs = []string{\n")
        for e in mcents:
            name = e["name"]
            ident = "minecraft:" + ENTITY_EXCEPTIONS.get(name, name)
            if ident not in idlist:
                unmapped.append(name)
                ident = ""
            f.write(f"\t{json.dumps(ident)}, // {name}\n")
        f.write("}\n")
    print(f"  bedrock_entities_gen.go: {len(mcents)} types, unmapped: {unmapped}")
    print(f"  entity_identifiers.dat: {len(dat)} bytes ({len(idlist)} bedrock ids)")

    subprocess.run(["gofmt", "-w", OUT], check=True)


if __name__ == "__main__":
    main()
