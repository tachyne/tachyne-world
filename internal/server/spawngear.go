package server

import (
	"math"
	"time"
)

// Spawn-time equipment (Mob.populateDefaultEquipmentSlots and its
// overrides, then populateDefaultEquipmentEnchantments). The regional
// difficulty's special multiplier gates it: with probability 0.15×f a
// humanoid hostile spawns in armour of one tier (leather, then gold,
// chain, iron, diamond — the tier rolls up three times at 9.5% each), the
// head first and each further piece kept only while a stop roll fails
// (25%, or 10% on hard). Zombies also have a 1% (hard 5%) chance of an
// iron sword (one in six), an iron spear (one in six) or a shovel;
// zombified piglins carry a golden sword, one in twenty a golden spear
// (zombifiedPiglinWeapon); skeletons and their kin carry a bow,
// wither skeletons a stone sword. Armour pieces are enchanted with
// probability 0.5×f each and the weapon at 0.25×f, at a cost of 5 plus up to
// 17×f, drawn from the on_mob_spawn_equipment set. What spawned on a mob drops at 8.5%.

const spawnGearDropChance = 0.085

// specialMultiplier is DifficultyInstance.getSpecialMultiplier: 0 below an
// effective difficulty of 2, 1 above 4, linear between.
func (h *hub) specialMultiplier() float64 {
	d := float64(h.effectiveDifficulty())
	switch {
	case d < 2:
		return 0
	case d > 4:
		return 1
	}
	return (d - 2) / 2
}

// armourTiers[tier][slot]: leather, golden, chainmail, iron, diamond for
// head, chest, legs, feet.
var armourTiers = [5][4]int32{}

func init() {
	for i, mat := range []string{"leather", "golden", "chainmail", "iron", "diamond"} {
		for j, part := range []string{"helmet", "chestplate", "leggings", "boots"} {
			armourTiers[i][j] = int32(itemByName[mat+"_"+part])
		}
	}
}

var (
	itemIronSword  = int32(itemByName["iron_sword"])
	itemIronShovel = int32(itemByName["iron_shovel"])
	itemIronSpear  = itemByName["iron_spear"]
	itemGoldSword  = itemByName["golden_sword"]
	itemGoldSpear  = itemByName["golden_spear"]
	itemStoneSword = int32(itemByName["stone_sword"])
)

// enchMobAllowed is the #on_mob_spawn_equipment set.
func enchMobAllowed(id int8) bool { return enchDefs[id].flags&enchOnMobEquipment != 0 }

// spawnGear dresses a freshly spawned hostile the way vanilla's
// finalizeSpawn does. Drowned keep their own trident roll (hostile2.go).
func (h *hub) spawnGear(players map[int32]*tracked, m *mob) {
	if !equipCapable[m.etype] || m.etype == entityDrowned {
		return
	}
	f := h.specialMultiplier()
	hard := h.rules.Difficulty == diffHard
	changed := false
	if h.rng.Float64() < 0.15*f {
		tier := h.rng.Intn(2)
		for i := 0; i < 3; i++ {
			if h.rng.Float64() < 0.095 {
				tier++
			}
		}
		stop := 0.25
		if hard {
			stop = 0.1
		}
		for slot := 0; slot < 4; slot++ {
			if slot > 0 && h.rng.Float64() < stop {
				break
			}
			if m.gear[slot].item != 0 {
				continue
			}
			m.gear[slot] = invStack{item: armourTiers[tier][slot], count: 1}
			changed = true
		}
	}
	switch m.etype {
	case entityZombie, entityHusk, entityZombieVillager:
		chance := 0.01
		if hard {
			chance = 0.05
		}
		if m.held == 0 && h.rng.Float64() < chance {
			switch h.rng.Intn(6) { // one in six a sword, one in six a spear, else a shovel
			case 0:
				m.held = itemIronSword
			case 1:
				m.held = itemIronSpear
			default:
				m.held = itemIronShovel
			}
			changed = true
		}
	case entitySkeleton, entityStray, entityBogged, entityParched:
		if m.held == 0 {
			m.held = itemBow
			changed = true
		}
	case entityWitherSkeleton:
		if m.held == 0 {
			m.held = itemStoneSword
			changed = true
		}
	}
	// Halloween (Zombie/AbstractSkeleton.finalizeSpawn): on the 31st of
	// October a bare head wears a carved pumpkin one time in four (a jack
	// o'lantern one in ten of those), and that head never drops.
	if m.gear[0].item == 0 && isHalloween(spawnClock()) && h.rng.Float64() < 0.25 {
		head := itemCarvedPumpkin
		if h.rng.Float64() < 0.1 {
			head = itemJackOLantern
		}
		m.gear[0] = invStack{item: head, count: 1}
		changed = true
	}
	if !changed {
		return
	}
	for slot := range m.gear {
		if m.gear[slot].item != 0 && h.rng.Float64() < 0.5*f {
			cost := 5 + h.rng.Intn(int(math.Floor(f*17))+1)
			m.gear[slot].ench = enchApplyList(enchSelect(h.rng, m.gear[slot].item, cost, enchMobAllowed))
		}
	}
	// enchantSpawnedWeapon: the main hand at 0.25×f, the same cost formula.
	if m.held != 0 && h.rng.Float64() < 0.25*f {
		cost := 5 + h.rng.Intn(int(math.Floor(f*17))+1)
		m.heldEnch = enchApplyList(enchSelect(h.rng, m.held, cost, enchMobAllowed))
	}
	m.spawnGear, m.gearDrop = true, spawnGearDropChance
	m.refreshGearArmor()
	h.reassessWeapon(m) // a skeleton that rolled a sword melees instead of shooting
	h.toTracking(players, m.eid, m.dim, m.x, m.z, equipEv(m.eid, m.heldStack(), invStack{}, m.gear))
}

// zombifiedPiglinWeapon is ZombifiedPiglin.populateDefaultEquipmentSlots: a
// golden sword, or one time in twenty a golden spear. Not on a reload (the
// saved weapon comes back instead) and not over something already held (a
// converted piglin keeps its own).
func (h *hub) zombifiedPiglinWeapon(players map[int32]*tracked, m *mob) {
	if h.reloading || m.held != 0 {
		return
	}
	m.held = itemGoldSword
	if h.rng.Intn(20) == 0 {
		m.held = itemGoldSpear
	}
	h.toTracking(players, m.eid, m.dim, m.x, m.z, mobEquip(m.eid, m.held))
}

// wearsAnything reports whether the mob shows any equipment.
func (m *mob) wearsAnything() bool {
	if m.held != 0 {
		return true
	}
	for _, g := range m.gear {
		if g.item != 0 {
			return true
		}
	}
	return false
}

// spawnClock is the wall clock finalizeSpawn reads for its calendar rules;
// a variable so tests can set the date.
var spawnClock = time.Now

// isHalloween is vanilla's date test: the 31st of October, server local time.
func isHalloween(now time.Time) bool {
	return now.Month() == time.October && now.Day() == 31
}

// isPumpkinHead reports the Halloween heads, which never drop.
func isPumpkinHead(item int32) bool { return item == itemCarvedPumpkin || item == itemJackOLantern }

// enchantPillagerCrossbow is Pillager.enchantSpawnedWeapon on the crossbow
// its finalizeSpawn hands it: Mob's roll first (mob_spawn_equipment at
// 0.25×f, the usual cost formula), then one spawn in 300 gets the
// pillager_spawn_crossbow provider's Piercing I on top (upgrade: an
// existing higher level stays).
func (h *hub) enchantPillagerCrossbow(m *mob) {
	f := h.specialMultiplier()
	if h.rng.Float64() < 0.25*f {
		cost := 5 + h.rng.Intn(int(math.Floor(f*17))+1)
		m.heldEnch = enchApplyList(enchSelect(h.rng, m.held, cost, enchMobAllowed))
	}
	if h.rng.Intn(300) == 0 && m.held == itemCrossbow {
		m.heldEnch = enchUpgrade(m.heldEnch, enchPiercing, 1)
	}
}
