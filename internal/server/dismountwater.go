package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// entity_type/dismounts_underwater (LivingEntity.baseTick): a rider whose
// eyes are in water — not a bubble column — is thrown off a vehicle in the
// tag, every tick it is so. Mobs riding mobs (a jockey on a chicken or a
// spider) go the same way as players.
var dismountsUnderwater = func() map[int]bool {
	m := map[int]bool{}
	for _, n := range []string{"camel", "chicken", "donkey", "happy_ghast", "horse", "llama", "mule",
		"pig", "ravager", "spider", "strider", "trader_llama", "zombie_horse"} {
		if id, ok := entityByName[n]; ok {
			m[id] = true
		}
	}
	return m
}()

// eyesInWater is isEyeInFluid(WATER) without a bubble column at the eyes.
func (h *hub) eyesInWater(dim int, x, eyeY, z float64) bool {
	w := h.worldFor(dim)
	if w == nil {
		return false
	}
	s := w.At(int(math.Floor(x)), int(math.Floor(eyeY)), int(math.Floor(z)))
	return worldgen.HoldsWater(s) && !worldgen.IsBubbleColumn(s)
}

// dismountUnderwater runs the rule for every rider, once a tick.
func (h *hub) dismountUnderwater(players map[int32]*tracked) {
	for _, t := range players {
		if t.ridingEID == 0 || t.dead {
			continue
		}
		v := h.mobs[t.ridingEID]
		if v == nil || !dismountsUnderwater[v.etype] || !h.eyesInWater(t.dim, t.x, t.y+t.eyeHeight(), t.z) {
			continue
		}
		if !h.leaveGhast(players, t) {
			h.dismountMob(players, t)
		}
	}
	for _, m := range h.mobs {
		if m.mount == 0 || m.dying > 0 {
			continue
		}
		v := h.mobs[m.mount]
		if v == nil || !dismountsUnderwater[v.etype] || !h.eyesInWater(m.dim, m.x, m.y+m.box().h*0.85, m.z) {
			continue
		}
		h.unseatMob(players, m)
	}
}
