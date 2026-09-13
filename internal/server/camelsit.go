package server

import "github.com/tachyne/tachyne-common/protocol"

// Camels sit (Camel.sitDown/standUp + CamelAi.RandomSitting): an idle camel
// that has held its pose for twenty seconds folds its legs now and then,
// and gets up the same way later; a rider pushing forward, a hit, water
// under it, or a lead pulled past six blocks stands it up. The client
// animates the fold and the rise from LAST_POSE_CHANGE_TICK — negative
// while sitting, the world tick of the change either way — and the pose.

const (
	metaIndexCamelPoseTick = 19 // LAST_POSE_CHANGE_TICK (VarLong); the ageable shift applies on 26.2
	metaTypeLong           = 2  // EntityDataSerializers.LONG
	poseSitting            = 10 // Pose.SITTING
	camelSitDownTicks      = 40 // isInPoseTransition while sitting
	camelStandUpTicks      = 52 // isInPoseTransition while standing
	camelMinPoseTicks      = 400
	camelSitOdds           = 60 // RandomSitting is one of four idle picks every few seconds
)

// camelSitting is isCamelSitting.
func (m *mob) camelSitting() bool { return m.poseTick < 0 }

// camelPoseTime is getPoseTime: ticks since the last pose change.
func (m *mob) camelPoseTime(now uint64) int64 {
	t := m.poseTick
	if t < 0 {
		t = -t
	}
	return int64(now) - t
}

// camelInTransition is isInPoseTransition.
func (m *mob) camelInTransition(now uint64) bool {
	limit := int64(camelStandUpTicks)
	if m.camelSitting() {
		limit = camelSitDownTicks
	}
	return m.camelPoseTime(now) < limit
}

// camelRefusesToMove is refuseToMove: sat, or mid-fold.
func (m *mob) camelRefusesToMove(now uint64) bool {
	return m.camelSitting() || m.camelInTransition(now)
}

// camelPoseMeta is the pose plus LAST_POSE_CHANGE_TICK.
func camelPoseMeta(m *mob) []byte {
	pose := int32(poseStanding)
	if m.camelSitting() {
		pose = poseSitting
	}
	b := protocol.AppendVarInt(nil, m.eid)
	b = protocol.AppendU8(b, metaIndexPose)
	b = protocol.AppendVarInt(b, metaTypePose)
	b = protocol.AppendVarInt(b, pose)
	b = protocol.AppendU8(b, metaIndexCamelPoseTick)
	b = protocol.AppendVarInt(b, metaTypeLong)
	b = protocol.AppendVarLong(b, m.poseTick)
	return protocol.AppendU8(b, itemMetaEnd)
}

func (h *hub) camelSitDown(players map[int32]*tracked, m *mob) {
	if m.camelSitting() {
		return
	}
	h.playSoundDim(players, m.dim, "minecraft:entity.camel.sit", sndNeutral, m.x, m.y, m.z, 1, 1)
	m.poseTick = -int64(h.tick.Load())
	h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(camelPoseMeta(m)))
}

func (h *hub) camelStandUp(players map[int32]*tracked, m *mob) {
	if !m.camelSitting() {
		return
	}
	h.playSoundDim(players, m.dim, "minecraft:entity.camel.stand", sndNeutral, m.x, m.y, m.z, 1, 1)
	m.poseTick = int64(h.tick.Load())
	h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(camelPoseMeta(m)))
}

// camelStandUpInstantly skips the rise animation (a hit, water).
func (h *hub) camelStandUpInstantly(players map[int32]*tracked, m *mob) {
	was := m.poseTick
	m.poseTick = max(0, int64(h.tick.Load())-camelStandUpTicks-1)
	if was < 0 || was != m.poseTick {
		h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(camelPoseMeta(m)))
	}
}

// camelSitStep runs each mob update. Returns whether the camel refuses to
// move (the caller holds it).
func (h *hub) camelSitStep(players map[int32]*tracked, m *mob) bool {
	now := h.tick.Load()
	if m.camelSitting() && (h.inWater(m.dim, m.x, m.y, m.z) || m.kb > 0) {
		h.camelStandUpInstantly(players, m) // tick(): water; actuallyHurt: a hit
	}
	if m.rider == 0 && m.leash == 0 && m.grounded() && !h.inWater(m.dim, m.x, m.y, m.z) &&
		m.camelPoseTime(now) >= camelMinPoseTicks && !m.tempted && m.loveTicks == 0 && m.kb == 0 &&
		h.rng.Intn(camelSitOdds) == 0 { // RandomSitting(20)
		if m.camelSitting() {
			h.camelStandUp(players, m)
		} else if m.panic == 0 {
			h.camelSitDown(players, m)
		}
	}
	if m.camelRefusesToMove(now) {
		m.vx, m.vz = 0, 0
		return true
	}
	return false
}

// camelRiderForward is tickRidden: the rider pushing forward stands a sat
// camel up (the client refuses the movement itself until then).
func (h *hub) camelRiderForward(players map[int32]*tracked, t *tracked) {
	if t.ridingEID == 0 {
		return
	}
	m := h.mobs[t.ridingEID]
	if m == nil || m.etype != entityCamel || !m.camelSitting() || m.camelInTransition(h.tick.Load()) {
		return
	}
	h.camelStandUp(players, m)
}
