package server

import (
	"encoding/binary"
	"log"
	"math"
)

// Raids — a faithful core of the vanilla 1.21.5 Raid.java. Killing a
// pillager-patrol captain grants Bad Omen; carrying it into a village consumes
// the omen and triggers a raid: waves of illagers spawn around the village and
// attack. Clear every wave to win. (Hero of the Village, the warning bell, and
// save/restore persistence are follow-ups.)

const (
	raidSpawnRadius     = 24   // raiders spawn within this of the village centre
	raidBarRange        = 64   // players within this see the raid boss bar
	raidNoPlayerTimeout = 2400 // ticks with nobody near before a raid gives up (2 min)
	badOmenSecs         = 6000 // Bad Omen lasts long enough to walk to a village
)

// raiderWaves[etype][wave 1..7] = how many of that raider spawn in that wave
// (Raid.RaiderType.spawnsPerWaveBeforeBonus, vanilla 1.21.5; bonus spawns on
// higher difficulty are a follow-up).
var raiderWaves = map[int][8]int{
	entityPillager:   {0, 4, 3, 3, 4, 4, 4, 2},
	entityVindicator: {0, 0, 2, 0, 1, 4, 2, 5},
	entityEvoker:     {0, 0, 0, 0, 0, 1, 1, 2},
	entityWitch:      {0, 0, 0, 0, 3, 0, 0, 1},
	entityRavager:    {0, 0, 0, 1, 0, 1, 0, 2},
}

type raid struct {
	center      blockPos
	uuid        [16]byte
	wave        int            // waves spawned so far
	numGroups   int            // total waves (by difficulty)
	waveSpawned int            // raiders spawned in the current wave (for the bar)
	alive       map[int32]bool // raider eids currently spawned
	shown       map[int32]bool // player eids currently shown the bar
	idleTicks   int            // ticks with no player near
}

// raidWaveCount is the wave total by difficulty (Raid.getNumGroups).
func raidWaveCount(diff int) int {
	switch diff {
	case diffEasy:
		return 3
	case diffHard:
		return 7
	default:
		return 5
	}
}

// startRaid begins a raid at a village centre (no-op if one is already active).
func (h *hub) startRaid(players map[int32]*tracked, center blockPos) {
	if h.raids[center] != nil {
		return
	}
	r := &raid{center: center, numGroups: raidWaveCount(h.rules.Difficulty),
		alive: map[int32]bool{}, shown: map[int32]bool{}}
	binary.BigEndian.PutUint32(r.uuid[8:], 0x52414944) // "RAID"
	binary.BigEndian.PutUint32(r.uuid[12:], uint32(center.x*31+center.z))
	h.raids[center] = r
	h.spawnWave(players, r)
	h.broadcastChat(players, "A raid has begun!")
	log.Printf("raid at (%d,%d): %d waves", center.x, center.z, r.numGroups)
}

// spawnWave spawns the next wave's raiders around the village.
func (h *hub) spawnWave(players map[int32]*tracked, r *raid) {
	r.wave++
	r.waveSpawned = 0
	if r.wave > 7 {
		return
	}
	for etype, counts := range raiderWaves {
		ravagerN := 0 // successful ravagers this wave (first one gets the evoker rider)
		for i := 0; i < counts[r.wave]; i++ {
			ang := h.rng.Float64() * 2 * math.Pi
			d := 8 + h.rng.Float64()*raidSpawnRadius
			x := r.center.x + int(math.Cos(ang)*d)
			z := r.center.z + int(math.Sin(ang)*d)
			if !h.world.Spawnable(x, z) {
				continue
			}
			m := h.spawnHostileY(players, etype,
				float64(x)+0.5, float64(h.world.SurfaceFeet(x, z)), float64(z)+0.5)
			if m == nil {
				continue
			}
			m.raidCenter = r.center
			r.alive[m.eid] = true
			r.waveSpawned++
			// Vanilla Raid.spawnGroup mounts a rider on each ravager on the
			// higher waves: a pillager on wave 5, an evoker (first ravager) or
			// vindicator (the rest) on wave 7+.
			if etype == entityRavager {
				if rt := raidRiderType(r.wave, ravagerN); rt != 0 {
					if rd := h.spawnHostileY(players, rt, m.x, m.y, m.z); rd != nil {
						rd.raidCenter = r.center
						rd.mount, m.mobRider = m.eid, rd.eid
						r.alive[rd.eid] = true
						r.waveSpawned++
						h.toNearbyEv(players, m.dim, m.x, m.z, passengersBody(m.eid, rd.eid))
					}
				}
				ravagerN++
			}
		}
	}
}

// raidRiderType picks the raider that mounts a ravager on a given wave, matching
// vanilla Raid.spawnGroup (getNumGroups NORMAL=5, HARD=7): a pillager on wave 5,
// and on wave 7+ an evoker on the first ravager, a vindicator on the rest.
// 0 = no rider.
func raidRiderType(wave, ravagerIdx int) int {
	switch {
	case wave == 5:
		return entityPillager
	case wave >= 7:
		if ravagerIdx == 0 {
			return entityEvoker
		}
		return entityVindicator
	}
	return 0
}

// updateRaids (1 Hz) refreshes the bar, spawns the next wave when the current is
// cleared, wins when all waves are down, and times out if nobody's around.
func (h *hub) updateRaids(players map[int32]*tracked) {
	for center, r := range h.raids {
		aliveN := 0
		for eid := range r.alive {
			if h.mobs[eid] == nil {
				delete(r.alive, eid)
			} else {
				aliveN++
			}
		}
		h.showRaidBar(players, r, aliveN)
		if !h.anyPlayerNear(players, center, raidBarRange) {
			if r.idleTicks++; r.idleTicks > raidNoPlayerTimeout {
				h.endRaid(players, r)
				delete(h.raids, center)
			}
			continue
		}
		r.idleTicks = 0
		if aliveN == 0 { // wave cleared
			if r.wave >= r.numGroups {
				h.broadcastChat(players, "Victory! The raid has been defeated.")
				// vanilla: everyone who helped (in the raid area) earns Hero of
				// the Village — 48000 ticks (40 min), which discounts villager
				// trades via updateSpecialPrices.
				for _, t := range players {
					if t.dim == 0 && dist3(t.x, t.y, t.z, float64(center.x), float64(center.y), float64(center.z)) <= raidBarRange {
						h.applyEffect(players, t, effHeroOfVillage, 0, 2400)
					}
				}
				h.endRaid(players, r)
				delete(h.raids, center)
			} else {
				h.spawnWave(players, r)
			}
		}
	}
}

// showRaidBar sends/updates/removes the purple raid bar for nearby players.
func (h *hub) showRaidBar(players map[int32]*tracked, r *raid, aliveN int) {
	frac := float32(0)
	if r.waveSpawned > 0 {
		frac = float32(aliveN) / float32(r.waveSpawned)
	}
	title := "Raid"
	for _, t := range players {
		near := t.dim == 0 && dist3(t.x, t.y, t.z, float64(r.center.x), float64(r.center.y), float64(r.center.z)) <= raidBarRange
		if near {
			if !r.shown[t.p.eid] {
				r.shown[t.p.eid] = true
				t.p.trySendEv(bossBarAdd(r.uuid, title, frac))
			} else {
				t.p.trySendEv(bossBarHealth(r.uuid, frac))
			}
		} else if r.shown[t.p.eid] {
			delete(r.shown, t.p.eid)
			t.p.trySendEv(bossBarRemove(r.uuid))
		}
	}
}

// endRaid clears the raid bar from everyone who saw it.
func (h *hub) endRaid(players map[int32]*tracked, r *raid) {
	for _, t := range players {
		if r.shown[t.p.eid] {
			t.p.trySendEv(bossBarRemove(r.uuid))
		}
	}
}

// broadcastChat sends a system message to every connected player.
func (h *hub) broadcastChat(players map[int32]*tracked, text string) {
	for _, t := range players {
		t.p.trySendEv(chatEv(text))
	}
}

// anyPlayerNear reports whether a live overworld player is within r of a point.
func (h *hub) anyPlayerNear(players map[int32]*tracked, pos blockPos, r float64) bool {
	for _, t := range players {
		if t.dim == 0 && !t.dead &&
			dist3(t.x, t.y, t.z, float64(pos.x), float64(pos.y), float64(pos.z)) <= r {
			return true
		}
	}
	return false
}

// villageNear returns a village centre within r of (x,z), if any.
func (h *hub) villageNear(x, z, r int) (blockPos, bool) {
	gen := h.world.Gen()
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			v := gen.VillageIn(x+dx*384, z+dz*384)
			if v.Exists && abs(v.X-x) < r && abs(v.Z-z) < r {
				return blockPos{v.X, v.Y, v.Z}, true
			}
		}
	}
	return blockPos{}, false
}

// checkRaidTrigger consumes a player's Bad Omen when they reach a village and
// starts a raid there (Raid trigger).
func (h *hub) checkRaidTrigger(players map[int32]*tracked, t *tracked) {
	if t.hasEffect(effBadOmen) == 0 {
		return
	}
	center, ok := h.villageNear(int(t.x), int(t.z), 64)
	if !ok {
		return
	}
	h.removeEffect(t, effBadOmen)
	h.startRaid(players, center)
}
