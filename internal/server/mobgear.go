package server

import "math"

// Mob equipment: a hostile humanoid that spawned able to pick up loot will grab
// a dropped weapon or armour piece it walks over, wear/wield it (better gear
// wins), gain its benefit, and drop it when it dies. Reimplemented from the
// vanilla Mob.pickUpItem / EquipmentUser behaviour, simplified to armour +
// main-hand.

// equipCapable are the humanoid hostiles that use armour and weapons.
var equipCapable = map[int]bool{}

func init() {
	for _, e := range []int{entityZombie, entityHusk, entityDrowned, entitySkeleton,
		entityStray, entityWitherSkeleton, entityPiglin, entityZombifiedPiglin, entityPiglinBrute} {
		equipCapable[e] = true
	}
}

// rollCanPickup sets a spawn-time loot-pickup flag on equip-capable hostiles
// (vanilla ~0.55 × difficulty; heavier on hard).
func (h *hub) rollCanPickup(m *mob) {
	if !equipCapable[m.etype] {
		return
	}
	chance := 0.1
	if h.rules.Difficulty == diffHard {
		chance = 0.55
	}
	m.canPickup = h.rng.Float64() < chance
}

// mobPickupScan lets a mob grab one nearby dropped weapon/armour it can use.
func (h *hub) mobPickupScan(players map[int32]*tracked, m *mob) {
	if !m.canPickup || m.dim != 0 || m.baby {
		return
	}
	for eid, it := range h.items {
		if it.dim != m.dim || it.count <= 0 {
			continue
		}
		if math.Abs(it.x-m.x) > 1 || math.Abs(it.z-m.z) > 1 || math.Abs(it.y-m.y) > 1 {
			continue
		}
		if !h.mobEquipItem(players, m, it.item) {
			continue
		}
		if it.count--; it.count <= 0 { // took one
			delete(h.items, eid)
			h.toNearbyEv(players, it.dim, it.x, it.z, entGone(eid))
		}
		h.playSound(players, "minecraft:entity.item.pickup", sndNeutral, m.x, m.y, m.z, 0.2, 1)
		h.toNearbyEv(players, m.dim, m.x, m.z, equipEv(m.eid, invStack{item: m.held, count: b2i(m.held != 0)},
			invStack{}, m.gear))
		return // one pickup per scan
	}
}

// mobEquipItem equips a weapon (main hand) or armour piece if it's an upgrade,
// dropping whatever it replaces. Returns whether the item was taken.
func (h *hub) mobEquipItem(players map[int32]*tracked, m *mob, item int32) bool {
	if ap, ok := armorInfo[item]; ok {
		cur := m.gear[ap.Slot]
		oldPts := 0
		if cur.item != 0 {
			oldPts = armorInfo[cur.item].Points
			if oldPts >= ap.Points {
				return false // not an upgrade
			}
			h.spawnItem(players, cur.item, 1, m.x, m.y, m.z) // shed the old piece
		}
		m.gear[ap.Slot] = invStack{item: item, count: 1}
		m.armor += float64(ap.Points - oldPts) // worn armour feeds the ARMOR attribute (m.hurt)
		return true
	}
	if d, ok := meleeDamage[item]; ok { // a weapon/tool
		if m.held != 0 {
			if cur, ok := meleeDamage[m.held]; ok && cur >= d {
				return false
			}
			h.spawnItem(players, m.held, 1, m.x, m.y, m.z)
		}
		m.held = item
		return true
	}
	return false
}

// mobHeldBonus is the extra melee damage a mob's held weapon adds.
func mobHeldBonus(m *mob) float32 {
	if m.held != 0 {
		if d, ok := meleeDamage[m.held]; ok {
			return float32(d)
		}
	}
	return 0
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
