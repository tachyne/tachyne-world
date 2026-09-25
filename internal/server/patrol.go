package server

import (
	"log"
	"math"
)

// Pillager patrols — reimplemented from the vanilla 1.21.5 PatrolSpawner.
// From day 5 on, during clear daylight, roaming illager squads led by a
// banner-bearing captain spawn out in the world (away from villages) and menace
// whoever they find. The captain drops the ominous banner; the ominous bottle → raids is
// a follow-up.

const (
	patrolInterval = 12000 // base ticks between attempts (~10 min at 20 TPS)
	patrolJitter   = 1200  // + up to this many ticks
	patrolMinDay   = 5     // no patrols before day 5 (vanilla)
	patrolMinDist  = 24    // spawn patrolMinDist..2× blocks from the chosen player
)

// updatePatrols runs on the 1 Hz survival step; it self-throttles via
// patrolNextAt so the effective cadence is ~10 min.
func (h *hub) updatePatrols(players map[int32]*tracked) {
	if !h.rules.SpawnPatrols {
		return // gamerule spawn_patrols
	}
	age := h.tick.Load()
	if h.patrolNextAt == 0 { // first arming after boot
		h.patrolNextAt = age + patrolInterval
		return
	}
	if age < h.patrolNextAt {
		return
	}
	h.patrolNextAt = age + patrolInterval + uint64(h.rng.Intn(patrolJitter))
	if !h.rules.DoMobSpawning {
		return
	}
	// Gate: day 5+, bright daytime, not raining (PatrolSpawner), 20% per attempt.
	dt := h.dayTime.Load()
	if dt/dayLengthTicks < patrolMinDay || dt%dayLengthTicks >= nightStart || h.raining {
		return
	}
	if h.rng.Intn(5) != 0 {
		return
	}
	// A random living overworld player, not near a village.
	var cand []*tracked
	for _, t := range players {
		if t.dim == 0 && !t.dead {
			cand = append(cand, t)
		}
	}
	if len(cand) == 0 {
		return
	}
	t := cand[h.rng.Intn(len(cand))]
	px, pz := int(t.x), int(t.z)
	// PatrolSpawner.tick: a spectator ends the attempt, and so does a player
	// within two sections of a village's points of interest.
	if t.gamemode == gmSpectator || h.closeToVillage(blockPos{floorInt(t.x), floorInt(t.y), floorInt(t.z)}, 2) {
		return
	}
	sx := px + (patrolMinDist+h.rng.Intn(patrolMinDist))*h.randSign()
	sz := pz + (patrolMinDist+h.rng.Intn(patrolMinDist))*h.randSign()
	if !h.world.Spawnable(sx, sz) {
		return
	}
	// can_pillager_patrol_spawn: false in the mushroom fields.
	if h.world.Gen().BiomeName(sx, sz) == "minecraft:mushroom_fields" {
		return
	}
	h.spawnPatrol(players, sx, sz)
}

// spawnPatrol drops a captain + squad of pillagers scattered around (sx,sz).
// Size scales with difficulty (vanilla ceil(effectiveDifficulty)+1).
func (h *hub) spawnPatrol(players map[int32]*tracked, sx, sz int) {
	// PatrolSpawner.tick: ceil(effective difficulty) + 1 members. The first
	// stands at the chosen spot and is the leader — if it cannot, there is no
	// patrol at all — and each next one steps on from where the last one
	// was, so the squad strings out rather than scattering.
	n := int(math.Ceil(float64(h.effectiveDifficulty()))) + 1
	spawned := 0
	var captain *mob
	x, z := sx, sz
	for i := 0; i < n; i++ {
		var m *mob
		if h.world.Spawnable(x, z) {
			m = h.spawnHostileYIn(players, entityPillager, dimOverworld,
				float64(x)+0.5, float64(h.world.SurfaceFeet(x, z)), float64(z)+0.5)
		}
		if m == nil && i == 0 {
			break
		}
		if m != nil {
			if i == 0 {
				h.makeCaptain(players, m)
				captain = m
			}
			spawned++
		}
		x += h.rng.Intn(5) - h.rng.Intn(5)
		z += h.rng.Intn(5) - h.rng.Intn(5)
	}
	if captain != nil {
		// PatrolSpawner gives only the leader a target; the rest start
		// patrolling when it hands them their first waypoint. Done after the
		// squad is placed so picking the target cannot shift the random draws
		// the members' own positions come from.
		h.findPatrolTarget(captain)
	}
	if spawned > 0 {
		log.Printf("pillager patrol spawned near (%d,%d): %d members", sx, sz, spawned)
	}
}

const (
	outpostScan  = 400 // matches worldgen outpostCell — samples the 3×3 cells around a player
	outpostRange = 80  // garrison the tower when a player gets this close
)

// updateOutposts garrisons a pillager outpost the first time a player comes near
// it this session: a banner-bearing captain plus a few pillagers around the
// tower base. (Vanilla keeps pillagers spawning around an outpost via a
// StructureSpawnOverride; a one-time garrison is the v1 — continuous respawn is
// a follow-up.) The captain drops the ominous banner and an ominous bottle on
// death via the shared patrolCaptain path (combat.go).
func (h *hub) updateOutposts(players map[int32]*tracked) {
	gen := h.world.Gen()
	for _, t := range players {
		if t.dim != 0 || t.dead {
			continue
		}
		px, pz := int(t.x), int(t.z)
		for dx := -1; dx <= 1; dx++ {
			for dz := -1; dz <= 1; dz++ {
				p := gen.OutpostIn(px+dx*outpostScan, pz+dz*outpostScan)
				if !p.Exists {
					continue
				}
				base := blockPos{p.X, p.Y, p.Z}
				if h.outpostDone[base] {
					continue
				}
				if math.Hypot(t.x-float64(p.X), t.z-float64(p.Z)) > outpostRange {
					continue
				}
				h.outpostDone[base] = true
				n := 3 + h.rng.Intn(2) // 3–4 illagers
				for i := 0; i < n; i++ {
					x := p.X + h.rng.Intn(9) - 4
					z := p.Z + h.rng.Intn(9) - 4
					if !h.world.Spawnable(x, z) {
						continue
					}
					m := h.spawnHostileYIn(players, entityPillager, dimOverworld,
						float64(x)+0.5, float64(h.world.SurfaceFeet(x, z)), float64(z)+0.5)
					if m == nil {
						continue
					}
					if i == 0 { // the captain
						h.makeCaptain(players, m)
					}
				}
				log.Printf("pillager outpost garrisoned at (%d,%d)", p.X, p.Z)
			}
		}
	}
}

// randSign returns -1 or +1.
func (h *hub) randSign() int {
	if h.rng.Intn(2) == 0 {
		return -1
	}
	return 1
}
