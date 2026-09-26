package server

import (
	"math"

	"github.com/tachyne/tachyne-common/protocol"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// What an enderman does about the player looking at it, and about the one
// it is after. Vanilla's EndermanLookForPlayerGoal is a staring contest with
// rules: a player who holds its eyes in their crosshair (or one it is angry
// at) becomes its target a moment later, and it keeps that target wherever
// they go, seen or not, for as long as they can be attacked. While the
// target stares it stops where it is and stares back
// (EndermanFreezeWhenLookedAt), it blinks away if they close to within four
// blocks, and if they are past sixteen and not looking it spends thirty
// ticks and then blinks TOWARDS them. A blow it takes turns it on the
// attacker (HurtByTargetGoal), and whoever it has had as a target it stays
// angry at for 20-39 seconds after it loses them (NeutralMob), taking them
// up again on sight. Daylight it simply cannot stand.
//
// Two flags ride the entity: DATA_CREEPY, set while it has a target (the
// open jaw, the shake, the scream), and DATA_STARED_AT, set when the look
// goal starts. The client plays enderman.stare when CREEPY changes while
// STARED_AT is set, so the stare sound needs nothing from the server.

const (
	endermanStareFreeze  = 256.0                                       // EndermanFreezeWhenLookedAt: its target within 16 blocks
	endermanStareBlink   = 16.0                                        // …but inside 4 it blinks away (distanceToSqr < 16)
	endermanChaseBlink   = 256.0                                       // beyond 16 it closes the gap with a blink
	endermanChaseDelay   = 30 / mobMoveInterval                        // …once teleportTime passes adjustedTickDelay(30), in updates
	endermanAggroDelay   = (5 + mobMoveInterval - 1) / mobMoveInterval // aggroTime: adjustedTickDelay(5), in updates
	endermanStareCone    = 0.025                                       // isBeingStaredBy: isLookingAtMe(0.025, adjusted for distance)
	endermanDayLightMin  = 0.5                                         // getLightLevelDependentMagicValue > 0.5
	endermanDaySettleMin = 600                                         // MIN_DEAGGRESSION_TIME: ticks since the target last changed
	endermanAttackSpeed  = 0.15                                        // SPEED_MODIFIER_ATTACKING: 0.3 → 0.45 while it has a target
	endermanSpeedSource  = "minecraft:attacking"                       // SPEED_MODIFIER_ATTACKING_ID
	endermanLookTurn     = 10.0                                        // lookAt(pendingTarget, 10, 10): degrees a goal tick
	endermanPitchResend  = 4.0                                         // degrees a held stare's pitch moves before it is re-sent
)

// The enderman's own synced fields, after DATA_CARRY_STATE (16): Entity 0-7,
// LivingEntity 8-14, Mob 15. Enderman is a Monster, not an AgeableMob, so
// 26.2's AGE_LOCKED insertion leaves these where they are.
const (
	metaIndexEndermanCreepy = 17 // DATA_CREEPY (BOOLEAN)
	metaIndexEndermanStared = 18 // DATA_STARED_AT (BOOLEAN)

	enderCreepyBit = 1 << 0
	enderStaredBit = 1 << 1
)

// gazeDisguise is #gaze_disguise_equipment: the head items that let a player
// look an enderman in the eye (LivingEntity.PLAYER_NOT_WEARING_DISGUISE_ITEM).
var gazeDisguise = itemTagSet("gaze_disguise_equipment")

// endermanTarget stands in for acquireTarget: updatePersistentAnger, then
// the target goals, the look goal's blinks, the freeze and the daylight
// flight, and last the synced flags and the chase speed they imply.
func (h *hub) endermanTarget(players map[int32]*tracked, m *mob) {
	m.enderHeld = false
	if m.dying != 0 {
		return
	}
	h.endermanAnger(players, m) // aiStep: before the goals

	// EndermanLookForPlayerGoal.canUse (priority 1): the nearest player it
	// can see who stares at it or whom it is angry at. It outranks the
	// hurt goal and the endermite goal, which stop (and clear the target)
	// when it starts.
	if m.enderPending == 0 && !m.enderLook {
		if t := h.endermanLookFor(players, m); t != nil {
			if m.hasTarget || m.targetEID != 0 {
				h.endermanSetTarget(m, nil) // TargetGoal.stop
			}
			m.wolfPrey = 0
			m.enderPending, m.enderAggroIn, m.stareTicks = t.p.eid, endermanAggroDelay, 0
			m.enderStared = true // setBeingStaredAt
		}
	}
	switch {
	case m.enderPending != 0:
		// The pending target: kept while it still stares (or is still the
		// grudge), watched, and taken aggroTime later.
		p := players[m.enderPending]
		if p == nil || !h.endermanCanAttack(m, p) || !(h.endermanStaredBy(m, p) || h.endermanAngryAt(m, p)) {
			m.enderPending = 0
			h.endermanSetTarget(m, nil)
			break
		}
		turn := wrapDegrees(float64(yawToward(m.x, m.z, p.x, p.z) - m.yaw))
		m.yaw += float32(math.Max(-endermanLookTurn, math.Min(endermanLookTurn, turn)))
		if m.enderAggroIn--; m.enderAggroIn <= 0 {
			m.enderPending = 0
			h.endermanSetTarget(m, p)
			m.enderLook = true
		}
	case m.enderLook:
		// continueAggroTargetConditions: forCombat with no sight test and no
		// range — the target is kept until it cannot be attacked.
		t := players[m.targetEID]
		if !m.hasTarget || t == nil || !h.endermanCanAttack(m, t) {
			h.endermanSetTarget(m, nil)
			break
		}
		m.tx, m.tz = t.x, t.z
		if m.mount == 0 {
			d2 := distSq(m, t)
			if h.endermanStaredBy(m, t) {
				if d2 < endermanStareBlink {
					h.endermanTeleport(players, m) // too close under that gaze
				}
				m.stareTicks = 0
			} else if d2 > endermanChaseBlink {
				ready := m.stareTicks >= endermanChaseDelay // teleportTime++ >= adjustedTickDelay(30)
				m.stareTicks++
				if ready && h.endermanTeleportTowards(players, m, t) {
					m.stareTicks = 0 // reset only by a blink that landed
				}
			}
		}
	case m.hasTarget && m.targetEID != 0:
		// HurtByTargetGoal (TargetGoal.canContinueToUse): in follow range,
		// seen within 300 ticks — and each update it sets the target again,
		// which keeps restarting the daylight clock.
		t := players[m.targetEID]
		if t == nil || !h.endermanCanAttack(m, t) || distSq(m, t) > sq(m.followRange()) {
			h.endermanSetTarget(m, nil)
			break
		}
		if h.mobSees(m, t) {
			m.unseenTicks = 0
		} else if m.unseenTicks += mobMoveInterval; m.unseenTicks > hurtByUnseenMemory {
			h.endermanSetTarget(m, nil)
			break
		}
		h.endermanSetTarget(m, t)
	default:
		m.hasTarget, m.targetEID = false, 0
	}

	// EndermanFreezeWhenLookedAt: its target, a player within sixteen
	// blocks, staring at it. It stops and looks back.
	if t := players[m.targetEID]; m.hasTarget && t != nil && distSq(m, t) <= endermanStareFreeze && h.endermanStaredBy(m, t) {
		m.enderHeld = true
		m.vx, m.vz = 0, 0
	}

	// customServerAiStep: a bright sky over an enderman whose target has not
	// changed in half a minute, and every tick it may be gone — target
	// dropped, angry or not. Hence you see them at night.
	if m.dim == 0 && h.isDaylight() && h.endermanSettled(m) {
		if f := h.lightMagic(m); f > endermanDayLightMin && h.skyExposed(m) {
			for i := 0; i < mobMoveInterval; i++ {
				if h.rng.Float32()*30 < float32(f-0.4)*2 {
					h.endermanSetTarget(m, nil)
					m.enderHeld = false
					h.endermanTeleport(players, m)
					break
				}
			}
		}
	}
	m.preyTarget = 0 // the endermite hunt is mobHuntStep's
	h.endermanSync(players, m)
}

// endermanAnger is updatePersistentAnger(level, true): a player target
// becomes the grudge and keeps it topped up (a fresh PERSISTENT_ANGER_TIME
// every update it is held); without one the time runs down, and when it is
// gone the enderman stops being angry. A grudge against a player who has
// turned creative or spectator is dropped at once.
func (h *hub) endermanAnger(players map[int32]*tracked, m *mob) {
	var tgt *tracked
	if m.hasTarget {
		tgt = players[m.targetEID]
	}
	if tgt != nil {
		m.angryAt, m.angryUUID = tgt.p.eid, tgt.p.uuid
		m.anger = h.neutralAngerTime() // startPersistentAngerTimer: new target, or held
		return
	}
	if m.anger > 0 {
		m.anger--
	}
	grudge := m.angryAt != 0 || m.angryUUID != [16]byte{}
	if a := h.endermanGrudge(players, m); (grudge && m.anger == 0) || (a != nil && !isSurvival(a.gamemode)) {
		h.calmDown(m) // stopBeingAngry
	}
}

// endermanHurtBy is HurtByTargetGoal for an enderman a player struck: the
// attacker becomes its target, and so its grudge, unless the look goal
// already holds one (it outranks the hurt goal). Under universal_anger the
// blow instead makes it angry at every player (ResetUniversalAngerTargetGoal).
func (h *hub) endermanHurtBy(m *mob, t *tracked) {
	if m.dying != 0 || !h.endermanCanAttack(m, t) {
		return
	}
	if h.rules.UniversalAnger {
		m.angryAt, m.angryUUID = 0, [16]byte{}
		m.anger = h.neutralAngerTime()
		return
	}
	if m.enderLook || m.enderPending != 0 {
		return
	}
	h.endermanSetTarget(m, t)
	m.angryAt, m.angryUUID, m.anger = t.p.eid, t.p.uuid, h.neutralAngerTime()
}

// endermanGrudge is the player the enderman is angry at, if they are here:
// by eid, or — once that eid has gone (a reload, a reconnect) — by their
// stable UUID, which re-points the eid. Nil when they are not online.
func (h *hub) endermanGrudge(players map[int32]*tracked, m *mob) *tracked {
	if t := players[m.angryAt]; m.angryAt != 0 && t != nil {
		return t
	}
	m.angryAt = 0
	if m.angryUUID == ([16]byte{}) {
		return nil
	}
	for _, t := range players {
		if t.p.uuid == m.angryUUID {
			m.angryAt = t.p.eid
			return t
		}
	}
	return nil
}

// endermanAngryAt is NeutralMob.isAngryAt for a player: the grudge (as
// endermanGrudge last resolved it), or universal anger with no one grudge.
func (h *hub) endermanAngryAt(m *mob, t *tracked) bool {
	if m.angryAt != 0 {
		return m.angryAt == t.p.eid
	}
	return m.angryUUID == ([16]byte{}) && h.rules.UniversalAnger && m.anger > 0
}

// endermanSetTarget is Enderman.setTarget for a player target (nil clears
// it). The flags and the speed modifier follow in endermanSync.
func (h *hub) endermanSetTarget(m *mob, t *tracked) {
	if t == nil {
		m.hasTarget, m.targetEID, m.enderLook, m.unseenTicks = false, 0, false, 0
		m.enderTargetAt = 0   // targetChangeTime = 0
		m.enderStared = false // DATA_STARED_AT false
		m.enderHeld = false   // nothing left to freeze for
		return
	}
	m.hasTarget, m.targetEID, m.tx, m.tz = true, t.p.eid, t.x, t.z
	m.enderTargetAt = h.tick.Load()
}

// endermanSettled is tickCount >= targetChangeTime + 600, with a cleared
// target's zero reading from the enderman's first tick.
func (h *hub) endermanSettled(m *mob) bool {
	since := m.spawnTick
	if m.enderTargetAt != 0 {
		since = m.enderTargetAt
	}
	return h.tick.Load() >= since+endermanDaySettleMin
}

// endermanCanAttack is TargetingConditions.forCombat's canAttack for a
// player: alive, in its world, and in survival or adventure.
func (h *hub) endermanCanAttack(m *mob, t *tracked) bool {
	return t != nil && !t.dead && t.dim == m.dim && isSurvival(t.gamemode)
}

// endermanLookFor is the look goal's startAggroTargetConditions: the
// nearest player within follow range (shrunk by how visible they are), in
// its line of sight, who stares at it or whom it is angry at.
func (h *hub) endermanLookFor(players map[int32]*tracked, m *mob) *tracked {
	reach := m.followRange()
	var best *tracked
	bestD2 := math.Inf(1)
	for _, t := range players {
		if !h.endermanCanAttack(m, t) {
			continue
		}
		d2 := distSq(m, t)
		vis := math.Max(reach*visibilityPercent(t, m), 2)
		if d2 >= bestD2 || d2 > vis*vis {
			continue
		}
		if !(h.endermanStaredBy(m, t) || h.endermanAngryAt(m, t)) || !h.mobSees(m, t) {
			continue
		}
		best, bestD2 = t, d2
	}
	return best
}

// endermanStaredBy is Enderman.isBeingStaredBy: no disguise on the head,
// and isLookingAtMe(0.025, adjusted for distance, not through glass) — the
// player's view vector within the cone of the enderman's eyes, seen from
// the player's real eye height, with nothing solid in between.
func (h *hub) endermanStaredBy(m *mob, t *tracked) bool {
	if t.dim != m.dim || (t.armor[0].count > 0 && gazeDisguise[t.armor[0].item]) {
		return false
	}
	eyeY, gaze := playerEyeY(t), m.y+mobEyeHeight(m)
	dx, dy, dz := m.x-t.x, gaze-eyeY, m.z-t.z
	d := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if d < 1e-6 {
		return false
	}
	vx, vy, vz := lookVector(t.yaw, t.pitch)
	if (vx*dx+vy*dy+vz*dz)/d <= 1-endermanStareCone/d {
		return false
	}
	return h.sightClear(m.dim, t.x, eyeY, t.z, m.x, gaze, m.z) // ClipContext.Block.COLLIDER
}

// endermanTeleportTowards is Enderman.teleportTowards: a hop that lands the
// enderman most of the way to its target rather than anywhere at all.
func (h *hub) endermanTeleportTowards(players map[int32]*tracked, m *mob, t *tracked) bool {
	// From the target's eyes to the enderman's middle, normalised (a vector
	// shorter than 1e-5 normalises to zero, as Vec3.normalize does), then 16
	// blocks back along it with a little jitter: past the target.
	dx, dy, dz := m.x-t.x, (m.y+m.box().h*0.5)-playerEyeY(t), m.z-t.z
	if d := math.Sqrt(dx*dx + dy*dy + dz*dz); d < 1e-5 {
		dx, dy, dz = 0, 0, 0
	} else {
		dx, dy, dz = dx/d, dy/d, dz/d
	}
	tx := m.x + (h.rng.Float64()-0.5)*8 - dx*16
	ty := m.y + float64(h.rng.Intn(16)-8) - dy*16
	tz := m.z + (h.rng.Float64()-0.5)*8 - dz*16
	return h.endermanTeleportTo(players, m, tx, ty, tz)
}

// endermanFaceTarget is the freeze goal's tick: the head turns to the
// target's eyes. Runs after idleLook, which would sit the head back on the
// body of a mob with a target.
func (h *hub) endermanFaceTarget(players map[int32]*tracked, m *mob) {
	t := players[m.targetEID]
	if !m.enderHeld || t == nil {
		return
	}
	m.headYaw = yawToward(m.x, m.z, t.x, t.z)
	dy := playerEyeY(t) - (m.y + mobEyeHeight(m))
	pitch := float32(-math.Atan2(dy, math.Hypot(t.x-m.x, t.z-m.z)) * 180 / math.Pi)
	if math.Abs(float64(pitch-m.enderPitch)) > endermanPitchResend {
		m.enderPitch = pitch
		h.toTracking(players, m.eid, m.dim, m.x, m.z, entMove(m.eid, m.x, m.y, m.z, m.yaw, pitch, m.grounded()))
	}
}

// endermanSync brings the synced flags and the attacking speed in line
// with the target: CREEPY and the speed modifier while it has one (a
// player, or an endermite it is after), STARED_AT from the look goal.
func (h *hub) endermanSync(players map[int32]*tracked, m *mob) {
	creepy := (m.hasTarget && m.targetEID != 0) || m.wolfPrey != 0
	speed := m.mobAttrs().Get(attr.MovementSpeed)
	switch {
	case creepy && !speed.HasModifier(endermanSpeedSource):
		speed.AddModifier(attr.Modifier{Source: endermanSpeedSource, Amount: endermanAttackSpeed * attrToStep, Op: attr.AddValue})
	case !creepy && speed.HasModifier(endermanSpeedSource):
		speed.RemoveModifier(endermanSpeedSource)
	}
	if !creepy && m.enderPending == 0 {
		m.enderStared = false // setTarget(null) took it down
	}
	var want uint8
	if creepy {
		want |= enderCreepyBit
	}
	if m.enderStared {
		want |= enderStaredBit
	}
	if want != m.enderSent {
		changed := want ^ m.enderSent
		m.enderSent = want
		h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(endermanFlagsMeta(m.eid, changed, want)))
	}
	if !m.enderHeld && m.enderPitch != 0 {
		m.enderPitch = 0 // the next move sends it level again
	}
}

// endermanFlagsMeta builds DATA_CREEPY and/or DATA_STARED_AT (the bits of
// which), in index order as one packet, so a client sees CREEPY change
// with STARED_AT as it was — the order its stare sound depends on.
func endermanFlagsMeta(eid int32, which, vals uint8) []byte {
	b := protocol.AppendVarInt(nil, eid)
	for _, f := range [2]struct {
		bit uint8
		idx byte
	}{{enderCreepyBit, metaIndexEndermanCreepy}, {enderStaredBit, metaIndexEndermanStared}} {
		if which&f.bit == 0 {
			continue
		}
		b = protocol.AppendU8(b, f.idx)
		b = protocol.AppendVarInt(b, metaTypeBool)
		var v byte
		if vals&f.bit != 0 {
			v = 1
		}
		b = protocol.AppendU8(b, v)
	}
	return protocol.AppendU8(b, itemMetaEnd)
}

// distSq is the squared distance from a mob to a player.
func distSq(m *mob, t *tracked) float64 {
	dx, dy, dz := m.x-t.x, m.y-t.y, m.z-t.z
	return dx*dx + dy*dy + dz*dz
}
