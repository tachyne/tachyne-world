package server

import (
	"math"
	"math/rand"
)

// Gifts for the Hero of the Village (GiveGiftToHero): a villager that can
// see a hero walks up to within five blocks and throws it something from its
// profession's gift table, then waits 30 s to 5½ min before the next.

// giftTableFor is GiveGiftToHero.getLootTableToThrow.
func giftTableFor(m *mob) string {
	if m.baby {
		return "gameplay/hero_of_the_village/baby_gift"
	}
	if m.profession >= 0 && m.profession < len(professionNames) {
		return "gameplay/hero_of_the_village/" + professionNames[m.profession] + "_gift"
	}
	return "gameplay/hero_of_the_village/unemployed_gift"
}

// nearestHero finds the closest player with Hero of the Village within r.
func (h *hub) nearestHero(players map[int32]*tracked, m *mob, r float64) *tracked {
	var best *tracked
	bestD := r
	for _, t := range players {
		if t.dim != m.dim || t.dead || t.hasEffect(effHeroOfVillage) == 0 {
			continue
		}
		if d := dist3(t.x, t.y, t.z, m.x, m.y, m.z); d < bestD {
			best, bestD = t, d
		}
	}
	return best
}

// villagerGiftSteer moves a villager toward a hero it owes a gift; returns
// the steering and whether it took over.
func (h *hub) villagerGiftSteer(players map[int32]*tracked, m *mob) (float64, float64, bool) {
	if h.tick.Load() < m.giftAt {
		return 0, 0, false
	}
	hero := h.nearestHero(players, m, giftSeekRange)
	if hero == nil {
		return 0, 0, false
	}
	if dist3(hero.x, hero.y, hero.z, m.x, m.y, m.z) <= giftThrowDistance {
		h.throwGift(players, m, hero)
		return 0, 0, true
	}
	vx, vz := h.pathSteer(m, hero.x, hero.z)
	return vx * 0.5, vz * 0.5, true // SPEED_MODIFIER 0.5
}

// throwGift rolls the gift table and tosses the stacks toward the hero.
func (h *hub) throwGift(players map[int32]*tracked, m *mob, hero *tracked) {
	m.giftAt = h.tick.Load() + giftGapMin + uint64(h.rng.Intn(giftGapSpan))
	tbl, ok := lootForChest(giftTableFor(m))
	if !ok {
		return
	}
	r := rand.New(rand.NewSource(h.rng.Int63()))
	ctx := &lootCtx{rng: r.Intn, randf: r.Float64}
	dx, dz := hero.x-m.x, hero.z-m.z
	if d := math.Hypot(dx, dz); d > 1e-6 {
		dx, dz = dx/d, dz/d
	}
	for _, st := range h.evalChestStacks(tbl, ctx, 0) {
		if st.item == 0 || st.count <= 0 {
			continue
		}
		if it := h.spawnItemIn(players, m.dim, st.item, st.count, m.x+dx, m.y+1, m.z+dz); it != nil {
			it.dmg, it.ench, it.potion = st.dmg, st.ench, st.potion
			h.refreshItemMeta(players, it)
		}
	}
	h.playSoundDim(players, m.dim, "minecraft:entity.villager.celebrate", sndNeutral, m.x, m.y, m.z, 1, 1)
}
