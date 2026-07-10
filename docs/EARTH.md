# Earth mode — real-world terrain from elevation data

*How tachyne serves the Cape Peninsula at 1:1 scale from real ESA elevation
data, and a recipe for doing the same to any Minecraft world. Built and
deployed 2026-07-10; the design conversation started as "here is a crazy
idea — spin up the entire planet."*

Connect to a tachyne cluster running `-earth capetown` and you spawn on the
real summit of Signal Hill, Cape Town — Lion's Head beside you, Table
Mountain's kilometre-high massif to the south, Table Bay full of ocean to the
north. Every hill is where it really is. 1 block = 1 metre.

---

## 1. The core idea: find your height seam

Every Minecraft-like terrain generator, whatever its internals, ultimately
answers one question: **"how high is the ground at column (x, z)?"** In
tachyne that answer is a single function — `worldgen.landHeight(wx, wz)` —
and *everything else derives from it*:

- the soil profile (grass on top, dirt below, stone underneath),
- ocean/lake fill (any column below sea level fills with water),
- biome resolution (columns at the waterline become **beaches**, columns
  below it become **oceans** classified by depth, high columns go cold via
  the altitude lapse and can snow),
- tree/flora decoration, the heightmap, sky-light computation.

Earth mode replaces **that one function** with a lookup into a real Digital
Elevation Model, and the entire game works unchanged on real terrain. Real
coastlines get real beach strips; Table Bay classifies itself as ocean; the
high plateaus cool down — all for free, because those systems never knew what
was underneath the height function in the first place.

**If you want to do this to your own server:** find your equivalent seam.
Vanilla-derived servers have one too (the `NoiseChunkGenerator`'s final
surface height). The narrower the seam, the less you touch — we changed one
function and disabled two features (below).

What we deliberately **disabled** in earth mode:

- **Noise rivers** — the DEM already carries real river valleys; procedural
  channels would cut fake ones across them.
- **Noise caves** — 3D cave noise would tunnel through Table Mountain. Real
  terrain gets no procedural holes (a later feature could whitelist real cave
  systems).

The Nether and End stay fully procedural — only the overworld is Earth.

## 2. The data: Copernicus GLO-30

We use the **Copernicus GLO-30 DSM** — a 30-metre global elevation model from
ESA, free and openly licensed, no account needed. It is served as 1°×1°
GeoTIFF tiles from a public AWS bucket:

```
https://copernicus-dem-30m.s3.amazonaws.com/
    Copernicus_DSM_COG_10_<TILE>_DEM/Copernicus_DSM_COG_10_<TILE>_DEM.tif
```

Tile names encode the SW corner: Cape Town needs `S34_00_E018_00` (lat −34…−33)
and `S35_00_E018_00` (lat −35…−34). Each is 3600×3600 float32 metres.

Why GLO-30 and not alternatives:

- **SRTM** requires a NASA Earthdata account and stops at ±60° latitude.
- **GLO-30** is anonymous HTTPS, global, and newer (TanDEM-X derived).
- It's a **DSM** (surface model — includes treetops/buildings, ~a few metres
  of noise in cities) rather than a bare-earth DTM. For block terrain that's
  fine; it's also why the sea reads as ~0 m with **no bathymetry** (see §7).

Two 1° tiles cover the whole peninsula. Cropped to the region and quantized
to int16 metres, **the entire Cape Peninsula is 724 KB gzipped** — small
enough to `go:embed` in the server binary. Scale intuition: a 1°×1° tile is
~25 MB raw; whole-country grids would want a tile service instead of embeds.

## 3. The pipeline: `scripts/gen_earthdem.py`

One script turns tiles into an embeddable grid:

1. Download the tiles (cached locally; `pip install tifffile numpy imagecodecs`
   — no GDAL needed, because the tile name tells you its exact georeferencing).
2. Crop to the region's bounding box, stitch across tile boundaries.
3. Round float32 metres → int16 (1 m vertical precision — far finer than a
   block after vertical scaling).
4. Write `internal/worldgen/earthdata/<name>.bin.gz`:

```
gzip( "TDEM1\n" | u32le header-len | JSON header | int16le grid )
JSON: {name, lat_n, lon_w, d (deg/sample), w, h, ref_lat, ref_lon}
grid: row-major, rows north→south, cols west→east, metres above sea level
```

The Go side (`internal/worldgen/earth.go`) embeds `earthdata/*.bin.gz` and
loads a grid by name. **Adding a region = one `REGIONS` entry in the script +
rerun + rebuild.** Landmark heights are pinned by unit tests
(`earth_test.go`) so a bad regeneration fails CI, not the player.

## 4. The mapping: blocks ↔ geography

We use a **local equirectangular projection** anchored at a reference point
(`ref_lat`, `ref_lon` — for Cape Town, the city centre at −33.92, 18.42,
which becomes block (0, 0)):

```
metres per degree latitude   mLat = 111,320            (WGS84 mean)
metres per degree longitude  mLon = 111,320 × cos(ref_lat)

block x = (lon − ref_lon) × mLon        (+x = east)
block z = (ref_lat − lat) × mLat        (+z = south, Minecraft convention)
```

Elevation is sampled **bilinearly** between the 30 m posts, so slopes are
smooth at block resolution instead of stair-stepping every 30 blocks.

Distortion honesty: cos(lat) is frozen at the reference parallel, so a region
spanning 0.6° of latitude carries <0.5% scale error — irrelevant at city
scale. A whole *continent* needs a real projection (Build The Earth uses a
conformal modified-airocean); a whole *planet* also collides with Minecraft's
±30M coordinate border (Earth's equator is 40M blocks — it *almost* fits).
Region-first sidesteps all of it.

## 5. The vertical problem (the one real compromise)

Minecraft's world is 384 blocks tall (−64…320). Table Mountain is 1,085 m;
Everest is 8,849 m. True 1:1 vertical is impossible without deep changes to
the chunk format, so earth mode scales elevation:

```
block y = SeaLevel(63) + round(metres / vscale),  clamped below the ceiling
```

Cape Town runs `vscale = 4.5`: Table Mountain tops at ~y 300, Signal Hill at
y 129, the city bowl at y 65–75. Pick the smallest vscale that clears your
region's summit — smaller regions with lower relief can run closer to 1:1.
(A single *global* scale that fits Everest, 1:35, would flatten Table
Mountain to 31 blocks — scale per region, not per planet.)

**The compression bites back — audit every consumer of height.** Our first
real login (from a phone) spawned in snow: the climate system's altitude
lapse and the mountain-biome tiers were still thinking in *blocks*, which
under 1:4.5 compression made every 300 m hill read as alpine — grove on
Signal Hill, a glacier on Table Mountain. Any rule keyed on height (snowlines,
biome tiers, mob spawn altitude, cloud level aesthetics) must be converted to
**real metres** (`blocks_above_sea × vscale`) with physically-sane thresholds:
we put the snowline at ~2,600 m real, high-mountain rock at 1,000 m, and
floored the climate noise at "cold" so temperate coasts can't roll random
frozen beaches. Regression-test it by sweeping the whole region and failing
on any snow-family biome.

The sea: the DSM reads ~0 m over water, carrying **no bathymetry**, so
columns below 1 m become a flat seabed 10 blocks under sea level. Real ocean
floors are a later layer (GEBCO is the open dataset for it).

## 6. Determinism is load-bearing

The generator must stay a **pure function of coordinates** — the same column
must produce the same blocks on every pod, every boot, every seed. In
tachyne three systems silently depend on that:

1. **The sharding overlap** — a shard *generates its neighbour's border
   terrain itself* so seams are invisible; divergent generators would show
   torn seams.
2. **The shared chunk cache** (Valkey) — pods share generated chunks.
3. **Edits-as-diffs persistence** — only player changes are stored; the
   terrain must be reproducible forever.

A DEM lookup is trivially deterministic, but there's a trap: **cache
poisoning across generator switches**. The same seed with noise vs. earth
terrain would collide on cache keys and serve stale noise chunks. Earth mode
prefixes every cache key with `E.<region>.<vscale>.` — and we flushed the
cache at cutover anyway.

## 7. Known v1 limitations

| Limitation | Cause | Later fix |
|---|---|---|
| Flat seabed | DSM has no bathymetry | GEBCO grid as a second layer |
| Generic biomes/trees | Only elevation is real | ESA WorldCover (10 m land cover) → biome map |
| Compressed mountains | 384-block ceiling | variable world height (deep engine change) |
| A few metres of city noise | DSM includes buildings/trees | bare-earth DTM, or ignore (it reads as texture) |
| No buildings/roads | terrain-only scope | OSM import — a huge, separate project |
| Region-scale only | local projection + embeds | tile service + real projection |

## 8. Deploying a region (tachyne-specific)

1. `python3 scripts/gen_earthdem.py <region>` → embeds the grid; commit.
2. `deploy/topology.yaml`: shard rectangles = your region's chunk bounds
   (the region map is literally an atlas; the world **ends** where no shard
   owns land — our edge is out at sea). Two shards split the peninsula at the
   Muizenberg line. Shard count scales with *players*, not area — ~7.2M
   chunks of ownership cost nothing until walked, and on a single node more
   pods add no capacity anyway.
3. StatefulSet args: `-earth <region> -earth-vscale <n> -spawn <x,z>`
   (auto-Y spawn resolution puts you on the real surface — ours lands on
   Signal Hill's summit).
4. Fresh world: old block-edit diffs from a previous terrain would overlay
   the new one — wipe the world PVCs. Flush the chunk cache.
5. Deterministic bonus: **no world data to migrate, ever.** The "world" is
   724 KB of DEM + player edits.

## 9. The recipe, condensed (for any server codebase)

1. **Find your height seam** — the one function that answers "how high is
   (x, z)". Everything else should derive from it. If it doesn't, refactor
   until it does; that refactor is most of the work.
2. **Get real elevation** — Copernicus GLO-30, public bucket, no auth.
3. **Preprocess offline** — crop, quantize to int16, compress. City-scale
   regions are sub-megabyte; don't parse GeoTIFF at runtime.
4. **Map coordinates** — local equirectangular at a reference parallel;
   bilinear sampling; +z is south.
5. **Scale vertically per region** — smallest divisor that clears the summit.
6. **Disable procedural carving** (caves/rivers) on real terrain.
7. **Stay deterministic** — pure function of coordinates; key any caches by
   generator identity.
8. **Test landmarks** — pin famous peaks/shores to expected block heights in
   unit tests, and assert cross-seed determinism.
9. Terrain first. Buildings are a different project.

## 10. Attribution

Elevation: **Copernicus DEM GLO-30** — © DLR e.V. 2010–2014 and © Airbus
Defence and Space GmbH 2014–2018, provided under COPERNICUS by the European
Union and ESA; free for any use with attribution. The generator script reads
the public AWS Open Data mirror.
