package server

import (
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Bats hang (Bat.customServerAiStep): a flying bat under a solid block
// settles one tick in a hundred and hangs there, upside down, until the
// block goes or a player comes within four blocks — then it drops with
// the takeoff flutter. Parrots mimic (Parrot.imitateNearbyMobs): one tick
// in four hundred, half the time, a parrot picks a mob within twenty and,
// if it is one of the monsters parrots know, squawks its call.

const (
	metaIndexBatFlags    = 16  // Bat DATA_ID_FLAGS (byte; a bat is not ageable): 1 = resting
	batRestOdds          = 100 // nextInt(100) == 0 under a solid block
	batWakeRange         = 4.0 // BAT_RESTING_TARGETING range
	worldEventBatTakeoff = 1025
	parrotImitateOdds    = 400 // nextInt(400) == 0, then nextInt(2) == 0
	parrotImitateRange   = 20.0
)

var parrotImitates = map[int]string{}

func init() {
	for _, n := range []string{"blaze", "bogged", "breeze", "cave_spider", "creaking", "creeper", "drowned", "elder_guardian",
		"ender_dragon", "endermite", "evoker", "ghast", "guardian", "hoglin", "husk", "illusioner", "magma_cube", "phantom",
		"piglin", "piglin_brute", "pillager", "ravager", "shulker", "silverfish", "skeleton", "slime", "spider", "stray",
		"vex", "vindicator", "warden", "witch", "wither", "wither_skeleton", "zoglin", "zombie"} {
		parrotImitates[entityID(n)] = "minecraft:entity.parrot.imitate." + n
	}
}

func batFlagsMeta(m *mob) []byte {
	var f byte
	if m.batResting {
		f = 1
	}
	b := protocol.AppendVarInt(nil, m.eid)
	b = protocol.AppendU8(b, metaIndexBatFlags)
	b = protocol.AppendVarInt(b, metaTypeByteFox)
	b = protocol.AppendU8(b, f)
	return protocol.AppendU8(b, itemMetaEnd)
}

func (h *hub) setBatResting(players map[int32]*tracked, m *mob, on bool) {
	if m.batResting == on {
		return
	}
	m.batResting = on
	h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(batFlagsMeta(m)))
}

// batStep runs each mob update. Returns whether it holds the bat.
func (h *hub) batStep(players map[int32]*tracked, m *mob) bool {
	w := h.worldFor(m.dim)
	bx, by, bz := int(math.Floor(m.x)), int(math.Floor(m.y)), int(math.Floor(m.z))
	solidAbove := worldgen.Collides(w.At(bx, by+1, bz))
	if m.batResting {
		wake := !solidAbove
		if !wake {
			if t := h.nearestPlayer(players, m.x, m.z, batWakeRange); t != nil && t.dim == m.dim && math.Abs(t.y-m.y) <= batWakeRange {
				wake = true
			}
		}
		if wake {
			h.setBatResting(players, m, false)
			h.toDimEv(players, m.dim, attachproto.WorldFX{Event: worldEventBatTakeoff, X: bx, Y: by, Z: bz})
			return false
		}
		m.vx, m.vy, m.vz = 0, 0, 0
		return true
	}
	if solidAbove && h.rng.Intn(batRestOdds/mobMoveInterval) == 0 {
		h.setBatResting(players, m, true)
		m.vx, m.vy, m.vz = 0, 0, 0
		return true
	}
	return false
}

// parrotImitateTick is the ambient mimicry.
func (h *hub) parrotImitateTick(players map[int32]*tracked, m *mob) bool {
	if h.rng.Intn(parrotImitateOdds/mobMoveInterval) != 0 || h.rng.Intn(2) != 0 {
		return false
	}
	var pick *mob
	n := 0
	h.grid().nearby(m.dim, m.x, m.z, parrotImitateRange, func(o *mob) {
		if o == m || o.etype == entityParrot || o.dying > 0 || math.Abs(o.y-m.y) > parrotImitateRange {
			return
		}
		n++
		if h.rng.Intn(n) == 0 {
			pick = o
		}
	})
	if pick == nil {
		return false
	}
	sound, ok := parrotImitates[pick.etype]
	if !ok {
		return false
	}
	h.playSoundDim(players, m.dim, sound, sndNeutral, m.x, m.y, m.z, 0.7, (h.rng.Float32()-h.rng.Float32())*0.2+1)
	return true
}
