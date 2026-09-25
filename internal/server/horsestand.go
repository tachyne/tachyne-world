package server

import "github.com/tachyne/tachyne-common/protocol"

// A horse rearing: AbstractHorse's STANDING flag. The client draws the rear
// from the flag, so until the engine sent it no horse ever reared — not idly,
// not bucking an untamed rider, not when angered.
//
//   - RandomStandGoal: every tick a counter climbs from minus the ambient
//     sound interval (80); past zero, a 1-in-1000-per-count roll resets it,
//     and one time in ten the horse rears with its ambient call.
//   - makeMad (horse.go) rears it too.
//   - A rear lasts 20 ticks (setStanding(20)); riding clears it.
// Llamas and camels never rear (canPerformRearing).

const (
	metaIndexHorseFlags = 17 // AbstractHorse DATA_ID_FLAGS (18 on 26.2+, the gateway shifts it)
	horseFlagTame       = 0x02
	horseFlagStanding   = 0x20
	horseStandTicks     = 20
	horseStandInterval  = 80 // getAmbientStandInterval = getAmbientSoundInterval
)

// horseCanRear is canPerformRearing.
func horseCanRear(etype int) bool {
	switch etype {
	case entityLlama, entityTraderLlama, entityCamel, entityCamelHusk:
		return false
	}
	return isEquine(etype)
}

// horseFlagsMeta is the AbstractHorse flags byte.
func horseFlagsMeta(m *mob) []byte {
	var flags byte
	if m.tamed {
		flags |= horseFlagTame
	}
	if m.standLeft > 0 {
		flags |= horseFlagStanding
	}
	b := protocol.AppendVarInt(nil, m.eid)
	b = protocol.AppendU8(b, metaIndexHorseFlags)
	b = protocol.AppendVarInt(b, 0) // type 0: byte
	b = protocol.AppendU8(b, flags)
	return protocol.AppendU8(b, itemMetaEnd)
}

// horseStand is standIfPossible: rear for 20 ticks.
func (h *hub) horseStand(players map[int32]*tracked, m *mob) {
	if !horseCanRear(m.etype) {
		return
	}
	m.standLeft = horseStandTicks
	h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(horseFlagsMeta(m)))
}

// horseStandTick runs every mob update for the horse family: the rear's
// countdown, and (unridden) RandomStandGoal's roll for each tick passed.
func (h *hub) horseStandTick(players map[int32]*tracked, m *mob) {
	if !horseCanRear(m.etype) {
		return
	}
	if m.standLeft > 0 {
		if m.standLeft -= mobMoveInterval; m.standLeft <= 0 {
			m.standLeft = 0
			h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(horseFlagsMeta(m)))
		}
		return
	}
	if m.rider != 0 || m.dying > 0 {
		return
	}
	for i := 0; i < mobMoveInterval; i++ {
		m.standNext++
		if m.standNext > 0 && h.rng.Intn(1000) < m.standNext {
			m.standNext = -horseStandInterval
			if h.rng.Intn(10) == 0 {
				h.horseStand(players, m)
				if _, _, amb := mobSounds(m.etype); amb != "" {
					h.playSoundDim(players, m.dim, amb, sndNeutral, m.x, m.y, m.z, 1, 1)
				}
				return
			}
		}
	}
}
