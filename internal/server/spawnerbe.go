package server

import (
	"strconv"
	"strings"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Spawner block entities that no seed accounts for. The dungeon, fortress,
// stronghold and mineshaft spawners are found again from the seed and keep
// their cooldowns in memory; a spawner that was placed, /setblock'd, cloned or
// given a spawn egg carries its own SpawnData (rules.SpawnerMobs, "dim,x,y,z"
// → entity name) and its own spawnDelay (spawnerDelays, saved with the
// containers), and ticks here as BaseSpawner.serverTick does. A seed spawner
// that has been given an egg becomes one of these, so no cage ticks twice.
//
// A spawner with no entity is vanilla's empty cage: it never spawns, so it
// needs no record at all.

const spawnerFreshDelay = 20 // BaseSpawner's spawnDelay before its first round

// placedSpawner reports whether the cage at pos is one of these.
func (h *hub) placedSpawner(dim int, pos blockPos) bool {
	_, ok := h.rules.SpawnerMobs[spawnerKey(dim, pos.x, pos.y, pos.z)]
	return ok
}

// parseSpawnerKey reads a spawnerKey back.
func parseSpawnerKey(k string) (simPos, bool) {
	parts := strings.Split(k, ",")
	if len(parts) != 4 {
		return simPos{}, false
	}
	var n [4]int
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil {
			return simPos{}, false
		}
		n[i] = v
	}
	return simPos{dim: n[0], blockPos: blockPos{n[1], n[2], n[3]}}, true
}

// setSpawnerEntity is BaseSpawner.setEntityId: the cage spawns name from now
// on, its delay untouched.
func (h *hub) setSpawnerEntity(pos simPos, name string) {
	if h.rules.SpawnerMobs == nil {
		h.rules.SpawnerMobs = map[string]string{}
	}
	h.rules.SpawnerMobs[spawnerKey(pos.dim, pos.x, pos.y, pos.z)] = name
	h.saveRules()
}

// dropSpawnerBE forgets a cage's block entity: the block is gone or replaced.
func (h *hub) dropSpawnerBE(pos simPos) {
	delete(h.spawnerDelays, pos)
	k := spawnerKey(pos.dim, pos.x, pos.y, pos.z)
	if _, ok := h.rules.SpawnerMobs[k]; ok {
		delete(h.rules.SpawnerMobs, k)
		h.saveRules()
	}
}

// spawnerEntityAt is the entity a spawner at pos spawns: its own SpawnData,
// else the seed's for a dungeon, fortress, stronghold or mineshaft cage.
// "" = an empty cage.
func (h *hub) spawnerEntityAt(pos simPos) string {
	if name, ok := h.rules.SpawnerMobs[spawnerKey(pos.dim, pos.x, pos.y, pos.z)]; ok {
		return name
	}
	w := h.worldFor(pos.dim)
	if w == nil {
		return ""
	}
	gen := w.Gen()
	switch pos.dim {
	case dimOverworld:
		if d := gen.DungeonIn(pos.x, pos.z); d.Exists && d.X == pos.x && d.Y == pos.y && d.Z == pos.z {
			return entityRegistryName(dungeonMobs[d.Mob%3])
		}
		for _, s := range h.structureSpawnersNear(pos.x, pos.z) {
			if s.pos == pos.blockPos {
				return entityRegistryName(s.etype)
			}
		}
	case dimNether:
		for _, f := range gen.FortressesNear(pos.x, pos.z) {
			for _, s := range gen.FortressSpawners(f) {
				if s[0] == pos.x && s[1] == pos.y && s[2] == pos.z {
					return entityRegistryName(entityBlaze)
				}
			}
		}
	}
	return ""
}

// updatePlacedSpawners is BaseSpawner.serverTick for these cages, on the
// once-a-second cadence: a cage in a loaded chunk with a living, non-spectator
// player within requiredPlayerRange counts its delay down, and at zero runs a
// round (spawnerCycle); a round that spawned or hit the cap re-arms it at
// 200..799 ticks, one that did neither tries again next time.
func (h *hub) updatePlacedSpawners(players map[int32]*tracked) {
	if !h.rules.SpawnerBlocks || len(h.rules.SpawnerMobs) == 0 {
		return
	}
	spawnerState := worldgen.BlockBase("spawner")
	var gone []simPos
	for k, name := range h.rules.SpawnerMobs {
		pos, ok := parseSpawnerKey(k)
		if !ok {
			continue
		}
		w := h.worldFor(pos.dim)
		if w == nil || !h.ownedBlock(pos.x, pos.z) || !h.cellWithinBorder(pos.dim, pos.x, pos.z) ||
			!w.Loaded(int32(chunkFloor(float64(pos.x))), int32(chunkFloor(float64(pos.z)))) {
			continue // LevelChunk.isTicking: only loaded, owned chunks tick their block entities
		}
		if w.At(pos.x, pos.y, pos.z) != spawnerState {
			gone = append(gone, pos) // broken or replaced by something the removal hooks missed
			continue
		}
		etype, ok := entityByName[strings.TrimPrefix(name, "minecraft:")]
		if !ok {
			continue
		}
		if !h.spawnerPlayerNear(players, pos) {
			continue
		}
		h.showSpawner(players, pos.dim, pos.blockPos, etype)
		delay, ok := h.spawnerDelays[pos]
		if !ok {
			delay = spawnerFreshDelay
		}
		if delay > 0 {
			h.spawnerDelays[pos] = max(delay-survivalTickN, 0)
			continue
		}
		if h.spawnerCycle(players, pos.dim, pos.blockPos, etype) {
			h.spawnerDelays[pos] = h.spawnerDelayRoll()
		} else {
			h.spawnerDelays[pos] = 0
		}
	}
	for _, pos := range gone {
		h.dropSpawnerBE(pos)
	}
}

// spawnerPlayerNear is BaseSpawner.isNearPlayer: a living player who is not a
// spectator within requiredPlayerRange of the cage's centre.
func (h *hub) spawnerPlayerNear(players map[int32]*tracked, pos simPos) bool {
	cx, cy, cz := float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5
	for _, t := range players {
		if t.dim == pos.dim && !t.dead && t.gamemode != gmSpectator && dist3(t.x, t.y, t.z, cx, cy, cz) <= spawnerRange {
			return true
		}
	}
	return false
}

// spawnerRulesOK is SpawnPlacements.checkSpawnRules with the SPAWNER reason,
// at the cell the mob would stand in. A spawner skips the floor test
// (checkMobSpawnRules), the sky test of the surface monsters and the
// drowned's water, but a monster still needs the dark (isDarkEnoughToSpawn
// is waived only for trial spawners) and an animal still needs its grass and
// light. Monsters never come out on Peaceful.
func (h *hub) spawnerRulesOK(dim, etype, x, y, z int) bool {
	peaceful := h.rules.Difficulty == diffPeaceful
	w := h.worldFor(dim)
	switch etype {
	case entityIronGolem, entitySnowGolem, entityVillager, entityWanderingTrader, entityPhantom, entityShulker, entityEnderDragon:
		return true // Mob.checkMobSpawnRules: nothing but the floor, which a spawner skips
	case entitySlime, entityBlaze, entityBreeze, entityZoglin, entitySilverfish, entityEndermite:
		return !peaceful // any light (a spawner skips the silverfish's player check)
	case entityGuardian, entityElderGuardian:
		sky, _ := w.LightAt(x, y, z)
		return (h.rng.Intn(20) == 0 || sky < 15) && !peaceful && worldgen.IsWater(w.At(x, y-1, z))
	case entityGhast, entityMagmaCube, entitySulfurCube, entityZombifiedPiglin, entityPiglin, entityHoglin, entityStrider:
		return h.spawnRulesOK(dim, catMonster, etype, x, y, z, 0, 0) // their own predicates, light-blind
	}
	sky, block := w.LightAt(x, y, z)
	cat := spawnerCategory(etype)
	if cat == catMonster { // checkMonsterSpawnRules
		return !peaceful && h.spawnerDarkEnough(dim, sky, block)
	}
	return h.spawnRulesOK(dim, cat, etype, x, y, z, sky, block)
}

// spawnerCategory is a species' MobCategory, as the biome spawn data lists
// it (a creature listed anywhere counts as one: the ocelot sits in the
// jungle's monster pool); a species in no biome pool is a creature if the
// roster calls it passive, else a monster (the cave spider, the wither
// skeleton).
func spawnerCategory(etype int) int {
	if c, ok := spawnPoolCategory[etype]; ok {
		return c
	}
	if isRosterPassive(etype) {
		return mobSpawnCategory(&mob{etype: etype})
	}
	return catMonster
}

var spawnPoolCategory = func() map[int]int {
	out := map[int]int{}
	for _, def := range biomeSpawnDefs {
		for _, r := range def.rows {
			et, ok := entityByName[r.etype]
			cat, known := spawnCatByName[r.cat]
			if !ok || !known {
				continue
			}
			if prev, seen := out[et]; !seen || prev == catMonster {
				out[et] = cat
			}
		}
	}
	return out
}()

// spawnerDarkEnough is Monster.isDarkEnoughToSpawn in the spawner's own
// dimension: the Nether has no block-light limit and a fixed light test of 7.
func (h *hub) spawnerDarkEnough(dim int, sky, block uint8) bool {
	if dim == dimNether {
		return h.rawBrightness(sky, block, 0) <= 7
	}
	return h.darkEnoughToSpawn(sky, block)
}

// recordSpawnerDelays snapshots the placed cages' delays (BaseSpawner's
// saved "Delay").
func (s *containerStore) recordSpawnerDelays(delays map[simPos]int) {
	snap := make(map[string]int, len(delays))
	for pos, d := range delays {
		snap[simKey(pos)] = d + 1 // +1: a spent delay (0) is still a record
	}
	s.mu.Lock()
	s.m.SpawnerDelays = snap
	s.mu.Unlock()
}

// loadSpawnerDelays restores them.
func (s *containerStore) loadSpawnerDelays() map[simPos]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[simPos]int{}
	for k, d := range s.m.SpawnerDelays {
		if pos, ok := parseSimKey(k); ok && d > 0 {
			out[pos] = d - 1
		}
	}
	return out
}

// loadSpawnerNBT is BaseSpawner.load for the parts the engine keeps:
// SpawnData's entity id (or the first of SpawnPotentials') and Delay. A cage
// given no entity stays empty.
func (h *hub) loadSpawnerNBT(pos simPos, nbt map[string]any) {
	sd, _ := nbt["SpawnData"].(map[string]any)
	if sd == nil {
		if pots, ok := nbt["SpawnPotentials"].([]any); ok && len(pots) > 0 {
			if p, ok := pots[0].(map[string]any); ok {
				sd, _ = p["data"].(map[string]any)
			}
		}
	}
	ent, _ := sd["entity"].(map[string]any)
	id, _ := ent["id"].(string)
	et, ok := entityByName[strings.TrimPrefix(id, "minecraft:")]
	if !ok {
		return
	}
	h.setSpawnerEntity(pos, entityRegistryName(et))
	if d, ok := snbtInt(nbt["Delay"]); ok && d >= 0 {
		h.spawnerDelays[pos] = int(d)
	}
}
