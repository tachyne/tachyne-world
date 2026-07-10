# Scaling tachyne: the distributed-pod approach (design notes for later)

> **Status: future direction, NOT built.** Captured here so the intent survives.
> Near-term focus is feature completeness on the single-process server. This
> document is the plan for when horizontal scale (Kubernetes, a large cluster)
> becomes the priority.
>
> **SUPERSEDED IN PART (2026-07-07):** the "per-region spatial shards" strategy
> is now fully designed in **`docs/SHARDING.md`** — written after the tachyne
> decomposition (dispatch → gateways → versionless world pod) and the
> domain-events refactor, both of which change the picture substantially (the
> proxy tier exists, the wire is gone from the engine, events merge per-viewer
> at the gateway). Read that document for anything shard-related; this one
> remains useful for the general principles and the coarser slicing options.

## The goal

Run tachyne as a horizontally scalable cluster on Kubernetes: many pods of the
same binary, so a massive player population (and/or a massive world) is served
by scaling out pods rather than scaling up one machine.

## The core principle: the tick loop is the *unit* of scale, not the obstacle

tachyne's deliberate architecture is **one 20-TPS tick goroutine that owns all
world state**, with per-connection goroutines doing I/O only. That single-writer
model is why the code avoids Java's lock-everywhere concurrency. It is **not** an
obstacle to scale — it is the unit of scale:

- Each pod runs one tick loop that owns a **disjoint slice** of the world.
- NATS carries anything that crosses slice boundaries.
- Valkey / object storage holds shared state any pod can read.
- **Invariant that keeps it correct: two pods never write the same chunk.**

Corollary (see also the "daemons" discussion): within a pod, keep the
single-writer-per-shard rule sacred. Heavy **pure computation** (pathfinding,
lighting, LLM-NPC decisions) may be offloaded to worker goroutines that read a
snapshot and return a result applied back on the tick loop; **state mutation**
is always serialized through the shard's tick loop. Cross-shard work is
message-passing (intents/handoffs over NATS), never a second writer.

## What already exists (the distributed primitives are in the tree)

- **NATS bus** (`internal/server/natsbus.go`): connects to a standalone NATS
  broker, subscribes to `mc.cmd.>` (commands in), publishes `mc.event.*`
  (events out). Fail-open. This is the inter-pod event/command transport, and
  the natural carrier for cross-shard handoffs.
- **Valkey chunk cache** (`internal/world/valkey.go`): a shared generated-chunk
  cache keyed `tachyne:chunk:v{GenVersion}.s{seed}.{cx}.{cz}`, fail-open (a dead
  cache is a local miss, never a stall). Multiple pods with the same seed already
  share generated chunks — no redundant worldgen across the cluster.
- **Protocol-agnostic core**: connections are I/O-only; the tick loop owns
  state. Clean separation for a proxy/backend split.
- **`Store` interface** (`internal/world/store.go`): world *edits* (the diff
  overlay) already go through an interface — today a single-file `FileStore`
  (`world.gob`). Swapping in shared storage is a new implementation, not surgery.
- **Fail-open everywhere**: dead NATS/Valkey degrade gracefully — exactly what
  you want when K8s pods come and go.

## The slicing strategies (coarse → fine)

Pick based on which "massive" you want.

### 1. Per-world / lobby — the proxy model (easy, huge aggregate scale)
A **stateless proxy** holds player connections and routes each player to a
backend game pod, each pod owning an **independent world**. This is how every
large MC network scales (Hypixel, etc.). Enormous aggregate scale (100k players
across many worlds/instances) for a fraction of the complexity. Does **not**
give one shared world. Best first target for K8s.

### 2. Per-dimension
Overworld / nether / end each on their own pod. Coarse but easy; portals are
already a natural handoff point.

### 3. Per-region spatial shards — one seamless massive world (hard)
Divide the overworld into regions, each pod owning a chunk range. Players near a
seam see **mirrored** (read-only) entities from the neighbouring shard; crossing
a boundary is a **handoff**: serialize the entity → NATS → deserialize on the
neighbour → authority transfers. The genuinely hard, research-grade version —
seam consistency, handoff races, hot-region rebalancing. (For perspective:
Mojang's own regionization, *Folia*, is thread-level on one machine; multi-machine
single-world is beyond it.) Take this on deliberately, only after the proxy
model proves the operational side.

## Retrofit-expensive things to get right early (cheap now)

Even before sharding, design these so the later split isn't a rewrite:

1. **Make region ownership explicit.** The tick loop should conceptually own "a
   set of chunks," not "the world." Today that set is "everything" —
   parameterizing it is the seam that makes sharding possible.
2. **Move world persistence off the single `world.gob` into per-chunk shared
   storage** (Valkey or object store, keyed like the chunk cache). *This is the
   concrete blocker for multi-pod today*: a single local file can't be
   co-owned. The `Store` interface is already abstracted → new implementation.
3. **Promote cross-cutting events to first-class NATS messages.**
   join/leave/chat/block-edit already publish; entity-crossing-a-seam and
   blast-spanning-a-boundary become the same kind of message.
4. **Extract the stateless proxy tier** (login/status/routing) so game pods are
   pure simulation.

## Kubernetes topology (target)

- **Proxy tier** — stateless. `Deployment` + HorizontalPodAutoscaler. Holds
  connections, does status/login, routes players to the right game pod.
- **Game-server tier** — stateful (each pod owns a shard/world/region).
  `StatefulSet`, not `Deployment`. Session affinity so a player sticks to the
  pod owning their region; dynamic reroute on region change / handoff.
- **Backing services** — NATS (cross-pod events + handoff), Valkey (chunk cache
  + hot state), object storage (durable world persistence).
- **LLM-NPC brain** (goal #4) — a separate daemon (possibly another language)
  attached over NATS; its decisions are hundreds of ms and *must* be async,
  feeding intents back to the owning shard's tick loop.

## Recommended phasing

1. **Phase 1 (high value, moderate effort):** per-chunk shared persistence
   (`Store` → Valkey/object store) + a stateless proxy front. Unlocks
   multi-pod-many-worlds on K8s.
2. **Phase 2 (ambitious):** per-region spatial sharding with NATS handoff.
3. Throughout: single-writer-**per-shard** stays sacred; workers compute,
   the shard's tick loop is the only writer for its chunks.

## Highest-leverage groundwork when we start

- A Valkey/object-store `Store` implementation to replace single-file world
  persistence.
- Parameterizing region ownership in the tick loop.

Both are cheap now and unblock everything downstream.
