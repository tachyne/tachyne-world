package server

import (
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Zombie villagers, both ways: a zombie that kills a villager turns it (the
// villager's profession, tier, trades and reputation ride along), and a
// zombie villager fed a golden apple under Weakness turns back — vanilla's
// Zombie.killedEntity / convertVillagerToZombieVillager and
// ZombieVillager.mobInteract / startConverting / finishConversion.

const (
	cureTimeMin        = 3600 // ticks, plus up to 2400 more
	cureTimeSpan       = 2401
	cureSpecialMax     = 14 // iron bars / beds counted around a converting zombie
	cureSpecialRange   = 4
	cureNauseaSecs     = 10
	cureStatusEvent    = 16   // ZombieVillager entity event: the cure begins (sound + shaking)
	worldEventInfect   = 1026 // level events: zombie infects, villager cured
	worldEventCured    = 1027
	cureMajorGossip    = 20 * 5 // MAJOR_POSITIVE 20 at weight 5…
	cureMinorGossip    = 25     // …plus MINOR_POSITIVE 25 at weight 1
	zombieVillagerHunt = 16.0   // how far a zombie looks for a villager (no player near)
)

// zombieKind: the zombies that hunt villagers.
func zombieKind(etype int) bool {
	return etype == entityZombie || etype == entityHusk || etype == entityDrowned || etype == entityZombieVillager
}

// nearestVillager finds the closest live villager (never an NPC) within r.
func (h *hub) nearestVillager(m *mob, r float64) *mob {
	var best *mob
	bestD := r
	h.grid().nearby(m.dim, m.x, m.z, r, func(c *mob) {
		if c.etype != entityVillager || c.dying > 0 || c.dim != m.dim {
			return
		}
		if h.npcs != nil && h.npcs[c.eid] != nil {
			return
		}
		if d := dist3(c.x, c.y, c.z, m.x, m.y, m.z); d < bestD {
			best, bestD = c, d
		}
	})
	return best
}

// zombieBitesVillager is the mob-vs-mob bite: a zombie with a villager
// target in reach hits it; a killing bite infects on Normal (half the time)
// and Hard (always), and simply kills on Easy.
func (h *hub) zombieBitesVillager(players map[int32]*tracked, m *mob) bool {
	if m.villagerTarget == 0 {
		return false
	}
	v := h.mobs[m.villagerTarget]
	if v == nil || v.dying > 0 || v.etype != entityVillager {
		m.villagerTarget = 0
		return false
	}
	if dist3(v.x, v.y, v.z, m.x, m.y, m.z) > attackReach+0.5 {
		return false
	}
	m.attackCD = attackCooldown
	h.toNearbyEv(players, m.dim, m.x, m.z, swingArm(m.eid))
	v.hurt(float64((hostileMelee(m) + mobHeldBonus(m)) * h.diffMult()))
	h.mobKnockFrom(players, v, m.x, m.z)
	if v.health > 0 {
		v.panic = 60 // the bitten villager runs
		return true
	}
	switch h.rules.Difficulty {
	case diffHard:
		h.infectVillager(players, m, v)
	case diffNormal:
		if h.rng.Intn(2) == 0 {
			h.infectVillager(players, m, v)
		} else {
			h.killMob(players, v)
		}
	default:
		h.killMob(players, v)
	}
	m.villagerTarget = 0
	return true
}

// mobKnockFrom shoves a mob away from a point (a bite's knockback).
func (h *hub) mobKnockFrom(players map[int32]*tracked, v *mob, fx, fz float64) {
	if dx, dz := v.x-fx, v.z-fz; dx != 0 || dz != 0 {
		d := math.Hypot(dx, dz)
		v.vx, v.vz = dx/d*0.4, dz/d*0.4
		v.kb = 2
		h.mobKnockVelocity(players, v)
	}
}

// infectVillager turns a villager into a zombie villager on the spot,
// carrying its identity (convertVillagerToZombieVillager).
func (h *hub) infectVillager(players map[int32]*tracked, zombie, v *mob) {
	zv := h.spawnMobIn(players, entityZombieVillager, v.dim, v.x, v.y, v.z)
	if zv == nil {
		h.killMob(players, v)
		return
	}
	copyVillagerIdentity(v, zv)
	zv.persistent = true
	h.removeMob(players, v)
	h.sendVillagerData(players, zv)
	h.toNearbyEv(players, zv.dim, zv.x, zv.z, attachproto.WorldFX{Event: worldEventInfect, X: floorInt(zv.x), Y: floorInt(zv.y), Z: floorInt(zv.z)})
}

// copyVillagerIdentity carries VillagerData, trades, XP, gossip and youth
// from one form to the other.
func copyVillagerIdentity(from, to *mob) {
	to.profession, to.tradeLevel, to.tradeXP = from.profession, from.tradeLevel, from.tradeXP
	to.offers = append([]mobOffer(nil), from.offers...)
	to.variant, to.variantSet = from.variant, from.variantSet
	to.baby, to.growLeft = from.baby, from.growLeft
	to.refreshBabySpeed()
	to.customName = from.customName
	to.gossip, to.cureRep = from.gossip, from.cureRep
	to.home, to.bed, to.work, to.meet = from.home, from.bed, from.work, from.meet
}

// cureZombieVillager is ZombieVillager.mobInteract: a golden apple on a
// weakened zombie villager starts the cure. Handled (true) whenever the
// held item is a golden apple, as vanilla consumes the interaction.
func (h *hub) cureZombieVillager(players map[int32]*tracked, t *tracked, m *mob) bool {
	if m.etype != entityZombieVillager || t.inv == nil {
		return false
	}
	slot := t.p.heldSlot()
	st := &t.inv.slots[slot]
	if st.count == 0 || st.item != itemByName["golden_apple"] {
		return false
	}
	if m.hasEffect(effWeakness) == 0 || m.converting > 0 {
		return true
	}
	if t.gamemode == gmSurvival {
		st.count--
		if st.count == 0 {
			*st = invStack{}
		}
		h.sendSlot(t, slot)
	}
	h.startCure(players, m, t.p.name, cureTimeMin+h.rng.Intn(cureTimeSpan))
	return true
}

// startCure is startConverting: the fuse, Weakness swapped for Strength for
// its length, the shaking flag synced, the cure sound from the entity event.
func (h *hub) startCure(players map[int32]*tracked, m *mob, curer string, ticks int) {
	m.converting, m.curer = ticks, curer
	m.persistent = true
	h.removeMobEffect(players, m, effWeakness)
	h.applyMobEffect(players, m, effStrength, 0, ticks/20)
	h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(zombieConvertingMeta(m.eid, true)))
	h.toNearbyEv(players, m.dim, m.x, m.z, attachproto.EntityStatus{EID: m.eid, Status: cureStatusEvent})
}

// zombieConvertingMeta is ZombieVillager's DATA_CONVERTING_ID (index 19).
func zombieConvertingMeta(eid int32, on bool) []byte {
	b := protocol.AppendVarInt(nil, eid)
	b = protocol.AppendU8(b, metaIndexZombieConverting)
	b = protocol.AppendVarInt(b, metaTypeBool)
	b = protocol.AppendBool(b, on)
	return protocol.AppendU8(b, itemMetaEnd)
}

// tickCure advances a converting zombie villager by one survival step (20
// ticks): a tick each, plus getConversionProgress's bonus — each tick has a
// 1% chance to count the iron bars and beds within four blocks (up to 14),
// every one of them adding a tick 30% of the time.
func (h *hub) tickCure(players map[int32]*tracked, m *mob) {
	if m.converting <= 0 {
		return
	}
	progress := survivalTickN
	for i := 0; i < survivalTickN; i++ {
		if h.rng.Float64() >= 0.01 {
			continue
		}
		w := h.worldFor(m.dim)
		if w == nil {
			continue
		}
		bx, by, bz := floorInt(m.x), floorInt(m.y), floorInt(m.z)
		count := 0
		for x := bx - cureSpecialRange; x < bx+cureSpecialRange && count < cureSpecialMax; x++ {
			for y := by - cureSpecialRange; y < by+cureSpecialRange && count < cureSpecialMax; y++ {
				for z := bz - cureSpecialRange; z < bz+cureSpecialRange && count < cureSpecialMax; z++ {
					if s := w.At(x, y, z); isIronBars(s) || isBedBlock(s) {
						if h.rng.Float64() < 0.3 {
							progress++
						}
						count++
					}
				}
			}
		}
	}
	m.converting -= progress
	if m.converting <= 0 {
		h.finishCure(players, m)
	}
}

var (
	ironBarsLo, ironBarsHi = worldgen.BlockRange("iron_bars")
)

func isIronBars(s uint32) bool { return s >= ironBarsLo && s <= ironBarsHi }

func isBedBlock(s uint32) bool {
	info, ok := worldgen.InfoForState(s)
	return ok && isBed(info)
}

// finishCure is finishConversion: the villager returns with everything the
// zombie kept, queasy for ten seconds, and the curer earns its gratitude
// (MAJOR_POSITIVE + MINOR_POSITIVE gossip: a steep discount).
func (h *hub) finishCure(players map[int32]*tracked, m *mob) {
	m.converting = 0
	v := h.spawnMobIn(players, entityVillager, m.dim, m.x, m.y, m.z)
	if v == nil {
		return
	}
	copyVillagerIdentity(m, v)
	if len(v.offers) == 0 { // a natural zombie villager: fresh trades for its profession
		h.initVillagerTrades(v, v.profession)
	}
	v.setMoveSpeed(0.135)
	if v.home == (blockPos{}) {
		v.home = blockPos{floorInt(v.x), floorInt(v.y), floorInt(v.z)}
	}
	v.behavior = villagerBehavior{}
	v.usesDoors = true
	if m.curer != "" {
		if v.cureRep == nil {
			v.cureRep = map[string]int{}
		}
		v.cureRep[m.curer] += cureMajorGossip + cureMinorGossip
		for _, t := range players {
			if t.p.name == m.curer {
				h.advance(players, t, "cured_zombie_villager", advMatch{})
			}
		}
	}
	h.removeMob(players, m)
	h.applyMobEffect(players, v, effNausea, 0, cureNauseaSecs)
	h.sendVillagerData(players, v)
	h.toNearbyEv(players, v.dim, v.x, v.z, attachproto.WorldFX{Event: worldEventCured, X: floorInt(v.x), Y: floorInt(v.y), Z: floorInt(v.z)})
}

// villagerFlee: a villager within eight blocks of a zombie runs from it
// (VillagerPanicTrigger / the zombie-avoidance walk target).
func (h *hub) villagerFlee(m *mob) (float64, float64, bool) {
	var threat *mob
	best := 8.0
	h.grid().nearby(m.dim, m.x, m.z, 8, func(c *mob) {
		if !zombieKind(c.etype) || c.dying > 0 {
			return
		}
		if d := dist3(c.x, c.y, c.z, m.x, m.y, m.z); d < best {
			threat, best = c, d
		}
	})
	if threat == nil {
		return 0, 0, false
	}
	dx, dz := m.x-threat.x, m.z-threat.z
	if d := math.Hypot(dx, dz); d > 1e-6 {
		return dx / d * m.moveSpeed(), dz / d * m.moveSpeed(), true
	}
	return m.moveSpeed(), 0, true
}
