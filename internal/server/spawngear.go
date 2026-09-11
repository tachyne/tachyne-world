package server

import "math"

// Spawn-time equipment (Mob.populateDefaultEquipmentSlots and its
// overrides, then populateDefaultEquipmentEnchantments). The regional
// difficulty's special multiplier gates it: with probability 0.15×f a
// humanoid hostile spawns in armour of one tier (leather, then gold,
// chain, iron, diamond — the tier rolls up three times at 9.5% each), the
// head first and each further piece kept only while a stop roll fails
// (25%, or 10% on hard). Zombies also have a 1% (hard 5%) chance of an
// iron sword (one in three) or shovel; skeletons and their kin carry a bow,
// wither skeletons a stone sword. Armour pieces are enchanted with
// probability 0.5×f each (the weapon's 0.25×f has nowhere to live yet — a
// mob's held item is a bare id) at a cost of 5 plus up to 17×f, drawn from
// the on_mob_spawn_equipment set. What spawned on a mob drops at 8.5%.

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
			if h.rng.Intn(3) == 0 {
				m.held = itemIronSword
			} else {
				m.held = itemIronShovel
			}
			changed = true
		}
	case entitySkeleton, entityStray, entityBogged:
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
	if !changed {
		return
	}
	for slot := range m.gear {
		if m.gear[slot].item != 0 && h.rng.Float64() < 0.5*f {
			cost := 5 + h.rng.Intn(int(math.Floor(f*17))+1)
			m.gear[slot].ench = enchApplyList(enchSelect(h.rng, m.gear[slot].item, cost, enchMobAllowed))
		}
	}
	m.spawnGear, m.gearDrop = true, spawnGearDropChance
	m.refreshGearArmor()
	h.toNearbyEv(players, m.dim, m.x, m.z, equipEv(m.eid, invStack{item: m.held, count: b2i(m.held != 0)}, invStack{}, m.gear))
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
