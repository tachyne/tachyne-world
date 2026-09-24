package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The camel husk (26.3): an undead camel that arrives carrying a husk
// cavalryman and a parched archer. Husk.finalizeSpawn gives every husk that
// spawns naturally, where a camel husk's box fits, one chance in ten of
// coming mounted: it takes an iron spear, a camel husk is spawned under it
// and it rides in the front seat, driving; a parched is spawned into the
// back seat, where it rides and shoots. The rest of the camel husk is its
// Camel parent with the overrides: rabbit's feet for food (they only heal),
// never a calf, never in love, despawning like a monster until a player
// interacts with it, and neither leashed nor panicking while a mob holds
// the reins.

const (
	camelHuskJockeyOdds = 0.1 // Husk.finalizeSpawn: random.nextFloat() < 0.1F
	camelHuskCharge     = 4.0 // CamelHusk.chargeSpeedModifier
	zombieHorseCharge   = 1.4 // ZombieHorse.chargeSpeedModifier
)

// rollCamelHusk is Husk.finalizeSpawn's camel-husk roll, for a husk that has
// just spawned naturally.
func (h *hub) rollCamelHusk(players map[int32]*tracked, husk *mob) {
	if h.reloading || husk == nil || husk.dying > 0 || husk.mount != 0 {
		return
	}
	if !h.camelHuskFits(husk.dim, husk.x, husk.y, husk.z) || h.rng.Float64() >= camelHuskJockeyOdds {
		return
	}
	husk.held, husk.heldEnch = itemIronSpear, enchList{} // setItemSlot(MAINHAND, IRON_SPEAR): replaces whatever it rolled
	h.reassessWeapon(husk)
	h.toTracking(players, husk.eid, husk.dim, husk.x, husk.z, equipEv(husk.eid, husk.heldStack(), invStack{}, husk.gear))
	camel := h.spawnSpecies(players, entityCamelHusk, husk.dim, husk.x, husk.y, husk.z)
	if camel == nil {
		return
	}
	h.mountMobOn(players, husk, camel, true)
	if p := h.spawnHostileYIn(players, entityParched, husk.dim, husk.x, husk.y, husk.z); p != nil {
		h.mountMobOn(players, p, camel, false)
	}
}

// camelHuskFits is level.noCollision(CAMEL_HUSK.getSpawnAABB): the camel
// husk's 1.7 × 2.375 box, centred on the husk's block, touches no collider.
func (h *hub) camelHuskFits(dim int, x, y, z float64) bool {
	w := h.worldFor(dim)
	if w == nil {
		return false
	}
	box := mobBoxes[entityCamelHusk]
	cx, cz := math.Floor(x)+0.5, math.Floor(z)+0.5
	x0, x1 := int(math.Floor(cx-box.w/2)), int(math.Ceil(cx+box.w/2))-1
	z0, z1 := int(math.Floor(cz-box.w/2)), int(math.Ceil(cz+box.w/2))-1
	y0, y1 := int(math.Floor(y)), int(math.Ceil(y+box.h))-1
	for bx := x0; bx <= x1; bx++ {
		for bz := z0; bz <= z1; bz++ {
			for by := y0; by <= y1; by++ {
				if worldgen.Collides(w.At(bx, by, bz)) {
					return false
				}
			}
		}
	}
	return true
}

// chargeSpeedModifier is the speed factor a spear-wielding rider's charges
// take from its root vehicle (Mob.chargeSpeedModifier): four on a camel
// husk, 1.4 on a zombie horse, one on foot or on anything else.
func (h *hub) chargeSpeedModifier(m *mob) float64 {
	if m.mount == 0 {
		return 1
	}
	v := h.mobs[m.mount]
	for v != nil && v.mount != 0 { // getRootVehicle
		v = h.mobs[v.mount]
	}
	if v == nil {
		return 1
	}
	switch v.etype {
	case entityCamelHusk:
		return camelHuskCharge
	case entityZombieHorse:
		return zombieHorseCharge
	}
	return 1
}
