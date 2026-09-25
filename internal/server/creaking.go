package server

import (
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The creaking and the heart that owns it. Ported from Creaking, CreakingAi,
// CreakingHeartBlock and CreakingHeartBlockEntity.
//
// The creaking is the one mob whose life is a property of a BLOCK: the heart
// spawns it after dark, it cannot be killed while the heart stands (blows land
// on the heart instead), it dies the instant the heart is broken, and it
// freezes solid whenever a player is looking at it. None of that is mob state —
// all of it hangs off the link, so the link is what this file is about.

const (
	creakingHeartRange   = 32   // a player must be this near for a heart to spawn one
	creakingRoamRadius   = 32   // how far it will wander from its heart
	creakingTooFar       = 34.0 // …and the distance at which the heart lets it go
	creakingSpawnXZ      = 16   // spawn attempt range around the heart
	creakingSpawnY       = 8
	creakingSpawnTries   = 5
	creakingHeartUpdate  = 20 // heart re-checks itself every 20 (+0-4) ticks
	creakingActivateR2   = 144.0
	creakingFreezeCone   = 0.5 // dot tolerance for "is that player looking at me"
	creakingInvulnTicks  = 8   // flinch animation when a blow is redirected to the heart
	creakingResinClumps  = 3   // resin spread per hurt call, 2..3
	creakingHurtCalls    = 10  // …on a budget of ten calls
	creakingHurtInterval = 10
)

// resinClump is what a hurt creaking's heart bleeds onto the tree: a clump
// with no face set yet, which the caller then turns on for the face it lands
// against.
var resinClump = multifaceBase("resin_clump")

// heartLink is one creaking heart and the creaking it currently owns.
type heartLink struct {
	pos      blockPos
	dim      int    // the world the heart stands in
	creaking int32  // eid of its protector (0 = none out)
	nextAt   uint64 // tick of the heart's next self-check
	hurtLeft int    // remaining resin-spread calls in this bout
}

// isCreakingHeart reports whether a state is a creaking heart, and whether it
// is awake.
func creakingHeartState(state uint32) (awake, ok bool) {
	base := worldgen.CreakingHeartBase
	if state < base || state > base+17 {
		return false, false
	}
	// axis*6 + state*2 + natural; state 2 is awake.
	return (state-base)%6/2 == 2, true
}

func isCreakingHeartBlock(s uint32) bool { _, ok := creakingHeartState(s); return ok }

// Creaking heart states, in the order the block lists them.
const (
	heartUprooted = 0
	heartDormant  = 1
	heartAwake    = 2
)

// heartWith is the same heart (axis and natural kept) in another state: a
// heart a player laid on its side stays on its side when it wakes.
func heartWith(state uint32, st int) uint32 {
	off := state - worldgen.CreakingHeartBase
	return worldgen.CreakingHeartBase + off/6*6 + uint32(st)*2 + off%2
}

// heartAxisDelta is the unit step along a heart's own axis (x, y or z).
func heartAxisDelta(state uint32) (int, int, int) {
	switch (state - worldgen.CreakingHeartBase) / 6 {
	case 0:
		return 1, 0, 0
	case 2:
		return 0, 0, 1
	}
	return 0, 1, 0
}

// heartIndexOnBlockChange registers a creaking heart wherever one is placed,
// in whichever dimension, so a heart a player builds ticks like a grown one.
// A heart that is removed drops out on its next update.
func (h *hub) heartIndexOnBlockChange(dim, x, y, z int, state uint32) {
	if _, ok := creakingHeartState(state); !ok {
		return
	}
	if h.hearts == nil {
		h.hearts = map[simPos]*heartLink{}
	}
	p := simPos{dim: dim, blockPos: blockPos{x, y, z}}
	if _, known := h.hearts[p]; !known {
		h.hearts[p] = &heartLink{pos: p.blockPos, dim: dim}
	}
}

// heartStanding reports whether a heart at pos still has the pale oak logs it
// needs — vanilla hasRequiredLogs, which checks BOTH neighbours along the
// heart's own axis. Every heart the generator plants is upright, so for those
// the axis is Y: cut the trunk above or below it and the heart uproots.
func (h *hub) heartStanding(dim int, pos blockPos) bool {
	w := h.worldFor(dim)
	if w == nil {
		return false
	}
	dx, dy, dz := heartAxisDelta(w.At(pos.x, pos.y, pos.z))
	above := w.At(pos.x+dx, pos.y+dy, pos.z+dz)
	below := w.At(pos.x-dx, pos.y-dy, pos.z-dz)
	return isPaleOakLog(above) && isPaleOakLog(below)
}

// isPaleOakLog covers the log's six orientation states.
func isPaleOakLog(state uint32) bool {
	base := worldgen.BlockBase("pale_oak_log")
	return state >= base && state <= base+2
}

// registerHeartChunks discovers worldgen-placed creaking hearts near players.
// Generated terrain never passes through the block-change index, so — exactly
// as with sculk — a scan is the only way a naturally grown heart ever starts
// ticking.
func (h *hub) registerHeartChunks(players map[int32]*tracked) {
	if h.heartScanned == nil {
		h.heartScanned = map[[2]int32]bool{}
	}
	if h.hearts == nil {
		h.hearts = map[simPos]*heartLink{}
	}
	scanned := 0
	for _, t := range players {
		if t.dim != dimOverworld {
			continue // pale gardens grow only in the overworld; built hearts register as placed
		}
		cx, cz := int32(chunkFloor(t.x)), int32(chunkFloor(t.z))
		for dx := int32(-2); dx <= 2 && scanned < 4; dx++ {
			for dz := int32(-2); dz <= 2 && scanned < 4; dz++ {
				key := [2]int32{cx + dx, cz + dz}
				if h.heartScanned[key] {
					continue
				}
				h.heartScanned[key] = true
				scanned++
				h.scanChunkHearts(key[0], key[1])
			}
		}
	}
}

// scanChunkHearts walks a chunk's surface band for creaking hearts. They only
// ever grow inside a tree, so the band above sea level is the whole search.
func (h *hub) scanChunkHearts(cx, cz int32) {
	bx, bz := int(cx)*16, int(cz)*16
	for lx := 0; lx < 16; lx++ {
		for lz := 0; lz < 16; lz++ {
			for wy := worldgen.SeaLevel; wy < worldgen.SeaLevel+96; wy++ {
				x, z := bx+lx, bz+lz
				if st := h.world.At(x, wy, z); isCreakingHeartBlock(st) {
					h.heartIndexOnBlockChange(dimOverworld, x, wy, z, st)
				}
			}
		}
	}
}

// updateHearts ticks every known heart: sleeping by day, awake by night, and
// keeping at most one creaking out at a time.
func (h *hub) updateHearts(players map[int32]*tracked) {
	if len(h.hearts) == 0 {
		return
	}
	now := h.tick.Load()
	overworldNight := h.nightNow()
	for key, link := range h.hearts {
		if now < link.nextAt {
			continue
		}
		link.nextAt = now + creakingHeartUpdate + uint64(h.rng.Intn(5))
		pos := key.blockPos
		// CREAKING_ACTIVE rides the overworld's day timeline: the Nether and
		// the End have no night, so a heart built there sleeps for good.
		night := overworldNight && key.dim == dimOverworld

		state := h.worldFor(key.dim).At(pos.x, pos.y, pos.z)
		if _, ok := creakingHeartState(state); !ok {
			h.loseCreaking(players, link) // the heart is gone: so is its creaking
			delete(h.hearts, key)
			continue
		}
		// A heart whose tree has been cut out of from under it uproots, and an
		// uprooted heart neither wakes nor spawns.
		if !h.heartStanding(key.dim, pos) {
			if up := heartWith(state, heartUprooted); state != up {
				h.setBlockAt(players, key.dim, pos, up)
			}
			h.loseCreaking(players, link)
			continue
		}
		want := heartWith(state, heartDormant)
		if night {
			want = heartWith(state, heartAwake)
		}
		if state != want {
			h.setBlockAt(players, key.dim, pos, want)
		}

		if link.creaking == 0 {
			if night && h.rules.DoMobSpawning && h.rules.Difficulty != diffPeaceful {
				h.trySpawnCreaking(players, link)
			}
			continue
		}
		m := h.mobs[link.creaking]
		switch {
		case m == nil || m.dying > 0:
			link.creaking = 0
		case !night:
			h.loseCreaking(players, link) // dawn: it comes apart
		default:
			dx, dy, dz := m.x-float64(pos.x), m.y-float64(pos.y), m.z-float64(pos.z)
			if math.Sqrt(dx*dx+dy*dy+dz*dz) > creakingTooFar {
				h.loseCreaking(players, link) // wandered out of its leash
			}
		}
	}
}

// trySpawnCreaking puts one protector out, if a player is near enough to be
// worth haunting and somewhere to stand can be found.
func (h *hub) trySpawnCreaking(players map[int32]*tracked, link *heartLink) {
	px, py, pz := float64(link.pos.x), float64(link.pos.y), float64(link.pos.z)
	if h.nearestHuntable(players, link.dim, px, pz, creakingHeartRange) == nil {
		return
	}
	for i := 0; i < creakingSpawnTries; i++ {
		x := link.pos.x + h.rng.Intn(2*creakingSpawnXZ+1) - creakingSpawnXZ
		z := link.pos.z + h.rng.Intn(2*creakingSpawnXZ+1) - creakingSpawnXZ
		y := link.pos.y + h.rng.Intn(2*creakingSpawnY+1) - creakingSpawnY
		if !h.standableAt(link.dim, x, y, z) {
			continue
		}
		m := h.spawnHostileYIn(players, entityCreaking, link.dim, float64(x)+0.5, float64(y), float64(z)+0.5)
		if m == nil {
			return // a plugin cancelled the spawn
		}
		m.home = link.pos
		m.heartBound = true
		link.creaking = m.eid
		h.playSoundDim(players, link.dim, "minecraft:entity.creaking.spawn", sndHostile, px, py, pz, 1, 1)
		h.playSoundDim(players, link.dim, "minecraft:block.creaking_heart.spawn", sndBlock, px, py, pz, 1, 1)
		return
	}
}

// standableAt reports whether a mob can stand in this cell (air with a solid
// floor and headroom) in dim.
func (h *hub) standableAt(dim, x, y, z int) bool {
	w := h.worldFor(dim)
	if w == nil {
		return false
	}
	feet, head, floor := w.At(x, y, z), w.At(x, y+1, z), w.At(x, y-1, z)
	return feet == worldgen.Air && head == worldgen.Air && worldgen.IsSolidFull(floor)
}

// loseCreaking takes the protector away — dawn, distance, or a broken heart.
// Vanilla plays a 45-tick tear-down; here it simply comes apart, since the
// animation is client-side and the entity is gone either way.
func (h *hub) loseCreaking(players map[int32]*tracked, link *heartLink) {
	if link.creaking == 0 {
		return
	}
	if m := h.mobs[link.creaking]; m != nil {
		h.playSoundDim(players, m.dim, "minecraft:entity.creaking.death", sndHostile, m.x, m.y, m.z, 1, 1)
		h.removeMob(players, m)
	}
	link.creaking = 0
}

// heartOf finds the link a creaking belongs to, if any.
func (h *hub) heartOf(m *mob) *heartLink {
	if m.etype != entityCreaking {
		return nil
	}
	if link, ok := h.hearts[simPos{dim: m.dim, blockPos: m.home}]; ok && link.creaking == m.eid {
		return link
	}
	return nil
}

// creakingInvulnerable reports whether a blow on this creaking should land on
// its heart instead. Only damage that bypasses invulnerability gets through —
// which the damage-type table already knows, so this is one tag rather than a
// list of exceptions to keep in step.
func (h *hub) creakingInvulnerable(m *mob, dt dmgType) bool {
	return h.heartOf(m) != nil && !dt.has(tagBypassesInvulnerability)
}

// creakingHurt is what a blow on an invulnerable creaking actually does: the
// heart takes the news, bleeds resin, and the creaking flinches.
func (h *hub) creakingHurt(players map[int32]*tracked, m *mob) {
	link := h.heartOf(m)
	if link == nil {
		return
	}
	m.spawnInvuln = creakingInvulnTicks
	h.toTracking(players, m.eid, m.dim, m.x, m.z, attachproto.Hurt{EID: m.eid, Yaw: m.yaw})
	h.toTracking(players, m.eid, m.dim, m.x, m.z, entityStatus(m.eid, entityStatusShake)) // the invulnerability shudder
	h.playSoundDim(players, m.dim, "minecraft:entity.creaking.sway", sndHostile, m.x, m.y, m.z, 1, 1)
	if link.hurtLeft <= 0 {
		link.hurtLeft = creakingHurtCalls
	}
	link.hurtLeft--
	h.spreadResin(players, link)
}

// spreadResin puts a clump or two of resin on the logs around the heart, which
// is how a player tracks a creaking back to what is keeping it alive.
func (h *hub) spreadResin(players map[int32]*tracked, link *heartLink) {
	clumps := 2 + h.rng.Intn(creakingResinClumps-1)
	for i := 0; i < clumps; i++ {
		x := link.pos.x + h.rng.Intn(5) - 2
		y := link.pos.y + h.rng.Intn(5) - 2
		z := link.pos.z + h.rng.Intn(5) - 2
		p := blockPos{x, y, z}
		if h.worldFor(link.dim).At(p.x, p.y, p.z) != worldgen.Air {
			continue
		}
		if !h.nextToLog(link.dim, p) {
			continue
		}
		h.setBlockAt(players, link.dim, p, resinClump)
		h.playSoundDim(players, link.dim, "minecraft:block.resin.place", sndBlock,
			float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1, 1)
		return
	}
}

// nextToLog reports whether a cell has a pale oak log beside it — resin grows
// on the tree, not in mid-air.
func (h *hub) nextToLog(dim int, p blockPos) bool {
	w := h.worldFor(dim)
	for _, d := range [6][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}} {
		if isPaleOakLog(w.At(p.x+d[0], p.y+d[1], p.z+d[2])) {
			return true
		}
	}
	return false
}

// creakingFrozen is the answer to Creaking.checkCanMove, inverted, with its
// side effects: it is what decides whether the creaking is ACTIVE.
//
// An idle creaking wakes when a player it could attack, within 12 blocks,
// looks at it: that player becomes its attack target and it goes still. An
// active one is held by any such player anywhere in its 32-block follow
// range looking at it — except one wearing a carved pumpkin, which only an
// idle creaking notices. With no attackable player in range at all it goes
// idle again and forgets its target.
//
// Vanilla tests three heights up the body against the player's view vector
// with a 0.5 tolerance; one line of sight from the eyes is the same question
// asked once, and the answer only differs at the very edge of the cone.
func (h *hub) creakingFrozen(players map[int32]*tracked, m *mob) bool {
	follow := m.followRange()
	potential := false
	for _, t := range players {
		if t.dim != m.dim || t.dead || !isSurvival(t.gamemode) {
			continue // canAttack: no creative, spectator or dead player counts
		}
		dx, dy, dz := m.x-t.x, (m.y+1)-(t.y+1.62), m.z-t.z
		d := math.Sqrt(dx*dx + dy*dy + dz*dz)
		if d > follow {
			continue // not among its NEAREST_PLAYERS
		}
		potential = true
		if m.creakActive && t.armor[0].item == itemCarvedPumpkin {
			continue // PLAYER_NOT_WEARING_DISGUISE_ITEM, asked only once it is awake
		}
		if d < 1e-6 || !creakingLookedAt(t, dx, dy, dz, d) {
			continue
		}
		if m.creakActive {
			return true
		}
		if d*d < creakingActivateR2 {
			h.creakingActivate(players, m, t)
			return true
		}
	}
	if !potential && m.creakActive {
		h.creakingDeactivate(players, m)
	}
	return false
}

// creakingLookedAt is isLookingAtMe with the 0.5 tolerance.
func creakingLookedAt(t *tracked, dx, dy, dz, d float64) bool {
	yaw := float64(t.yaw) * math.Pi / 180
	pitch := float64(t.pitch) * math.Pi / 180
	lookX := -math.Sin(yaw) * math.Cos(pitch)
	lookY := -math.Sin(pitch)
	lookZ := math.Cos(yaw) * math.Cos(pitch)
	return (lookX*dx+lookY*dy+lookZ*dz)/d > creakingFreezeCone
}

// creakingActivate is Creaking.activate: the watcher becomes the target.
func (h *hub) creakingActivate(players map[int32]*tracked, m *mob, t *tracked) {
	m.creakActive = true
	m.targetEID, m.unseenTicks = t.p.eid, 0
	h.vibAt(m.dim, freqEntityAction, m.x, m.y, m.z, m.eid)
	h.playSoundDim(players, m.dim, "minecraft:entity.creaking.activate", sndHostile, m.x, m.y, m.z, 1, h.voicePitch(m))
}

// creakingDeactivate is Creaking.deactivate: the target is forgotten.
func (h *hub) creakingDeactivate(players map[int32]*tracked, m *mob) {
	m.creakActive = false
	m.targetEID, m.hasTarget = 0, false
	h.vibAt(m.dim, freqEntityAction, m.x, m.y, m.z, m.eid)
	h.playSoundDim(players, m.dim, "minecraft:entity.creaking.deactivate", sndHostile, m.x, m.y, m.z, 1, h.voicePitch(m))
}

// creakingTarget is the creaking's attack target. Only an ACTIVE creaking has
// one (StartAttacking's isActive gate), and it is dropped the moment it is no
// longer among the players the creaking can see within its follow range
// (isAttackTargetStillReachable) — then the nearest one it can see is taken.
func (h *hub) creakingTarget(players map[int32]*tracked, m *mob) *tracked {
	if !m.creakActive {
		m.targetEID = 0
		return nil
	}
	follow := m.followRange()
	valid := func(t *tracked) bool {
		return t != nil && t.dim == m.dim && !t.dead && isSurvival(t.gamemode) &&
			dist3(t.x, t.y, t.z, m.x, m.y, m.z) <= follow && h.mobSees(m, t)
	}
	if t := players[m.targetEID]; valid(t) {
		return t
	}
	var best *tracked
	bestD := follow + 1
	for _, t := range players {
		if d := dist3(t.x, t.y, t.z, m.x, m.y, m.z); d < bestD && valid(t) {
			best, bestD = t, d
		}
	}
	m.targetEID = 0
	if best != nil {
		m.targetEID = best.p.eid
	}
	return best
}

// updateCreakings runs the two things that are true of a creaking and nothing
// else: it is a statue while watched, and a blow it took is the heart's to
// answer. Both are read here rather than acted on where they happen — the
// freeze because movement is decided in one place, the blow because the mob's
// own damage arithmetic has no hub to reach for.
func (h *hub) updateCreakings(players map[int32]*tracked) {
	for _, m := range h.mobs {
		if m.etype != entityCreaking || m.dying > 0 {
			continue
		}
		link := h.heartOf(m)
		if link == nil && !m.heartBound && m.home == (blockPos{}) {
			// No home at all (a summoned creaking): Creaking.tick only
			// checks a heart when it has a home position.
			m.frozen = h.creakingFrozen(players, m)
			continue
		}
		if link == nil {
			// Its heart is gone. Vanilla sets health to 0 on the spot; the
			// creaking simply comes apart, which is what the player sees.
			m.heartBound = false
			h.playSoundDim(players, m.dim, "minecraft:entity.creaking.death", sndHostile, m.x, m.y, m.z, 1, 1)
			h.removeMob(players, m)
			continue
		}
		m.heartBound = true
		if m.heartHit {
			m.heartHit = false
			h.creakingHurt(players, m)
		}
		m.frozen = h.creakingFrozen(players, m)
	}
}
