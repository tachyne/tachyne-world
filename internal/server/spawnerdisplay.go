package server

import attachproto "github.com/tachyne/tachyne-common/attach"

// What a spawner LOOKS like. A spawner block on its own draws an empty cage:
// the little mob turning inside it comes from the block entity's SpawnData,
// and until now nothing ever told a client what a spawner spawns. Every
// dungeon and every fortress throne was a bare box.
//
// The data is pushed rather than carried with the chunk, because the engine
// finds its spawners from worldgen (a pure function of the seed) rather than
// from block entities loaded with it — the same reason the dungeon and
// fortress spawners tick from a per-player lookup. The client creates the
// block entity itself from the block state, so a pushed update always lands
// on something; what it is missing is only the contents.
//
// It is re-sent on the spawner cadence while a player is in range rather than
// tracked per player: the packet is a few dozen bytes, a world has a handful
// of spawners near anyone at a time, and re-sending is what makes it survive
// a chunk reload without any bookkeeping.

// showSpawner tells everyone near a spawner what it spawns.
func (h *hub) showSpawner(players map[int32]*tracked, dim int, pos blockPos, etype int) {
	name := spawnerEntityName(etype)
	if name == "" {
		return
	}
	ev := attachproto.SpawnerData{
		X: pos.x, Y: pos.y, Z: pos.z, Entity: name,
		MinDelay: spawnerMinDelay, MaxDelay: spawnerMinDelay + spawnerDelaySpan,
		SpawnCount: spawnerCount, MaxNearbyEntities: spawnerMobCap,
		PlayerRange: spawnerRange, SpawnRange: spawnerSpawnRange,
	}
	for _, t := range players {
		if t.dim != dim {
			continue
		}
		if dist3(t.x, t.y, t.z, float64(pos.x), float64(pos.y), float64(pos.z)) > spawnerShowRange {
			continue
		}
		t.p.trySendEv(ev)
	}
}

const (
	spawnerSpawnRange = 4  // BaseSpawner.spawnRange
	spawnerShowRange  = 48 // far enough that it is already drawn when you see it
)

// spawnerEntityName is the QUALIFIED name SpawnData asks for
// ("minecraft:blaze"); the rest of the engine speaks the bare one.
func spawnerEntityName(etype int) string {
	if n := entityRegistryName(etype); n != "" {
		return "minecraft:" + n
	}
	return ""
}
