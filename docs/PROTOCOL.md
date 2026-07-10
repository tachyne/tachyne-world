# Protocol reference

Working notes captured from the Minecraft Wiki protocol pages. These are the
facts our implementation targets. Verify packet bodies against the wiki when
implementing each one — IDs and field layouts shift per version.

> **WHERE THE IMPLEMENTATION LIVES (updated 2026-07-07):** this engine repo
> contains NO wire code. Everything below is implemented in the shared module
> `github.com/tachyne/tachyne-common` — `protocol/` (framing, NBT,
> palettes, registries, config-phase composition, the 770→776 translation
> chain) and `render770/` (canonical-770 renderers + serverbound parsers for
> the domain events, with byte-oracle tests) — and exercised by the gateway
> repos (`tachyne-gw-java-770`/`-776`). File references below to
> `configuration.go` / `play.go` / `internal/protocol/*` describe the
> pre-refactor monolith (git history pre-`c15e1e4`); the FACTS still hold.

## Java Edition

- Transport: **TCP**, default port **25565**.
- Frame format (uncompressed): `VarInt length` (of id+data) → `VarInt packet ID` → `data`.
- Compression: once `Set Compression` is sent, frames gain a `VarInt data length`
  prefix and the body is zlib-compressed above the threshold.
- Encryption: AES/CFB8, negotiated during Login for online-mode via Mojang
  session servers.
- **Latest protocol version: 773 (Minecraft 1.21.10).**
- States: Handshaking → Status → Login → Configuration → Play.

### Packet IDs we care about now

| State       | Direction   | Packet               | ID   |
|-------------|-------------|----------------------|------|
| Handshaking | serverbound | Handshake            | 0x00 |
| Status      | serverbound | Status Request       | 0x00 |
| Status      | serverbound | Ping Request         | 0x01 |
| Status      | clientbound | Status Response      | 0x00 |
| Status      | clientbound | Pong Response        | 0x01 |
| Login       | serverbound | Login Start          | 0x00 |
| Login       | serverbound | Encryption Response  | 0x01 |
| Login       | serverbound | Login Acknowledged   | 0x03 |
| Login       | clientbound | Disconnect           | 0x00 |
| Login       | clientbound | Encryption Request   | 0x01 |
| Login       | clientbound | Login Success        | 0x02 |
| Login       | clientbound | Set Compression      | 0x03 |

Canonical version is now **1.21.5 (protocol 770)** — the newest version with a
complete, verified registry dump available to source (see registry note below).
The version landscape has since moved to date-based releases (26.x); multi-
version support comes via the ViaVersion-style translation seam.

Status (server-list ping): **DONE** (milestone 1).
Login → Configuration → Play join: **WORKING — verified with a real 1.21.5
client (player spawns on the flat platform and can move around).**

Three real bugs fixed during the first live test (all surfaced only in the
client's `logs/latest.log`, not the generic "Network Protocol Error" screen):
- `minecraft:enchantment` with has_data=false needs tags (Update Tags packet) —
  skipped for now (see configuration.go).
- dimension_type / worldgen/biome / damage_type must carry inline NBT even when
  the known pack matches (see registry_data.go).
- 1.21.5 removed the paletted-container data-array-length field — a single value
  is just bitsPerEntry(0) + the palette VarInt (see play.go flatChunk).

- Login: Login Success (0x02) with offline UUID → Login Acknowledged (0x03) ✅
- Configuration: Known Packs (minecraft:core@1.21.5) → 18 Registry Data packets
  (names only, `has_data=false`) → Finish Configuration ✅
- Play: Join (0x2b) → Game Event 13 → Set Center Chunk → 5×5 flat Chunk Data
  (0x27) → Synchronize Player Position (0x41) ✅
- Follow-ups: lighting (world renders dark), player abilities, more chunks.

### Registry sourcing

No Java/data-generator available, so registry data was sourced from
**misode/mcmeta** tag `1.21.5-data-json` / `1.21.5-summary` (entry names) and
packet layouts from **PrismarineJS/minecraft-data** protocol 770. The Known
Packs handshake lets us send entry **names only** — the client supplies the
content — so no NBT blob is embedded. See `internal/protocol/registries_gen.go`
(regenerate with `scratchpad/gen_registries.py`).

## Bedrock Edition

- Transport: **RakNet over UDP**, default port **19132** (length prefix is
  unnecessary — UDP carries packet length).
- Packets are **always compressed (zlib)** and may be encrypted; multiple packets
  are batched into one.
- Login uses a JWT identity chain + skin data, then an ECDH handshake with an
  encryption salt.
- Go library: **gophertunnel** (Sandertv/gophertunnel) implements RakNet +
  Bedrock protocol + login/encryption. **Dragonfly** (df-mc) is a full Bedrock
  server built on it. We build on gophertunnel rather than reimplement RakNet.

## References

- https://minecraft.wiki/w/Java_Edition_protocol
- https://minecraft.wiki/w/Java_Edition_protocol/Packets
- https://minecraft.wiki/w/Bedrock_Edition_protocol
