package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// Nether fortresses: the structure is stamped by worldgen (fortress.go
// there). The server runs what vanilla hangs on it — the fortress's own
// spawn table inside its pieces (NetherFortressStructure.FORTRESS_ENEMIES)
// and the blaze spawners on the monster thrones — and routes its chests to
// the nether_bridge loot table (structloot.go).

// fortressPool is FORTRESS_ENEMIES: weight, entity, pack size.
var fortressPool = []struct {
	weight   int
	etype    int
	min, max int
}{
	{10, entityBlaze, 2, 3},
	{5, entityZombifiedPiglin, 4, 4},
	{8, entityWitherSkeleton, 5, 5},
	{2, entitySkeleton, 5, 5},
	{3, entityMagmaCube, 4, 4},
}

// inFortressPiece reports whether a nether column lies inside a fortress
// piece's footprint — where the structure's spawn override applies.
func (h *hub) inFortressPiece(x, z int) bool {
	nw := h.worldFor(dimNether)
	if nw == nil {
		return false
	}
	gen := nw.Gen()
	for _, f := range gen.FortressesNear(x, z) {
		for _, p := range gen.FortressPieces(f) {
			if x >= p.X0 && x <= p.X1 && z >= p.Z0 && z <= p.Z1 {
				return true
			}
		}
	}
	return false
}

// updateFortressSpawners is updateSpawners for the fortresses' blaze
// spawners (the monster thrones): the same cadence, cap and four attempts
// a cycle, while the spawner block still stands.
func (h *hub) updateFortressSpawners(players map[int32]*tracked) {
	nw := h.worldFor(dimNether)
	if !h.rules.DoMobSpawning || h.rules.Difficulty == diffPeaceful || nw == nil {
		return
	}
	gen := nw.Gen()
	now := h.tick.Load()
	done := map[blockPos]bool{}
	spawnerState := worldgen.BlockBase("spawner")
	for _, t := range players {
		if t.dim != dimNether {
			continue
		}
		var spawners [][3]int
		for _, f := range gen.FortressesNear(int(t.x), int(t.z)) {
			spawners = append(spawners, gen.FortressSpawners(f)...)
		}
		for _, s := range spawners {
			pos := blockPos{s[0], s[1], s[2]}
			if done[pos] {
				continue
			}
			done[pos] = true
			if dist3(t.x, t.y, t.z, float64(s[0]), float64(s[1]), float64(s[2])) > spawnerRange {
				continue
			}
			if nw.At(s[0], s[1], s[2]) != spawnerState {
				continue // mined out
			}
			if next, ok := h.spawnerNext[pos]; ok && now < next {
				continue
			}
			h.spawnerNext[pos] = now + spawnerMinDelay + uint64(h.rng.Intn(spawnerDelaySpan))
			near := 0
			for _, m := range h.mobs {
				if m.dim == dimNether && m.etype == entityBlaze && dist3(m.x, m.y, m.z, float64(s[0]), float64(s[1]), float64(s[2])) < 9 {
					near++
				}
			}
			if near >= spawnerMobCap {
				continue
			}
			for i := 0; i < spawnerCount; i++ {
				sx := float64(s[0]) + (h.rng.Float64()-0.5)*8 + 0.5
				sz := float64(s[2]) + (h.rng.Float64()-0.5)*8 + 0.5
				sy := float64(s[1]) + float64(h.rng.Intn(3)-1)
				if nw.At(floorInt(sx), floorInt(sy), floorInt(sz)) != worldgen.Air {
					continue
				}
				m := h.spawnMobIn(players, entityBlaze, dimNether, sx, sy, sz)
				h.configureNetherMob(players, m)
			}
			h.playSoundDim(players, dimNether, "minecraft:block.fire.ambient", sndHostile,
				float64(s[0])+0.5, float64(s[1])+0.5, float64(s[2])+0.5, 0.6, 0.8)
		}
	}
}
