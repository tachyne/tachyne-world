package server

import "math"

// Mob equipment. A hostile that spawned able to pick up loot (Zombie and
// AbstractSkeleton finalizeSpawn: 0.55 × the special multiplier) grabs what
// it walks over and, when it is better, wears or wields it, the way vanilla's
// Mob.pickUpItem → equipItemIfPossible does: armour goes to its slot if it
// beats what is there (points, then toughness, then more enchantments, less
// wear, a name), otherwise into an empty hand; a weapon goes to the hand if
// it beats what is held — a skeleton keeps a bow over any sword and a drowned
// a trident, the preferred weapon first, then attack damage, then the same
// tie-breaks — and an empty hand takes anything at all, a whole stack of it.
// Whatever it replaces drops at max(r − 0.1, 0) < that slot's drop chance
// (8.5 % for spawn gear, always for something picked up earlier), and what it
// picked up is a guaranteed drop on death; a looter never despawns.

// equipCapable are the humanoid hostiles that use armour and weapons.
var equipCapable = map[int]bool{}

func init() {
	for _, e := range []int{entityZombie, entityHusk, entityDrowned, entitySkeleton,
		entityStray, entityBogged, entityParched, entityWitherSkeleton, entityPiglin, entityZombifiedPiglin, entityPiglinBrute} {
		equipCapable[e] = true
	}
}

const (
	gearSlotHand     = 4                   // index of the hand in gearSure (0-3 are the armour slots)
	defaultGearDrop  = spawnGearDropChance // DropChances.DEFAULT: 8.5 % per slot
	replaceDropFudge = 0.1                 // equipItemIfPossible: max(r − 0.1, 0) < chance
	pickupReachXZ    = 1.0                 // Mob.getPickupReach: the box inflated 1 sideways
	pickupReachY     = 1.0                 // …and the box's own height (an item on its head counts)
)

// rollCanPickup sets a spawn-time loot-pickup flag on equip-capable hostiles
// (vanilla ~0.55 × difficulty; heavier on hard).
func (h *hub) rollCanPickup(m *mob) {
	if !equipCapable[m.etype] {
		return
	}
	m.canPickup = h.rng.Float64() < 0.55*h.specialMultiplier() // Zombie.finalizeSpawn: 0.55 × the special multiplier
}

// preferredWeapon is getPreferredWeaponType: the item a species holds over
// anything else (0 = no preference).
func preferredWeapon(etype int) int32 {
	switch etype {
	case entitySkeleton, entityStray, entityBogged, entityParched: // SKELETON_PREFERRED_WEAPONS
		return int32(itemByName["bow"])
	case entityDrowned: // DROWNED_PREFERRED_WEAPONS
		return int32(itemByName["trident"])
	case entityPiglin: // PIGLIN_PREFERRED_WEAPONS
		return int32(itemByName["crossbow"])
	}
	return 0
}

// mobWantsToPickUp is wantsToPickUp: everything, bar a zombie's glow ink sac.
func mobWantsToPickUp(m *mob, item int32) bool {
	if zombieKind(m.etype) && item == int32(itemGlowInkSac) {
		return false
	}
	return true
}

// mobPickupScan is Mob.aiStep's looting: a looter takes what lies within
// reach, one item entity per scan.
func (h *hub) mobPickupScan(players map[int32]*tracked, m *mob) {
	if !m.canPickup || m.dying > 0 || !h.rules.MobGriefing {
		return
	}
	now := h.tick.Load()
	half := m.box().w/2 + pickupReachXZ
	for eid, it := range h.items {
		if it.dim != m.dim || it.count <= 0 || now < it.noPickupUntil {
			continue
		}
		if math.Abs(it.x-m.x) > half || math.Abs(it.z-m.z) > half || it.y < m.y-pickupReachY || it.y > m.y+m.box().h {
			continue
		}
		if m.etype == entityPiglin && h.piglinTakesItem(players, m, it) {
			if it.count--; it.count <= 0 {
				delete(h.items, eid)
				h.entityGone(players, it.dim, eid)
			}
			return
		}
		if !mobWantsToPickUp(m, it.item) {
			continue
		}
		taken := h.mobEquipItem(players, m, it.stack())
		if taken == 0 {
			continue
		}
		if it.count -= taken; it.count <= 0 {
			delete(h.items, eid)
			h.entityGone(players, it.dim, eid)
		} else {
			h.refreshItemMeta(players, it)
		}
		h.playSoundDim(players, m.dim, "minecraft:entity.item.pickup", sndNeutral, m.x, m.y, m.z, 0.2, 1)
		h.toTracking(players, m.eid, m.dim, m.x, m.z, equipEv(m.eid, m.heldStack(), invStack{}, m.gear))
		m.persistent = true // setPersistenceRequired: a looter never despawns
		return
	}
}

// mobEquipItem is equipItemIfPossible: the stack goes to its slot if it is
// an upgrade (armour that is not falls back to an empty hand), the replaced
// piece drops at the slot's odds, and the new one becomes a guaranteed drop.
// Returns how many of the stack were taken (0 = not taken).
func (h *hub) mobEquipItem(players map[int32]*tracked, m *mob, st invStack) int {
	if st.item == 0 || st.count <= 0 {
		return 0
	}
	slot := gearSlotHand
	var cur invStack
	var canReplace bool
	if ap, ok := armorInfo[st.item]; ok {
		slot, cur = ap.Slot, m.gear[ap.Slot]
		canReplace = cur.item == 0 || compareArmor(st, cur)
		if !canReplace { // armour it will not wear goes into an empty hand
			slot, cur = gearSlotHand, m.heldStack()
			canReplace = cur.item == 0
		}
	} else {
		cur = m.heldStack()
		canReplace = cur.item == 0 || compareWeapons(m, st, cur)
	}
	if !canReplace {
		return 0
	}
	if cur.item != 0 && math.Max(h.rng.Float64()-replaceDropFudge, 0) < float64(h.gearDropChance(m, slot)) {
		h.dropGearStack(players, m, cur) // spawnAtLocation(current)
	}
	taken := 1
	if slot == gearSlotHand { // EquipmentSlot.limit: a hand takes the whole stack
		taken = st.count
		m.held, m.heldEnch, m.heldDmg, m.heldCount = st.item, st.ench, st.dmg, st.count
	} else {
		m.gear[slot] = invStack{item: st.item, count: 1, dmg: st.dmg, ench: st.ench, name: st.name}
		m.refreshGearArmor() // worn armour is a modifier on ARMOR, re-derived from the slots
	}
	m.gearSure[slot] = true // setItemSlotAndDropWhenKilled
	m.persistent = true
	return taken
}

// gearDropChance is DropChances.byEquipment for one slot: a guaranteed drop
// for what the mob picked up, the spawn roll for what it spawned with, and
// the 8.5 % default otherwise. Looting adds its own percent per level on top
// — the enchantment's `equipment_drops` effect, which is why a looting sword
// pulls armour off mobs more often and not just more drops out of them.
func (h *hub) gearDropChance(m *mob, slot int) float32 {
	base := float32(defaultGearDrop)
	switch {
	case m.gearSure[slot]:
		return 1
	case m.spawnGear:
		base = m.gearDrop
	}
	return base + lootingGearBonus*float32(m.looting)
}

// lootingGearBonus is Looting's equipment_drops contribution, 0.01 per level.
const lootingGearBonus = 0.01

// dropGearStack puts a replaced piece on the ground with its enchantments
// and wear.
func (h *hub) dropGearStack(players map[int32]*tracked, m *mob, st invStack) {
	if it := h.spawnItemIn(players, m.dim, st.item, st.count, m.x, m.y, m.z); it != nil {
		it.ench, it.dmg, it.name = st.ench, st.dmg, st.name
		h.refreshItemMeta(players, it)
	}
}

// compareArmor is Mob.compareArmor: never over a bound piece; more points,
// then more toughness, then the equal-item tie-break.
func compareArmor(newSt, cur invStack) bool {
	if cur.enchLvl(enchBindingCurse) > 0 { // PREVENT_ARMOR_CHANGE
		return false
	}
	n, c := armorInfo[newSt.item], armorInfo[cur.item]
	if n.Points != c.Points {
		return n.Points > c.Points
	}
	if n.Toughness != c.Toughness {
		return n.Toughness > c.Toughness
	}
	return canReplaceEqualItem(newSt, cur)
}

// compareWeapons is Mob.compareWeapons: the species' preferred weapon beats
// anything and is beaten by nothing else; then attack damage; then the
// equal-item tie-break.
func compareWeapons(m *mob, newSt, cur invStack) bool {
	if pref := preferredWeapon(m.etype); pref != 0 {
		if cur.item == pref && newSt.item != pref {
			return false
		}
		if cur.item != pref && newSt.item == pref {
			return true
		}
	}
	n, c := meleeDamage[newSt.item], meleeDamage[cur.item]
	if n != c {
		return n > c
	}
	return canReplaceEqualItem(newSt, cur)
}

// canReplaceEqualItem is Mob.canReplaceEqualItem: more enchantments, else
// less wear, else a name over none.
func canReplaceEqualItem(newSt, cur invStack) bool {
	if n, c := enchCount(newSt.ench), enchCount(cur.ench); n != c {
		return n > c
	}
	if newSt.dmg != cur.dmg {
		return newSt.dmg < cur.dmg
	}
	return newSt.name != "" && cur.name == ""
}

func enchCount(e enchList) int {
	n := 0
	for _, a := range e {
		if a.id != 0 || a.lvl != 0 {
			n++
		}
	}
	return n
}

// mobHeldBonus is the extra melee damage a mob's held weapon adds: the
// item's ATTACK_DAMAGE modifier, which is its meleeDamage less the player's
// own base of 1. Adding the whole meleeDamage made every armed mob hit one
// point too hard (a vindicator 14 for vanilla's 13) until 2026-09-24.
func mobHeldBonus(m *mob) float32 {
	if m.held != 0 {
		if d, ok := meleeDamage[m.held]; ok {
			return float32(d - 1)
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

// mobSharpness is the held weapon's Sharpness bonus: 1.0 + 0.5·(lvl-1).
func mobSharpness(m *mob) float32 {
	if lvl := m.heldStack().enchLvl(enchSharpness); lvl > 0 {
		return 0.5*float32(lvl) + 0.5
	}
	return 0
}
