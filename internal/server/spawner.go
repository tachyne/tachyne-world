package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// Live dungeon spawners + loot chests. Dungeons are pure functions of the
// seed (worldgen.DungeonIn), so the hub needs no block scan: every 2 seconds
// it checks the dungeon cells around each player, and any spawner block still
// standing within activation range rolls mob spawns into its room. The
// dungeon chest fills with deterministic loot the first time it's opened.

const (
	spawnerRange     = 16  // vanilla activation distance
	spawnerMobCap    = 6   // nearby same-dungeon hostiles before it pauses
	spawnerMinDelay  = 200 // ticks between spawns (vanilla 200-800)
	spawnerDelaySpan = 600
	spawnerCount     = 4 // vanilla BaseSpawner DEFAULT_SPAWN_COUNT: 4 attempts/cycle
)

var dungeonMobs = [3]int{entityZombie, entitySkeleton, entitySpider}

// updateSpawners runs on a 40-tick cadence from the hub loop.
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
				etype := h.spawnerMobFor(0, d.X, d.Y, d.Z, dungeonMobs[d.Mob%3])
				h.showSpawner(players, 0, pos, etype) // the mob turning in the cage
				key := simPos{blockPos: pos}          // dungeons are overworld
				if next, ok := h.spawnerNext[key]; ok && now < next {
					continue
				}
				h.spawnerNext[key] = now + spawnerMinDelay + uint64(h.rng.Intn(spawnerDelaySpan))
				h.spawnerCycle(players, pos, etype)
			}
		}
	}
}

// (Structure chest loot is now data-driven — see structloot.go/chestloot.go.
// The old hand-rolled dungeonLoot + hash01ServerSeed lived here.)

// spawnerCycle is one BaseSpawner.serverTick spawn round at an overworld
// cage: nothing when maxNearbyEntities of the same type already stand in
// the cage's cell inflated by spawnRange; otherwise four attempts, each at
// x/z ±spawnRange (triangular) and y −1..+1 around the cage, where the
// mob's box is clear of colliding blocks (noCollision), then the spawn
// particles and the caged mob's turn reset.
func (h *hub) spawnerCycle(players map[int32]*tracked, pos blockPos, etype int) {
	near := 0
	lo := [3]float64{float64(pos.x) - spawnerSpawnRange, float64(pos.y) - spawnerSpawnRange, float64(pos.z) - spawnerSpawnRange}
	hi := [3]float64{float64(pos.x) + 1 + spawnerSpawnRange, float64(pos.y) + 1 + spawnerSpawnRange, float64(pos.z) + 1 + spawnerSpawnRange}
	for _, m := range h.mobs {
		if m.dim != dimOverworld || m.etype != etype || m.dying > 0 {
			continue
		}
		b := m.box()
		if m.x+b.w/2 > lo[0] && m.x-b.w/2 < hi[0] && m.y+b.h > lo[1] && m.y < hi[1] && m.z+b.w/2 > lo[2] && m.z-b.w/2 < hi[2] {
			near++
		}
	}
	if near >= spawnerMobCap {
		return
	}
	box, ok := mobBoxes[etype]
	if !ok {
		box = defaultMobBox
	}
	for i := 0; i < spawnerCount; i++ {
		sx := float64(pos.x) + (h.rng.Float64()-h.rng.Float64())*spawnerSpawnRange + 0.5
		sz := float64(pos.z) + (h.rng.Float64()-h.rng.Float64())*spawnerSpawnRange + 0.5
		sy := float64(pos.y + h.rng.Intn(3) - 1)
		if !h.spawnBoxClear(dimOverworld, sx, sy, sz, box.w, box.h) {
			continue
		}
		h.spawnHostileYIn(players, etype, dimOverworld, sx, sy, sz)
	}
	h.levelEvent(players, 0, worldEventSpawnerSpawn, pos.x, pos.y, pos.z, 0) // the smoke and flames
	h.spawnerReset(players, 0, pos)                                          // …and the caged mob starts its turn again
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
