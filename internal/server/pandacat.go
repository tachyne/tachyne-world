package server

// Baby pandas sneeze (vanilla PandaSneezeGoal + Panda.afterSneeze): a cub
// starts a sneeze about once in six thousand ticks, the sneeze lands twenty
// ticks after the wind-up, and one sneeze in seven hundred leaves a slime
// ball. Cats bring gifts (CatRelaxOnOwnerGoal): a tamed cat that is not
// told to sit, near an owner who slept the night through, appears beside
// them at sunrise and, seven times in ten, drops a small present.

const (
	pandaSneezeOdds   = 6000 // ticks between sneezes, on average
	pandaSneezeWindup = 20
	pandaSneezeGift   = 700 // one sneeze in this many drops a slime ball
	catGiftMinSleep   = 100 // the owner's sleep timer must have reached this
	catGiftRange      = 16.0
	catGiftChance     = 0.7
)

// catGifts is the cat_morning_gift table: weight 10 each, the membrane 2.
var catGifts = []struct {
	item   string
	weight int
}{
	{"rabbit_hide", 10}, {"rabbit_foot", 10}, {"chicken", 10}, {"feather", 10},
	{"rotten_flesh", 10}, {"string", 10}, {"phantom_membrane", 2},
}

func (h *hub) pandaSneezeTick(players map[int32]*tracked, m *mob) {
	now := h.tick.Load()
	if m.sneezeAt == 0 {
		if h.rng.Intn(pandaSneezeOdds/mobMoveInterval) == 0 {
			m.sneezeAt = now + pandaSneezeWindup
			h.playSoundDim(players, m.dim, "minecraft:entity.panda.pre_sneeze", sndNeutral, m.x, m.y, m.z, 1, 1)
		}
		return
	}
	if now < m.sneezeAt {
		return
	}
	m.sneezeAt = 0
	h.playSoundDim(players, m.dim, "minecraft:entity.panda.sneeze", sndNeutral, m.x, m.y, m.z, 1, 1)
	if h.rules.DoMobLoot && h.rng.Intn(pandaSneezeGift) == 0 {
		h.spawnItemIn(players, m.dim, itemSlimeball, 1, m.x, m.y+0.5, m.z)
	}
}

// catMorningGifts runs when a player wakes at sunrise after a full sleep.
func (h *hub) catMorningGifts(players map[int32]*tracked, t *tracked) {
	total := 0
	for _, g := range catGifts {
		total += g.weight
	}
	for _, m := range h.mobs {
		if m.etype != entityCat || !m.tamed || m.sitting || m.owner != t.p.eid || m.dim != t.dim || m.dying > 0 {
			continue
		}
		if dist3(m.x, m.y, m.z, t.x, t.y, t.z) > catGiftRange {
			continue
		}
		if h.rng.Float64() >= catGiftChance {
			continue
		}
		// The cat turns up beside the bed…
		m.x, m.z = t.x+float64(h.rng.Intn(3)-1), t.z+float64(h.rng.Intn(3)-1)
		m.y = t.y
		m.sx, m.sy, m.sz = m.x, m.y, m.z
		h.toNearbyEv(players, m.dim, m.x, m.z, entMove(m.eid, m.x, m.y, m.z, m.yaw, 0, m.grounded()))
		// …with something from the table.
		roll := h.rng.Intn(total)
		for _, g := range catGifts {
			if roll -= g.weight; roll < 0 {
				h.spawnItemIn(players, m.dim, int32(itemByName[g.item]), 1, m.x, m.y+0.5, m.z)
				break
			}
		}
	}
}
