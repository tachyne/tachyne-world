package server

import "math"

// A piglin's spawn (Piglin.finalizeSpawn): outside a structure one in five
// is born a baby, and a grown one takes a weapon — a crossbow half the time,
// otherwise a golden sword, or one time in ten of those a golden spear.
// Grown piglins then wear each piece of gold armour one time in ten, and the
// usual spawn-equipment enchantment rolls follow. A bastion's piglins come
// from its template pieces, which put the crossbow or the sword in hand, so
// the structure spawn neither rolls a weapon nor makes a baby.

const (
	metaIndexPiglinBaby     = 17 // Piglin DATA_BABY_ID (AbstractPiglin's immune flag is 16)
	metaIndexPiglinCharging = 18 // Piglin DATA_IS_CHARGING_CROSSBOW
	piglinBabyChance        = 0.2
	piglinCrossbowChance    = 0.5
	piglinSpearOdds         = 10  // nextInt(10) == 0: a spear instead of the sword
	piglinArmourChance      = 0.1 // maybeWearArmor, per piece
	piglinBabySpeedBonus    = 0.2 // SPEED_MODIFIER_BABY, ADD_MULTIPLIED_BASE
	piglinCrossbowRange     = 8.0 // CrossbowItem.getDefaultProjectileRange
	piglinBackUpDist        = 5.0 // BackUpIfTooClose.create(5, 0.75)
	piglinBackUpSpeed       = 0.75
)

// piglinGoldArmour is the four pieces maybeWearArmor offers, head to feet
// (the mob's gear order).
var piglinGoldArmour = [4]int32{itemByName["golden_helmet"], itemByName["golden_chestplate"],
	itemByName["golden_leggings"], itemByName["golden_boots"]}

// piglinSpawnWeapon is Piglin.createSpawnWeapon.
func (h *hub) piglinSpawnWeapon() int32 {
	if h.rng.Float32() < piglinCrossbowChance {
		return itemCrossbow
	}
	if h.rng.Intn(piglinSpearOdds) == 0 {
		return itemGoldSpear
	}
	return itemGoldSword
}

// finalizePiglin runs Piglin.finalizeSpawn on a fresh piglin. A reload
// restores the saved piglin instead. structureHand is the weapon a bastion
// template put in its hand (0 = not a structure spawn).
func (h *hub) finalizePiglin(players map[int32]*tracked, m *mob, structure bool, structureHand int32) {
	if m == nil || m.etype != entityPiglin || h.reloading {
		return
	}
	if structure {
		m.held = structureHand
	} else if h.rng.Float32() < piglinBabyChance {
		h.setPiglinBaby(players, m, true)
	} else {
		m.held = h.piglinSpawnWeapon()
	}
	if !m.baby { // populateDefaultEquipmentSlots: adults only
		for slot, it := range piglinGoldArmour {
			if h.rng.Float32() < piglinArmourChance {
				m.gear[slot] = invStack{item: it, count: 1}
			}
		}
	}
	// populateDefaultEquipmentEnchantments: the weapon at 0.25×f, then each
	// armour piece at 0.5×f.
	f := h.specialMultiplier()
	if m.held != 0 && h.rng.Float64() < 0.25*f {
		cost := 5 + h.rng.Intn(int(math.Floor(f*17))+1)
		m.heldEnch = enchApplyList(enchSelect(h.rng, m.held, cost, enchMobAllowed))
	}
	for slot := range m.gear {
		if m.gear[slot].item != 0 && h.rng.Float64() < 0.5*f {
			cost := 5 + h.rng.Intn(int(math.Floor(f*17))+1)
			m.gear[slot].ench = enchApplyList(enchSelect(h.rng, m.gear[slot].item, cost, enchMobAllowed))
		}
	}
	m.refreshGearArmor()
	if m.wearsAnything() {
		h.toTracking(players, m.eid, m.dim, m.x, m.z, equipEv(m.eid, m.heldStack(), invStack{}, m.gear))
	}
}

// setPiglinBaby is Piglin.setBaby: its own flag (not the ageable one) and a
// +20% walking speed.
func (h *hub) setPiglinBaby(players map[int32]*tracked, m *mob, baby bool) {
	m.baby = baby
	m.setBabySpeed(baby)
	h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(mobBabyMeta(m, baby)))
}

// mobBabyMeta is the baby flag at the index the species keeps it: the
// piglin's sits after AbstractPiglin's field, everyone else's at 16.
func mobBabyMeta(m *mob, baby bool) []byte {
	if m.etype == entityPiglin {
		return boolMeta(m.eid, metaIndexPiglinBaby, baby)
	}
	return babyMeta(m.eid, baby)
}

// babySpeedBonus is the species' SPEED_MODIFIER_BABY amount: the zombie
// family's +50%, the piglin's +20%.
func babySpeedBonus(etype int) float64 {
	if etype == entityPiglin {
		return piglinBabySpeedBonus
	}
	return 0.5
}

// piglinCrossbowTick is a crossbow piglin's fight (PiglinAi's fight
// activity): BackUpIfTooClose steps it away inside five blocks, it stands
// once the target is in reach, and CrossbowAttack — only while the target
// is seen and within the crossbow's eight blocks — charges, holds 20-39
// ticks and fires (performCrossbowAttack at 1.6). It never melees.
func (h *hub) piglinCrossbowTick(players map[int32]*tracked, m *mob) {
	var t *tracked
	if !m.baby && m.admireUntil == 0 {
		t = h.nearestPiglinPrey(players, m, m.followRange())
	}
	if t == nil {
		if m.cbState == cbCharging {
			h.setCrossbowState(players, m, cbUncharged)
		}
		return
	}
	d := dist3(t.x, t.y, t.z, m.x, m.y, m.z)
	sees := h.mobSees(m, t)
	m.yaw = float32(math.Atan2(-(t.x-m.x), t.z-m.z) * 180 / math.Pi)
	if sees && d < piglinCrossbowRange-1 { // SetWalkTargetFromAttackTargetIfTargetOutOfReach: in reach, it stops
		m.vx, m.vz = 0, 0
		if d < piglinBackUpDist && d > 1e-6 { // BackUpIfTooClose: a step back
			s := m.moveSpeed() * piglinBackUpSpeed
			m.vx, m.vz = -(t.x-m.x)/d*s, -(t.z-m.z)/d*s
		}
	}
	if !sees || d >= piglinCrossbowRange { // CrossbowAttack stops: the draw is let go
		if m.cbState == cbCharging {
			h.setCrossbowState(players, m, cbUncharged)
		}
		return
	}
	switch m.cbState {
	case cbUncharged:
		h.setCrossbowState(players, m, cbCharging)
		m.cbTicks = 0
	case cbCharging:
		m.cbTicks += mobMoveInterval
		if m.cbTicks >= mobCrossbowCharge(m) {
			h.setCrossbowState(players, m, cbCharged)
			m.cbTicks = crossbowAimMin + h.rng.Intn(crossbowAimRandom)
		}
	case cbCharged:
		m.cbTicks -= mobMoveInterval
		if m.cbTicks <= 0 {
			m.cbTicks = 0
			h.setCrossbowState(players, m, cbReady)
		}
	case cbReady:
		h.spawnArrowAt(players, m, t.x, t.y+0.6, t.z)
		h.playSoundDim(players, m.dim, "minecraft:item.crossbow.shoot", sndHostile, m.x, m.y, m.z, 1, 1)
		h.setCrossbowState(players, m, cbUncharged)
	}
}
