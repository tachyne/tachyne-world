# WEBMAP3D — native Go 3D world map (replaces Java BlueMap)

Status: **in progress** (started 2026-07-24). This document is the design of
record; keep it current as milestones land.

## Why

Today the 3D map is the **last Java process in the whole tachyne stack**. The
`daemons/bluemap` daemon serializes the engine world to vanilla Anvil
(`internal/anvil`) on a 5-minute timer, then shells out to the official BlueMap
CLI jar (`exec java -jar bluemap -r -u -w`, Java 25) to render and serve tiles.
So every map update is: engine world → `.mca` → BlueMap re-parses `.mca` →
tiles.

`internal/maprender` renders tiles **natively in Go, straight from the engine's
in-memory world**, and `daemons/webmap3d` serves them. This:

- **kills the last Java runtime** (and the Java-25-in-a-pod dependency);
- **deletes the double serialization** (no engine→anvil→re-parse round-trip);
- gives a **real-time incremental map** — the engine already publishes
  `mc.event.block_change` on the bus (see `internal/server/hub.go`, currently
  consumed by no renderer); we subscribe and re-render only dirty tiles in
  seconds, which snapshot-polling BlueMap structurally cannot do;
- keeps the map a **native Go k8s pod** like everything else.

## Decisions (2026-07-24)

1. **Full 3D**, not a 2D top-down first cut.
2. **Own tile format + own three.js viewer.** We do *not* reproduce BlueMap's
   undocumented binary hires-tile format. Owning the contract means no chasing
   their format across BlueMap versions, and it plugs into our event stream on
   our terms. (BlueMap is MIT — we *may* port/read its code with attribution;
   we simply choose not to couple to its wire format.)
3. **Anvil is decoupled, not deleted.** `internal/anvil` +
   `cmd/anvil-export` come out of the render loop entirely, but survive as a
   standalone "export tachyne world → real vanilla Minecraft save" utility
   (independent, genuinely useful). `daemons/bluemap` and the BlueMap jar
   retire once `webmap3d` ships.
4. **Its own repo: `github.com/tachyne/tachyne-map`** (checkout
   `~/minecraft/tachyne-map`), matching the org naming pattern and the
   "pod-deployed components get their own repos" doctrine. Its heavy render
   dependencies (image encoding, the three.js viewer) stay out of the engine
   image. The library core is package `render` in that module; the pod is its
   root `main`.
5. **tachyne-world exposes a narrow public world-read facade.** The renderer
   needs low-level world state (block-state IDs, per-block sky/block light,
   biomes, heightmaps) that today lives in the *internal* packages
   `worldgen`/`world` — which another module cannot import. Rather than promote
   those whole packages, tachyne-world grows a small stable public package
   (`worldread`) that opens a world read-only from
   `(seed, {world,nether,end}.gob)` and yields exactly what a renderer needs
   per chunk. `tachyne-map` depends on `tachyne-world` as a library, pinned by
   sha like the gateways pin `tachyne-common`.
6. **Runs as its own pod.** The engine world is a pure function of
   `(seed, edits)` (exactly how `cmd/anvil-export` works today), so the map pod
   mounts the world PVC **read-only** via the `worldread` facade and subscribes
   to NATS for live `block_change` updates. No write access to the precious PVC.

## Licensing

- **BlueMap is MIT** — the opposite of the Mojang decompile. We may read *and*
  port its code; the only obligation is preserving BlueColored's copyright +
  MIT notice for any derived portions (a `NOTICE` / `THIRD-PARTY-LICENSES`
  entry). MIT → Apache-2.0 (tachyne's license) is a clean permissive combo.
- **Mojang assets are NOT MIT.** Block models/textures come from the vanilla
  client jar (Mojang's proprietary assets). Like BlueMap, we **read them at
  runtime and never vendor them into the repo**; provisioning is gated on an
  operator EULA-consent flag (`-accept-download`, mirroring `daemons/bluemap`).

## Architecture

```
world PVC (ro)  ─┐
                 ├─►  daemons/webmap3d (pod)
NATS bus         ─┘        │  open world from (seed, {world,nether,end}.gob)
(mc.event.               │  provision vanilla client.jar → asset cache
 block_change)           ▼
                    tachyne-map/render
                      assets   client-jar provisioning + asset access (zip)
                      model    blockstate + block-model JSON → resolved models
                      atlas    stitch block textures → atlas + biome colormaps
                      mesh     per-chunk: face-cull + light bake + AO + tint
                      tile     own tile format (hires mesh + lowres color/height)
                         │
                         ▼
                    HTTP: /tiles/... + the three.js viewer (web/)  :8100
```

Data the renderer reads from the engine (all already in memory — see the
`internal/anvil` exporter for the same accessors):

- block state IDs — `worldgen.Chunk.Sections` (`[][4096]uint32`, YZX)
- biomes — `worldgen.Chunk.Biomes` (one name per section)
- sky + block light — `world.LightData{Sky,Block}` via `w.Light(cx,cz)` (0–15/block)
- heightmaps — `worldgen.Chunk.Heightmap` (`[256]int16`)

## Tile format (own)

Deliberately simple and documented (contrast BlueMap's opaque `.prbm`):

- **hires tile** = one chunk-column region's mesh, gzipped little-endian:
  a header (tile x/z, section span) + interleaved vertex records
  `{pos x,y,z (f32) | uv u,v (f32) | color rgba (u8×4, biome tint pre-baked) |
  light (u8, max(sky·daymix, block)) }` + a u32 index buffer. The viewer draws
  it with one `BufferGeometry` + the shared atlas texture.
- **lowres tile** = a downsampled top-surface color+height raster for far zoom
  (heightmap-shaded), PNG.
- **atlas** = one stitched PNG of every block texture used, plus a JSON
  `atlas.json` mapping resource-location → UV rect. Served once, cached.

Exact struct layouts are pinned in `tile.go` with a round-trip test.

## Milestones

1. **Asset loader** (`assets.go`, `model.go`, `blockstate.go`) — provision the
   client jar; parse blockstate JSON (variants + multipart) and block-model
   JSON (parent flattening, `#texture` var resolution, element/face geometry).
   *The hard 80%; pure data, unit-testable without the jar.*
2. **Atlas + mesher** (`atlas.go`, `mesh.go`) — stitch textures, biome tint
   colormaps; per-chunk mesh with face culling, light bake, AO, tint.
3. **Serving + viewer** (`daemons/webmap3d`, `web/`) — tile HTTP server in a
   pod + the three.js viewer; player markers from the bus (reuse the
   `daemons/webmap` bus-query pattern).
4. **Incremental + retire Java** — subscribe to `mc.event.block_change`,
   re-render dirty tiles; delete `daemons/bluemap` + the jar; move
   `internal/anvil` to standalone-export-only.

## Package layout

`tachyne-map` (new module `github.com/tachyne/tachyne-map`):

```
render/                 the rendering library (deps: tachyne-world/worldread, stdlib, image)
  assets.go             client-jar provisioning + asset access (archive/zip)
  model.go              block-model JSON: RawModel, parent flatten, texture resolve
  blockstate.go         blockstate JSON: variants/multipart → chosen model refs
  atlas.go   (M2)       texture atlas + biome colormaps
  mesh.go    (M2)       per-chunk mesh (cull/light/AO/tint)
  tile.go    (M2)       own tile format encode/decode
main.go      (M3)       the pod: open world ro, drive render, serve tiles+viewer
web/         (M3)       the three.js viewer (embedded)
```

`tachyne-world` grows one public package (the only engine surface tachyne-map imports):

```
worldread/              stable public facade: OpenReadOnly(seed, dir) →
                        per-chunk block states / biomes / light / heightmap,
                        wrapping internal/{world,worldgen}.
```
