# Sharding — current design & build plan (authoritative)

> **This is the source of truth for the sharding build as of 2026-07-09.**
> `docs/SHARDING.md` is the earlier, deeper implementation spec — its *mechanism*
> detail (viewer/stream-merge model, per-viewer `EntityView`, phase acceptance
> tests, protocol gotchas) is still valid and worth reading — but several of its
> **infrastructure choices are SUPERSEDED here**: no Postgres, no NATS in the
> handover path, handover is direct pod-to-pod. Where the two disagree, THIS doc
> wins. `docs/SHARDING-MVP.md` (a transient hard-border descope) has been retired
> into this file.

## The design in one paragraph

**N world pods, each authoritative over a disjoint region of one shared world.**
Each pod **simulates and persists only its own region** (to its own PVC, gob —
like today); beyond its cells the world does not exist *for that pod*. Clients
see a **seamless** world because the **gateway assembles each client's view by
streaming from multiple pods at once** (its home pod + the neighbours whose
regions are within view) — SHARDING.md's "**reads are global; writes are
owned**". When a player, mob, vehicle, or weather crosses a seam, ownership is
**handed over directly between the two neighbouring pods** over a pre-warmed
point-to-point link. There is **no shared database and no message broker in the
critical path.**

## Invariants (the non-negotiables, in priority order)

1. **Handover is practically INSTANT.** Nothing may be established, dialed, or
   queried at handover time on the critical path — no broker hop, no DB
   round-trip, no fresh connection. Rule: **make-before-break; everything
   pre-warmed** (see §4). This is the top constraint; every other choice bends to
   it.
2. **The client connection NEVER drops across a handover.** The gateway holds the
   socket; only the *authority* behind it moves. Ceiling on jarring-ness: nothing
   — in the seamless model the crossing is invisible (no reload).
3. **Reads are global; writes are owned.** Any pod serves *its own* chunks to a
   viewer stream; only its owner *mutates* a region. Movement validation and
   rendering can read across; effects route to the one owner.
4. **Neighbour-scoped, never all-to-all.** A pod communicates only with the
   neighbours it shares a border with (`shard.Neighbours`). No mechanism may
   broadcast to every shard. Message/connection volume scales with border
   adjacency, not N.

## Dependencies — what we DON'T use, and why

- **No Postgres.** Its three jobs evaporate under owned-writes: durable edits →
  each pod is sole writer of its region, persists its own cells to its own PVC;
  handover medium → state travels pod-to-pod, not through a DB; epoch fencing →
  no shared writers to arbitrate. It was a production-durability choice, never a
  correctness requirement. Dropping it holds today's durability (per-pod
  checkpoint to PVC). *Optional later hardening:* a store would help resolve a
  **logged-out** player homed on a different pod than they log into — a
  multi-session concern that does not block building/testing traversal.
- **No NATS in the handover path.** Handover is point-to-point, ordered,
  ack-required, between known neighbours — the anti-case for a pub/sub broker,
  and a broker hop violates invariant #1. Direct links give structural
  neighbour-scoping (can't spew), ordered+ack'd delivery (dedup trivial vs
  at-least-once), explicit neighbour liveness (no ack ⇒ no release ⇒ entity
  safely stays put), and zero critical-path dependency. NATS stays available for
  genuinely-global fan-out only (all-players chat); the clock is *derived* from a
  shared anchor (SHARDING.md §5.1, no messaging); the existing `natsbus.go`
  external-plugin bus is untouched and orthogonal.
- **Per-pod local persistence.** Each pod keeps its today-style gob PVC for its
  owned cells + checkpoints its homed players. Static topology ⇒ a pod's PVC
  always holds exactly its own region.

## 1. Topology & coordinates (`tachyne-common/shard` — BUILT ✅)

Ownership is an **explicit region map**, NOT a modular formula — because the
world GROWS by adding pods, and a mod-N formula would reshuffle the whole world
every time N changes (and can't express a world edge). See §10 decision 9.

- `Map{Version, Regions []Region}` where `Region{SID, MinCX, MinCZ, W, H}` is a
  contiguous rectangle of chunks (half-open `[MinCX, MinCX+W) × [MinCZ,
  MinCZ+H)`). A pod may own more than one region (keyed by SID, for a future
  hot-split).
- `ShardOf(dim,cx,cz)` = region lookup → owning SID, or **`Unowned`** (the world
  edge / void) if no region covers it. `Neighbours(sid)` = SIDs of regions
  **edge-adjacent** (share a positive-length border) to any of sid's regions —
  corner-touch excluded; N/S and E/W seams both count. `Validate` rejects
  overlaps (two writers), bad SIDs, non-positive sizes. `TopoHash` is
  region-order-independent (sorts before hashing).
- **Growth is non-disruptive:** adding a pod = append a `Region`; no existing
  region changes, so no ownership reshuffles. Placement is admin-assigned
  coordinates (primitive); an optional spiral "next tile" helper is future sugar.
- **Continuous shared coordinates** across seams (resume at same x/z).
- **Player-lane eids** (`MintEID(counter, PlayerSID)`) so a player keeps ONE
  session-stable eid across pods and never collides with a pod's mob-lane eids.
- Built, `gofmt`/`vet` clean, `go test -race` green — region lookup, geometric
  adjacency (incl. corner/detached/stacked cases), overlap rejection,
  order-independent hash, eid lanes.

## 1a. Boot & discovery — NO coordinator pod

A pod knows "where it fits in the greater world" from two **static** inputs, and
derives everything else. There is deliberately **no coordinator pod** — that
would be a SPOF, a bootstrap-ordering dependency, and (worst) something in the
hot path that could threaten invariant #1.

1. **SID = StatefulSet ordinal** (`POD_NAME` `tachyne-world-<n>` → SID n; fail
   hard on malformed). Kubernetes guarantees stable, unique ordinals.
2. **Region map = a ConfigMap** (`{version, regions:[{sid,min_cx,min_cz,w,h}…]}`
   + seed + genVersion) mounted into **every** pod AND gateway. `ShardOf` is a
   pure function of it, so all components compute identical ownership. The map is
   small and **replicated everywhere** — every pod holds the WHOLE map and can
   compute `ShardOf(anychunk)`, not just its own region.
3. **Neighbours + addresses are DERIVED**, not told: `NeighboursWithin(mySID,
   viewRadius)` (the awareness set — edges AND diagonals) →
   `tachyne-world-<sid>.tachyne-world.tachyne.svc` via headless-service DNS.
   Dial with retry/backoff (boot order is arbitrary; the mesh converges).

Boot: ordinal → mount+`Validate` map (refuse start on genVersion mismatch or bad
partition) → compute neighbours → warm the peer links → open attach listener →
ready. **No running coordinator is consulted, ever.**

The coordination here is **config-time (K8s ConfigMap/StatefulSet/DNS), not
runtime** — entirely out of the data path. Agreement is guarded, not assumed:
every pod/gateway asserts `TopoHash()` matches at session start, so a stale
ConfigMap fails loudly instead of corrupting a seam.

A coordinator would only be warranted for **dynamic rebalancing** (changing
`Shards`/reassigning regions on a live world) — a deliberate non-goal (map
`Version` reserved for a future coordinated *cutover Job*, not an always-on
steady-state coordinator).

## 2. Two channels (do not conflate)

- **gateway↔world** — the existing `attach` protocol. Rendering. Extended with
  **viewer sessions**: near a border the gateway opens a `Purpose:"view"` attach
  session to each neighbour pod in view and merges its spatial frames with the
  home stream (per-viewer `EntityView` de-dups; tab-list de-dup by uuid). This is
  what makes the world seamless AND is the pre-warm that makes handover instant.
- **world↔world** — a NEW small **direct peer protocol** for handover/migration
  AND shadow-push. Reuses `attach`'s `WriteFrame`/`ReadFrame` codec. Each pod
  dials its `NeighboursWithin(mySID, viewRadius)` at boot (the awareness set —
  edges + diagonals, so corner cases have a warm link; resolved to
  `tachyne-world-<sid>.tachyne-world.…` DNS) and keeps the links warm. Frames:
  `MigrateEntity{kind, state, migId}`, `Ack{migId}`, and a `Shadow{eid,pose}`
  push for awareness. Lower-SID dials higher to avoid double-connect.

## 3. Ownership (each pod = a finite authoritative island)

- Gen/sim/spawn confined to owned cells: Want handler filters non-owned chunks
  (never marks them sent); mob spawn/movement, block-update `schedule`, dig/place
  guarded by `ShardOf(...) == mySID`. A pod's world genuinely ends at its edge.
- Foreign chunks the *gateway* needs for seamless view come from the neighbour's
  own viewer stream, NOT from this pod reading across — keeps writes-owned clean.

## 4. Handover — direct, pre-warmed, instant

**Pre-warm (make-before-break).** Because you SEE across the border before
crossing, by the time you reach a seam: (a) the pod↔neighbour peer link is
already open (dialed at boot), and (b) the gateway is already streaming a viewer
session from the destination pod. Nothing on the hot path is created lazily.

**Player crossing A→B:**
1. On a validated `Move` into a B-owned cell, pod A sends `MigrateEntity{player
   state, migId}` to B over the warm peer link.
2. B applies the state (same eid), replies `Ack{migId}`. A releases the player
   only on the ack (kept if no ack — B might be down). Idempotent by `migId`.
3. A tells the gateway the player is now on B (`MsgRehome{DestSID}` on the attach
   session). The gateway **promotes its already-open B viewer session to the home
   session** and demotes A — no dial, no reload. Client keeps walking; seam
   invisible.

**Mob / vehicle / weather crossing:** same peer-link `MigrateEntity` + ack, no
gateway involved (they have no client). Observers see a remove-on-A / add-on-B at
the seam (an acceptable flicker for mobs; SHARDING.md §9.6). Weather: one owner
computes and propagates along neighbour links.

**Why no split-brain without fencing:** the ack-gated release means exactly one
pod owns the entity at any instant (A holds until B acks; then A releases). The
warm direct link makes the ack sub-ms, so this costs nothing against invariant #1.

## 4a. Awareness pre-warm — the approach (before handover)

Cross-border awareness is NOT a free parameter: it must begin at the **same range
entities/terrain are visible within a shard**, or the border becomes visible (a
mob would "pop in" at the seam instead of being there all along). Engine radii:
terrain/chunk `maxRadius = 8` chunks = **128 blocks** (`attach.go:68`); entity
interest `viewRadius = 6` chunks = **96 blocks** (`play.go:22`); mob aggro
(FOLLOW_RANGE) ≤ **64 blocks** (enderman; most 16–35). All ≤ 128, so **one
pre-warm threshold at 8 chunks / 128 blocks** covers everything with margin.

**Two DISTINCT thresholds — do not conflate:**
- **Awareness (pre-warm), ~128 blocks from a neighbour edge:** (a) the gateway
  opens/activates the **viewer session** to the neighbour → the player sees its
  terrain + entities; (b) the owner pod pushes a **shadow of the player** to the
  neighbour over the warm peer link → the neighbour's mobs aggro the player and
  its players see them. Mutual, real-time, well before the line.
- **Handover (ownership flip), at the seam itself:** instant, because everything
  has been warm for ~128 blocks. (Small hysteresis past the line to avoid
  seam-dancing — tune during testing.)

The viewer session + shadow **activate/deactivate** as the player enters/leaves
the 128-block band — that activation logic is what the enlarged (256-block) test
tiles exist to exercise; smaller tiles would leave awareness always-on.

**Corners — awareness ≠ handover neighbours.** The view box is a *square*, so near
a corner where four tiles meet it overlaps THREE other tiles at once — all three
get a viewer session + a shadow-push. This is a runtime, position-based set:
**0** deep in the core, **1** along an edge, **up to 3** at a 4-way corner. Two
distinct `shard` primitives:
- `Neighbours(sid)` — **edge-adjacent only** → HANDOVER targets (you migrate
  across an edge; the diagonal tile is not walkable-into directly).
- `NeighboursWithin(sid, viewRadius)` — **includes diagonals** (corner-touch =
  gap 0) → the AWARENESS/peer set. This is what the **peer-link mesh is dialed
  from at boot**, so shadow-push and even a degenerate diagonal-corner handover
  always have a warm link. (Both built + tested, incl. a 2×2-corner case.)

Because tiles (256) ≥ view radius (128), a corner exposes at most the 3
immediately-touching tiles — never deeper. The current 2-tile testbed has no
4-way corner; it first appears when we grow to a 2×2.

## 5. Protocol additions

**`tachyne-common/attach`** (gateway↔world): `Hello.Purpose`
(`""|"login"|"view"|"resume"`) + `Hello.ResumeToken` (correlates a resume dial to
a pending migrated player — NOT the state itself), `Welcome.SID`/`Topo`, viewer
wiring for `Purpose:"view"`, and `MsgRehome{DestSID, Token}` (w→gw re-point
signal). **State never rides the attach path** — it goes pod→pod over the peer
link. Free frame IDs: `0x18`, `0x19`, `0x44+`.

**New `tachyne-common/handover`** (world↔world peer protocol): frames
`MigrateEntity{Kind, MigID, …}` / `Ack{MigID, OK}`, and the transport structs.
Framing reuses `attach.WriteFrame`/`ReadFrame`.

`PlayerState` mirrors the **live `tracked`** (`hub.go:169-239`), NOT the on-disk
subset — disk omits health/food/saturation/exhaustion/air/effects/fire, which a
live crossing must preserve. Rules:
- **Tick-anchored fields (cooldowns, grace, fire, eat/draw/block timers) travel
  as REMAINING ticks**, rebuilt against the destination pod's local clock (world-
  age tick is per-pod). Effects already store `left` (relative) — no conversion.
- **Transient UI/container state is NOT carried** (cursor/craft/anvil/enchant/
  trade/win\*) — closed on handover; you can't cross with a UI open.
- **Anti-cheat counters reset** on arrival (`moveBudget`/`rejectStreak`/
  `floatTicks`/`lastRubber`) — safe.
- Inventory reuses the engine's `[item,count,dmg,ench]` `[4]int32` pack
  (savedInv-compatible). Item `name`/`potion` NOT carried → a crossing is **at
  most as lossy as a relog** (documented TODO: widen the encoding for lossless
  renames/potions if wanted).
- Carried (correctness): eid (player-lane), name, uuid, dim, x/y/z, yaw/pitch,
  onGround, sprinting, gamemode, health, absorption, food, saturation,
  exhaustion, air, fireSecs, xpLevel, xpPoints, peakY+airborne (fall calc),
  effects `[{id,amp,left}]`, inv (36 slots + armor[4] + offhand), bed spawn.

`VehicleState` — memory-only in the engine (no disk store), so the transport
carries it wholesale (`eid,dim,uuid,etype,x/y/z,yaw,rider`). `MobState` — carries
identity/pos/health/combat/age/tame/species fields + slices (path/riders/offers);
the `behavior` **interface is re-resolved from `etype` on arrival**, not
marshaled. All **cross-entity eid refs** (`vehicle.rider`, `mob.rider/owner/
riders`, `tracked.tradeWith`) are **remapped to the destination pod's eid space**.

**Fidelity guard:** a `tracked ⇄ PlayerState` round-trip test lives in
`tachyne-world/internal/server` — add a `tracked` field that must survive a
crossing and the test fails until `PlayerState` carries it. (MobState/VehicleState
get their own round-trip tests in PR3.)

## 6. Debug: `-debug-borders` red seam line

Each pod emits a vertical red-dust particle curtain along its owned boundary near
players, via the **existing `MsgParticles` (0x29)** frame — no new frame/renderer.
Non-blocking, version-agnostic (rendered per-gateway on 1.21.5 AND 26.2), gated
behind `-debug-borders`. Verify the `Particles` frame can carry a red `dust`
(RGB+scale); else fall back to a red-concrete floor strip at gen. (Build-time aid
that also lets us confirm the handover trigger fires exactly at the seam.)

## 7. Grounded code map (current file:line — build against THIS)

_(Verified 2026-07-09, post-consolidation; SHARDING.md's refs had drifted.)_

- **`tachyne-common/attach`**: frames `attach.go:52-60`+`entities.go`; `Hello`
  `attach.go:92-99`; `Welcome` `:102-112` (**`Gamemode` already present**);
  codec `WriteFrame:165`/`ReadFrame:180`/`WriteJSON:197`/`EncodeChunk:206`.
- **`tachyne-world/internal/world`**: `edits map[chunkPos]map[int]uint32`
  `world.go:37`; `SetBlock:487`; `At:522`; `Chunk:545`; LRU `:180-225`; `Store`
  iface `store.go:14-20` (whole-map gob — stays; no Postgres). No tombstones (a
  broken block is a positive `→Air` edit).
- **`tachyne-world/internal/server`**: eid mint `allocEID` `hub.go:415` (player
  `remote.go:20`, mob `mob.go:137` → swap to `shard.MintEID` lanes); spatial
  fan-out `toNearbyEv` `mob.go:561` (+`broadcastBlock` `sim.go:84`,
  `broadcastEquipment` `equipment.go:50`) → viewer + debug-border hook; `Move`
  `remote.go:130` → handover trigger; `tracked` `hub.go:169-239` → snapshot
  source; ownership gates `handleDig` `interaction.go:17` / `handlePlace` `:133` /
  mob spawn `animals.go:192`,`species.go:485`,`hostile.go:413`,`spawner.go:21` /
  `updateMobs` `mob.go:150` / `schedule` `sim.go:29`; `JoinRemote` `remote.go:19`
  (→ `resume`/`view` variants); attach `session()` `attach.go:97`, Want handler
  `:254-282` (→ ownership filter), `Config` `:27-44`, `Join` hook `:43`; flags
  `cmd/server/main.go:16-28` (→ `-sid`/`-topology`/`-debug-borders`/peer addr).
- **`tachyne-gw-java-770/internal/gw`**: dial+Hello+Welcome `session.go:95-117`;
  `play()` `:247` (single `w`; → multi-backend for viewer merge + home swap);
  world→client switch `:288`; client→world `:616`; **dimension-switch template**
  `:520-528`; Want sends `:263/:743-747/:536/:716/:566`; single `Backend`
  `gateway.go:45`, env `TACHYNE_BACKEND` `cmd/gw/main.go:24`; SID from `POD_NAME`
  `:27,51-61`. gw-776 is a separate, less-mature module — **770-only** for now.

## 8. Build plan (phases) + confidence

Build order is hard-borders-first even though the goal is seamless — the handover
machinery is the foundation the viewer layer sits on. Confidence is provisional
until PR2 puts the engine code in hand.

- **PR1 — `shard` package + attach/peer protocol types.** ✅ DONE. `shard`,
  `handover` (PlayerState/Mob/Vehicle + MigrateEntity/Ack/Shadow/PeerHello), attach
  additions (Hello.Purpose/ResumeToken, Welcome.SID/Topo, MsgRehome), and the
  engine `tracked ⇄ PlayerState` fidelity round-trip test — all `-race` green.
  MobState is CORE-only (species/behavior/path/offers → PR3). **Dev note:**
  `tachyne-world/go.mod` has a LOCAL `replace … => ../tachyne-common` so the engine
  sees the unpushed common changes — MUST be removed + pinned to a sha before any
  deploy (push common → `go get …@<sha>` in every consumer, per the golden rule).
- **PR2 — engine: finite per-shard world + ownership + peer links + border.**
  IN PROGRESS. ✅ DONE: boot wiring (`-sid`/`-topology`, `LoadTopology` +
  Validate, SID from POD_NAME, hub `owned` closure — `internal/server/shardown.go`);
  finite world (attach Want serves only owned chunks); ownership gates on dig/
  place/sim-schedule. Unsharded = owns everything (nil closure) so zero
  regressions; full suite green. ✅ eid lanes: `allocEID` mints in the pod's SID
  lane, `mintPlayerEID` in the player lane (shared counter, no collision),
  unsharded = plain counters (`shardown.go`). ✅ mob gates: spawn confined to
  owned cells at the 4 drivers (wildSpawn/waterSpawn/hostile night-spawn/
  updateSpawners); `updateMobs` walk-clamp bounces land mobs at the region edge
  (fly/swim clamp = small TODO). ✅ `-debug-borders`: crit-particle wall along
  region seams (`emitDebugBorders` in shardown.go, throttled age%10). NOTE it's
  WHITE crit (the only version-safe payload-free particle set — crit/poof/
  explosion); a true RED block-frame needs the glow-entity + a new `Teams` color
  frame (deferred — team color is the only way to tint an entity glow outline).
  ⏭️ REMAINING: warm neighbour peer-link dialer (`NeighboursWithin`) — really the
  start of PR3.
- **PR3 — handover.** MOSTLY DONE. ✅ warm peer-link mesh (`peer.go`). ✅
  release→ack→resume state machine (`handoff.go`): `checkSeamCrossing` (in the
  `evMove` path) → `beginHandover` (snapshot + send over mesh, register before
  send) → dest `applyMigration` (recreate player, announce, ack) → `finishHandover`
  (remove from source, emit `MsgRehome`). `onPeerFrame`→`evPeerFrame`→
  `handlePeerFrame` dispatch. Hub `owned` closure refactored to `shardOf` (returns
  the dest SID). Unit-tested end-to-end (two hubs + fake in-process mesh: state
  fidelity, preserved player-lane eid, Rehome emitted, void≠handover). Full suite
  green. ✅ **mob migration** (`migrateMobAcross` replaces the edge-clamp when the
  step lands in a connected neighbour; `applyMigration` KindMob re-mints the eid in
  the dest lane + re-resolves behavior from type via `migratedBehavior`; fire-and-
  forget, flicker OK per §9.6; unit-tested). ⏭️ REMAINING in PR3: **vehicle +
  weather migration** — vehicles are deferred because the meaningful case is a
  RIDDEN vehicle, whose crossing must be coordinated atomically with its rider's
  player-handover (pairs naturally with PR4); weather is shared-owner state. ✅
  **PR4a engine side — resume-binding.** `applyMigration` KindPlayer now holds the
  snapshot PENDING (`setPending`, mutex-guarded); `ResumeRemote` (`remote.go`)
  claims it on `Hello{Purpose:"resume",token}`, recreates the player with the same
  eid + gamemode override, and `onJoin` seeds from the snapshot (no fresh survival,
  no join broadcast). attach `Config.Resume` hook + session `Purpose` branch.
  Unit-tested: cross → pending on dest → resume → live with full state. ⏭️
  **REMAINING — GATEWAY (tachyne-gw-java-770):** consume `MsgRehome` → swap the
  backend conn to DestSID with `Hello{resume,token}` (reuse the dimension-switch
  path). This is the last piece for a visible crossing, and where real-client
  verification (Wesley) begins. Then PR4b viewers (seamless), vehicle/weather tail.
- **PR4 — gateway: viewer sessions + multi-pod stream merge + home-swap on
  `MsgRehome` (promote warm viewer→home).** The seamless/instant layer; trickiest.
  **~55%.**
- **PR5 — testbed + acceptance.** Two-pod StatefulSet; extend attachprobe (two
  probes across a seam: migrate, same eid, no disconnect, sub-tick swap, state
  intact). Then Wesley real-client sign-off (seamless feel is a perception check).

## 9. Kubernetes testbed (isolated — live world untouched)

New StatefulSet `shardtest-world` `replicas:2`, fresh seed, **throwaway** PVCs
(NOT `data-tachyne-world-0`). ConfigMap `shardtest-topology` = two edge-adjacent
16×16-chunk (256×256-block) tiles split at block x=0:

```json
{"version":1,"regions":[
  {"sid":0,"min_cx":-16,"min_cz":-8,"w":16,"h":16},  // west tile, blocks [-256,0)×[-128,128)
  {"sid":1,"min_cx":0,"min_cz":-8,"w":16,"h":16}     // east tile, blocks [0,256)×[-128,128)
]}
```

Tiles are >2× the ~128-block view radius, so each has an ~128-block **unaware
core** and a ~128-block **approach zone** near the seam (§4a). Spawn deep in the
west core (e.g. −200,y,0), walk east: the east tile's entities fade in ~128
blocks out, then you cross the seam at x=0. One gw-java-770 pointed at it (needs
the topology to pick the login/home pod). Live `tachyne-world-0` never in the blast radius; the 2026-07-09
pre-sharding backup (`~/world-backups/presharding-20260709.tgz`, sha `7a2c2ecb…`)
is the insurance.

## 10. Decision log (how we got here, 2026-07-09)

1. Descoped to hard-border islands → 2. added inter-shard handover of ALL entity
types (mobs/vehicles have no gateway) → 3. neighbour-scoped, no all-to-all spew →
4. chose FULL SEAMLESS (viewers in) → 5. dropped Postgres (owned-writes make it
unnecessary) → 6. dropped NATS from the handover path (direct pod-to-pod peer
links) → 7. handover must be practically INSTANT ⇒ pre-warmed make-before-break,
nothing lazy on the hot path → 8. NO coordinator pod — boot identity from
StatefulSet ordinal + replicated topology ConfigMap; config-time coordination
only, never runtime (§1a) → 9. ownership is an EXPLICIT REGION MAP (contiguous
rectangles), not the mod-N interleave — the world grows by appending a region
(non-disruptive), unowned chunks are the world edge, admin-assigned coords with
optional spiral sugar; the first testbed is two edge-adjacent tiles (west SID 0 /
east SID 1) split at x=0 → 10. cross-border awareness radius is FIXED at the view
radius (8 chunks / 128 blocks), not a free choice (else the border shows);
pre-warm (viewer session + player shadow) at 128 blocks out, handover at the seam
— two distinct thresholds (§4a). Test tiles enlarged to 16×16 chunks (256 blocks,
>2× view radius) so the approach/activation transition is real and testable.
gateway-carried handover: REJECTED (doesn't generalize to mobs). Continuous
coords, `MintEID` player lane, 770-only: standing.
