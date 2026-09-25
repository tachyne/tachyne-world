package server

import (
	"math"

	"github.com/tachyne/tachyne-common/protocol"
)

// Guardian + Elder Guardian behaviour, ported from Guardian.GuardianAttackGoal
// and ElderGuardian: the beam attack (charge then indirect-magic damage that
// pierces armour, plus the melee) and the elder's periodic Mining Fatigue curse.
// Numbers match vanilla 26.2 behaviour.

const (

	// Guardian.getAttackDuration()==80 (elder 60); the goal's attackTime starts
	// at -10, so a beam lands ~90 ticks after lock-on.
	guardianBeamR = 16.0 // beam reach (target must also be > 3 blocks: d2 > 9)

	// ElderGuardian: every 1200 ticks apply MINING_FATIGUE (amp 2, 6000-tick =
	// 300 s) to players within 50 blocks.
	elderAuraUpd = 600 // 1200 ticks / 2
	elderAuraR   = 50.0

	// Guardian.hurtServer: the spikes are worth two points, elder or not.
	guardianThornsDamage = 2
)

// guardianTick drives one guardian/elder-guardian mob update.
func (h *hub) guardianTick(players map[int32]*tracked, m *mob) {
	elder := m.etype == entityElderGuardian
	if elder {
		if m.digClock++; m.digClock >= elderAuraUpd {
			m.digClock = 0
			for _, t := range players {
				if t.dim != m.dim {
					continue
				}
				dx, dy, dz := t.x-m.x, t.y-m.y, t.z-m.z
				if dx*dx+dy*dy+dz*dz <= elderAuraR*elderAuraR {
					h.applyEffect(players, t, effMiningFatigue, 2, 300) // level 3, 5 minutes
				}
			}
		}
	}
	h.syncGuardianSpikes(players, m)
	// GuardianAttackGoal: lock on (the client draws the beam from the synced
	// attack target), let attackTime run from -10 up to the attack duration
	// while the target stays in reach and sight, then land the two hits and
	// let go — the goal restarts at once, so the cycle is ~90 ticks.
	if m.beamTarget == 0 {
		q, ok := h.rangedQuarry(players, m, guardianBeamR) // a player, or the squid it hunts
		if !ok {
			return
		}
		if dx, dz := q.x-m.x, q.z-m.z; dx*dx+dz*dz <= 9 { // the beam only fires past 3 blocks
			return
		}
		if (q.t != nil && !h.mobSees(m, q.t)) || (q.o != nil && !h.mobSeesMob(m, q.o)) {
			return // its target goal must see the target
		}
		m.beamTarget, m.beamTicks = q.eid(), -10
		h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(guardianTargetMeta(m.eid, m.beamTarget)))
		return
	}
	if o := h.mobs[m.beamTarget]; o != nil && players[m.beamTarget] == nil {
		h.guardianBeamMob(players, m, o, elder)
		return
	}
	t := players[m.beamTarget]
	if t == nil || t.dead || t.dim != m.dim || t.gamemode == gmCreative || t.gamemode == gmSpectator ||
		dist3(t.x, t.y, t.z, m.x, m.y, m.z) > guardianBeamR || !h.mobSees(m, t) { // GuardianAttackGoal.tick: out of sight, let go
		h.guardianRelease(players, m)
		return
	}
	m.beamTicks += mobMoveInterval
	dx, dz := t.x-m.x, t.z-m.z
	m.yaw = float32(math.Atan2(-dx, dz) * 180 / math.Pi)
	duration := 80 // Guardian.getAttackDuration
	if elder {
		duration = 60
	}
	if m.beamTicks < duration {
		return
	}
	magic := float32(1)
	if h.rules.Difficulty == diffHard {
		magic += 2
	}
	if elder {
		magic += 2
	}
	melee := hostileMelee(m)
	h.toTracking(players, m.eid, m.dim, m.x, m.z, swingArm(m.eid))
	h.playSoundDim(players, m.dim, "minecraft:entity.guardian.attack", sndHostile, m.x, m.y, m.z, 1, 1)
	// TWO hits, as the attack goal deals them: the beam's indirect_magic, which
	// armour does not stop, and then an ordinary bite, which it does. Folding
	// them into one number would have to pick a single answer to that question.
	cause := deathCause{by: mobDisplayName(m.etype)}
	h.hurtBy(players, t, magic, dtIndirectMagic, cause)
	if !t.dead {
		h.hurtFrom(players, t, melee, dtMobAttack, cause, fromMob(m.x, m.z))
	}
	h.guardianRelease(players, m)
	h.thornsRetaliate(players, t, m)
	if t.dead {
		h.advance(players, t, "entity_killed_player", advMatch{entity: advEntityName[m.etype]})
	}
}

// guardianBeamMob is the attack goal against a mob target: the same lock,
// the same wait, the same two hits.
func (h *hub) guardianBeamMob(players map[int32]*tracked, m, o *mob, elder bool) {
	if o.dying > 0 || o.dim != m.dim || dist3(o.x, o.y, o.z, m.x, m.y, m.z) > guardianBeamR || !h.mobSeesMob(m, o) {
		h.guardianRelease(players, m)
		return
	}
	m.beamTicks += mobMoveInterval
	m.yaw = float32(math.Atan2(-(o.x-m.x), o.z-m.z) * 180 / math.Pi)
	duration := 80
	if elder {
		duration = 60
	}
	if m.beamTicks < duration {
		return
	}
	magic := 1.0
	if h.rules.Difficulty == diffHard {
		magic += 2
	}
	if elder {
		magic += 2
	}
	h.toTracking(players, m.eid, m.dim, m.x, m.z, swingArm(m.eid))
	h.playSoundDim(players, m.dim, "minecraft:entity.guardian.attack", sndHostile, m.x, m.y, m.z, 1, 1)
	o.lastAttacker = m.eid
	o.hurtKind(magic, dtIndirectMagic)
	if o.health > 0 {
		o.hurtKind(float64(hostileMelee(m)), dtMobAttack)
	}
	h.guardianRelease(players, m)
	if o.health <= 0 {
		h.killMob(players, o)
	}
}

// guardianMovingMeta is DATA_ID_MOVING (index 16, BOOLEAN): whether the move
// control is driving the guardian. The client draws the spikes from it — out
// when the guardian is still, folded back while it swims — so without it a
// guardian's spines never move however faithfully the damage is reflected.
// Guardian is a Monster, so the index is the same on every served client.
func guardianMovingMeta(eid int32, moving bool) []byte {
	b := protocol.AppendVarInt(nil, eid)
	b = protocol.AppendU8(b, 16)
	b = protocol.AppendVarInt(b, 8) // BOOLEAN
	b = protocol.AppendBool(b, moving)
	return protocol.AppendU8(b, itemMetaEnd)
}

// syncGuardianSpikes broadcasts the flag when it turns over.
func (h *hub) syncGuardianSpikes(players map[int32]*tracked, m *mob) {
	moving := !guardianSpikesOut(m)
	if moving == m.guardMoving {
		return
	}
	m.guardMoving = moving
	h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(guardianMovingMeta(m.eid, moving)))
}

// guardianSpikesOut is Guardian's isMoving(), inverted. GuardianMoveControl
// raises that flag only while it is driving the guardian toward a wanted
// point and drops it the moment the navigation is done — and the attack goal
// stops the navigation outright, which is why a guardian bearing down on you
// has its spikes folded back and one holding station over you has them out.
// The engine's steering asks the same question: a swimming guardian is
// closing on its target, and one already inside the standoff distance has
// nowhere left to go. With no target at all, the idle half of the stroll
// cycle is the still one.
func guardianSpikesOut(m *mob) bool {
	if m.hasTarget {
		return math.Hypot(m.tx-m.x, m.tz-m.z) < standoffDist
	}
	return m.rest > 0
}

// guardianThorns is Guardian.hurtServer's retaliation: land a blow on a
// guardian with its spikes out and you take two points back, whoever you are.
//
// Vanilla reads the DIRECT entity of the damage source and only reflects onto
// a LivingEntity, so this belongs on the melee seam and nowhere else: an
// arrow's direct entity is the arrow, not the archer, and a guardian never
// spikes someone who shot it. The other half of the guard — magic, explosions
// and thorns themselves (#avoids_guardian_thorns) — cannot reach a melee blow
// at all, which is why there is no damage-type test here.
//
// The reflection runs BEFORE the guardian takes the hit, as it does in
// vanilla, so even a killing blow costs the killer its two points.
func (h *hub) guardianThorns(players map[int32]*tracked, m *mob, attacker int32) {
	if m.etype != entityGuardian && m.etype != entityElderGuardian {
		return
	}
	if m.dying > 0 || !guardianSpikesOut(m) {
		return
	}
	if t := players[attacker]; t != nil {
		h.hurtFrom(players, t, guardianThornsDamage, dtThorns,
			deathCause{by: mobDisplayName(m.etype)}, fromMob(m.x, m.z))
		return
	}
	if o := h.mobs[attacker]; o != nil && o != m {
		h.hurtMobOf(players, o, guardianThornsDamage, dtThorns)
	}
}

// populateMonuments seeds a monument's 3 elder guardians and a guardian patrol
// the first time a player comes near it — vanilla monuments spawn a fixed elder
// trio via structure spawn overrides; here the server seeds them, and the
// "elder already present" guard keeps it a one-time event (mobs persist, so a
// reload does not duplicate them).
func (h *hub) populateMonuments(players map[int32]*tracked) {
	g := h.world.Gen()
	for _, t := range players {
		if t.dim != 0 {
			continue
		}
		mn := g.MonumentIn(int(t.x), int(t.z))
		if !mn.Exists {
			continue
		}
		dx, dz := t.x-float64(mn.X), t.z-float64(mn.Z)
		if dx*dx+dz*dz > 48*48 {
			continue // only when the player is actually at the monument
		}
		if h.countMobNear(entityElderGuardian, 0, float64(mn.X), float64(mn.Z), 64) > 0 {
			continue // already populated
		}
		cy := float64(mn.Y + 7) // inside the hall
		for i, off := range [][2]float64{{0, 0}, {6, 6}, {-6, -6}} {
			_ = i
			h.spawnHostileYIn(players, entityElderGuardian, dimOverworld, float64(mn.X)+off[0], cy, float64(mn.Z)+off[1])
		}
		for i := 0; i < 8; i++ {
			ox := float64((i%4)*3 - 4)
			oz := float64((i/4)*6 - 3)
			h.spawnHostileYIn(players, entityGuardian, dimOverworld, float64(mn.X)+ox, cy, float64(mn.Z)+oz)
		}
	}
}

// populateMansions seeds a woodland mansion's illagers (evokers, vindicators and
// a few allays) at the vanilla marker positions the first time a player reaches
// it. mansionDone is persisted, so a cleared mansion stays cleared across
// restarts (no evoker/totem farm by re-approaching).
func (h *hub) populateMansions(players map[int32]*tracked) {
	g := h.world.Gen()
	illager := [3]int{entityEvoker, entityVindicator, entityAllay}
	for _, t := range players {
		if t.dim != 0 {
			continue
		}
		mn := g.MansionIn(int(t.x), int(t.z))
		if !mn.Exists {
			continue
		}
		key := [2]int32{int32(mn.X), int32(mn.Z)}
		if h.mansionDone[key] {
			continue // already populated (persisted) — stays cleared
		}
		dx, dz := t.x-float64(mn.X), t.z-float64(mn.Z)
		if dx*dx+dz*dz > 80*80 {
			continue // only once the player is at the mansion (it is large)
		}
		h.mansionDone[key] = true
		for _, s := range g.MansionMobs(mn) {
			if s.Type < 0 || s.Type > 2 {
				continue
			}
			h.spawnMobIn(players, illager[s.Type], dimOverworld, float64(s.X)+0.5, float64(s.Y), float64(s.Z)+0.5)
		}
	}
}

// countMobNear counts live mobs of a type within radius r (horizontal) of a
// point in a dimension.
func (h *hub) countMobNear(etype, dim int, x, z, r float64) int {
	n := 0
	for _, m := range h.mobs {
		if m.etype != etype || m.dim != dim || m.dying > 0 {
			continue
		}
		if dx, dz := m.x-x, m.z-z; dx*dx+dz*dz <= r*r {
			n++
		}
	}
	return n
}

// guardianRelease is GuardianAttackGoal.stop: the beam lets go.
func (h *hub) guardianRelease(players map[int32]*tracked, m *mob) {
	if m.beamTarget == 0 {
		return
	}
	m.beamTarget, m.beamTicks = 0, 0
	h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(guardianTargetMeta(m.eid, 0)))
}

// guardianTargetMeta is DATA_ID_ATTACK_TARGET (index 17, INT): the entity id
// the beam is locked on, 0 for none. Guardian is a Monster, so the index is
// the same on every served client.
func guardianTargetMeta(eid, target int32) []byte {
	b := protocol.AppendVarInt(nil, eid)
	b = protocol.AppendU8(b, 17)
	b = protocol.AppendVarInt(b, 1) // INT
	b = protocol.AppendVarInt(b, target)
	return protocol.AppendU8(b, itemMetaEnd)
}
