package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// The overworld structures' spawners that worldgen stamps as plain spawner
// blocks: the stronghold portal room's silverfish spawner (PortalRoom sets
// it to SILVERFISH) and the cave spider spawner in a mineshaft nest corridor
// (MineShaftCorridor sets it to CAVE_SPIDER). Like the dungeon and fortress
// spawners they are pure functions of the seed, so the hub asks the
// generator where they are instead of scanning blocks, and a spawner that
// has been mined out stops.

type structSpawner struct {
	pos   blockPos
	etype int
}

// structureSpawnersNear lists the stronghold and mineshaft spawners whose
// structures reach the column (x,z).
func (h *hub) structureSpawnersNear(x, z int) []structSpawner {
	gen := h.world.Gen()
	var out []structSpawner
	for _, st := range gen.StrongholdsNear(x, z) {
		if p, ok := st.Spawner(); ok {
			out = append(out, structSpawner{blockPos{p[0], p[1], p[2]}, entitySilverfish})
		}
	}
	for _, m := range gen.MineshaftsNear(x, z) {
		for _, p := range gen.MineshaftSpawners(m) {
			out = append(out, structSpawner{blockPos{p[0], p[1], p[2]}, entityCaveSpider})
		}
	}
	return out
}

// updateStructureSpawners is updateSpawners for those spawners: the same
// cadence, cap and four attempts a cycle, within BaseSpawner's spawn range
// of the cage, while the spawner block still stands.
func (h *hub) updateStructureSpawners(players map[int32]*tracked) {
	if !h.rules.DoMobSpawning || h.rules.Difficulty == diffPeaceful || !h.rules.SpawnerBlocks {
		return
	}
	now := h.tick.Load()
	done := map[blockPos]bool{}
	spawnerState := worldgen.BlockBase("spawner")
	for _, t := range players {
		if t.dim != dimOverworld {
			continue
		}
		for _, s := range h.structureSpawnersNear(int(t.x), int(t.z)) {
			pos := s.pos
			if done[pos] {
				continue
			}
			done[pos] = true
			if !h.ownedBlock(pos.x, pos.z) {
				continue
			}
			if dist3(t.x, t.y, t.z, float64(pos.x), float64(pos.y), float64(pos.z)) > spawnerRange {
				continue
			}
			if h.world.At(pos.x, pos.y, pos.z) != spawnerState {
				continue // mined out
			}
			etype := h.spawnerMobFor(dimOverworld, pos.x, pos.y, pos.z, s.etype)
			h.showSpawner(players, dimOverworld, pos, etype)
			key := simPos{blockPos: pos}
			if next, ok := h.spawnerNext[key]; ok && now < next {
				continue
			}
			h.spawnerNext[key] = now + spawnerMinDelay + uint64(h.rng.Intn(spawnerDelaySpan))
			h.spawnerCycle(players, pos, etype)
		}
	}
}
