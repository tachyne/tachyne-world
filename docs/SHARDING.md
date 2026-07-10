# Sharding: one world, many pods — implementation spec

> **⚠ PARTIALLY SUPERSEDED (2026-07-09) — read `docs/SHARDING-BUILD.md` first.**
> That doc is the authoritative current design/build plan. This spec's
> *mechanism* detail (viewer/stream-merge model, per-viewer `EntityView`, phase
> acceptance tests, protocol gotchas) remains valid and worth reading, but its
> **infrastructure choices are replaced**: NO Postgres store (owned-writes make
> it unnecessary), NO NATS in the handover path (handover is **direct pod-to-pod**
> over pre-warmed neighbour links), handover must be **practically instant**
> (make-before-break; nothing lazy on the hot path). Where the two disagree,
> SHARDING-BUILD.md wins.

> **Status: design, not built. This document is written to be implemented
> from directly.** It supersedes the "per-region spatial shards" section of
> `docs/SCALING.md`. Design direction agreed 2026-07-06 (SIDs/epochs, gateway
> merge, make-before-break); spec written 2026-07-07 against server @55c94e9,
> tachyne-common @78074e8, gw-770 @1c2da85. All file/line references below were
> verified against those commits — re-verify before editing, code drifts.
>
> Prerequisite reading: `docs/DOMAIN-EVENTS.md`. The domain-events refactor is
> load-bearing: events carry absolute state, per-viewer rendering lives in the
> gateway (`render770.EntityView`), and the engine has no Minecraft wire code.

The goal: N **world pods**, each simulating a disjoint region of ONE shared
world, behind the existing gateway tier — players walk across region borders
without noticing. Independent Kubernetes pods; a single seamless world.

**The one invariant everything hangs on:**

> **Reads are global; writes are owned.** Every pod can *read* any chunk
> (deterministic gen + shared edit store). Only *mutation* — block edits, mob
> simulation, player survival state — is owned by exactly one pod at a time.
> Movement validation, lighting, AI line-of-sight work anywhere; only effects
> route to an owner. A player briefly homed on the "wrong" shard is degraded,
> never broken.

Why this is now cheap, in three facts:

1. **Stream merging already works.** Events are typed and absolute;
   `EntityView` derives deltas against what it actually rendered. A gateway can
   interleave frames from N pods with zero rewriting, and a mid-session stream
   switch cannot desync anything.
2. **Chunks are a pure function of (seed, GenVersion, edit store).** Any pod
   serves byte-identical chunks. Crossing a border needs no Respawn, no chunk
   churn, no client-visible anything.
3. **Handover is a domain-state transfer + a routing flip**, not packet
   surgery — the wire lives only in gateways.

---

## 1. Shared constants + identity — new package `tachyne-common/shard`

Everything gateways AND pods must compute identically lives in one new package
so it cannot drift. No dependencies beyond stdlib.

```go
// Package shard: identities and the region map for multi-pod worlds.
package shard

const (
    // MaxSIDs is the modulus of the entity-ID interleave. Never change it on a
    // live world: every persisted eid-derived fact would re-route.
    MaxSIDs = 64

    // PlayerSID is the reserved minting lane for PLAYER entity IDs (see §4.3).
    // Player eids are session-stable across handovers, so their lane must not
    // collide with any real shard's mob lane. Region maps must therefore keep
    // Shards <= 62 (SID 63 = players; SID 62 spare).
    PlayerSID = MaxSIDs - 1
)

// MintEID interleaves a per-pod counter with the minting shard's SID.
// counter is a monotonically increasing int64 (never reuse within a boot);
// the result stays positive int32. Wrap happens at ~33.5M mints per boot —
// log a warning at 2^24, accept the theoretical reuse beyond it.
func MintEID(counter int64, sid int32) int32 {
    return int32((counter*MaxSIDs + int64(sid)) & 0x7fffffff)
}

// Minter recovers the SID lane an eid was minted in. NOTE: for mobs this is
// also the CURRENT owner (mobs re-mint on migration, §9.6). For players
// (lane == PlayerSID) it is NOT ownership — resolve via the gateway's
// entity-source map or the player row.
func Minter(eid int32) int32 { return eid % MaxSIDs }
```

### 1.1 SID and epoch acquisition (pod boot)

- SID = the pod's StatefulSet ordinal. `cmd/server/main.go` gains
  `-sid <n>` (default 0) and, if `-sid` is unset and env `POD_NAME` is set,
  parses the trailing `-<ordinal>` (`tachyne-world-3` → 3). Fail hard on a
  malformed POD_NAME — a wrong SID corrupts the world.
- Epoch: claimed from Postgres at boot, **before** the hub starts and before
  the attach listener opens (§3.2). The pair `(sid, epoch)` is immutable for
  the process lifetime; store it on `Server` and `hub`.

---

## 2. The region map

A **pure function** `ShardOf(dim, cx, cz) → sid` that every component
computes identically. Parameters come from one ConfigMap; the function itself
is code in `tachyne-common/shard`.

```go
// Map is the world's ownership partition. Marshal deterministically (fields
// in this order, no omitempty) — TopoHash() hashes the canonical JSON.
type Map struct {
    Version    int32 `json:"version"`     // bump on ANY parameter change
    Shards     int32 `json:"shards"`      // active shards; 1..62
    CellChunks int32 `json:"cell_chunks"` // ownership cell side, in chunks
}

// ShardOf returns the owning SID for a chunk. The world is tiled into
// CellChunks×CellChunks cells; adjacent cells map to different shards via a
// fixed 2D interleave. Same function for every dimension (a nether cell and
// an overworld cell at the same coords may have different owners — that is
// fine, ownership is per-(dim,chunk)).
func (m Map) ShardOf(dim, cx, cz int32) int32 {
    if m.Shards <= 1 {
        return 0
    }
    cellX := floorDiv(cx, m.CellChunks)
    cellZ := floorDiv(cz, m.CellChunks)
    return mod(cellX+cellZ*31, m.Shards) // 31: coprime-ish spread, stable forever
}

// ShardsIn returns the distinct SIDs owning any chunk in the square window
// [cx±r, cz±r] — the gateway's Want fan-out set. Iterates CELLS, not chunks.
func (m Map) ShardsIn(dim, cx, cz, r int32) []int32

// Border reports whether the chunk is within h chunks of a cell whose owner
// differs — the hysteresis/dual-stream predicate (§9.2).
func (m Map) Border(dim, cx, cz, h int32) bool

func (m Map) TopoHash() string // hex sha256 of canonical JSON
```

`floorDiv`/`mod` are floored (not truncated) division — negative chunk coords
must partition continuously. Copy the pattern from `worldgen` (it already does
floored chunk math); table-test both around zero.

**Defaults:** `CellChunks = 64` (1024 blocks — borders are rare),
`Shards = <replica count>`, `Version = 1`.

**Distribution:** the ConfigMap `tachyne-world-topology` (§10) carries the Map
JSON plus `seed` and `genVersion`. World pods mount it and **refuse to start**
if their compiled `worldgen.GenVersion` disagrees — two gen versions in one
world means seam tearing. Gateways receive the map the same way (mounted
ConfigMap) AND assert at attach time that every upstream's `Welcome.Topo`
equals their own `TopoHash()` — a mixed-topology cluster must fail loudly at
session start, not corrupt at a border.

**Changing the map** (rebalancing, changing N) is a coordinated cutover: not
built in v1. `Version` exists so it *can* be; until then, N is chosen at world
creation. Document in the ConfigMap: "do not edit on a live world".

---

## 3. The shared store (Postgres)

CNPG Postgres in the `databases` ns, same credential pattern as
`tachyne-access` (database `tachyne`, new schema `worldstate`, new role
`tachyne_world` with `search_path=worldstate`). Driver: `pgx` stdlib-style via
`database/sql` — this is the second accepted external dep (after the CNPG
break for tachyne-access); no ORM.

New engine package: `internal/store` (Postgres client, fencing, batching).
`internal/world` keeps zero knowledge of SQL — it sees interfaces (§3.4).

### 3.1 DDL

```sql
CREATE SCHEMA IF NOT EXISTS worldstate;
SET search_path TO worldstate;

CREATE TABLE shard_epochs (
  sid          int PRIMARY KEY,
  epoch        bigint      NOT NULL,
  claimed_by   text        NOT NULL,          -- pod name, diagnostics only
  claimed_at   timestamptz NOT NULL DEFAULT now(),
  heartbeat_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE chunk_edits (
  dim   smallint NOT NULL,
  cx    int      NOT NULL,
  cz    int      NOT NULL,
  idx   int      NOT NULL,   -- (y-minY)*256 + z*16 + x, 0..98303 (24 sections)
  state bigint   NOT NULL,   -- block-state id (uint32; bigint dodges sign)
  PRIMARY KEY (dim, cx, cz, idx)
);
-- The PK index serves the only read pattern: load one chunk's edits.

CREATE TABLE players (
  name              text PRIMARY KEY,          -- offline mode: name = identity
  uuid              uuid        NOT NULL,
  eid               int         NOT NULL,      -- current session eid (player lane)
  home_sid          int         NOT NULL DEFAULT -1,  -- -1 = offline/released
  home_epoch        bigint      NOT NULL DEFAULT 0,
  state             jsonb       NOT NULL,      -- PlayerState doc, §4.1
  handover_token    uuid,                      -- non-NULL while a rehome is in flight
  handover_dest     int,
  handover_deadline timestamptz,
  updated_at        timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE world_meta (
  key   text PRIMARY KEY,   -- 'clock', 'weather', 'spawn'
  value jsonb NOT NULL
);

CREATE SEQUENCE player_eid_seq;
```

### 3.2 Epoch claim + heartbeat

At boot, in one transaction:

```sql
INSERT INTO shard_epochs (sid, epoch, claimed_by)
VALUES ($1, 1, $2)
ON CONFLICT (sid) DO UPDATE
  SET epoch = shard_epochs.epoch + 1, claimed_by = $2,
      claimed_at = now(), heartbeat_at = now()
RETURNING epoch;
```

Failure to claim = failure to start (crash loop is correct — no epoch, no
authority). Then a goroutine updates `heartbeat_at = now() WHERE sid=$1 AND
epoch=$2` every 5 s; if that UPDATE ever reports 0 rows, **the pod has been
superseded: log and `os.Exit(1)` immediately** (a newer incarnation claimed
our SID — we are the zombie).

### 3.3 Fencing protocol (every write)

Every write transaction begins with the fence check:

```sql
SELECT 1 FROM shard_epochs WHERE sid = $1 AND epoch = $2 FOR SHARE;
```

Zero rows → `ErrFenced`: abort the tx, and treat it like a failed heartbeat
(exit). `FOR SHARE` vs the claim's row UPDATE (which takes a row lock)
linearizes claim-vs-write: a concurrent new claimant blocks until in-flight
fenced writes commit, then every later write from the old epoch fails. Wrap
this in one helper so it cannot be forgotten:

```go
// internal/store/store.go
type Fence struct{ SID int32; Epoch int64 }

// WithFence runs fn inside a tx that has passed the fence check.
func (s *PG) WithFence(ctx context.Context, f Fence, fn func(tx *sql.Tx) error) error
```

### 3.4 Chunk-edit store — the `world.Store` swap

`internal/world/store.go`'s existing interface is whole-map Load/Save (built
for `world.gob`). Add the per-chunk interface alongside it; `FileStore` stays
for solo dev (`-world world.gob` keeps working, N=1 only).

```go
// internal/world/store.go
type ChunkStore interface {
    // LoadChunk returns one chunk's edit overlay (empty map = no edits).
    LoadChunk(dim, cx, cz int32) (map[int]uint32, error)
    // WriteEdits persists a batch (fenced). idx/state as in chunk_edits.
    WriteEdits(dim int32, edits []Edit) error
}
type Edit struct {
    CX, CZ int32
    Idx    int32
    State  uint32
}
```

The Postgres impl (`internal/store/chunks.go`) binds the fence at
construction: `store.NewChunkStore(pg, fence, dim)` — `WriteEdits` is a single
fenced tx with a multi-row `INSERT ... ON CONFLICT (dim,cx,cz,idx) DO UPDATE
SET state = EXCLUDED.state`. Breaking a block back to its *generated* state
still writes a row (the overlay stores "player changed this", not "differs
from gen" — matches today's semantics in `world.edits`).

**`internal/world/world.go` changes** (the mutable-world side):

- `World` gains `cstore ChunkStore` (nil = legacy whole-map mode) and
  `editsLoaded map[chunkPos]bool`.
- **Lazy load:** every read path that consults `w.edits` (chunk assembly,
  `At`, `BlockAt`) first calls `w.ensureEdits(cx, cz)` under the write lock:
  if `!editsLoaded[cp]`, `LoadChunk` and install. Store error → empty overlay
  + `log.Printf` + mark loaded=false so the next touch retries (reads
  fail-open; the chunk renders as pure generation until the store answers).
- **Write-through, batched:** `SetBlock` applies to `w.edits` in memory
  exactly as today (players see edits instantly), then enqueues the Edit on
  `w.pendingWrites chan Edit` (cap 8192). One flusher goroutine drains it:
  flush when 512 edits are buffered or 200 ms after the first, whichever
  first. On store error: retry with capped backoff (1 s → 30 s) holding the
  batch; **never drop** — and surface store health (§3.6). This is at most
  one tx per 200 ms per pod; a piston spam session is a few hundred rows per
  tx. Fine.
- **Eviction:** when the generated-chunk LRU evicts a chunk, also drop its
  `editsLoaded` entry and overlay **only if** no pending write references it
  (simplest correct rule: flusher completion clears a per-chunk pending
  count; evict only at zero).
- **Foreign-chunk reads** (movement validation, mob AI looking across a
  border): same lazy-load path — reads are global. Overlays for chunks the
  pod does NOT own get a 5 s TTL (`editsLoadedAt`); on expiry the next read
  reloads. Owned chunks never expire (we are the only writer, our memory is
  truth). `World` therefore needs to know ownership: give it a callback
  `Owned func(cx, cz int32) bool` set at construction (nil = own everything).

`Save()` / the autosave goroutine / `-world` flag: unchanged in legacy mode;
in ChunkStore mode `Save()` is a no-op (write-through already persisted) and
autosave only logs pending-queue depth.

### 3.5 Player, meta stores

```go
// internal/store/players.go — all methods fenced where they write.
type PlayerStore interface {
    // Login resolves where a connecting player should be homed. See §4.2.
    Login(name string, uuid [16]byte, me Fence) (LoginResult, error)
    // Resume claims a handover token. See §9.4.
    Resume(name string, token uuid.UUID, me Fence) (PlayerState, int32, error) // state, eid
    // Release writes the final snapshot and marks the row in-flight. §9.3.
    Release(name string, me Fence, snap PlayerState, dest int32) (uuid.UUID, error)
    // Checkpoint persists a periodic snapshot (30 s cadence, replaces invStore flush).
    Checkpoint(name string, me Fence, snap PlayerState) error
    // Logout snapshots and sets home_sid = -1.
    Logout(name string, me Fence, snap PlayerState) error
}
```

`PlayerState` (the `state` jsonb doc) is one Go struct in `internal/store`
mirroring what `invStore.record` + `modes` + `spawns` persist today — dim,
x/y/z/yaw/pitch, gamemode, health/food/saturation, xpLevel/xpPoints,
inventory (hotbar+main+armor+offhand as `[]savedStack{Slot,Item,Count,Dmg,Ench}`),
active effects, bed spawnpoint. Field-for-field port of the existing
`players.json`/inventory gob records — do it mechanically, keep names.

`world_meta`: `MetaStore.Get/Set(key string, v any)` (Set fenced). Keys:
`clock` (§5.1), `weather` (§5.3), `spawn`.

### 3.6 Store health

Postgres down must not kill gameplay (players keep playing on in-memory
state) but must be loud: `Server` exposes `Healthy() bool` (store reachable,
pending-writes queue < 75% cap, heartbeat succeeding) wired to a readiness
probe on a tiny HTTP listener `:8081/readyz` (new, trivial). Liveness stays
TCP :25500. Readiness-fail keeps traffic away and alerts without murdering a
pod that is mid-recovery.

---

## 4. Player identity, login, eids

### 4.1 PlayerState is the handover payload

There is deliberately no separate "handover message body": the store row IS
the transfer medium. Whatever a pod needs to resume a player must be in
`PlayerState` — if you add a survival field to `tracked` that must survive a
border crossing, you add it to `PlayerState` or it silently resets on rehome.
Add a test that constructs `tracked` from `PlayerState` and back and asserts
round-trip equality (`internal/server/handover_test.go`).

### 4.2 Login flow (gateway ↔ pods ↔ store)

Gateways never talk to Postgres. The world pods answer placement questions
via the attach protocol:

1. Gateway authorizes via tachyne-access (unchanged), then dials **any**
   world pod — default `tachyne-world-0` — with
   `Hello{Purpose: "login", ...}`.
2. That pod runs `PlayerStore.Login` in one tx:
   - Row absent → INSERT with `home_sid = my sid`, fresh eid (§4.3), state =
     spawn defaults → **I am home**: proceed to Welcome.
   - `home_sid == my sid` (crash leftover or reconnect) → refresh
     `home_epoch = my epoch`, proceed.
   - `home_sid == -1` (clean logout) → claim: `UPDATE ... SET home_sid=me,
     home_epoch=mine WHERE name=$1 AND home_sid=-1`; if the row's saved
     position now maps to a *different* shard (`ShardOf(state.dim,
     state.cx, state.cz) != me`), do NOT claim — reply
     `Redirect{Sid: shardOf(...)}` instead.
   - Homed elsewhere and that shard's heartbeat is fresh (< 15 s) → reply
     `Redirect{Sid: home_sid}` (covers gateway retry after a gateway crash —
     the old session may still be draining; the home pod resolves the
     duplicate exactly as the hub does for a TCP double-join today: new
     session wins, old `Leave`s).
   - Homed elsewhere and heartbeat **stale** → dead-pod override:
     `UPDATE ... SET home_sid=me, home_epoch=mine WHERE name=$1 AND
     home_sid=$old AND home_epoch=$old_epoch` (guarded by the row's own
     values — two rescuers race safely; loser gets 0 rows → re-run Login).
3. Gateway on `Redirect{sid}`: dial `tachyne-world-{sid}...` and repeat (max
   3 hops, then disconnect the client with "world unavailable").

### 4.3 Player eids — the reserved lane

Minted once per session at first Login (not per pod):
`eid = shard.MintEID(nextval('player_eid_seq'), shard.PlayerSID)`, stored in
the row, reused verbatim by every Resume. Viewers therefore see ONE stable
eid across handovers — no despawn/respawn of the player entity (the old home
emits no `EntityRemove` for a rehomed player; the new home's first
`EntityAdd` for that eid is idempotent at the gateway: `EntityView.Add`
overwrites the tracker entry; verify `render770.EntityView.Add` handles
re-add by replacing state — it does, `pos[eid]` is a plain map write — and
add a test pinning that).

Mob eids: `shard.MintEID(h.eidCounter, h.sid)` — see §7.1.

---

## 5. Global state: clock, chat, weather

### 5.1 The world clock is derived, not broadcast

Replace hub-owned `dayTime` advancement with a shared formula. `world_meta`
key `clock`:

```json
{"anchor_unix_ms": 1751846400000, "anchor_daytime": 6000, "frozen": false}
```

`dayTime(now) = frozen ? anchor_daytime : (anchor_daytime + (now_ms − anchor_unix_ms)/50) mod 24000`.

- `hub.dayTime` becomes a cached computation refreshed each tick from the
  loaded clock doc (no store round-trip per tick — the doc is in memory).
- `/time set N` (and `doDaylightCycle false` → `frozen`) = `MetaStore.Set`
  with a new anchor + NATS publish `mc.shard.meta {"key":"clock"}`; every pod
  (including the setter) reloads on the NATS message. NATS down → pods also
  poll `world_meta` every 60 s (fail-open convergence).
- `hub.tick` (world *age*, drives scheduling) stays per-pod and local — only
  day-time is shared. `timeEv` (hub.go:390) keeps sending `Age` from the
  local tick; nothing renders divergently from that.

### 5.2 Global chat / broadcast fan-out

`evChat` today iterates local players. Sharded, a chat line must reach
players homed on every pod. NATS subject `mc.global.bcast`, payload:

```json
{"sid": 2, "epoch": 7, "kind": "chat", "text": "<wesley> hi"}
```

- Hub change: the `evChat` handler (and `/say`, join/leave notices,
  sleep-skip announcements — every "to everyone" send) goes through one new
  `h.broadcastGlobal(kind, text)` which (a) delivers locally, (b) publishes.
- NATS consumer: on `mc.global.bcast`, if `msg.sid == h.sid` skip (own echo),
  else deliver locally as a plain local broadcast. No re-publish — no loops
  possible with one hop, but keep the origin tag anyway (provenance, and the
  bus doctrine says every inter-server message carries `(sid, epoch)`).
- NATS down: chat degrades to per-shard. Acceptable; log once.
- `/list`: count from the store (`SELECT name FROM players WHERE
  home_sid >= 0`) — cheap, exact enough.

### 5.3 Weather

One owner: the pod whose SID equals `ShardOf(0, 0, 0)` (deterministic, no
election) runs the weather cycle and writes `world_meta['weather']`
(`{"raining":bool,"thundering":bool,"until_unix_ms":...}`) + publishes
`mc.shard.meta`. Everyone else applies it read-only (rain render events,
mob-spawn modifiers). Lightning strikes stay owner-local (they mutate blocks
— owned writes only). If the weather owner is down, weather freezes; shrug.

---

## 6. Attach protocol changes (tachyne-common/attach)

All additive. New frame IDs take the 0x50 block (0x42–0x4f left free for
ordinary event growth).

### 6.1 Extended Hello / Welcome

```go
type Hello struct {
    // ... existing fields ...
    Purpose string `json:"purpose,omitempty"` // "" or "home" | "login" | "view" | "resume"
    Resume  string `json:"resume,omitempty"`  // handover token (uuid) when Purpose=="resume"
}

type Welcome struct {
    // ... existing fields ...
    SID      int32  `json:"sid"`
    Epoch    int64  `json:"epoch"`
    Topo     string `json:"topo"`      // shard.Map TopoHash — gateway asserts match
    Gamemode int32  `json:"gamemode"`  // fixes the known joinPacket-hardcodes-survival debt
}
```

`Purpose` semantics at the world pod (`internal/attach/attach.go session()`):

| Purpose | Store action | Hub action | Serves |
|---|---|---|---|
| `""`/`"home"` | none (solo/dev, Join nil) or as `"login"` | JoinRemote | everything (today's behavior) |
| `"login"` | `PlayerStore.Login` → maybe `Redirect` | JoinRemote at stored position/state | everything |
| `"resume"` | `PlayerStore.Resume(token)` → maybe `Bye` | JoinRemote **from PlayerState** (no spawn reset, no join broadcast, no "joined the game" chat) | everything |
| `"view"` | none | `JoinViewer` (§7.3) — NO hub player | chunks (owned only) + spatial events in the Want window |

### 6.2 New frames

```go
const (
    MsgRedirect      = 0x52 // w→gw JSON Redirect{sid}: this player is homed elsewhere
    MsgRehome        = 0x54 // w→gw JSON Rehome{sid, token}: I released your player; resume there
    MsgViewerWindow  = 0x55 // (reserved; v1 reuses Want for viewer windows)
)

type Redirect struct {
    Sid int32 `json:"sid"`
}

// Rehome: the home pod has written the player's snapshot + token to the
// store and STOPPED simulating them. The gateway must Hello{resume} at Sid
// within the token deadline (10 s).
type Rehome struct {
    Sid   int32  `json:"sid"`
    Token string `json:"token"`
}
```

### 6.3 Ownership filtering of Want (no Want change)

Want keeps its shape. The **pod** filters: in the `MsgWant` handler
(`internal/attach/attach.go:252`), skip chunks where
`ShardOf(w.Dim, cx, cz) != mySID` (do not mark them `sent`). The gateway
sends the same Want to every intersecting shard (`Map.ShardsIn`); each pod
serves exactly its own chunks. Zero double-sends, zero gaps, no rect-set
protocol needed.

### 6.4 Frame classification: home vs view sessions

View sessions must not duplicate what the home session sends. Rule enforced
POD-side by construction: a viewer has no hub player, so it can only ever
receive what the viewer subscription forwards. The viewer forwards **spatial
frames only**:

- Spatial (viewer gets them, filtered to its Want window + dim):
  `EntityAdd/Move/Head/Remove`, `PlayerInfo/PlayerGone`*, `BlockSet`,
  `Sound`, `Particles`, `WorldFX`, `EntityStatus`, `Swing`, `Velocity`,
  `Equipment`, `EntityMeta`, `Passengers`, `VehicleMove`, `Chunk`.
- Home-only (everything else): chat/UI, survival state, windows, abilities,
  time, dimension/teleport, boss bars, etc.

\* PlayerInfo (tab list) is global-ish but must accompany EntityAdd of a
player entity or the client renders no skin. Simplest correct rule: viewer
forwards PlayerInfo/PlayerGone; **gateway dedups tab-list entries by uuid**
(keep a set; drop repeats). Tab-list "N online" comes from home only.

---

## 7. Engine changes (minecraft/server), file by file

### 7.1 `internal/server/hub.go`

- Fields: `sid int32`, `epoch int64`, `regionMap shard.Map`,
  `eidCounter int64` (replaces `nextEID int32`), `players *store.PG`
  handles… (wire actual store handles on `Server`, hub gets narrow funcs).
- `allocEID()` (line 401): `return shard.MintEID(atomic.AddInt64(&h.eidCounter, 1), h.sid)`.
  Player join paths must NOT use `allocEID` anymore — `JoinRemote` receives
  the eid from `Login`/`Resume` (§7.4).
- `owned(dim int, x, z int) bool` helper:
  `h.regionMap.ShardOf(int32(dim), int32(x>>4), int32(z>>4)) == h.sid`.
- **Ownership gates** (each is a 1–3 line guard, but they are the correctness
  of the whole design — review as a set):
  - `handleDig`/`handlePlace`/`evSetBlock`: reject non-owned targets
    (`log.Printf` — the gateway should never have routed it here).
  - `sim.go schedule()`: drop updates for non-owned positions (v1
    sim-stops-at-border; §11 upgrades this).
  - Mob spawning (`wildSpawn`, `waterSpawn`, `updateHostiles`,
    `updateSpawners`, herd seeding in `run()`): candidate positions filtered
    by `owned`.
  - Mob movement (`updateMobs`): a mob stepping into a non-owned cell
    triggers migration (§9.6) — v1 may simply clamp (behavior turns at the
    border) and that is acceptable to ship.
  - `runRandomTicks`, furnace/brewing/redstone maps: these are keyed by
    blockPos the pod placed/loaded itself — they only ever contain owned
    positions once spawning/scheduling is gated; add a debug assert, not a
    filter.
- **Broadcast fan-out**: `evChat` + every "send to all players" loop →
  `broadcastGlobal` (§5.2).
- **Time**: delete `dayTime.Add(1)` advancement; compute from the clock doc
  (§5.1). `/time` handler moves to MetaStore.Set + publish.
- **Checkpointing**: the 30 s `h.invs.record/flush` block (hub.go:536) is
  replaced by `PlayerStore.Checkpoint` per player (same cadence). Container
  stores (furnaces/chests/bins/items) stay on the per-pod PVC in v1 — they
  are position-keyed and only ever owned positions, so pod-local files remain
  correct. (Migrating containers to Postgres is a later, independent chore;
  note it in the debt list.)

### 7.2 `internal/server/hub.go` — the viewer registry

New lightweight subscriber type, hub-goroutine-owned like `players`:

```go
type viewer struct {
    out    chan<- outPkt   // same pump as a player session
    quit   <-chan struct{}
    dim    int32
    cx, cz int32           // window center (updated by Want)
    radius int32
}
```

- `evViewJoin{v *viewer}` / `evViewMove{v, dim, cx, cz, r}` / `evViewLeave`.
- Every **spatial** emission point (`toNearbyEv` and the entity/effect
  broadcast helpers) additionally iterates `h.viewers` and `trySendEv`s to
  viewers whose window contains the event position. Factor one helper:
  `h.emitSpatial(dim, x, z, ev)` — then convert emission sites to call it;
  the compiler finds them all when `toNearbyEv`'s signature changes. Dropped
  frames stay safe (absolute events, per-viewer render at the gateway).
- Viewers are cheap: no tracked entry, no survival state, no join broadcast.

### 7.3 `internal/attach/attach.go`

- Parse `Hello.Purpose`; dispatch per §6.1 table.
- `"view"`: skip `cfg.Join`; register a viewer via new
  `cfg.View func(dim, cx, cz, r int32, emit func(typ byte, payload []byte)) (Viewer, error)`
  (mirrors Join; `Viewer` has `Move(dim,cx,cz,r)` and `Leave()`). MsgWant
  updates both the chunk streamer AND the viewer window.
- Want handler: ownership filter (§6.3) — needs `cfg.Owned func(dim, cx, cz int32) bool`.
- `"login"`/`"resume"`: thread through to new `cfg.Login`/`cfg.Resume`
  callbacks on Config (wired in `cmd/server/main.go` to Server methods);
  on Redirect outcome, send `MsgRedirect` and close.
- Welcome: fill `SID/Epoch/Topo/Gamemode`.

### 7.4 `internal/server/remote.go` + `server.go`

- `JoinRemote` signature grows: `JoinRemote(name string, uuid [16]byte, st *store.PlayerState, eid int32, emit ...)`.
  With `st != nil`: position/dim/gamemode/survival state seeded from the
  snapshot; **resume mode** additionally skips the join chat line and join
  sounds. `evJoin` gains `resume bool` and `state *store.PlayerState`; the
  hub's onJoin fills `tracked` from it (the round-trip test in §4.1 pins
  this mapping).
- New `Server.RehomeCheck(p *player, x, z float64)` called from
  `remotePlayer.Move` right next to `checkPendingDim` (remote.go:129): if
  `!owned(...)` for `H_out` consecutive checks (§9.2), run Release (§9.3)
  and emit `MsgRehome` on the session.
- `Server.New()` gains the store handles + `shard.Map` + `(sid, epoch)`.

### 7.5 `cmd/server/main.go`

Flags/env: `-sid` (+POD_NAME fallback), `-pg <dsn>` (or env `PG_DSN`; empty =
legacy file mode, sharding features off), `-topology <path>` (mounted
ConfigMap JSON; absent = `Map{Version:1, Shards:1, CellChunks:64}`). Wire:
claim epoch → construct stores → `world.NewWithChunkStore(...)` → Server.

Startup order matters and is easy to get wrong: **epoch claim → store
handles → world load → hub start → attach listener**. The listener opening
is the last thing; readiness (§3.6) gates traffic anyway.

---

## 8. Gateway changes (tachyne-gw-java-770; 776 is the same code + translator)

Today `session()` dials one backend (`session.go:76`) and `play()` threads
one conn `w` through both pumps. That becomes an upstream set.

### 8.1 Types

```go
// upstreams manages one client's world connections.
type upstreams struct {
    s      *Server
    hello  attach.Hello        // identity, resent per dial (Purpose varies)
    mu     sync.Mutex
    home   int32               // home SID
    conns  map[int32]*upConn   // by SID
    frames chan taggedFrame    // fan-in, cap 512
    dead   chan int32          // upstream reader exit notifications
}
type upConn struct {
    sid  int32
    c    net.Conn
    wmu  sync.Mutex            // frame-write serialization
    view bool                  // true = Purpose:"view" session
}
type taggedFrame struct {
    sid     int32
    typ     byte
    payload []byte
}
```

- `dial(sid, purpose, resumeToken)` → `tachyne-world-{sid}.tachyne-world.tachyne.svc:25500`
  (template from env `WORLD_ADDR_PATTERN`, default that; dev override
  `-world-addr-{n}` flags). Reads Welcome, asserts
  `welcome.Topo == s.topoHash`, spawns a reader goroutine pushing
  `taggedFrame`s and reporting on `dead`.
- The login flow (§4.2) happens before `play()`: dial with
  `Purpose:"login"`, follow up to 3 `MsgRedirect`s, keep the final Welcome
  (it carries EID/spawn/gamemode). That conn becomes `conns[home]`.

### 8.2 The two pumps, updated

**World→client** (the big switch, session.go:201): reads from `ups.frames`
instead of one conn. Changes inside the switch:

- `MsgEntityAdd`: record `entitySrc[e.EID] = f.sid` (see routing); for a
  player-lane eid already tracked, this is a handover re-add — EntityView
  overwrites, no packet oddity.
- `MsgEntityRemove`: delete `entitySrc[eid]` **only if** `f.sid ==
  entitySrc[eid]` (an old home's despawn must not clobber the new home's
  claim — this one condition is what makes mob migration flicker-free at
  the gateway when §9.6 lands; write the unit test).
- `MsgPlayerInfo`/`Gone`: dedup by uuid (§6.4).
- `MsgRehome`: run the resume sequence (§9.4). This arrives on the OLD home's
  session; handle it inline (it is ordered after that pod's last frame for
  this player — exactly what we want).
- `MsgRedirect`: only legal during login; mid-session = protocol error, drop
  session.
- `MsgTime`/`MsgChat`/survival/windows: unchanged (they only ever arrive on
  the home session).
- `MsgBye`/reader death of a **view** upstream: drop it, clear its
  `entitySrc` entries (emit EntityRemove to the client for entities sourced
  from it — they will re-add when the upstream returns), retry dial with
  backoff while its shard still intersects the window.
- `MsgBye`/death of the **home** upstream: the recovery path (§9.5).

**Client→world** (session.go:484): every `attach.WriteJSON(w, ...)` becomes a
routed send:

| Serverbound | Route |
|---|---|
| Move, Chat, Command, HeldSlot, UseItem, PlayerAction, WindowClick/Close, Craft, NameItem, Enchant, SelTrade, Input, RespawnReq, CreativeSlot | `home` |
| Dig, Place | `ShardOf(curDim, x>>4, z>>4)` from the frame's block coords |
| UseEntity | `entitySrc[target]`; fallback `shard.Minter(target)`; fallback `home` |
| VehicleMove | `entitySrc[vehicle eid]` if riding is tracked, else `home` |
| Want (on center-chunk change, session.go:613) | every SID in `Map.ShardsIn(dim, ccx, ccz, viewRadius)`: ensure a `view` upstream exists (except `home`, which gets it on its own session), send the Want to each |

Dig/Place routed to a non-home shard arrive on a **view session** — a
session with the player's identity (Hello.Name) but no hub player. Do NOT
try to reuse `handleDig/handlePlace`'s player plumbing there, and do NOT
bounce the edit through the home pod (that would make the home pod write a
foreign block — violating writes-are-owned). The design:

- **Shadow position.** The gateway sends Move to `home` AND to every
  currently-active border-adjacent view upstream (Move frames are tiny).
  Each view session keeps a session-local `viewerPos` (plain struct in the
  attach session, no hub event) — that gives the owner pod reach-check
  context for Dig/Place.
- **Shadow dig timing.** Survival dig progress (`digStartAt`/`digPos`
  equivalents) also lives session-local on the view session — dig timing is
  a property of the digger-block pair, and the block's owner is the
  authority on it.
- **Gamemode claim.** New `Hello.Claims map[string]any` field (v1 key:
  `"gamemode"`), stamped by the gateway from the home Welcome and refreshed
  when the home session delivers a gamemode-change GameEvent. The owner pod
  uses it for creative-vs-survival dig/place rules.
- Pod-side entry points: `Config.ViewerDig/ViewerPlace(v *Viewer, ...)`
  callbacks that run the same block-mutation + broadcast + drop-spawning
  logic as the homed path, taking (pos, face, claims, viewerPos) instead of
  a `*player`. Extract the shared core out of `handleDig`/`handlePlace`
  first (pure refactor commit), then wire both callers to it.

This confines every cross-shard interaction complication to the
view-session handler in `internal/attach`; the hub only ever applies owned,
authority-checked mutations. (Item drops from a foreign player's dig spawn
on the OWNER shard — correct: drops are world entities and belong to the
block's region; the player walks over and their HOME pod's pickup scan
can't see them… so pickups of foreign-region drops are v1-degraded: the
item is collectable only once the player rehomes or the mob-migration
machinery (§9.6) later carries items too. Note this in the debt list.)

### 8.3 Buffering during rehome

Between `MsgRehome` arriving and the new home's Welcome, client serverbound
frames destined for `home` queue in a slice (cap 256, drop-oldest beyond;
Moves collapse to latest). On resume: flush in order. Target: the whole flip
under one tick (the resume claim is one UPDATE); the buffer exists for the
p99, not the mean.

---

## 9. Handover ("rehome") — the full protocol

### 9.1 Ownership of the decision

The **home pod** decides (it has the map + validated position — the gateway
could, but the pod's position is the authoritative one). The gateway
executes. The store arbitrates.

### 9.2 Trigger + hysteresis

In `RehomeCheck` (per validated Move, §7.4): compute `dest =
ShardOf(dim, cx, cz)`. Maintain per-player `foreignStreak int` on `tracked`:
`dest != h.sid` increments, else zeroes. Trigger Release when
`foreignStreak >= 40` moves (~2 s at movement cadence) **and** the chunk is
`>= 2` chunks past the cell edge (`!Map.Border(dim, cx, cz, 2)` from the
dest side — i.e. deeply foreign, not skimming). Both conditions together are
the hysteresis: border-dancing never triggers; a committed crossing triggers
once, ~2 s in, while the player is still well within the (radius 6) view of
both shards.

### 9.3 Release (home pod)

```sql
-- inside WithFence(me):
UPDATE players SET
  state = $snapshot, handover_token = $newToken, handover_dest = $dest,
  handover_deadline = now() + interval '10 seconds',
  home_sid = -1, home_epoch = 0, updated_at = now()
WHERE name = $name AND home_sid = $mySid AND home_epoch = $myEpoch;
```

- 0 rows → we were already fenced/raced; tear the session down (Bye).
- 1 row → the pod immediately: stops simulating the player (remove from
  `players` map — but do NOT emit PlayerGone/EntityRemove broadcasts;
  neighbors keep rendering the eid, the new home continues its move stream),
  then emits `MsgRehome{dest, token}` on the attach session, then treats the
  session as a *zombie*: serverbound frames from it are dropped (the gateway
  stops sending anyway), clientbound spatial events continue only if the
  session also had a viewer role — v1: the gateway immediately re-Hellos
  this same TCP conn? No — **v1 keeps it simple: after MsgRehome the old
  session carries nothing for this player; the gateway opens/reuses a
  `view` session to the old home for continued border visibility** and
  closes the old home session (Bye) once the view session's Want is acked
  by first chunk/frame.

### 9.4 Resume (gateway → dest pod)

Gateway: ensure upstream to `dest` (it almost always exists as a view
session — **upgrade in place is NOT supported**; open a NEW conn with
`Hello{Purpose:"resume", Resume: token}`; the view session stays a view
session for symmetry and simpler pod code). Pod:

```sql
UPDATE players SET
  home_sid = $mySid, home_epoch = $myEpoch,
  handover_token = NULL, handover_dest = NULL, handover_deadline = NULL
WHERE name = $name AND handover_token = $token AND handover_deadline > now()
RETURNING state, eid;
```

- 1 row → `JoinRemote` in resume mode from `state` (same eid), reply Welcome
  (its Spawn = the snapshot position — the gateway does NOT re-sync the
  client position on resume: the client never stopped walking; **do not send
  SyncPosition**, suppress the `spawnSync` once-guard on resume sessions).
- 0 rows → token expired/raced: `Bye{"stale handover"}`; gateway falls back
  to the full login flow (§4.2) — worst case the player gets one position
  re-sync. Log loudly; this path indicates timing problems.

### 9.5 Failure matrix

| Failure | Detection | Outcome |
|---|---|---|
| Dest pod down at Release time | `RehomeCheck` consults nothing about dest — Release happens anyway; gateway's resume dial fails | Gateway retries dial 3× (250 ms backoff), then falls back to login flow → Login sees released row (`home_sid=-1`), position maps to dest, dest is down → `Redirect` loops fail → gateway parks the player on ANY live shard via override rule: Login's claim-released branch accepts `ShardOf != me` **iff** dest heartbeat is stale. Degraded (foreign-homed, edits near them route correctly anyway) — self-heals on next crossing/reconnect. |
| Home pod dies mid-play (no Release) | Gateway home-upstream reader errors | Gateway holds the CLIENT (keepalive keeps flowing — the keepalive ticker is gateway-local, session.go:472), shows action-bar "world shard restarting…", retries login flow every 2 s. Login's stale-heartbeat override claims the row on any live shard (or the restarted pod itself, fresh epoch). State loss: since last Checkpoint (≤ 30 s) — same blast radius as today's crash story. Client never disconnects. |
| Token expires (gateway stalled > 10 s) | Resume returns 0 rows | Fallback to login flow; released row claimable by anyone; one visible position sync. |
| Old home's stale frames after SWITCH | None needed | Harmless by construction: absolute events, EntityView per-viewer, `entitySrc` remove-guard (§8.2). |
| Both shards think they're home (split brain) | Impossible at the store: home is one row, transitions are guarded CAS; a fenced/stale pod's writes all fail §3.3 and it exits on heartbeat | — |

### 9.6 Mob migration (border-crossing mobs)

v1 clamps mob movement at cell edges (§7.1). v1.5 (small, do soon after):
in `updateMobs`, a mob whose next step leaves owned territory is serialized
(type, pos, health, behavior name, herd) into a NATS message
`mc.shard.mig.<destSid>` `{sid, epoch, mob:{...}}`; the owner deletes it
locally + broadcasts `EntityRemove`. Dest pod (subscribed to
`mc.shard.mig.<mySid>`): validates the source epoch against `shard_epochs`,
re-mints (`MintEID(counter, mySid)`), spawns, `EntityAdd`. Viewers see
remove+add — a flicker on mobs is acceptable (players never re-mint).
Idempotency: include a `mig_id` uuid; dest keeps a 60 s dedup set (NATS
at-least-once).

---

## 10. Kubernetes

```yaml
# ConfigMap tachyne-world-topology (namespace tachyne)
apiVersion: v1
kind: ConfigMap
metadata: {name: tachyne-world-topology, namespace: tachyne}
data:
  topology.json: |
    {"version":1,"shards":2,"cell_chunks":64}
  seed: "20260630"
  # genVersion is asserted by pods against their compiled worldgen.GenVersion
  genVersion: "9"
```

StatefulSet deltas (repo `minecraft/tachyne-world`, house style — nonroot,
drop-all, RO rootfs, forgejo-pull — unchanged):

```yaml
spec:
  replicas: 2                       # == topology shards; keep in lockstep
  serviceName: tachyne-world        # existing headless svc → per-pod DNS
  template:
    spec:
      containers:
      - name: world
        env:
        - name: POD_NAME
          valueFrom: {fieldRef: {fieldPath: metadata.name}}
        - name: PG_DSN
          valueFrom: {secretKeyRef: {name: tachyne-world-pg-cred, key: dsn}}
        - name: ATTACH_TOKEN
          valueFrom: {secretKeyRef: {name: tachyne-attach-token, key: token}}
        args: ["-attach", ":25500", "-topology", "/etc/tachyne/topology.json"]
        volumeMounts:
        - {name: topology, mountPath: /etc/tachyne, readOnly: true}
        readinessProbe:
          httpGet: {path: /readyz, port: 8081}
          periodSeconds: 5
        livenessProbe:
          tcpSocket: {port: 25500}
      volumes:
      - name: topology
        configMap: {name: tachyne-world-topology}
---
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata: {name: tachyne-world, namespace: tachyne}
spec:
  maxUnavailable: 1
  selector: {matchLabels: {app: tachyne-world}}
```

- DB provisioning: schema `worldstate` + role `tachyne_world` on the CNPG
  `pg-1` cluster (databases ns) — copy the tachyne-access provisioning steps
  from memory/tachyne-infra. Secret `tachyne-world-pg-cred` in tachyne ns.
- The per-pod PVC (`volumeClaimTemplates`) stays: containers/furnace files
  (§7.1) + scratch. The nightly backup CronJob keeps running; add a
  `pg_dump` of schema `worldstate` to it.
- Gateways mount the same ConfigMap for the topology JSON; env
  `WORLD_ADDR_PATTERN=tachyne-world-%d.tachyne-world.tachyne.svc:25500`.
- Rollouts: `kubectl rollout restart sts/tachyne-world` restarts pods
  one-by-one (default podManagementPolicy) — with PDB and the §9.5
  home-death recovery, a rollout is a per-shard ~seconds action-bar pause,
  not an outage. This is the operational payoff of the whole design; smoke
  it explicitly (Phase-2 acceptance).

---

## 11. Cross-border simulation (phase 4 — sketch only, do not build early)

1. **Border-strip mirroring**: each pod subscribes
   `mc.shard.edge.<dim>.<cellX>.<cellZ>` for cells adjacent to its own;
   owners publish BlockSet-equivalent messages for edits within 2 chunks of
   a cell edge. Received edits install into the foreign-overlay cache
   (§3.4), replacing the 5 s TTL with push-freshness.
2. **Cross-border intents**: `sim.go schedule()` for a non-owned pos → NATS
   `mc.shard.intent.<destSid>` `{sid,epoch,pos,due}`; dest validates epoch
   and schedules locally. Water crosses a seam with one tick of extra
   latency.
3. Mob migration is §9.6 (earlier).
4. Explicitly rejected forever: whole-world state broadcast between pods.
   Message volume must scale with border length, not world size or N.

---

## 12. Build order — phases, PRs, acceptance

Each phase is independently shippable and leaves the cluster deployable.

### Phase 1 — the substrate (single pod, no behavior change)

Worth doing even if sharding never happens: durable store, restart without
`world.gob`, real backups.

1. `tachyne-common/shard` package + tests (floored-division edge cases,
   `ShardsIn` window enumeration, `TopoHash` stability, `MintEID`/`Minter`
   round-trip incl. wrap).
2. `internal/store`: PG client, DDL migration (embedded, applied at boot with
   `CREATE ... IF NOT EXISTS`), epoch claim/heartbeat/fence, ChunkStore,
   PlayerStore, MetaStore. Tests run against `PG_TEST_DSN` env (skip
   without); fencing test = two fake epochs racing, assert loser's writes
   all fail. **Verify the fence actually fences**: claim epoch 2 while an
   epoch-1 `WithFence` tx is mid-flight, assert serialization.
3. `internal/world`: ChunkStore mode (lazy load, write-through batcher,
   foreign TTL, eviction rule). Race-test the batcher.
4. Engine: `-sid/-pg/-topology` wiring, clock derivation, Checkpoint
   replacing invStore flush, readiness endpoint.
5. Migration job: one-shot `cmd/migrate-store` reading
   `world.gob/nether.gob/end.gob` + `players.json` + inventory gobs from the
   PVC and writing Postgres. Run as a k8s Job with the PVC mounted; verify
   counts (`EditCount` == row count per dim).

**Accept:** cluster on N=1 Postgres mode; attachprobe smoke green; edit a
block, `kubectl delete pod tachyne-world-0`, block persists; `SELECT
count(*) FROM chunk_edits` ≈ 89262+ (the migrated monolith edits); kill
Postgres, gameplay continues, readiness goes red, edits flush on recovery.

### Phase 2 — two pods, hard borders, coarse rehome

1. Hub ownership gates + viewer registry + `emitSpatial` (§7.1–7.2).
2. Attach: Purpose dispatch, viewer sessions, Want ownership filter,
   Welcome extensions, Redirect (§6, §7.3).
3. Gateway: upstream set + fan-in + routing table + Want fan-out +
   entitySrc + tab-list dedup (§8).
4. Login flow + eid lanes (§4).
5. Rehome without PREPARE polish: Release/MsgRehome/Resume + serverbound
   buffer (§9.3–9.4) — correctness, not invisibility.
6. Global chat/`/list`/clock over NATS+store (§5).
7. Topology ConfigMap + replicas 2 + PDB (§10).

**Accept (headless, extend attachprobe):** two probes homed on different
shards see each other's moves across a border cell (viewer path); a probe
walking A→B rehomes (watch `MsgRehome`, new Welcome, same eid, no
EntityRemove for it on a third observing probe); dig routed to the foreign
owner mutates + broadcasts + persists; chat crosses shards; kill shard 1 →
probe homed there reconnects to a live shard within 10 s, inventory intact
(≤30 s stale); rollout-restart the sts and stay connected.

### Phase 3 — invisible crossing

Hysteresis tuning (§9.2), suppress resume SyncPosition, pre-warmed view
upstreams at `Border(…, 2)`, serverbound buffer flush ordering, old-home
view-session swap (§9.3 tail). **Accept:** Wesley walks a real 1.21.5 +
26.2 client back and forth across a border repeatedly; no rubber-band, no
entity flicker, no chunk flash, action-bar HUD continuous; combat across
the border line works (hit a mob owned by the other shard).

### Phase 4 — border simulation (§11) + mob migration (§9.6)

**Accept:** water poured on shard A flows into shard B; a cow walks across
and keeps its herd behavior; sand falls across a seam.

---

## 13. Explicit non-goals / punts (v1)

- Rebalancing / changing `Shards` on a live world (map `Version` reserved).
- Containers (chests/furnaces) in Postgres — stay on per-pod PVC (owned
  positions ⇒ correct; migrate later for pod-death durability parity).
- Cross-shard TNT/piston chains, mob pathfinding across borders beyond
  read-only targeting.
- Cross-shard item pickup (§8.2 tail): a drop in a foreign region is
  collectable only after rehoming. Phase-4 fix: the item's owner despawns it
  and sends a NATS credit-intent to the player's home pod (inventory
  mutation stays home-owned).
- Binary attach codec (pre-existing debt; JSON stays until profiling says
  otherwise — sharding adds view-session fan-out, so it will say so sooner).
- Per-shard worldgen parallelism tuning, hot-cell splitting, viewer-session
  pooling across clients on one gateway (one view session per client per
  shard in v1 — N×M sessions; pool only if it measurably hurts).
