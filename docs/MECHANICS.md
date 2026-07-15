# Game-Mechanics Reference & Tuning Numbers

A single source of truth for the numbers that drive gameplay: the **vanilla
Minecraft Java 1.21.x** reference value, its source, **our current value**, and
whether they match. It also lists the systems we have **not** implemented yet,
with vanilla numbers attached so each blind spot doubles as a spec.

Vanilla sources are the [Minecraft Wiki](https://minecraft.wiki); a few values
(noted) come from the game source where the wiki is silent.

> **Tooling note (2026-07-07):** `cmd/diffprobe` (the oracle differential
> probe referenced below) was a 770-TCP client and was deleted with the wire
> layer (git history pre-`c15e1e4`). Its findings and methodology stand; a
> replacement would today be an attach-protocol probe on the engine side plus
> a real-client transcript at a gateway. The vanilla oracle server + datagen
> reports in `reference/1.21.5/` remain the authority for numbers.

### How our numbers map to time/space

- The hub ticks at **20 TPS** (50 ms/tick), like vanilla.
- Mobs step every `mobMoveInterval = 2` ticks, moving `speed` blocks per step, so
  **effective speed (blocks/sec) = speed × 10**.
- `survivalTick` runs every `survivalTickN = 20` ticks (once per second), so any
  per-tick survival rule we run "per survival tick" happens at **1 Hz**.

Legend: ✅ matches vanilla · ⚠️ diverges (intentional or not — see note) ·
🟡 partial · ❌ not implemented.

---

## 1. Health & regeneration

| Mechanic | Vanilla (1.21.x) | Ours | Status |
|---|---|---|---|
| Max health | 20 HP | `maxHealth = 20` | ✅ |
| Regen condition | food ≥ 18 (slow) or saturation > 0 (fast) | food ≥ 18 (`regenFood`) | 🟡 no fast/saturation regen |
| Slow regen rate | **1 HP / 80 ticks (4 s)** | **1 HP / 80 ticks** (`regenPeriod`) | ✅ |
| Regen exhaustion cost | **6.0 exhaustion / HP** | `6.0` (`regenExhaustion`) | ✅ |
| Fast "saturation" regen | 1 HP / 10 ticks when food = 20 & sat > 0 | up to 2 HP per 1 Hz survival tick (same average) | ✅ |

Source: [Hunger](https://minecraft.wiki/w/Hunger).

## 2. Hunger, saturation & exhaustion

| Mechanic | Vanilla | Ours | Status |
|---|---|---|---|
| Max food | 20 | `maxFood = 20` | ✅ |
| Saturation on spawn | 5 (cap = current food) | `5` | ✅ |
| Exhaustion threshold | 4.0 → −1 saturation, then −1 food | `4.0`, sat then food | ✅ |
| Walking | **0.0** (no exhaustion) | **0.0** | ✅ (sprint tracked separately) |
| Sprinting | 0.1 / block | `0.1 / block` (`sprintExhaustion`) | ✅ |
| Attacking | 0.1 | `0.1` (`attackExhaustion`) | ✅ |
| Jumping / sprint-jump | 0.05 / 0.2 | upward ground-leave in onFallAndExhaust | ✅ |
| Mining a block | 0.005 | evDrop (survival breaker) | ✅ |
| Taking damage | 0.1 | in damage() | ✅ |

Source: [Hunger](https://minecraft.wiki/w/Hunger). Sprint state comes from the
Entity Action packet; walking is free, sprinting/attacking drain food.

### Starvation (food = 0)

| Mechanic | Vanilla | Ours | Status |
|---|---|---|---|
| Damage rate | **1 HP / 80 ticks (4 s)** | **1 HP / 80 ticks** (`regenPeriod`) | ✅ |
| Floor | Easy 10 / Normal 1 / Hard 0 | stops at 1; peaceful never starves | 🟡 (no easy/hard floors) |

## 3. Food values

Vanilla: `saturation = foodPoints × saturationModifier × 2`, capped at food level
([Food](https://minecraft.wiki/w/Food)). We generate both from minecraft-data
(`foods_gen.go`: `foodPoints` for hunger, `foodSaturation` = `saturationRatio/2`
for saturation), all 40 foods — **✅ both correct**.

| Food | Vanilla hunger / saturation | Ours | Status |
|---|---|---|---|
| Apple | 4 / 2.4 | 4 / 2.4 | ✅ |
| Raw beef | 3 / 1.8 | 3 / 1.8 | ✅ |
| Bread | 5 / 6.0 | 5 / 6.0 | ✅ |
| Cooked beef | 8 / 12.8 | 8 / 12.8 | ✅ |
| Golden apple | 4 / 9.6 | 4 / 9.6 | ✅ |

**Eating time:** vanilla **32 ticks (1.6 s)**; ours matches — `use_item` starts
the eat-hold, the food applies after `eatDuration=32` ticks, an early
`release_use_item` or hotbar switch cancels (a release within 2 ticks of done
counts as finished, absorbing the client/server timer race) — **✅**.

## 4. Damage

| Mechanic | Vanilla | Ours | Status |
|---|---|---|---|
| Fall damage | `floor(fallDistance) − 3` HP, 3-block grace, no cap | `floor(dist − 3)` past 3 | ✅ (equivalent) |
| Void damage amount | 4 HP | `4` | ✅ |
| Void rate | 4 HP every 10 ticks → 8 HP/s | 8 HP/s (`voidDamagePerSec`) | ✅ (same rate) |
| Void start Y | 64 below min build = **Y −128** | `MinY − 64` = **−128** | ✅ |
| Drowning | ~15 s breath, then 2 HP/s | `maxAir=300` ticks (1 Hz drain/refill), `drownDamagePerSec=2` | 🟡 breath ticks at 1 Hz (bubbles pop in steps), no water-breathing |
| Lava contact | ~4 HP / 0.5 s + lingering fire | `lavaDamagePerSec=8` | 🟡 same rate; post-exit burning not modelled |
| Cactus contact | 1 HP / 0.5 s on hitbox overlap | `cactusDamagePerSec=1` (4-neighbour feet/body check) | 🟡 approximated, 1 Hz |
| Fire block / burning status | fire 1 HP/s + 8s afterburn; lava 15s afterburn; water/rain douse | same (`fire.go`: fireSecs on tracked, flame overlay metadata, afterburn paused while still in the source) | ✅ (no suffocation yet) |
| TNT | 80-tick fuse entity, power 4, chains, ~1/4 drops | `primedTNT` + fuse metadata, `explodeAt` shared with creepers, chain-priming with random 10-30 fuses, blast resistance ≥100 survives (obsidian) | ✅ (no propelled/falling TNT) |
| Fire spread | ignite odds by flammability, age, burnout | placed fire burns out in ~5 s, NO spread (deliberate until mobGriefing gamerule lands) | 🟡 |

Sources: [Damage](https://minecraft.wiki/w/Damage), [Void](https://minecraft.wiki/w/Void).

## 5. Player melee attack

| Mechanic | Vanilla | Ours | Status |
|---|---|---|---|
| Bare-fist damage | **1 HP** | `fistDamage = 1` | ✅ (was 2 pre-tools) |
| Attack speed / cooldown | per-weapon (hand 4/s, sword 1.6/s, axe ~1/s); early hits ×(0.2+0.8c²) | `attackPeriod` per class (hand 5t, sword 13t, pick 17t, axe/shovel 20t), same formula | ✅ |
| Weapon damage | swords 4/4/5/6/7/8, axes 7/7/9/9/9/10, picks 2-6, shovels 2.5-6.5 | `meleeDamage` table; shovel halves rounded down | ✅ (no cooldown scaling) |
| Knockback (base) | 1.552 blocks horizontal, 0.8125 vertical | server-physics impulse 0.5 b/t (3 uncapped updates, ×0.6 decay) | ✅ tuned |
| Sprint / Knockback-enchant bonus | additive | sprint doubles the impulse (no enchant yet) | 🟡 |
| Critical hits, sweep | falling full-charge ×1.5; sweep 1+sharpness | same: crit gated on ≥90% charge + descending + not sprinting, crit particles+sound; sweep on grounded full-charge sword swings, 1.5-block ring | ✅ |

Sources: [Attribute](https://minecraft.wiki/w/Attribute),
[Knockback](https://minecraft.wiki/w/Knockback).

## 6. Mobs (cow)

| Mechanic | Vanilla | Ours | Status |
|---|---|---|---|
| Max health | 10 HP | `cowHealth = 10` | ✅ |
| `movement_speed` attribute | 0.2 | — (we use absolute b/s) | n/a |
| Walk speed | ~0.2 attr; theoretical max ≈ 8.6 b/s, ambles much slower | `mobSpeed 0.09` → **0.9 b/s** | 🟡 tuned slow for "grazing" feel |
| Panic multiplier | ×2.0 (game source) | — | — |
| Panic/flee speed | theoretical max ≈ 17 b/s; observed "noticeably faster than walk" | `fleeSpeed 0.40` → **4 b/s** (≈ player walk) | ✅ tuned to feel |
| Panic duration | until goal ends (~a few s) | `panicTicks 40` ≈ 4 s | ✅ approx |
| Beef drop | 1–3 (+1/Looting level) | `1 + rng(3)` = 1–3 | ✅ (no Looting) |
| Leather drop | 0–2 (+1/Looting level) | `rng(3)` = 0–2 | ✅ |
| Death animation | ~20-tick tip-over + sound, then drop | Entity Status 3, freeze `deathAnimTicks=20`, then despawn + drop | ✅ (applies to all mobs) |
| Cooked beef if on fire / XP on kill | yes / 1–3 | — | ❌ |

Sources: [Cow](https://minecraft.wiki/w/Cow), [Attribute](https://minecraft.wiki/w/Attribute).
The wiki's ~43×`movement_speed` → blocks/s is an "approximately" upper bound only
realized when actively pathing; we tune to observed feel instead.

## 6b. Hostile mobs (zombie)

The first mobs that fight back (`hostile.go`). They share the mob movement +
collision (fences pen them, they can't climb walls); only steering, melee, and
spawn/cleanup rules are new.

| Mechanic | Vanilla | Ours | Status |
|---|---|---|---|
| Max health | 20 HP | `zombieHealth = 20` | ✅ |
| Melee damage | 2/3/4 (easy/normal/hard) | `zombieDamage = 3` × diffMult (easy 0.5 / hard 1.5); /difficulty persisted, peaceful clears + blocks hostiles | ✅ |
| Attack interval | ~1 s | `attackCooldown = 10` updates ≈ 1 s | ✅ |
| Follow / aggro range | ~35 blocks follow, 16 detect | `aggroRange 16` / `deaggroRange 24` (hysteresis) | 🟡 shorter |
| Chase speed | ~0.23 attr (sprinting) | `hostileSpeed 0.13` → **1.3 b/s** | 🟡 tuned |
| Pathfinding | A* (opens doors, avoids hazards) | steer straight at target, holding a `standoffDist 1.1` so it bites from the front (hittable, not buried in the player); shared walkability/step/fence gate | 🟡 no true pathfinding |
| Attack animation | swing + hurt | arm-swing (Entity Animation) on each bite | ✅ |
| Player knockback on hit | ~0.4 horizontal + up | `knockbackH 0.42`/`knockbackV 0.36` via Set Entity Velocity | ✅ |
| Night spawning | light-level ≤ 0, packs, biome caps | 1 attempt/s near a player, `hostileCap 12`, dist 24–44, **block-light 0 only** (skips lit rooms) | 🟡 simplified (no pack/biome rules) |
| Daylight burn | sky-exposed → fire, ~ a few s | `burnDamagePerSec 2` while sky-exposed after dawn | ✅ (thematic cleanup) |
| Rotten flesh drop | 0–2 | `rng(3)` = 0–2 | ✅ (no XP, no armor pickups) |
| Baby / variants / reinforcements | yes | — | ❌ |

Sources: [Zombie](https://minecraft.wiki/w/Zombie), [Spawn](https://minecraft.wiki/w/Spawn).

### Skeletons, spiders, creepers (added 2026-07-04)

Night spawns roll a species (`rollHostileType`: 45% zombie / 25% skeleton /
18% spider / 12% creeper). Hostiles with no player within 64 blocks despawn
(vanilla ~128) so the non-burning species can't pile up against the cap.

| Mechanic | Vanilla | Ours | Status |
|---|---|---|---|
| Skeleton health / bow | 20 HP, arrows ~1-4 dmg | `skeletonHealth 20`, `arrowDamage 3` through armor | ✅ |
| Skeleton AI | strafe-kite at bow range | advance >10, retreat <5, shoot ≤15 every ~2 s (`rangedBehavior`) | 🟡 no strafing |
| Arrows | raycast projectiles, pickup | real hub-owned projectiles: 1.6 b/t, gravity 0.05, drag 0.99, stick in blocks, despawn 10 s; PLAYER-shot arrows (bow: 3-20 tick draw → 0.45-3.0 b/t, dmg ceil(2v)+1 at full, one ammo + one durability per shot, 5-tick self-hit grace) also hit mobs (knockback + hitByPlayer XP credit) and are retrievable while stuck; snowball/egg (1.5 b/t, 0 dmg, shatter with poof) | ✅ |
| Crossbow | charge → loaded → fire; quick_charge/multishot/piercing | two-phase (`crossbow.go`): hold to charge 25 t (−5/quick_charge lvl), release latches loaded + spends one arrow, next use fires a fixed-power bolt (3.15 b/t, dmg ceil(2v)=7, no crit); multishot = 3 bolts ±10° yaw (side bolts creative-pickup only), piercing passes a bolt through lvl+1 mobs (never twice); durability on shot | ✅ (no firework/tipped ammo; loaded visual client-predicted) |
| Trident (player) | charge-throw; loyalty/riptide/impaling/channeling | charge-held (`trident.go`): release after 10 t throws it (2.5 b/t, 8 dmg), consumed from a survival hand and retrievable (returns the exact enchanted stack); loyalty flies it home + auto-collects; riptide launches the player (look × 3(1+lvl)/4) in water/rain with a `spinUntil` movement-authority grace; impaling +2.5/lvl vs targets in water or rain | 🟡 (no channeling — needs a lightning entity) |
| Skeleton daylight burn | yes | `burns` like zombies (staggered dawn ignition) | ✅ |
| Spider | 16 HP, 2 dmg, fast, neutral in day, retaliates | `spiderHealth 16`, `spiderDamage 2`, `spiderSpeed 0.17`, day-neutral via `acquireTarget`, `spiderAnger 100` updates after a hit | ✅ (no wall-climb) |
| Creeper fuse | 1.5 s swell within ~3, defuse ~7 | `creeperFuseTicks 30`, ignite ≤3 (dy≤2), defuse >6, swell via metadata idx 16 | ✅ |
| Creeper explosion | power 3: crater + up to ~43 dmg, drops ~1/3 blocks | sphere r=3 crater (skips bedrock/fluids), linear falloff to 6, `blastMaxDamage 30` through armor, knockback, 30% block drops (hand-harvestable only), damages mobs too, generic.explode boom + explosion_emitter particle | ✅ |
| Drops | bones+arrows / string+eye / gunpowder | 0-2 bone + 0-2 arrow; 0-2 string + ⅓ eye; 0-2 gunpowder (only if killed before the bang) | ✅ |

### The full creature roster (`species.go`, added 2026-07-05)

Every remaining 1.21.5 living species lives in one **data table** (`speciesTable`),
its numbers lifted from the vanilla server source (`createAttributes`
builders — MAX_HEALTH / MOVEMENT_SPEED / ATTACK_DAMAGE / ARMOR / FOLLOW_RANGE)
and reimplemented in Go (facts only, no copied code). Each row maps onto a
shared **archetype** that reuses the existing behavior primitives:

- `archPassive` / `archSkittish` — wander/panic/breed; skittish species bolt
  from any close player (fox, ocelot, rabbit).
- `archHostile` / `archRanged` — the zombie chase / skeleton kite.
- `archWater` / `archWaterHostile` — locked inside a water column (`swimMove`):
  squid, cod/salmon/tropical fish/pufferfish, dolphins, guardians.
- `archFlyer` / `archFlyerHostile` — free flight with an altitude spring
  (`flyMove`): bats, parrots, bees, phantoms, ghasts, vexes, the wither.
- `archStatic` — anchored (shulker).

Shared vanilla mechanics wired in from source: **melee poison** (cave spider
`triangle`-free 7/15 s, bee 10/18 s — normal+hard only), **wither** on a
wither-skeleton bite (10 s), **neutral-pack retaliation** (`provoke`: hitting
one wolf/bee wakes the whole pack within 16 blocks), and a family of
**projectiles** (ghast explosive fireball power 1, breeze wind-charge = pure
knockback, wither skull = damage + wither, shulker bullet, guardian charge-beam
that applies its hit when the wind-up completes). Melee `damage` = the species'
ATTACK_DAMAGE attribute at normal difficulty (× `diffMult` like the zombie's 3).

| Group | Species | Notes |
|---|---|---|
| Land passives | mooshroom, rabbit, fox, ocelot, cat, wolf, goat, panda, polar bear, armadillo, sniffer, camel, horse/donkey/mule, skeleton/zombie horse, llama/trader llama, turtle, frog, wandering trader, snow golem | attrs from source; wolves/goats/pandas/polar bears/llamas retaliate; foxes/ocelots/cats/rabbits skittish |
| Water | squid, glow squid, cod, salmon, tropical fish, pufferfish, tadpole, axolotl, dolphin, guardian, elder guardian | `swimMove`; fish are `quiet` (no ambient); guardians beam |
| Flyers | bat, parrot, allay, bee | `flyMove` with per-species hover altitude |
| Overworld hostiles | cave spider, silverfish, endermite, bogged, wither skeleton, phantom, creaking, breeze, warden, ravager, pillager, vindicator, evoker, illusioner, vex, giant, zombie villager, shulker | poison/wither/ranged per source; warden/ravager `noKB` |
| Nether | ghast, piglin, piglin brute, hoglin, zoglin, strider | added to the Nether spawn ring; piglin/brute melee = ATTACK_DAMAGE 5/7 |
| Boss | wither | 300 HP, armor 4, flies, wither-skull volleys, drops the nether star |

Natural spawning is biome-aware: `biomeAnimal` picks the countryside species
by biome (polar bears/wolves in snow, goats/llamas on peaks, horses on plains,
rabbits/camels in desert); `waterSpawn` stocks oceans; `updateNetherMobs` rolls
the expanded Nether pool; a low-odds `spawnPhantom` sends a phantom over a
survival player at night. All species are `/summon`-able by their registry name
(registered in `init`). XP, loot and sounds all derive from the table
(unknown sound-event names are silently ignored by the client, so a species
without a matching event is simply quiet).

### A* pathfinding (`pathfind.go`, added 2026-07-05)

Target-seeking mobs (melee hunters, following pets) now route AROUND obstacles
instead of steering blindly into walls/water/cliffs and jittering. The graph is
2.5-D — one node per (x,z) column at its `MobFeet` height, 8-way steps that
climb ≤1 block or drop ≤3, no corner-cutting through walls, `TallObstacle`
(fences) impassable. `findPath` is A* with an octile heuristic, bounded to 350
node expansions and a 20-block range; an unreachable goal returns a best-effort
path to the closest explored column so the mob keeps advancing. Paths are cached
on the mob and replanned only when stale (target moved >2 blocks, path consumed,
or every 40 ticks), so the amortised cost is well under 1 ms/tick. A per-search
memo of the column queries (`memoPather`) cut each search ~10× (7 ms → ~0.7 ms)
since `MobFeet` scans a column and is hit from many directions. Fliers and
swimmers skip it (they pass over/through obstacles). `hostileBehavior` steers
toward the next waypoint at full speed; only the final approach to the target
applies the bite standoff.

### Villager door use + roaming (`villager_ai.go`, added 2026-07-05)

Regular villagers used to amble on the pure-random `wanderBehavior` — which never
purposefully threaded a one-wide doorway or climbed the step outside it, so a
villager that wandered into a house stayed effectively trapped until a player dug
it out (reported in-world). Two fixes, matching vanilla:

- **Goal-directed roaming.** `villagerBehavior` steers via `pathSteer` toward a
  roam target it re-picks on a timer (a random spot within ~10 blocks of its home
  house), and heads straight home when it drifts too far. So villagers walk in and
  out of their houses on real A* routes instead of bouncing off the walls.
- **Door use.** A door-using mob (`mob.usesDoors`, set on village + LLM villagers)
  plans through closed **wooden** doors: `pathSteer` wraps the world in a
  `doorPather` that clears `TallObstacle` for a closed wooden door (`world.
  ClosedWoodenDoorFeet`) so A* routes out of a doored room. As the villager steps
  up to the door, `villagerDoors` (called each mob update *before* the move) opens
  it — both halves, with the wooden-door sound — and records it in `hub.openDoors`.
  `updateOpenDoors` shuts it once no villager is within reach and a ~5 s grace has
  passed (villagers close doors behind them). Iron/copper doors are excluded
  (`worldgen.IsWoodenDoor`): vanilla mobs can't operate them, so a player's iron
  door still pens a villager.

The one-block step-up out of a doorway already worked in the walk collision test
(`step <= 1`); the missing piece was villagers never *deliberately* walking at the
door — pathfinding supplies that intent.

**Daily schedule.** `villagerSegment(dayTime)` maps the day clock to a coarse
version of vanilla's schedule, and `villagerBehavior.steer` picks the destination
for the current segment (each villager keeps its own anchors, set at spawn from the
house's deterministic furniture layout):

- **work** (t 2000-9000) → the profession workstation (`house+{+1,-1}`)
- **gather** (t 9000-11000) → the village bell (shared meeting point)
- **sleep** (t in the night window `sleepStart..sleepEnd`) → the bed (`house+{-1,0}`)
- **roam** (dawn/afternoon) → the timed random stroll near home

At night, once a villager reaches its bed (`villagerSleep`), it snaps onto the bed
surface, `updateMobs` holds it still, and it's sent the SLEEPING pose + bed anchor
metadata (the same `sleepMetadata` players use, re-asserted in the 2 s sync for late
joiners); it wakes at sunrise. The sleep pose is best-effort — even if a client
doesn't render the lying pose, the villager is parked motionless on its bed, which
is the schedule-correct outcome. Not modeled: work-site *claiming* (professions are
fixed at spawn, not earned by touching a job block), gossip, raids, or hiding
indoors from monsters.

### Riding + taming (`mount.go`, `tame.go`, added 2026-07-05)

**Rideable mounts** — horses/donkeys/mules/camels/skeleton+zombie horses ride on
a saddle alone; pigs and striders need a saddle plus their steer item
(carrot/warped-fungus on a stick). Right-click with a saddle saddles the animal;
right-click a saddled mount to board. Riding reuses the vehicle machinery:
`mob.rider` marks the passenger, the mount's AI pauses (`updateMobs` skips it),
and the client drives it — the riding client streams `vehicle_move`, and
`applyMountMove` validates the delta (same `vehicleMoveCap` authority as boats),
adopts it, drags the rider along for chunk streaming, and relays the move to
everyone else. Sneaking posts `evDismount`, which tries `dismountMob` before the
boat/cart path. Saddle + passenger state is re-asserted in the 2 s sync so late
joiners see it.

**Tameable companions** — wolves (bone), cats/ocelots (cod), parrots (seeds):
each feeding has a 1-in-3 tame chance (vanilla), with the fail/success entity
events (smoke/hearts). A tamed pet drops its wild AI and instead "hunts" the
owner to follow — `petAcquire` targets the owner past `petFollowStart` (10 b),
stops within `petFollowStop` (3 b), and blinks to them past `petTeleport`
(20 b), just like vanilla. An empty-handed right-click by the owner toggles
sitting (`petFlagsMeta` bit 0x01), which holds the pet in place. Collar/tamed
flag (bit 0x04) renders on the client.

**v1 simplifications** (documented, not silent): shulker bullets fly straight
rather than homing; the guardian beam has no travelling render; fliers use an
altitude spring rather than full 3D pathfinding; mounts don't yet apply
per-species speed/jump attributes to the client (movement is client-simulated,
so it already feels right); pets don't yet fight the owner's attacker or persist
across restart (mobs are runtime-only); horse inventory/armor and breeding of
mounts are future work.

## 7. Item drops & pickup

| Mechanic | Vanilla | Ours | Status |
|---|---|---|---|
| Despawn time | 6000 ticks (5 min) | `itemDespawnTicks = 6000` | ✅ |
| Pickup delay | 10 ticks (0.5 s) | `pickupDelay = 10` | ✅ |
| Stack size | 64 | `stackMax = 64` | ✅ |
| Pickup radius | ~1 block + box | `±1 x/z, ±1.5 y` | ✅ approx |
| Item merging on ground | yes | same item/dmg/ench within 1 block merge (1 Hz) | ✅ |
| Drop on death | items scatter (unless keepInventory) | `dropInventory` scatters every stack as item entities (small x/z jitter), then clears the inventory | ✅ (no velocity arc) |

## 8. Block drops (loot)

| Block | Vanilla | Ours | Status |
|---|---|---|---|
| Grass/fern → wheat seeds | 1/8 (12.5%) | `rng(8)==0` | ✅ |
| Gravel → flint | 1/10 (10%), else gravel | `rng(10)==0` | ✅ |
| Leaves → sapling / stick / apple | 5% / 2% (1–2) / 0.5% | 1/20 / 1/50 (1–2) / 1/200 | ✅ |
| Everything else → its item | per block | generated `loot_gen.go` (931 blocks) | ✅ default-drop |
| Correct-tool requirement | stone/ore drop only with the right tool tier | `HarvestableBy` gates `evDrop` via blocks.json `harvestTools` | ✅ data-driven |
| Ore counts (lapis 4–9, redstone 4–5…) | varied | single item | 🟡 no count/Fortune |
| Silk Touch / Fortune | modifies drops | — | ❌ |

> **Tool-gating caveat:** harvest gating is faithful — break stone with an empty
> hand and it drops nothing, exactly like vanilla. But there is no crafting/tool
> acquisition yet, so a survival player has no pickaxe; stone/ore are effectively
> undroppable until tools land. Creative is unaffected (drops nothing anyway).

Sources: [Drops](https://minecraft.wiki/w/Drops),
[Short Grass](https://minecraft.wiki/w/Short_Grass), [Leaves](https://minecraft.wiki/w/Leaves).

## 9. Random ticks & growth

| Mechanic | Vanilla | Ours | Status |
|---|---|---|---|
| Random tick speed | 3 per section per tick | `randomTickSpeed = 3` | ✅ |
| Sim range | server sim-distance | `simRadius = 4` chunks | 🟡 fixed |
| Cane/cactus max height | 3 | `3` | ✅ |
| Crop stages | wheat/carrot/potato 0–7, beet 0–3 | same ranges | ✅ |
| Crop growth rate | `1/(floor(25/points)+1)` per tick; needs light ≥ 9; farmland hydration & row bonus | advance 1 stage/tick if sky-lit | ⚠️ simplified (no points/hydration) |
| Sapling → tree | staged, checks spacing/light | stage then grow oak | 🟡 all saplings → oak shape |
| Leaf decay | `distance` 1–7 propagated; decays at 7 | scan for log within 4, else decay | 🟡 no distance propagation |
| Grass spread / death | light-gated, to dirt 3×3×3 | sky-exposed proxy | 🟡 daylight proxy, no true light value |
| Bone-meal, bamboo, sugarcane-on-water, mushrooms, copper oxidation, etc. | yes | — | ❌ |

Sources: [Tick](https://minecraft.wiki/w/Tick), [Crops](https://minecraft.wiki/w/Crops).

## 10. Fluids & falling blocks

| Mechanic | Vanilla | Ours | Status |
|---|---|---|---|
| Water flow delay | 5 ticks/step | `waterDelay = 5` | ✅ |
| Lava flow delay (overworld) | **30 ticks/step** | `lavaDelay = 30` | ✅ |
| Water horizontal reach | 7 blocks | level-based spread | ✅ approx |
| Lava horizontal reach (overworld) | 3 blocks | level-based | 🟡 |
| Infinite water source formation | 2 adjacent sources → source | — | ❌ |
| Water + lava → stone/cobble/obsidian | yes | — | ❌ |
| Waterlogging | per-block `waterlogged` | — | ❌ |
| Falling block step | gravity-accelerated entity | `fallDelay = 1` tick/cell | 🟡 constant fall, block-by-block |
| Concrete powder + water → concrete | yes | — | ❌ |

Sources: [Water](https://minecraft.wiki/w/Water).

## 11. Block properties (minecraft-data `blocks.json`)

Every field `blocks.json` provides, and whether the server enforces it. Generated
into `worldgen/blockmeta_gen.go` by `scripts/gen_blockmeta.py`.

| Field | Meaning | Ours | Status |
|---|---|---|---|
| `id` / `name` / `minStateId` / `maxStateId` | identity + state range | item↔state maps, range tables | ✅ |
| `defaultState` / `states` | placement state layout | `block_states_gen.go` + `SetProperty` | ✅ |
| `filterLight` / `transparent` | light dimming | `lightfilter_gen.go` → `SkyOpacity` | ✅ |
| `emitLight` | block-light emission | `light_emission_gen.go` | ✅ |
| `drops` | loot | `loot_gen.go` + `rollDrops` | ✅ |
| `diggable` | breakable by mining | `Diggable()` — unbreakable (bedrock/barrier/portal) refused in survival | ✅ |
| `hardness` | mining time | survival breaks on client `Finish` (timed by hardness); hardness-0 breaks on `Start` | ✅ client-timed |
| Client-side tool speed (26.x) | mineable/* + needs_*_tool tags | real tag contents sent to 776 (`tags26x`); 775 gets empty tags (wrong-tool 5x penalty persists there) | ✅ 776 |
| `harvestTools` | tool needed for drops | `HarvestableBy()` gates `evDrop` | ✅ |
| `stackSize` | max stack (64/16/1) | `StackSizeState()` → inventory `stackCap` | ✅ block items |
| `boundingBox` | empty vs block | `Collides()` → mob `standable` (excludes torch/rail/sign/plant) | ✅ |
| `resistance` | blast resistance | `Resistance()` generated, **no consumer** | 🟡 parked (no explosions) |
| `material` | tool-class string | encoded via `harvestTools` set membership | ✅ via harvestTools |
| `displayName` | UI label | — | ❌ client renders names |

Server-side hardness *validation* (rejecting impossibly-fast `Finish` to stop
fast-break cheats) is deferred — mining time is currently client-authoritative,
which is correct in feel but trusts the client's timer.

Sources: [minecraft-data `blocks.json`](https://github.com/PrismarineJS/minecraft-data/blob/master/data/pc/1.21.5/blocks.json).

## 11b. Crafting & container clicks (`crafting.go`)

| Mechanic | Vanilla | Ours | Status |
|---|---|---|---|
| Recipes | datapack recipes (tags resolved) | generated `recipes_gen.go` (1293 shaped + 279 shapeless, exact item ids, material variants pre-expanded) | ✅ |
| Shaped matching | bounding box + horizontal mirror | same (`matchRecipe`) | ✅ |
| 2x2 player grid | window 0 slots 1-4 | ✅ server-computed result slot | ✅ |
| Crafting table | right-click → 3x3 menu | `open_window` menu 12 + full click handling | ✅ |
| Result take | consume 1/cell, to cursor or shift→inventory | same; shift-click crafts ONE per click (vanilla crafts max) | 🟡 |
| Slot moves | server-authoritative click state machine | trust-apply the client's declared changed-slots; result slot server-owned; resync on stale window | 🟡 (offline-trust model) |
| Close window | grid+cursor returned to inventory | `reclaimCraft`; leave also reclaims armor/offhand | ✅ |
| Recipe book sync | recipe_book_add + place-recipe flow | all 1572 recipes sent on join (per-version item ids); clicking an entry auto-fills the grid from the inventory (`placeRecipe`); makeAll fills one craft | ✅ (misc tab only, no unlock progression) |
| Durability | tools wear 1/block + 1/hit, break at max | same (`durability.go`, items.json maxima); wear rides the `minecraft:damage` component so all clients render the bar; survives moves/tosses/drops/storage/persistence | ✅ |
| Armor | 1.9+ formula (points/toughness), wears max(1,dmg/4)/piece | same (`armorReduce`/`wearArmor`, vanilla point tables); no equipment render on other players yet | 🟡 (no visuals to others) |

STATIC-REGISTRY CAUTION: menu ids and block_entity_type ids are vanilla
*registration* order — source them from ViaVersion mappings, NOT mcmeta's
alphabetical summary (that ordering bug shipped once: bed/chest/menu ids).

## 11c. Ores & furnace (`worldgen/ores.go`, `server/furnace.go`)

| Mechanic | Vanilla | Ours | Status |
|---|---|---|---|
| Ore types | coal/copper/iron/gold/diamond (+ more) | those five, stone + deepslate variants | 🟡 (no redstone/lapis/emerald) |
| Distribution | per-ore y-bands, triangular peaks | vanilla-ish bands: coal 0..110, copper -16..80△, iron -56..64△, gold -60..28, diamond -60..12 (ramps deep) | 🟡 approximated |
| Vein shape | blob features, cross-chunk | per-chunk random walk (7-10 cells), deterministic by seed+chunk | 🟡 chunk-local |
| Ore drops / tool gating | raw metals, pickaxe tiers | from generated blocks.json tables (raw_iron etc.; stone pick for iron, iron for gold/diamond) | ✅ |
| Smelting recipes | datapack (type=smelting) | generated `smelting_gen.go`: 140 inputs (tags resolved), per-recipe cook time | ✅ |
| Fuel burn times | code constants | coal 1600, planks/logs 300, stick 100, sapling 100, coal block 16000, lava bucket 20000, blaze rod 2400 | ✅ common set |
| Furnace rules | ignite only when smeltable; cook 200; progress decays -2 without heat | same (`updateFurnaces`, 20 TPS) | ✅ |
| Lit state | block lit=true while burning | flips + broadcasts | ✅ |
| Progress bars | container properties 0-3 | sent to the viewer each tick | ✅ |
| Furnace contents persistence | saved with the world | containers.json snapshot every 30s + on shutdown; boot reconcile relights/extinguishes | ✅ |
| XP from smelting | yes | furnace xpBank, paid on output withdrawal | ✅ (no blast furnace/smoker yet) |

## 12. World clock & infrastructure

| Mechanic | Vanilla | Ours | Status |
|---|---|---|---|
| Day length | 24000 ticks (20 min) | `dayLengthTicks = 24000` | ✅ |
| Tick rate | 20 TPS | 20 TPS | ✅ |
| Entity position resync | server periodically corrects | `entitySyncInterval = 40` (2 s) | ✅ (anti-desync) |

## 12b. Biomes (`worldgen/biomes.go`, added 2026-07-05)

Vanilla places biomes from a 6-parameter multi-noise climate (temperature,
humidity, continentalness, erosion, depth, weirdness). We use a practical
**temperature × humidity × elevation** model that reproduces the full overworld
biome set at a coarser grain. Elevation bands (read from the terrain Height)
pick ocean → shore → lowland → hill → peak directly; within the lowland band a
temperature/humidity matrix (`landBiome`, modelled on vanilla's table) selects
the land biome, and a low-frequency **variety** field chooses sub-variants
(plains↔sunflower, forest↔flower_forest, taiga↔old_growth, badlands variants,
jungle↔bamboo↔sparse). Each `Biome` carries its registry name (the client
colours/fog from it), surface blocks, and tree + flora kinds.

| Aspect | Vanilla | Ours | Status |
|---|---|---|---|
| Biome count (overworld) | ~50 land + ocean/cave | **47 generating** for a sampled seed (all families) | ✅ coarser grain |
| Placement | 6-param multi-noise | temp × humidity × elevation + variety field | 🟡 approximation |
| Climate | per-biome temperature/downfall | fBm temp + humidity, altitude lapse, warm-biased median | 🟡 |
| Surface blocks | per-biome (grass/sand/podzol/terracotta/mud/mycelium/snow) | biome `Top`/`Sub`; badlands bands coloured terracotta by height | ✅ |
| Trees | per-biome species | oak/birch/spruce(conical)/jungle/acacia/dark-oak/cherry/mangrove, biome density | ✅ shapes simplified |
| Ground flora | tall grass, flowers, cactus, bamboo, mushrooms, berries, lily pads | per-biome `floraKind` in `stampGroundCover` | ✅ (single-block placements) |
| Rivers | noise-carved channels | `riverDepth` carves lowland channels to the waterline → river/frozen_river | ✅ |
| Ocean variants | frozen/cold/lukewarm/warm + deep | temperature × depth in `resolveBiome` | ✅ |
| Cave biomes | dripstone/lush/deep_dark by 3D noise | per-section (vertical) below the surface via `caveBiome` | 🟡 no cave features yet |
| Nether/End biomes | full sub-biomes | `netherBiome`/`endBiome` split by noise/distance (biome tint only) | 🟡 no sub-biome terrain |
| Biome resolution | 4×4×4 cells | one biome per chunk section (16×16 horizontal) | 🟡 coarser |

Three generation artifacts were fixed alongside the biome work:
- **Floating lava lakes**: `stampLakes` took the lake centre as a flat rim and
  draped the fluid disc across the whole radius, so on a slope the downhill half
  hung in mid-air. Now `lakeSiteFlat` rejects steep sites and each column is
  clamped to its local ground.
- **River abysses**: the first river cut carved *proportional to the surrounding
  height*, gouging channels up to 55 blocks deep through hills (a crater ringed
  with floating dirt/trees). `riverDepth` now only touches near-sea-level land
  (`SeaLevel-1 … SeaLevel+3`) and carves a fixed shallow channel (2–4 below sea),
  so rivers read as gentle valleys — max carve ~8 blocks.
- **Floating surface crusts**: near-surface caves undercut the top block while
  the carve taper protected it, leaving grass/dirt hovering over small voids.
  `supportSurface` fills one sub-material block under any undercut surface before
  decoration (a deep cave just gains a 1-block-thicker ceiling).
- **Severed terrain fragments**: a 3D cave cutting through thin terrain (mountain
  spires, river banks) disconnects a cap of surface — snow/stone/dirt blocks left
  hanging in the sky. `removeFloatingFragments` is a final flood fill from the
  always-solid deep ground + chunk side walls: any solid block it can't reach
  (through solid neighbours) is carved away. Trees ride along — a normal tree
  connects trunk→ground and survives; one perched on a severed fragment is
  removed with it. Verified by a cross-chunk connectivity test
  (`TestNoSkyFloaters`) — zero disconnected blocks above sea level.

**`GenVersion` bumped 5 → 8** (terrain output changed; player block edits are a
separate persisted overlay and survive the bump — only natural terrain regenerates).

---

## Blind spots — not implemented yet (with vanilla numbers as spec)

These systems have **zero** implementation. Vanilla reference numbers are listed
so each entry is a ready starting spec.

**Combat depth** — attack cooldown (4.0 atk/s, full hit every 5 ticks), weapon
damage (wood sword 4, stone 5, iron 6, diamond 7, netherite 8; axes higher),
armor points + toughness damage reduction, critical hits (×1.5 on falling hit),
sweep attack, real knockback impulse (1.552 horizontal), Looting/Fortune/Silk-Touch,
mobs attacking the player, mob aggro/targeting.

### Farm animals + breeding (added 2026-07-04, `server/animals.go`)

| Mechanic | Vanilla | Ours | Status |
|---|---|---|---|
| Species | chicken 4 HP, pig 10, sheep 8 | same (+ drops: feather+chicken / porkchop 1-3 / wool+mutton) | ✅ |
| Breeding | love-food (wheat/carrot/seeds), 30 s love, pair within 8, 5-min cooldown, XP 1-7 | same (`feedAnimal`/`updateBreeding`, hearts via entity status 18) | ✅ |
| Babies | half-size 20 min, no drops/XP | baby metadata idx 16, growUpTicks 24000, loot+XP gated | ✅ |
| Sheep | shear 1-3 wool, regrow by grazing, 16 colors | shears + wear, ~40 s regrow, white only | 🟡 colors |
| Chickens | egg every 5-10 min; thrown egg 1/8 chick | same | ✅ (no ¼-more-damage falls: mobs take no fall dmg yet) |
| Idling | mobs stand/graze most of the time | passive mobs pause 6-20 s about every 20 s (`m.rest`) | ✅ tuned |
| Wild population | biome spawn cycle | 30 s top-up attempts near players: families of 2-4, cap 40 | 🟡 simplified |

### Hostile pack 2 (added 2026-07-04, `server/hostile2.go`)

| Species | Vanilla | Ours | Status |
|---|---|---|---|
| Husk | desert zombie, no burn, hunger effect | desert-biome spawn swap, no burn | ✅ (no hunger effect yet) |
| Stray | cold skeleton, slowness arrows | cold-biome swap, kiting bow AI | ✅ (no slowness yet) |
| Drowned | water spawn, swims, trident | shoreline spawn swap, land melee | 🟡 no swimming |
| Slime | sizes 4/2/1 (16/4/1 HP), splits 2-4, size dmg, bounce | same sizes/split/damage via metadata idx 16 | ✅ (walks, no bounce) |
| Enderman | 40 HP neutral, stare+hit aggro, teleports, water phobia, pearls | hit-aggro + blink-on-hit + rain warps + 0-1 pearls; player pearls teleport (5 HP toll) | ✅ (no stare aggro / block carrying) |
| Witch | 26 HP, splash potions, drinks cures | kiting + splash projectile 4 dmg every ~3 s, redstone/glowstone/sugar/stick drops | 🟡 (real potions after brewing) |

**Mob systems still missing** — hostile spawn-cycle depth (packs/biome caps),
mob environmental damage (fire/drowning), pathfinding/A*, tamables, swimming.

### Status effects (added 2026-07-04, `server/effects.go`)

Framework: tracked.effects, 1 Hz tick, entity_effect 0x7d / remove 0x47
(effect ids stable 770-26.2 — ViaVersion never remaps them). Implemented:
regeneration (1 HP/3 s at I, 1/s at II — vanilla-rate), poison (1/s, never
lethal), strength +3/lvl and weakness -4/lvl in melee, speed raises the
movement-validator budget +20%/lvl (client does the visual speed), fire
resistance (snuffs burning, makes lava a warm bath), instant health/harm.
Golden apple = Regen II 5 s; enchanted = Regen II 20 s + Fire Res 5 min
(absorption pending). Witch splashes now poison (10 s). /effect give|clear
(ops). Death strips everything. Missing: absorption/haste/night-vision
server effects, potion ITEMS + brewing (Nether-gated), beacon.

**SWIM-PHYSICS FIX #2 — THE REAL ONE (live bug, same day):** the 26.1+
chunk format's per-section fluid count (i16 after the non-air count) is
LOAD-BEARING: the 26.x client builds its fluid layer from it — water
RENDERING and SWIM PHYSICS both. We wrote 0 ("just bookkeeping"), which
made world-generated water invisible (deep ocean = pure-water sections =
nothing rendered) and unswimmable on 26.x. Now computed exactly like
ViaVersion does (BlockItemPacketRewriter26_1, fact-referenced): count the
section's fluid-state blocks (canonical water 86-101 + lava 102-117)
while remapping the palette. NEVER assume a new chunk field is inert.

**SWIM-PHYSICS FIX #1 (fluid tags):** the client's swimming runs off the
#minecraft:water FLUID TAG, not the block — with it empty/absent, players
sink through water like air, walk on the bottom, and "can't swim up".
The fluid registry is static with version-stable ids (flowing_water 1,
water 2, flowing_lava 3, lava 4), so water/lava tags now carry REAL
contents on EVERY version: legacy 770-774 (whose manifest is otherwise
names-only), 26.1 (otherwise all-empty), and 26.2 (already full).

**Player survival extras still missing** — sprint/sneak poses server-side,
elytra.

### Redstone tier 1a (added 2026-07-04, `server/redstone.go`)

| Mechanic | Vanilla | Ours | Status |
|---|---|---|---|
| Model | strong/weak power, block conduction, instant graph updates | cellular ripple on the scheduled-update pump: 1 block/tick propagation, no block conduction, wire-only network + direct adjacency | 🟡 simplified |
| Sources | lever, buttons (20/30t), torch (inverts support; never powers its own support — self-oscillator guard), pressure plates, redstone block | all but plates ✅ (stone-button timing for both) | 🟡 |
| Dust | 15-power decay, shaped connections | power property + side/up connection shaping | ✅ |
| Consumers | lamps, doors/trapdoors/gates, TNT, rails… | lamp, powered+open blocks (iron doors), TNT priming | 🟡 |
| Turn-off | instant unpowering | decay ripple (a long line takes power÷1 ticks to drain) | ⚠️ slower than vanilla |
| Repeater | 1-4 redstone-tick delay, locking, pulse stretching | delay honored via due-tick map; NO locking; input flicker shorter than the delay is dropped, not stretched | 🟡 |
| Comparator | compare/subtract + container fullness reading via block entity | both modes; output level in a server map; NO container reading yet (needs 1c hoppers) | 🟡 |
| Observer | watches facing block, 2-gt pulse, also fires on placement | 2-gt pulse on watched-state change; no pulse on its own placement; cardinal + up/down watch | 🟡 |
| Plates | wooden also from arrows/items; light/heavy weighted curves (heavy = count/10) | players + mobs scanned per tick; weighted power = min(15, count) for both | 🟡 |
| Daylight detector | light-level based, biome/rain aware | day-curve approximation from world time (rain ignored); invertible by right-click | 🟡 |
| Piston | 12-block push, moving_piston animation, carries entities, quasi-connectivity | instant block shift (no animation entity), fragile blocks crushed silently (no drops), entities not carried, no quasi-connectivity | 🟡 |
| Dispenser | per-item behaviors (armor equip, bonemeal, fire charge, shears…) | arrows fire, TNT primes, water/lava buckets pour (bucket stays, emptied); everything else drops like a dropper | 🟡 |
| Hopper | 8-gt cadence, pulls furnace OUTPUT only, minecart hoppers | 8-gt cadence; pulls first non-empty slot of any container (incl. furnace input — vanilla only takes output), vacuums drops in/above its cell, pauses while powered | 🟡 |
| Comparator container read | signal = 1 + floor(14 × fill), per-item max stack sizes | same formula but flat 64 max-stack assumption | 🟡 |

### The End (started 2026-07-04, `worldgen/end.go`)

| Mechanic | Vanilla | Ours | Status |
|---|---|---|---|
| Dimension | the_end effects, void, fixed time | third world (end.gob, own cache namespace), main island lens + 10-pillar ring, void beyond r=95; vanilla spawn platform at (100,49,0); /end op command | 🟡 |
| Stronghold | full maze, libraries, silverfish spawner | portal room only (15x15 shell, lava dais, 12-frame ring, ~10%% pre-filled eyes), 1536-cell grid, never near spawn (GenVersion 4→5, underground-only change) | 🟡 |
| Eyes of ender | fly + drop/shatter, 20%% break | fly toward the nearest stronghold, always consumed, no drop | 🟡 |
| End portal | filling animation, per-frame checks | server recomputes the ring from the seed on every eye (the click is a wish); 12 eyes fill the 3x3 instantly | ✅ |
| Dragon | phases (circling/strafing/perching), breath, bossbar, crystal beams | 200 HP flyer: circles the ring, 12s swoop cycles at survival players, 8 contact damage, +2 HP/s while any crystal lives; no perch/breath/bossbar/beam visuals | 🟡 |
| Crystals | beam visuals, respawnable fight | 10 crystals on pillar tops, any hit detonates (5-block blast), staged once per world (dragonDefeated persists in settings.json) | 🟡 |
| Victory | portal + egg + 12k XP, gateway | bedrock exit portal + egg + 1500 XP + the ELYTRA dropped beside it; exit portal returns to overworld spawn | 🟡 |
| Elytra | fall-flying pose, fireworks boost, durability | wearable (chest slot), client glide physics honored (3.0/tick airborne budget); no pose broadcast/fireworks/wear | 🟡 |
| Pending | outer islands, end cities, gateway | — | ❌ |

### The Nether (started 2026-07-04, `worldgen/nether.go` + `server/dimension.go`)

| Mechanic | Vanilla | Ours | Status |
|---|---|---|---|
| Dimension | min_y 0, height 256, has_ceiling | custom registry NBT: nether effects/ultrawarm/no-skylight but -64..384 bounds so ONE chunk pipeline serves both dims | 🟡 by design |
| Terrain | biomes (wastes/forests/deltas/valleys), fortresses | one biome (nether_wastes): 3D cavern sponge, lava sea y<-16, glowstone/soul sand/quartz, bedrock floor, open void above y120 | 🟡 |
| Travel | portals: frame validation, 4s dwell, portal search + linking, spawn platform | full frame validation (2-21 interior, both axes), flint ignite, 80-tick dwell (creative ~instant), 8:1 scaling, arrival builds a 2x3 return portal if none within 7 blocks (no cross-portal registry — nearest-reuse only) | 🟡 |
| Portal breaking | breaking frame pops portal blocks | overworld side only (block sim is dim-0); nether-side portal blocks persist until re-lit area edited | ⚠️ |
| Portal linking | 128-block search, forced platforms | ±16-block portal reuse w/ snap-to-portal; nether landing picked by walkability scan (3x3 solid floor + headroom, no lava shore, spiral to r=24) else a carved obsidian refuge; arrival latch stops instant bounce-back | 🟡 |
| Isolation | full per-dimension entity worlds | players/blocks/light/mobs/items/arrows/XP all dimension-tagged and isolated (toNearby routes by dim); vehicles + block simulation remain overworld-only | 🟡 |
| Entity metadata across versions | per-version data indexes | 26.2 moved cube-mob SIZE from index 16 to 18 (baby/age-locked inserted) — the chain now shifts index-16 VarInts for 776. LESSON: metadata INDEXES shift between versions like packet ids do; a type mismatch is an instant client disconnect naming the entity | ✅ fixed 2026-07-04 |
| Nether mobs | piglins/ghasts/blazes/magma cubes/wither skeletons, fortresses | zombified piglin (neutral, 16-block pack anger, gold drops), magma cube (splits, magma cream), blaze (fireballs ignite, rods on player kills); spawn on netherrack around players, cap 14; no ghasts/fortresses | 🟡 |
| Brewing | stand w/ per-slot progress arrow, potion_contents component, splash/lingering, modifiers (redstone/glowstone) | full water→awkward→6 potions chain, 20s brews, blaze-powder fuel; potions carry a server-side type + custom NAME (no potion_contents on the wire — its component id shifts per version; liquid renders default purple but labels/effects are real); no splash/modifiers; stand contents in-session only | 🟡 |
| Nether wart | soul sand farms, 3 growth stages | wild wart on nether soul-sand floors (GenVersion 4); plantable; grows only in overworld farms (block sim is dim-0) | 🟡 |
| Portal pairing | proximity re-search each trip | STICKY LINKS: first travel records the pair (both directions, hub registry); later trips land at the exact partner portal's doorstep if it's still intact + safe, else fall back to the scan and re-link. In-session only (links reset on restart → first trip re-learns) | ✅ |
| Portal linking round-trip | 128-block overworld search | portal search scans the EDIT OVERLAY within 128 blocks both ways; matched portals must be INTACT (obsidian footing + held sheet) and lava-safe or they're rebuilt | ✅ |
| Portal breaking | frame break pops the sheet | orphaned portal blocks cascade-pop on neighbor updates (overworld sim; nether-side strays only harmless — never link targets) | ✅ |
| Death across dimensions | respawn to overworld spawn/bed | FIXED: death in the nether/End now routes through the switch machinery — the connection resets its dimension, restreams overworld chunks, everyone's views swap. (Previously the client respawned overworld while the server kept them in the old dim: phantom players, thin-air blocks, cross-world damage.) Join-time entity visibility is also dim-filtered now (tab list stays global) | ✅ |
| Arrival safety | invulnerability frames | 3s environmental-damage grace on arrival; refuge fallback = obsidian island ON the lava sea (full-pocket floor, no edges); natural-landing scan radius 48 | ✅ |
| Containers | per-dimension block entities | container maps are keyed by position only — a nether chest at the exact coords of an overworld chest would collide (no generated containers in the nether, so only reachable by deliberate construction) | ⚠️ known |

### Villages (added 2026-07-04, `worldgen/village.go` + `server/villager.go`, GenVersion 2→3)

| Mechanic | Vanilla | Ours | Status |
|---|---|---|---|
| Generation | biome styles, blueprint pool, streets | one style: well+bell, 4-7 plank houses on a ring, farms, L-paths; flat dry land only | 🟡 |
| Villagers | 15 professions, leveling, restocks, schedules, breeding | 3 professions (farmer/fletcher/toolsmith), 3 static offers each; spawn once per session on approach (not persisted) | 🟡 |
| Villager AI | roam village, open doors, flee, sleep, work sites, gossip | goal-directed A* roaming + open/close wooden doors + daily schedule (work@station / gather@bell / sleep@bed by day-clock) (`villager_ai.go`); no work-site claiming, gossip, or raid hiding | 🟡 |
| Trading | dynamic pricing, demand, XP, restock | fixed prices, unlimited uses, server recomputes the offer on the result click (the click is a wish) | 🟡 |
| Iron golem | 100 HP, spawned by villagers, po-faced | 100 HP, spawns with the village, punches hostiles for 8 with launch knockback, drifts home | 🟡 |
| Villager look | profession skins via entity metadata | default skin (no villager_data metadata yet) | ❌ |

### Structures (added 2026-07-04, `worldgen/structures.go` + `server/spawner.go`, GenVersion 1→2)

| Mechanic | Vanilla | Ours | Status |
|---|---|---|---|
| Dungeons | 1-2 chests, cobble/mossy floor mix, spawner w/ spinning mob | one chest, mossy shell mix, live spawner (200-800t delay, 16-block activation, cap 6) — no spinning-mob display (needs block-entity data packet) | 🟡 |
| Spawner | block entity, XP on break, silk-touch rules | pure-seed lookup (no block entity); mine it → dead | 🟡 |
| Mineshafts | vast nets, corridors+stairs, cave spider spawners, loot carts | straight arms + one branch per arm, supports/cobwebs/rails; no stairs/spiders/carts | 🟡 |
| Lakes |水 both types, underground too | surface bowls only, 20%% lava | 🟡 |
| Ruins | (n/a — flavor) | small broken stone-brick shells | ✅ |
| GenVersion | — | bumped 1→2 (2026-07-04): terrain regenerates under existing builds; VM state backed up to ~/backups/20260704-1807 first | ⚠️ |

### Vehicles (added 2026-07-04, `server/rail.go` + `server/vehicle.go`)

| Mechanic | Vanilla | Ours | Status |
|---|---|---|---|
| Rails | auto-shape, corners/slopes, 9-rail powered chains | auto-shape on place + neighbour updates; specials degrade corners to straight; powered rail takes DIRECT power only (no chain) | 🟡 |
| Minecart | rail physics server-side for empty carts, damage/HP, pushing | ridden carts client-simulated + server-validated (3-block/packet cap, NaN reject, snap-back); empty carts sit still; punch = instant break + item drop | 🟡 |
| Boats | drift physics, paddle animation, 2 seats, fall damage rules | oak..pale_oak entities, place on water, ride + validated moves, single seat, no drift/paddles | 🟡 |
| Detector rail | powers while any cart is on it | same (per-tick vehicle occupancy scan, like plates) | ✅ |
| Special carts | chest/hopper/TNT/furnace minecarts | none | ❌ |
| Persistence | carts/boats saved with the world | vehicles vanish on restart | ❌ |

### Online mode (added 2026-07-04, `protocol/crypto.go` + `server/auth.go`)

| Mechanic | Vanilla | Ours | Status |
|---|---|---|---|
| Encryption | RSA-1024 + AES/CFB-8 | same (stdlib primitives; CFB-8 hand-rolled; wiki.vg digest test vectors) | ✅ |
| Auth | sessionserver hasJoined | same, 5s timeout, hard fail = login disconnect; OFFLINE by default (-online opts in) | ✅ |
| Skins | textures property via PlayerInfo | carried through Login Success + Player Info adds | ✅ |
| Velocity/chat signing | 1.19+ chat signatures | not implemented (secure chat off, System Chat everywhere — unchanged) | ❌ |
| Whitelist/bans | UUID-keyed json + kick on ban | name-keyed gatekeeper.json, login-time gate only (no live kick yet) | 🟡 |

**End portal rings (2026-07-05):** ANY complete 12-frame eyed ring around a
3x3 interior opens the portal — the stronghold's generated ring and
player-built rings alike (one generic detector in insertEye; the seed-ring
special case is gone). Vanilla's inward-facing requirement is deliberately
NOT enforced — a wrongly-faced ring silently failing is a support burden,
not gameplay. Interior fill only replaces air/replaceables. Ring detection
is overworld-scoped (insertEye reads h.world) — building frame rings in
other dimensions stays inert for now.

**RESOLVED (2026-07-05, the invisible-dragon saga — five bugs deep):** the
End dragon was (1) terrain-pinned: it lived in h.mobs so shared mob gravity
overpowered updateDragon's flight every tick, burying it inside the island
(the debug heartbeat showed staged y=85 → y=59 in four seconds); the
original "dragon below the island" sighting was this bug seen through the
+64 render shift. (2) Its spawn/syncs were radius-culled (toNearby) while
arrivals land ~100 blocks out — boss entities now broadcast dim-wide
(toDim). (3) Its movement used sync_entity_position — the only use of that
packet, never client-verified; it now flies on the same relative
entity_move_look deltas as every other mob (sent-position shadow, no
drift). (4) Redundant staging broadcasts could duplicate the spawn for the
arriving player — every End entity reaches an arrival exactly once, via
the dimension-switch view swap. (5) Even then the arrival-window spawn was
sometimes lost client-side (unsolved 26.x quirk; wire provably correct) —
the BOSS REFRESHER routes around it: when the bossbar first shows for a
player (~1s post-arrival), they get a clean reliable destroy+spawn of the
boss. Bisect tools that cracked it: /summon ender_dragon (dimension-aware)
and the 10s dragon heartbeat log. has_ender_dragon_fight is FALSE for our
End (the flag was tested both ways and is not the renderer gate, but true
implies vanilla fight machinery we don't run).

**RESOLVED (2026-07-05, the void-arrival saga):** dimension bounds are
LOAD-BEARING inline registry data. `sendRegistries` suppressed ALL inline
NBT for 775+ clients ("26.x schemas changed"), so 26.x clients used their
BUILT-IN dimension types: overworld built-in (-64..384) happens to match
ours — but nether/End built-ins are 0..256, so those dimensions rendered
64 blocks up-shifted from the server's simulation. Symptoms: void-fall at
portal arrival (survival), dragon + crystals rendering below the End
island, "walls in thin air" (creative players float and never notice).
Fix: dimension_type is now inlined for EVERY version via
RegistryEntryDataFor(version) — 26.x additionally requires
has_ender_dragon_fight (bool, true for the_end; the exact field ViaVersion
adds), and 1.21.9+ wants the End's ambient_light 0.25. The wire-forensic
chain that found it (portalprobe): respawn byte-perfect → chunks byte-exact
→ cell decode = correct obsidian → vanilla client bisect (not mods) →
End "dragon below the island" (user observation = the +64 signature) →
config scan showed the dimension_type packet was 115 bytes: names only.

**Prior investigation notes (superseded):** a Fabric-modded 26.2
client stops PROCESSING the Respawn packet on nether travel (no pipeline
reload client-side, falls through unstreamed terrain) — while the production
wire is PROVEN correct end to end by cmd/portalprobe walking the identical
path live: yield → dwell → linked switch → byte-perfect 0x52 respawn → 169
batch-framed nether chunks. Worked pre-21:00, regressed after; server-side
deltas in the window don't touch the respawn path. Suspect client-side
(Sodium-family pipeline). Bisect pending: vanilla 1.21.5 (protocol 770, no
translation) vs modded 26.2. The probe (cmd/portalprobe) is the wire oracle:
it logs in at 776, asserts a position until the authority yields, dwells,
and audits the switch sequence.

**Movement authority addendum (2026-07-04):** after ~100 consecutive
rejections the authority YIELDS to the client position (loud log) — an
authority that rejects forever is a deadlock, not security; a client that
lost sync (e.g. across a dimension switch) could fall/snap in a loop
eternally. Teleport corrections now use fresh incrementing teleport IDs
(a constant ID risks the client ignoring repeat corrections).

**World simulation still missing** — fire spread odds/age, full fluid
pathfinding + lava/water conversion, ice/snow by light, leaf-decay distance
propagation.

### Weather (added 2026-07-04, `server/weather.go`)

| Mechanic | Vanilla | Ours | Status |
|---|---|---|---|
| Cycle | clear 12k-180k, rain 12k-24k ticks | same ranges, 30% thunder | ✅ |
| Announce | game events 1/2 + levels 7/8 | same (+ resent to joiners) | ✅ |
| Lightning | strikes near players, 5 dmg, fire | bolt entity 74 + thunder crack (vol 10) + 5 dmg in a 3-block box, sky-exposed columns only, ~1/8 s per player | ✅ (no fire until #36) |
| Rain effects | undead don't burn; crops; fills cauldrons | burn gate only | 🟡 |
| Sleep clears weather | yes | on night-skip | ✅ |
| /weather | clear/rain/thunder [duration] | clear/rain/thunder (ops) | ✅ (no duration arg) |
| Snow/ice formation, biome rain-vs-snow | yes | — | ❌ |

**Crafting & blocks-with-state** — crafting recipes, furnace (200-tick smelt,
fuel burn times), brewing, block entities (chests/hoppers/barrels inventories),
anvil/enchanting/grindstone, signs.

**DONE since this list was written** — chests (27-slot persisted storage,
menu generic_9x3=2, spill on break), beds (claim respawn point, all-asleep
night skip, monster check, and the real lying-down pose via set_entity_data
— pose serializer id remapped 21→20 for 773+ clients in the chain), tool durability + armor (see scorecard).

### XP + enchanting (added 2026-07-04)

| Mechanic | Vanilla | Ours | Status |
|---|---|---|---|
| XP sources | kills, mining, smelting, bottles | player kills (hostiles 5, cows 1-3) + coal 0-2 / diamond 3-7 ore + smelting (banked per furnace, ~0.7/item, 0.35 food, paid on output take) | ✅ |
| Level curve | 3-segment formula | `xpToNext`/`totalXP` exact | ✅ |
| Death | drop 7×level ≤100, reset | same (one orb at the spot) | ✅ |
| Orbs | drift toward players, merge | drift 0.3 b/t toward nearest survival player within 8, pickup 1.5, despawn 5 min | ✅ |
| Enchanting table | 3 offers, bookshelf power, lapis+levels | vanilla cost formula (shelf ring counted), pools: swords sharpness / tools efficiency / armor protection (+unbreaking on ≥15-cost top row), pay button+1 lapis AND levels, creative free | 🟡 curated pool |
| Enchant display | glint + tooltip everywhere | enchantments component on Slots; registry declared for ALL versions (legacy 770-774 get the 1.21.5 tag manifest w/ empty contents; component id remapped 10→13 for 774+) | ✅ |
| Effects | many | sharpness +lvl melee, protection 4%/EPF ≤80%, unbreaking lvl/(lvl+1) wear skip (tools+armor); efficiency client-visual only (mining time is client-timed) | 🟡 |
| Anvil | merge/repair/rename, prior-work penalty, material repair | `anvil.go`: sacrifice+book merges (equal levels bump, capped), repair = combined remaining +12%, rename via name_item (custom_name component, ids 5→6@774; NOT persisted across restarts yet), level costs enforced server-side | 🟡 no prior-work/material repair |
| Grindstone | strip enchants, XP refund | strip + 6 XP/level orb; enchanted book → book | ✅ approx |
| Enchanted books | table + anvil | books enchant at the table (stored_enchantments component — ids 34→41@774→42@776, chain-remapped), apply via anvil | ✅ |
| Fortune / Looting / Silk Touch | drop modifiers | Fortune ×(1+rng(lvl+1)) on ore drops; Looting +rng(lvl+1)/roll (killer's level stamped per hit); Silk Touch drops the block itself for a curated set (stone/grass/all ores) and skips ore XP | ✅ |

### Sounds & particles (added 2026-07-04, `server/sound.go`)

Sounds are sent INLINE BY NAME (sound_effect holder id 0 + identifier +
category + ×8 fixed-point pos) — version-proof, no sound-registry remap ever;
unknown names are silently ignored by clients. Particles carry version-shifted
type ids; the chain remaps the payload-free set we emit (`remapParticleID`:
explosion_emitter 21→22@773→29@776, explosion, poof, crit — ViaVersion-
verified). world_event 2001 gives OTHER players break particles+sound (its
data is a block state — remapped via RegBlockState in the chain). Emitted
today: mob hurt/death/ambient (1 Hz, 1-in-12 chance), player hurt/death,
skeleton shoot + arrow thunk, creeper primed + explosion, XP ding/level-up,
item pickup pop, eat burp, chest open/close, enchant. NOT yet: block place
sounds, footsteps (client-side anyway), music, weather (needs #33), note
blocks/jukebox.

**Misc** — villager trades, advancements/stats, scoreboard,
biome-specific rules (temperature, mob spawns), structures.

**Server authority (standing rule: clients must not override the server).**
Validated today: set_creative_slot is gamemode-gated; melee hits are
reach-checked (6 blocks); window clicks reject net item fabrication for
non-creative players (resync instead of apply); digs revert on unbreakable
blocks; movement is validated in the hub (`movement.go`): per-tick speed
budget (10 b/s survival · 20 b/s creative, 1.5 s burst bank; falling free),
12-block single-event teleport cap, NaN/Inf rejection, and vanilla-style
floating detection (80 ticks neither descending nor near ANY block → snapped
to the local floor; ladders/water/jumps can't false-positive because they all
touch a block). Rejected moves are not applied; the client is rubber-banded
back with a rotation-relative sync (camera untouched), throttled to 1/s.
ALL CLOSED 2026-07-04 (`authority.go`): mining time is validated (dig Start
arms a timer; a Finish faster than 30×hardness/toolSpeed × 0.5 tolerance —
with Efficiency headroom — reverts the block); noclip moves (destination
hitbox inside full solid cubes, per the sky-opacity full-cube test) are
rejected unless the player is escaping from INSIDE one (sand burial must
stay escapable); suffocation deals 2/s to a buried head; and chunk
streaming is gated on the hub-validated position mirror (a rubber-banded
pretender no longer downloads the map around the claimed spot).

---

## Vanilla oracle & differential probe (2026-07-05)

Parity claims are now testable against ground truth instead of the wiki. Setup
(all on the LAN VM, user@<vm-host>):

- **Java**: user-local Temurin 21 at `~/java` (no root needed; VM python/urllib
  has no IPv6 route — use `curl -4` for Mojang endpoints).
- **Vanilla 1.21.5 server** (SHA-1 verified against Mojang's version manifest):
  `~/vanilla/server-1.21.5.jar`, running from `~/vanilla/run` on **:25566**
  (offline mode, seed 7, creative, view-distance 8). Restart:
  `cd ~/vanilla/run && nohup ~/java/bin/java -Xmx1536M -jar ../server-1.21.5.jar nogui &`.
- **Datagen reports** (`java -DbundlerMainClass=net.minecraft.data.Main -jar
  server-1.21.5.jar --reports`): checked into `reference/1.21.5/reports/` —
  Mojang's own registries/blocks/items/packets/commands dumps. These OUTRANK
  minecraft-data and the wiki. Spot-check passed: stone=1, bedrock=85,
  obsidian=2400, end_portal=8190, end_portal_frame=8191, 27 914 total block
  states — all match our generated tables.
- **`cmd/diffprobe`**: joins any 770 server as a real client (client_information,
  known-packs echo, teleport confirms, keepalive + chunk-batch acks) and prints a
  normalized transcript — config/play packet histograms named from Mojang's
  `packets.json` + field decodes of join/position/registries. Run against both
  servers and diff; the diff IS the parity gap list:
  `go run ./cmd/diffprobe <vm-host>:25566 8 > vanilla.txt` (and `:25565 > tachyne.txt`).

### First differential run — join-sequence gaps (vanilla → tachyne)

Config phase — vanilla additionally sends: `custom_payload` (server brand, shows
in F3), `update_enabled_features`, and 2 more registries (`test_environment`,
`test_instance`). **Discovery: vanilla sends dimension_type/biome/damage_type as
names-only (115/1587/1098 B) to a matching-known-packs 770 client** — our
"three registries must carry inline NBT" rule (gotcha #2) is stricter than
vanilla; ours works too (and 26.x genuinely NEEDS inline dimension_type), just
bigger (1191/2336/5527 B). Registry order also differs (harmless; vanilla's is
the code-defined registry order).

Join packet — we send `dims=[overworld]`; vanilla lists all three dimensions.
Vanilla honors the client's view distance (capped by server); ours is fixed 6.

Play phase — packets vanilla uses that we never send: `bundle_delimiter`
(entity-packet atomicity), `commands` (the command tree → client tab-completion),
`change_difficulty`, `initialize_border`, `server_data`,
`set_default_spawn_position` (compass target), `light_update` (standalone
relight), `entity_event`, `set_entity_motion` (mob velocities → client
interpolation), `set_equipment`, `set_held_slot`, `set_passengers`,
`update_attributes`, `update_advancements`, `update_recipes`,
`recipe_book_settings`, `ticking_state`/`ticking_step`, plus steady
`block_update`/`section_blocks_update` traffic from random ticks around spawn.

Efficiency divergences (ours, same window): we send `entity_position_sync` for
every mob every 2 s (vanilla: on spawn/teleport only) and always the combined
`move_entity_pos_rot` (vanilla splits pos-only/rot-only — its 8 s totals were
682/691/85 vs our 1934 combined); our action-bar HUD accounts for the
`system_chat` stream. Vanilla mobs also simply move less (AI idles).

**Parity batch 1 landed (2026-07-05, `parity.go`):** the command tree
(`commands` 0x10 — root + one shared greedy-string node + a literal per
command → client-side tab-completion; brigadier:string parser id 5 verified
append-stable 770→776), `change_difficulty` (join + live change broadcast),
`initialize_border` (static vanilla-default border), `set_default_spawn_position`
(two wire variants: Position+angle ≤772, GlobalPos+yaw+pitch 773+ per
ViaVersion's 1.21.9 rewriter — chain lands them at 26.x's documented 0x2B/0x61),
the config-phase brand (`minecraft:brand` = "tachyne") + `update_enabled_features`,
and the join packet now lists all three dimensions. Deliberately NOT sent:
`server_data` — 770 (minecraft-data) and 773 (wiki) disagree on a trailing
secure-chat boolean with no ViaVersion rewriter to arbitrate; cosmetic, so it
waits until a capture pins the layout per version.

**Parity batch 3 landed (2026-07-05, mob duty cycle — the first BEHAVIORAL
oracle finding):** the probe now measures per-mob-type behavior (entity
tracking + move-delta integration + attribute harvest + tick-rate check). The
30 s comparison showed vanilla's unprovoked mobs idle ~80-90% (passives drift
0.16 b/s perceived, unaggroed hostiles 0.0-0.07 b/s) while ours wandered
near-constantly (0.30-0.43 b/s, ~8× the move packets). Fixed in `mob.go`:
idle is now the DEFAULT state — rest 8-20 s (hostiles ×2), stroll 2.5-4.5 s,
repeat; hunting (hasTarget) and courtship (loveTicks) override the cycle, and
a resting hostile wakes the instant prey appears. Tick rates matched exactly
(19.3 measured on both). Bonus find: TestHubMultiplayer had been passing on
mob packet noise — its 7-block instant move was silently rejected by the
movement authority all along; the test now makes a legal move.

**Parity batch 4 landed (2026-07-05, join extras + the server_data capture):**
`server_data` is now sent — the disputed trailing boolean was settled by
capturing vanilla's actual bytes (25 B: TAG_String MOTD + one 0x00 icon-absent;
minecraft-data was right, the wiki listed a field 770 doesn't have; ViaVersion
never rewrites this packet 770→776 so one layout serves every version).
Also: `set_held_slot` on join (varint, confirmed from protocol.json),
`entity_event` 24/28 op-permission level on join (ops get the F3+F4 gamemode
switcher), and `set_equipment` now rides WITH player spawns in both directions
at join and dimension-switch (armor was invisible until the 2 s resync).
**Parity batch 5 (same day): client view distance honored** — the
`client_information` view distance (config phase seeds it; mid-game changes
re-stream immediately) clamps to [2, server cap 6] and drives the per-player
chunk window + the join packet's viewDist field, like vanilla's
min(client, server). Verified: request 3 → 49 chunks (7×7), request 8 → 169
(capped). Still open from the diff: `bundle_delimiter` grouping (needs
all-or-nothing queue writes — a dropped bundle-close wedges the client),
standalone `light_update` (client dynamic relight covers torches; low value),
`update_attributes` for mobs.

**Parity batch 6 landed (2026-07-05, the first INTERACTIVE oracle experiment —
zombie combat):** RCON enabled on the vanilla oracle (`:25575`,
`scripts/oracle_rcon.py`); diffprobe gained a combat mode (stands still in
survival, logs every set_health / own set_entity_velocity with timestamps).
Vanilla measured, normal difficulty, unsprinting zombie: **3.0 HP per hit,
metronomic 995 ms (20 tick) cadence, knockback on the wire h≈0.22-0.24 /
v=0.275 b/t**, saturation regen healing between hits. Ours matched on damage
(3, `TestZombieBitesIdlePlayer` pins it) and cadence (attackCooldown 10×2
ticks); knockback was ~2× too strong (0.42/0.36) → now 0.23/0.275.
The experiment also flushed out two real bugs the fight exposed:
(1) **mobs at EXACTLY sea level were frozen solid** — `Walkable` required
`Height > SeaLevel` strictly, so on beaches/coastal flats (like the VM spawn)
no neighbouring cell ever qualified and every mob stood paralyzed; Walkable
now tests the actual floor (dry feet), so beaches walk and water stays
off-limits; (2) **open doors now let mobs through** (vanilla behavior) and
**closed doors block in every geometry** — doors are never mob floors
(worldgen.IsDoor/IsClosedDoor; previously a door on a slope was a climbable
stair, and the door test's "closed" fixture was secretly an open-door state
that the old collision-only rule blocked anyway).

**Vanilla vanilla source ONLINE (2026-07-05):** the 1.21.5 server,
deobfuscated with Mojang's official mappings, lives on the VM at
`~/the vanilla-reference tooling/src/1.21.5/server/net/minecraft/` (pipeline: the vanilla-reference tooling +
the mappings tooling + CFR on the bundler's INNER jar — the wrapper jar references
to a 24 KB stub; VM python needs an IPv4-only getaddrinfo patch). RULE:
**facts only, never copy code** — read the formula, reimplement in Go
(stricter than the ViaVersion rule; this is Mojang proprietary code).
It immediately corroborated the combat measurements (Zombie ATTACK_DAMAGE
3.0; `LivingEntity.knockback(0.4,…)` + one tick of ground friction/gravity
reproduces the measured wire 0.23/0.275 exactly) and yielded the first
source-driven fix: **FOLLOW_RANGE is per-species** (Mob base 16 = our old
global; Zombie family 35, Blaze 48, EnderMan 64) — `m.aggro` now carries the
override, de-aggro is aggro+8 hysteresis. Zombies now notice you from 35
blocks like vanilla. **Mob ARMOR applied (same day):** the zombie family
(zombie/husk/drowned/zombified piglin) has base ARMOR 2.0 (the only base
armor among our mobs; wither's 4.0 awaits a wither). `mob.hurt()` implements
CombatRules.getDamageAfterAbsorb exactly (toughness 0:
`clamp(armor − dmg/2, armor×0.2, 20)/25` absorbed) with a fractional-damage
carry so integer HP reproduces vanilla hits-to-kill — a 5-damage weapon kills
an armor-2 zombie in 5 hits (4.92/hit), not 4 (`TestZombieArmorHitsToKill`).
Routed through armor: player melee, sweep, arrows, explosions, golem
punches, lightning. Deliberately NOT: fire ticks/daylight burn
(`on_fire` is in `bypasses_armor`).

**Source audit sweep (2026-07-05, batch: attributes/food/fall/fuses):**
diffed the vanilla createAttributes + FoodData + fall/explosion constants
against our tables. MATCHED already: every mob max-health (chicken 4, pig 10,
sheep 8, cow 10, spider 16, witch 26, enderman 40, zombie/skeleton/creeper 20,
golem 100), fall damage `floor(d − SAFE_FALL_DISTANCE 3)`, exhaustion
threshold 4.0, slow regen 1 HP/80t at food≥18 costing 6.0, starvation 80t
cadence, TNT fuse 80, creeper fuse 30 + blast radius 3. FIXED from source:
- **Per-species movement speeds** (`speedFor`, attr×0.45 calibrated on the
  oracle-measured cow): attr 0.20 cow/magma → 0.09; 0.23 sheep/zombie family/
  blaze → 0.104; 0.25 pig/chicken/skeleton/creeper/witch/golem → 0.112;
  0.30 spider/enderman → 0.135; villager 0.135 (attr 0.5 × ~0.6 goal mods).
  Replaced the flat hostile 0.13 / spider 0.17 / chicken 0.07 tiers.
- **Panic speed = 2.0× the mob's own speed** (PanicGoal; chickens 1.4×) —
  was a flat 0.40 (≈4 b/s) bolt for everything.
- **Starvation floor by difficulty** (FoodData): easy stops at 10 HP, normal
  at half a heart, HARD STARVES TO DEATH — ours stopped at 1 HP everywhere.
- **Iron golem KNOCKBACK_RESISTANCE 1.0** (`noKB`): never shoved by melee
  or arrows.
- **Creeper defuse range 7** (SwellGoal; was 6).

**Skeleton bow + player bow from source (2026-07-05):** already matching:
range 15, arrow speed 1.6, gravity 0.05, drag 0.99, bow max velocity 3.0.
Fixed from the vanilla formulas:
- **Skeletons now MISS like vanilla** — `performRangedAttack`/`shoot`: aim at
  ⅓ target height, gravity lob folded into the direction (dy += horizDist×0.2),
  then per-axis triangle(0, 0.0172275×inaccuracy) spread with inaccuracy
  14−4×difficulty (easy 10 / normal 6 / hard 2). Ours were aimbots — harder
  than vanilla hard.
- **Cadence by difficulty** (`RangedBowAttackGoal`): 40 ticks easy/normal,
  20 on hard (was a flat ~42).
- **Player bow charge is QUADRATIC** (`BowItem.getPowerForTime`):
  power=((t/20)²+2(t/20))/3 capped at 1, refuse under 0.1 — was linear, which
  overpowered half-draws. Full-draw crit bonus is vanilla's random
  nextInt(dmg/2+2), not a flat +1.
The skeleton hit test now fires a volley — under real spread a single
8-block shot legitimately misses, and asserting one shot hits was asserting
the aimbot.

**Despawn rules + spawn densities from source (2026-07-05):**
- **Despawn = vanilla `Mob.checkDespawn`** (hostiles only; CREATURE-category
  animals are persistent and never despawn): instant beyond 128 blocks of
  every same-dimension player (was a flat 64); beyond 32 blocks an idle
  clock runs — past 600 ticks, 1/800 per tick (≈2.5%/s; our 1 Hz sweep uses
  Intn(40)); within 32 the clock resets.
- **Mob cap = vanilla `NaturalSpawner`**: MONSTER 70 × streamedChunks / 17²,
  computed from the union of overworld players' actual view windows — one
  radius-6 player caps at ≈40 hostiles (was a flat 12: nights were ~3× too
  quiet). Nether mobs no longer eat the overworld cap (per-dimension count).
- **Pack spawning**: each attempt now places 1-4 of one species scattered
  ±4 blocks around the anchor, like vanilla spawn groups; spawn window
  24..80 blocks (vanilla: 24..any loaded chunk ≤128 — ours spawns inside the
  streamed radius so players actually meet them; adaptation, not oversight).

**Enderman behaviors + zombie reinforcements from source (2026-07-05):**
- **Stare aggro** (`isBeingStaredBy`): a survival player whose view vector
  hits the enderman's eyes (dot > 1 − 0.025/d, eye heights 2.55 vs 1.62)
  within its 64-block follow range provokes ~20 s of hunting; a carved
  pumpkin on the head exempts the starer (`TestEndermanStareAggro`).
- **Projectile immunity** (`EnderMan.hurtServer`): arrows NEVER land — the
  enderman teleports out from under them, taking no damage. Random teleport
  widened to vanilla's ±32 blocks.
- **Zombie reinforcements** (`Zombie.hurtServer`, HARD only + doMobSpawning):
  each zombie spawns with SPAWN_REINFORCEMENTS_CHANCE random 0..0.1, and 5%
  are "leaders" at +0.5..0.75. A hurt zombie under the roll summons a
  same-species recruit 7-40 blocks away (never within 7 of a player) already
  hunting the attacker; caller and recruit each lose 0.05 charge. Zombie,
  husk and drowned carry it (zombified piglin is explicitly 0 in vanilla).
  (`TestZombieReinforcements`)

**Breeding + XP numbers audited against source (2026-07-05):** breeding was
ALREADY exact — love 600 ticks, parent cooldown 6000, baby growth 24000,
breeding orb 1-7 (`nextInt(7)+1`) all match `Animal`/`AgeableMob`. The XP
table was the gap: only 4 hostile species + 4 farm animals paid anything.
Now per the vanilla xpReward values: Monster base 5 covers EVERY hostile
(husk/drowned/stray/enderman/witch/zombified piglin included), Blaze pays its
constructor-override 10, slimes/magma cubes pay their SIZE (4/2/1 down the
splits), baby zombies pay 2.5×, animals stay 1-3, and villagers/iron golems
pay nothing.

**Attack-cooldown verification + slime hops (2026-07-05):** the 1.9 combat
curve from #34 checked out against `Player.attack` — damage ×(0.2+0.8c²),
crit at charge>0.9 while falling and not sprinting, ×1.5: all exact. The
recovery PERIODS needed fixing (Mojang items report, sampled at vanilla's
ticker+0.5): sword 12t (was 13), axes are per-tier — wood/stone 25t, iron
22t, gold/diamond/netherite 20t (all were a flat 20) — the axe's
slow-but-heavy tradeoff now exists. Hoes still swing as fists (not in the
melee table; niche). **Slimes now HOP** (`SlimeMoveControl`): travel only
mid-bound (~8 ticks), sit still between bounds — jumpDelay rand(20)+10
ticks, ÷3 while hunting; each launch rides a 0.42 jump impulse to the client
so the arc animates, with the vanilla jump squish. Slime speed is the
size-scaled attribute (0.2+0.1×size; magma cubes flat 0.2), also applied to
split halves.

**FINAL ORACLE CONFIRMATION (2026-07-05 evening):** all three instruments
re-run against the deployed build. Join sequence: structural match (vanilla's
dims-list order is itself nondeterministic; one open cosmetic gap found — we
don't persist/send the LAST-DEATH LOCATION in the join packet, which feeds
the client's death-screen compass). Behavior: tick rates 20.1 vs 19.9;
passive drift within the idle-cycle variance. Combat, side by side: damage
3.0 = 3.0; cadence 970-1005 ms vs vanilla 985-1002; knockback 0.230/0.275 vs
0.218-0.240/0.275; saturation-regen taper matches (+0.8 → +0.1 as sat
drains) with food preserved at 20 — after fixing the last divergence the
fight itself caught: fast+slow regen stacking (vanilla is else-if). Probe
lesson recorded: a DEAD probe account freezes all entity ticking on vanilla
(no entity-ticking tickets) — diffprobe now clicks Respawn on join.

**Source-menu closeout (2026-07-05):** the last minor items.
- **Fast saturation regen at vanilla's TRUE 10-tick cadence** (`fastRegen`
  runs from the hub loop): heal min(saturation, 6)/6 HP costing that many
  exhaustion points — healing tapers as saturation drains, replacing the
  2-HP-per-second chunks.
- **Skeletons STRAFE** in the bow sweet spot (`RangedBowAttackGoal`): circle
  the target at 0.5× speed, flipping direction ~30%/s, instead of standing
  frozen while shooting.
- **Hoe attack periods** (per-tier, from the items report): wood/gold 20t,
  stone 10t, iron 7t, diamond/netherite 5t.
- **Baby zombies**: 5% of zombie-family spawns (`getSpawnAsBabyOdds`) —
  half-size (baby metadata), 1.5× speed (SPEED_MODIFIER_BABY), never mature,
  2.5× XP. The animal growth loop now guards `!m.hostile`.
- Creeper swell noted as done: ignite ≤3 blocks, defuse at 7, fuse 30t,
  blast radius 3 all match; vanilla's gradual swell-down vs our instant
  defuse is client-animated either way (state -1 shrinks smoothly).

**Parity batch 2 landed (2026-07-05, movement economy):** mob moves now split
vanilla-style — pos-only `move_entity_pos` (0x2e) unless the body yaw changed
at wire granularity (angle byte), then `move_entity_pos_rot`; and every mob
knockback impulse (melee, arrow, golem punch) broadcasts
`set_entity_velocity` so clients play the shove + hit-hop between relative
moves (pure animation: the client's server-tracked position still follows the
authoritative deltas). Remaining known divergence: the 2 s
`entity_position_sync` resync stays — it self-heals lossy trySend drops,
which vanilla (lossless writes) doesn't need.

---

*Generated as a tuning + gap reference. When you change a constant, update the
relevant row. The "blind spots" section roughly tracks the world-logic roadmap in
the project memory (Tier-1 done: drops, random-tick growth, survival loop; the
big deferred items remain fire, redstone, full mob AI).*
