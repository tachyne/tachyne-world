package server

import (
	"math"
	"sort"

	"github.com/tachyne/tachyne-common/protocol"
)

// Shoulder parrots. A tamed parrot that is not sitting, not on a lead and has
// been about for five seconds (ShoulderRidingEntity.RIDE_COOLDOWN) lands on
// its owner's shoulder when their boxes touch (LandOnOwnersShoulderGoal),
// provided the owner stands on the ground, out of water and powder snow, and
// rides nothing (ServerPlayer.setEntityOnShoulder): the left shoulder first,
// then the right. The parrot entity is discarded and its saved form rides
// with the player (the shoulder tag), shown to every client as the player's
// SHOULDER_PARROT_LEFT/RIGHT (the parrot's variant).
//
// It comes back as an entity at the player's side, 0.7 above their feet,
// owned by them (removeEntitiesOnShoulder), when the player falls more than
// half a block — a jump will do — is in water or powder snow, sleeps, is
// hurt, dies, turns spectator or starts a riptide. Never within a second of
// landing, though: a parrot that has just settled stays put.

const (
	shoulderRideCooldown = 100 // ShoulderRidingEntity.RIDE_COOLDOWN
	shoulderSettleTicks  = 20  // removeEntitiesOnShoulder: timeEntitySatOnShoulder + 20
	shoulderSoundOdds    = 200 // playShoulderEntityAmbientSound: random.nextInt(200)
	shoulderRespawnLift  = 0.7 // respawnEntityOnShoulder: getY() + 0.7F

	metaIndexShoulderLeft  = 19 // Player.DATA_SHOULDER_PARROT_LEFT
	metaIndexShoulderRight = 20 // Player.DATA_SHOULDER_PARROT_RIGHT
	metaTypeOptUInt        = 20 // OPTIONAL_UNSIGNED_INT, canonical numbering (the gateway renumbers)
)

// shoulderMeta is the player's two shoulder entries: each the parrot
// variant + 1, or 0 for an empty shoulder.
func shoulderMeta(t *tracked) []byte {
	b := protocol.AppendVarInt(nil, t.p.eid)
	for i, idx := range [2]byte{metaIndexShoulderLeft, metaIndexShoulderRight} {
		b = protocol.AppendU8(b, idx)
		b = protocol.AppendVarInt(b, metaTypeOptUInt)
		v := int32(0)
		if sm := t.shoulders[i]; sm != nil {
			v = shoulderVariant(sm) + 1
		}
		b = protocol.AppendVarInt(b, v)
	}
	return protocol.AppendU8(b, itemMetaEnd)
}

// shoulderVariant is the parrot's colour from its saved row (stored + 1).
func shoulderVariant(sm *savedMob) int32 {
	if sm.Variant > 0 {
		return sm.Variant - 1
	}
	return 0
}

// broadcastShoulders shows a player's shoulders to them and every player in
// their dimension (player entities are tracked dimension-wide).
func (h *hub) broadcastShoulders(players map[int32]*tracked, t *tracked) {
	ev := metaEv(shoulderMeta(t))
	for _, o := range players {
		if o.dim == t.dim {
			o.p.trySendEv(ev)
		}
	}
}

// shoulderOccupied reports whether anything rides either shoulder.
func (t *tracked) shoulderOccupied() bool { return t.shoulders[0] != nil || t.shoulders[1] != nil }

// canCarryOnShoulder is the owner half of LandOnOwnersShoulderGoal.canUse.
// Creative flight is not tracked here, but a flier is never on the ground,
// which setEntityOnShoulder asks for anyway.
func (h *hub) canCarryOnShoulder(t *tracked) bool {
	return t.gamemode != gmSpectator && !t.dead && !h.inWater(t.dim, t.x, t.y, t.z) && !h.inPowderSnow(t)
}

// inPowderSnow is Entity.isInPowderSnow for a player: the block at their feet.
func (h *hub) inPowderSnow(t *tracked) bool {
	w := h.worldFor(t.dim)
	return w != nil && isPowderSnow(w.At(int(math.Floor(t.x)), int(math.Floor(t.y)), int(math.Floor(t.z))))
}

// shoulderBound reports whether a parrot could take one of its owner's
// shoulders right now: LandOnOwnersShoulderGoal.canUse plus a free shoulder.
func (h *hub) shoulderBound(m *mob, t *tracked) bool {
	return m.etype == entityParrot && m.tamed && !m.sitting && m.leash == 0 &&
		h.tick.Load()-m.spawnTick > shoulderRideCooldown &&
		t.dim == m.dim && h.canCarryOnShoulder(t) && (t.shoulders[0] == nil || t.shoulders[1] == nil)
}

// parrotLandOnShoulder is LandOnOwnersShoulderGoal for one parrot. Reports
// whether it landed (and so no longer exists as an entity).
func (h *hub) parrotLandOnShoulder(players map[int32]*tracked, m *mob) bool {
	if m.etype != entityParrot || m.dying > 0 || m.mount != 0 {
		return false
	}
	t := players[m.owner]
	if t == nil || !h.shoulderBound(m, t) {
		return false
	}
	b := m.box()
	lo := [3]float64{m.x - b.w/2, m.y, m.z - b.w/2}
	hi := [3]float64{m.x + b.w/2, m.y + b.h, m.z + b.w/2}
	if !psBoxHits(lo, hi, t.x, t.y, t.z, psPlayerWidth, playerHeight(t)) {
		return false
	}
	// ServerPlayer.setEntityOnShoulder.
	if t.ridingEID != 0 || !t.onGround {
		return false
	}
	side := -1
	switch {
	case t.shoulders[0] == nil:
		side = 0
	case t.shoulders[1] == nil:
		side = 1
	default:
		return false
	}
	sm := toSavedMob(m)
	t.shoulders[side] = &sm
	t.shoulderAt = h.tick.Load()
	h.removeMob(players, m) // discard: the saved form rides the shoulder now
	h.broadcastShoulders(players, t)
	return true
}

// shoulderTick is ServerPlayer.handleShoulderEntities, every tick: the
// riders' chatter, and the conditions that knock them off.
func (h *hub) shoulderTick(players map[int32]*tracked) {
	for _, t := range players {
		if !t.shoulderOccupied() || t.dead {
			continue
		}
		for _, sm := range t.shoulders {
			if sm != nil && h.rng.Intn(shoulderSoundOdds) == 0 {
				h.shoulderChatter(players, t)
			}
		}
		falling := t.airborne && t.peakY-t.y > 0.5 // fallDistance > 0.5
		if falling || h.inWater(t.dim, t.x, t.y, t.z) || t.sleeping || h.inPowderSnow(t) {
			h.dropShoulderParrots(players, t)
		}
	}
}

// shoulderChatter is playShoulderEntityAmbientSound for a parrot: an
// imitation of a nearby mob, or else Parrot.getAmbient — a random mob's call
// one time in a thousand off peaceful, the parrot's own otherwise.
func (h *hub) shoulderChatter(players map[int32]*tracked, t *tracked) {
	if h.parrotImitateNearby(players, t.dim, t.x, t.y, t.z, sndPlayer) {
		return
	}
	sound := "minecraft:entity.parrot.ambient"
	if h.rules.Difficulty != diffPeaceful && h.rng.Intn(1000) == 0 {
		keys := parrotImitateKeys()
		sound = parrotImitates[keys[h.rng.Intn(len(keys))]]
	}
	h.playSoundDim(players, t.dim, sound, sndPlayer, t.x, t.y, t.z, 1, parrotPitch(h))
}

// parrotImitateKeys is MOB_SOUND_MAP's key set in a fixed order.
func parrotImitateKeys() []int {
	keys := make([]int, 0, len(parrotImitates))
	for k := range parrotImitates {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}

// dropShoulderParrots is removeEntitiesOnShoulder: both riders come back as
// entities beside the player, owned by them — unless they only just landed.
func (h *hub) dropShoulderParrots(players map[int32]*tracked, t *tracked) {
	if !t.shoulderOccupied() || t.shoulderAt+shoulderSettleTicks >= h.tick.Load() {
		return
	}
	for i, sm := range t.shoulders {
		if sm == nil {
			continue
		}
		t.shoulders[i] = nil
		row := *sm
		row.X, row.Y, row.Z, row.Dim = t.x, t.y+shoulderRespawnLift, t.z, t.dim
		row.Sitting = false
		prev := h.reloading
		h.reloading = true // the saved row is the parrot: no fresh spawn rolls
		m := h.reloadMob(players, &row)
		h.reloading = prev
		if m != nil {
			m.tamed, m.owner, m.ownerUUID = true, t.p.eid, t.p.uuid
			m.spawnTick = h.tick.Load() // a new entity: the ride cooldown starts again
		}
	}
	h.broadcastShoulders(players, t)
}
