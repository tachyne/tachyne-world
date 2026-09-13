package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// Sheep graze (EatBlockGoal + Sheep.ate): one tick in a thousand (fifty
// for a lamb) a sheep standing on a grass block, or in short grass, lowers
// its head for forty ticks and eats — the grass at its feet gone, or the
// grass block under it turned to dirt — growing its wool back and, for a
// lamb, growing up a little. Striders shiver (Strider.tick setSuffocating):
// off lava a strider goes purple and shakes, walking a third slower.

const (
	grazeOddsAdult       = 1000 // EatBlockGoal: nextInt(1000) == 0
	grazeOddsBaby        = 50
	grazeTicks           = 40    // eatAnimationTick
	grazeBiteAt          = 4     // the bite lands with four ticks left
	grazeBabyAge         = 60    // Sheep.ate: ageUp(60)
	entityStatusEat      = 10    // the head-down animation
	metaIndexStriderCold = 18    // Strider DATA_SUFFOCATING (1.21.5; 19 on 26.2)
	striderColdSlow      = -0.34 // SUFFOCATING_MODIFIER (ADD_MULTIPLIED_BASE)
	striderColdSource    = "suffocating"
)

// grazeStep is EatBlockGoal for a sheep. Returns whether it holds the mob.
func (h *hub) grazeStep(players map[int32]*tracked, m *mob) bool {
	w := h.worldFor(m.dim)
	bx, by, bz := int(math.Floor(m.x)), int(math.Floor(m.y)), int(math.Floor(m.z))
	if m.grazeTicks == 0 {
		odds := grazeOddsAdult
		if m.baby {
			odds = grazeOddsBaby
		}
		if h.rng.Intn(odds/mobMoveInterval) != 0 || m.panic > 0 || m.loveTicks > 0 {
			return false
		}
		if w.At(bx, by, bz) != worldgen.ShortGrass && !isGrassBlock(w.At(bx, by-1, bz)) {
			return false
		}
		m.grazeTicks = grazeTicks
		h.toNearbyEv(players, m.dim, m.x, m.z, entityStatus(m.eid, entityStatusEat))
		m.vx, m.vz = 0, 0
		return true
	}
	was := m.grazeTicks
	m.grazeTicks -= mobMoveInterval
	if m.grazeTicks < 0 {
		m.grazeTicks = 0
	}
	if was > grazeBiteAt && m.grazeTicks <= grazeBiteAt { // the bite
		ate := false
		if w.At(bx, by, bz) == worldgen.ShortGrass {
			h.setBlockAt(players, m.dim, blockPos{bx, by, bz}, worldgen.Air) // destroyBlock(pos, false)
			ate = true
		} else if s := w.At(bx, by-1, bz); isGrassBlock(s) && h.rules.MobGriefing {
			h.toNearbyEv(players, m.dim, m.x, m.z, blockBreakEvent(bx, by-1, bz, s))
			h.setBlockAt(players, m.dim, blockPos{bx, by - 1, bz}, worldgen.Dirt)
			ate = true
		} else if isGrassBlock(s) {
			ate = true // no griefing: it still eats (vanilla only skips the block change)
		}
		if ate {
			h.sheepAte(players, m)
		}
	}
	m.vx, m.vz = 0, 0
	return m.grazeTicks > 0
}

func isGrassBlock(s uint32) bool {
	lo, hi := worldgen.BlockRange("grass_block")
	return s >= lo && s <= hi
}

// sheepAte is Sheep.ate: the wool grows back, a lamb grows a little.
func (h *hub) sheepAte(players map[int32]*tracked, m *mob) {
	if m.sheared {
		m.sheared = false
		h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(sheepMeta(m, false)))
	}
	if m.baby {
		h.ageUp(m, grazeBabyAge)
	}
}

func striderColdMeta(m *mob) []byte { return boolMeta(m.eid, metaIndexStriderCold, m.striderCold) }

// striderShiverTick is Strider.tick's setSuffocating: warm on lava (or
// carried by a strider that is), cold anywhere else.
func (h *hub) striderShiverTick(players map[int32]*tracked, m *mob) {
	w := h.worldFor(m.dim)
	bx, by, bz := int(math.Floor(m.x)), int(math.Floor(m.y)), int(math.Floor(m.z))
	warm := worldgen.IsLava(w.At(bx, by, bz)) || worldgen.IsLava(w.At(bx, by-1, bz))
	cold := !warm
	if cold == m.striderCold {
		return
	}
	m.striderCold = cold
	in := m.mobAttrs().Get(attr.MovementSpeed)
	in.RemoveModifier(striderColdSource)
	if cold {
		in.AddModifier(attr.Modifier{Source: striderColdSource, Amount: striderColdSlow, Op: attr.AddMultipliedBase})
	}
	h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(striderColdMeta(m)))
}
