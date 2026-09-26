package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// Live dungeon spawners + loot chests. Dungeons are pure functions of the
// seed (worldgen.DungeonIn), so the hub needs no block scan: every second it
// checks the dungeon cells around each player, and any spawner block still
// standing within activation range rolls mob spawns into its room. The
// dungeon chest fills with deterministic loot the first time it's opened.
// spawnerCycle is the one BaseSpawner round every kind of spawner runs: the
// seed-derived ones here, in fortress.go and structspawner.go, and the placed
// ones in spawnerbe.go.

const (
	spawnerRange     = 16  // vanilla activation distance
	spawnerMobCap    = 6   // nearby same-dungeon hostiles before it pauses
	spawnerMinDelay  = 200 // ticks between spawns (vanilla 200-800)
	spawnerDelaySpan = 600
	spawnerCount     = 4 // vanilla BaseSpawner DEFAULT_SPAWN_COUNT: 4 attempts/cycle
)

var dungeonMobs = [3]int{entityZombie, entitySkeleton, entitySpider}

// updateSpawners runs on the once-a-second survival cadence from the hub loop.
func (h *hub) updateSpawners(players map[int32]*tracked) {
	if !h.rules.DoMobSpawning || h.rules.Difficulty == diffPeaceful || !h.rules.SpawnerBlocks {
		return
	}
	gen := h.world.Gen()
	now := h.tick.Load()
	done := map[blockPos]bool{}
	for _, t := range players {
		if t.dim != dimOverworld {
			continue // BaseSpawner.isNearPlayer asks the spawner's own level: dungeons are overworld
		}
		px, pz := int(t.x), int(t.z)
		for dx := -1; dx <= 1; dx++ {
			for dz := -1; dz <= 1; dz++ {
				d := gen.DungeonIn(px+dx*48, pz+dz*48)
				if !d.Exists {
					continue
				}
				pos := blockPos{d.X, d.Y, d.Z}
				if done[pos] {
					continue
				}
				done[pos] = true
				if !h.ownedBlock(d.X, d.Z) || !h.cellWithinBorder(dimOverworld, d.X, d.Z) {
					continue // outside this pod's region, or past the world border (LevelChunk.isTicking)
				}
				if dist3(t.x, t.y, t.z, float64(d.X), float64(d.Y), float64(d.Z)) > spawnerRange {
					continue
				}
				if h.world.At(d.X, d.Y, d.Z) != worldgen.BlockBase("spawner") { // mined out → dead spawner
					continue
				}
				if h.placedSpawner(dimOverworld, pos) {
					continue // a spawn egg made it a block entity of its own (updatePlacedSpawners)
				}
				etype := dungeonMobs[d.Mob%3]
				h.showSpawner(players, 0, pos, etype) // the mob turning in the cage
				h.seedSpawnerTick(players, dimOverworld, pos, etype, now)
			}
		}
	}
}

// (Structure chest loot is now data-driven — see structloot.go/chestloot.go.
// The old hand-rolled dungeonLoot + hash01ServerSeed lived here.)

// spawnerCycle is one BaseSpawner.serverTick spawn round at a cage whose
// delay has run out: four attempts, each at x/z ±spawnRange (triangular) and
// y −1..+1 around the cage, where the mob's box is clear of colliding blocks
// (noCollision) and the species' spawn rules pass for a SPAWNER spawn. An
// attempt that finds maxNearbyEntities of the kind already in the cage's
// cell inflated by spawnRange ends the round. Each mob that goes in gets the
// spawn particles at the cage, its ENTITY_PLACE and its poof. It reports
// whether the spawner re-arms (delay()): when something spawned or the cap
// was reached; otherwise vanilla leaves the delay at zero and tries again.
func (h *hub) spawnerCycle(players map[int32]*tracked, dim int, pos blockPos, etype int) bool {
	box, ok := mobBoxes[etype]
	if !ok {
		box = defaultMobBox
	}
	spawned := false
	for i := 0; i < spawnerCount; i++ {
		sx := float64(pos.x) + (h.rng.Float64()-h.rng.Float64())*spawnerSpawnRange + 0.5
		sz := float64(pos.z) + (h.rng.Float64()-h.rng.Float64())*spawnerSpawnRange + 0.5
		sy := float64(pos.y + h.rng.Intn(3) - 1)
		if !h.spawnBoxClear(dim, sx, sy, sz, box.w, box.h) {
			continue
		}
		if !h.spawnerRulesOK(dim, etype, floorInt(sx), floorInt(sy), floorInt(sz)) {
			continue
		}
		if h.spawnerNearby(dim, pos, etype) >= spawnerMobCap {
			h.spawnerReset(players, dim, pos)
			return true
		}
		if h.boxHasLiquid(dim, sx, sy, sz, box.w, box.h) {
			continue // Mob.checkSpawnObstruction
		}
		m := h.spawnConfigured(players, etype, dim, sx, sy, sz)
		if m == nil {
			h.spawnerReset(players, dim, pos) // tryAddFreshEntity refused it
			return true
		}
		h.levelEvent(players, dim, worldEventSpawnerSpawn, pos.x, pos.y, pos.z, 0) // the smoke and flames
		h.vib(dim, freqEntityPlace, floorInt(sx), floorInt(sy), floorInt(sz), m.eid)
		h.toTracking(players, m.eid, dim, m.x, m.z, entityStatus(m.eid, entityStatusSpawnAnim))
		spawned = true
	}
	if spawned {
		h.spawnerReset(players, dim, pos)
	}
	return spawned
}

// spawnerNearby counts the living mobs of the kind whose boxes touch the
// cage's cell inflated by spawnRange (getEntities over that AABB).
func (h *hub) spawnerNearby(dim int, pos blockPos, etype int) int {
	near := 0
	lo := [3]float64{float64(pos.x) - spawnerSpawnRange, float64(pos.y) - spawnerSpawnRange, float64(pos.z) - spawnerSpawnRange}
	hi := [3]float64{float64(pos.x) + 1 + spawnerSpawnRange, float64(pos.y) + 1 + spawnerSpawnRange, float64(pos.z) + 1 + spawnerSpawnRange}
	for _, m := range h.mobs {
		if m.dim != dim || m.etype != etype || m.dying > 0 {
			continue
		}
		b := m.box()
		if m.x+b.w/2 > lo[0] && m.x-b.w/2 < hi[0] && m.y+b.h > lo[1] && m.y < hi[1] && m.z+b.w/2 > lo[2] && m.z-b.w/2 < hi[2] {
			near++
		}
	}
	return near
}

// spawnerDelayRoll is BaseSpawner.delay's new spawnDelay: 200..799.
func (h *hub) spawnerDelayRoll() int {
	return spawnerMinDelay + h.rng.Intn(spawnerDelaySpan)
}

// boxHasLiquid is level.containsAnyLiquid over a mob's box.
func (h *hub) boxHasLiquid(dim int, x, y, z, w, ht float64) bool {
	wd := h.worldFor(dim)
	for bx := floorInt(x - w/2); bx <= floorInt(x+w/2-1e-9); bx++ {
		for by := floorInt(y); by <= floorInt(y+ht-1e-9); by++ {
			for bz := floorInt(z - w/2); bz <= floorInt(z+w/2-1e-9); bz++ {
				if st := wd.At(bx, by, bz); worldgen.HoldsWater(st) || worldgen.IsLava(st) {
					return true
				}
			}
		}
	}
	return false
}

// spawnBoxClear is level.noCollision(type.getSpawnAABB(x, y, z)) against
// blocks: no colliding block in any cell the box reaches.
func (h *hub) spawnBoxClear(dim int, x, y, z, w, ht float64) bool {
	wd := h.worldFor(dim)
	for bx := floorInt(x - w/2); bx <= floorInt(x+w/2-1e-9); bx++ {
		for by := floorInt(y); by <= floorInt(y+ht-1e-9); by++ {
			for bz := floorInt(z - w/2); bz <= floorInt(z+w/2-1e-9); bz++ {
				if worldgen.Collides(wd.At(bx, by, bz)) {
					return false
				}
			}
		}
	}
	return true
}

// seedSpawnerTick runs a seed-derived spawner's cooldown: in memory, as the
// spawner itself is found again from the seed. A round that neither spawned
// nor hit the cap leaves the spawner armed for the next pass.
func (h *hub) seedSpawnerTick(players map[int32]*tracked, dim int, pos blockPos, etype int, now uint64) {
	key := simPos{dim: dim, blockPos: pos}
	if next, ok := h.spawnerNext[key]; ok && now < next {
		return
	}
	if h.spawnerCycle(players, dim, pos, etype) {
		h.spawnerNext[key] = now + uint64(h.spawnerDelayRoll())
	} else {
		delete(h.spawnerNext, key)
	}
}
