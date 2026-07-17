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
)

var dungeonMobs = [3]int{entityZombie, entitySkeleton, entitySpider}

// updateSpawners runs on a 40-tick cadence from the hub loop.
func (h *hub) updateSpawners(players map[int32]*tracked) {
	if !h.rules.DoMobSpawning || h.rules.Difficulty == diffPeaceful {
		return
	}
	gen := h.world.Gen()
	now := h.tick.Load()
	done := map[blockPos]bool{}
	for _, t := range players {
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
				if !h.ownedBlock(d.X, d.Z) {
					continue // dungeon spawner outside this pod's region
				}
				if dist3(t.x, t.y, t.z, float64(d.X), float64(d.Y), float64(d.Z)) > spawnerRange {
					continue
				}
				if h.world.At(d.X, d.Y, d.Z) != worldgen.BlockBase("spawner") { // mined out → dead spawner
					continue
				}
				if next, ok := h.spawnerNext[pos]; ok && now < next {
					continue
				}
				h.spawnerNext[pos] = now + spawnerMinDelay + uint64(h.rng.Intn(spawnerDelaySpan))
				near := 0
				for _, m := range h.mobs {
					if m.hostile && dist3(m.x, m.y, m.z, float64(d.X), float64(d.Y), float64(d.Z)) < 9 {
						near++
					}
				}
				if near >= spawnerMobCap {
					continue
				}
				etype := dungeonMobs[d.Mob%3]
				for i := 0; i < 1+h.rng.Intn(2); i++ {
					sx := float64(d.X-d.W) + h.rng.Float64()*float64(2*d.W) + 0.5
					sz := float64(d.Z-d.D) + h.rng.Float64()*float64(2*d.D) + 0.5
					h.spawnHostileY(players, etype, sx, float64(d.Y), sz)
				}
				h.playSound(players, "minecraft:block.fire.ambient", sndHostile,
					float64(d.X)+0.5, float64(d.Y)+0.5, float64(d.Z)+0.5, 0.6, 0.8)
			}
		}
	}
}

// (Structure chest loot is now data-driven — see structloot.go/chestloot.go.
// The old hand-rolled dungeonLoot + hash01ServerSeed lived here.)
