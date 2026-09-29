package server

import attachproto "github.com/tachyne/tachyne-common/attach"

// EquipmentDispenseItemBehavior: a dispenser facing a living thing puts an
// equippable item on it — the first LivingEntity whose box meets the cell
// ahead and that canEquipWithDispenser. That is any player, an armour
// stand, a mob that may pick up loot (a looting zombie or skeleton, a
// piglin, a pillager, a villager), a pig or strider for a saddle, a happy
// ghast for a harness, and a tame nautilus for its saddle or armour. The
// slot must be empty and the entity alive and adult where the slot asks it.
// A horse, llama or wolf is none of these: canDispenserEquipIntoSlot is
// Mob's canPickUpLoot for them, and they never may.

// dispenseWearSlot is the humanoid armour slot (0 head … 3 feet) an item's
// equippable component names, -1 when it has none: the armour sets, the
// elytra, the heads and the carved pumpkin.
func dispenseWearSlot(item int32) int {
	if s := equipSlotOnUse(item); s >= 0 {
		return s
	}
	switch standSlotFor(item) {
	case attachproto.EquipHead:
		return 0
	case attachproto.EquipChest:
		return 1
	case attachproto.EquipLegs:
		return 2
	case attachproto.EquipFeet:
		return 3
	}
	return -1
}

// wearSlotToStand maps a humanoid armour slot to the stand's equip index.
var wearSlotToStand = [4]int{attachproto.EquipHead, attachproto.EquipChest, attachproto.EquipLegs, attachproto.EquipFeet}

// boxMeetsCell is AABB(pos).intersects for an entity box of half-width hw
// and height ht standing at (x, y, z).
func boxMeetsCell(x, y, z, hw, ht float64, c blockPos) bool {
	return x+hw > float64(c.x) && x-hw < float64(c.x)+1 &&
		z+hw > float64(c.z) && z-hw < float64(c.z)+1 &&
		y+ht > float64(c.y) && y < float64(c.y)+1
}

// mobCanPickUpLoot is Mob.canPickUpLoot: the spawn roll of a zombie or
// skeleton, and the species that are always allowed.
func mobCanPickUpLoot(m *mob) bool {
	switch m.etype {
	case entityPiglin, entityPiglinBrute, entityPillager, entityVillager:
		return true
	}
	return m.canPickup
}

// dispenseEquipment is EquipmentDispenseItemBehavior.dispenseEquipment:
// reports whether one of st went onto something in the cell ahead.
func (h *hub) dispenseEquipment(players map[int32]*tracked, dim int, front blockPos, st *invStack) bool {
	if st.item == 0 || st.count <= 0 {
		return false
	}
	one := *st
	one.count = 1
	wear := dispenseWearSlot(st.item)
	take := func() {
		if st.count--; st.count <= 0 {
			*st = invStack{}
		}
	}
	if wear >= 0 {
		for _, t := range players {
			if t.dim != dim || t.dead || t.health <= 0 || t.gamemode == gmSpectator || t.inv == nil ||
				t.armor[wear].item != 0 || !boxMeetsCell(t.x, t.y, t.z, 0.3, 1.8*t.scale(), front) {
				continue
			}
			t.armor[wear] = one
			take()
			t.inv.stateId++
			t.p.trySendEv(attachproto.WindowSlot{ID: 0, StateID: t.inv.stateId, Slot: int32(5 + wear), Item: stackEv(one)})
			t.refreshArmorAttrs()
			h.broadcastEquipment(players, t)
			h.playSoundDim(players, t.dim, equipSound(one.item), sndPlayer, t.x, t.y, t.z, 1, 1)
			return true
		}
		slot := wearSlotToStand[wear]
		for _, sd := range h.armorStands {
			if sd.dim != dim || sd.equip[slot].item != 0 || !boxMeetsCell(sd.x, sd.y, sd.z, 0.25, 1.975, front) {
				continue
			}
			sd.equip[slot] = one
			take()
			h.toNearbyEv(players, sd.dim, sd.x, sd.z, h.standEquipEv(sd))
			h.playSoundDim(players, sd.dim, equipSound(one.item), sndNeutral, sd.x, sd.y, sd.z, 1, 1)
			return true
		}
	}
	for _, m := range h.mobs {
		if m.dim != dim || m.dying > 0 || m.health <= 0 {
			continue
		}
		b := m.box()
		if !boxMeetsCell(m.x, m.y, m.z, b.w/2, b.h, front) {
			continue
		}
		if h.dispenseOnto(players, m, one, wear) {
			take()
			return true
		}
	}
	return false
}

// dispenseOnto is canEquipWithDispenser and setItemSlot for one mob.
func (h *hub) dispenseOnto(players map[int32]*tracked, m *mob, one invStack, wear int) bool {
	switch {
	case wear >= 0:
		if !mobCanPickUpLoot(m) || m.gear[wear].item != 0 {
			return false
		}
		m.gear[wear] = one
		m.refreshGearArmor()
		m.gearSure[wear] = true // setGuaranteedDrop
		m.persistent = true     // setPersistenceRequired
		h.toTracking(players, m.eid, m.dim, m.x, m.z, equipEv(m.eid, m.heldStack(), invStack{}, m.gear))
		h.playSoundDim(players, m.dim, equipSound(one.item), sndHostile, m.x, m.y, m.z, 1, 1)
		return true
	case one.item == itemSaddle:
		switch m.etype {
		case entityPig, entityStrider:
			// Pig/Strider.canUseSlot(SADDLE): alive and not a baby; their
			// canDispenserEquipIntoSlot always takes a saddle.
			if m.baby || m.saddled {
				return false
			}
			m.saddled = true
			m.saddleSt = one
			m.persistent = true
			h.toTracking(players, m.eid, m.dim, m.x, m.z, saddleEquip(m.eid))
			h.playSoundDim(players, m.dim, "minecraft:entity."+entityNameByID[m.etype]+".saddle", sndNeutral, m.x, m.y, m.z, 1, 1)
			return true
		case entityNautilus:
			if m.baby || !m.tamed || m.saddled {
				return false
			}
			m.saddleSt = one
			m.persistent = true
			h.horseEquipSync(players, m)
			snd := "minecraft:item.nautilus_saddle_equip" // AbstractNautilus.getEquipSound
			if h.inWater(m.dim, m.x, m.y, m.z) {
				snd = "minecraft:item.nautilus_saddle_underwater_equip"
			}
			h.playSoundDim(players, m.dim, snd, sndNeutral, m.x, m.y, m.z, 1, 1)
			return true
		}
	case isHarness(one.item):
		if m.etype != entityHappyGhast || m.baby || m.harness != 0 {
			return false
		}
		m.harness = one.item
		m.persistent = true
		h.toTracking(players, m.eid, m.dim, m.x, m.z, ghastHarnessEquip(m.eid, m.harness))
		h.playSoundDim(players, m.dim, harnessEquipSound, sndNeutral, m.x, m.y, m.z, 1, 1)
		return true
	case nautilusArmorItems[one.item]:
		if m.etype != entityNautilus || m.baby || !m.tamed || m.armorSt.item != 0 {
			return false
		}
		m.armorSt = one
		m.persistent = true
		m.refreshGearArmor()
		h.horseEquipSync(players, m)
		h.playSoundDim(players, m.dim, "minecraft:item.armor.equip_nautilus", sndNeutral, m.x, m.y, m.z, 1, 1)
		return true
	}
	return false
}

// wearDispensed is ItemStack.hurtAndBreak with no player: Unbreaking spares
// each point with chance lvl/(lvl+1), and a worn-out tool is simply gone.
func (h *hub) wearDispensed(st *invStack, n int) {
	max, ok := wearMax(*st)
	if !ok || max <= 0 {
		return
	}
	if lvl := st.enchLvl(enchUnbreaking); lvl > 0 {
		n = h.unbreakingKept(n, float64(lvl)/float64(lvl+1))
	}
	if st.dmg += n; st.dmg >= max {
		*st = invStack{}
	}
}

// dispenseEquippable is getDefaultDispenseMethod's EQUIPPABLE test for the
// items no behaviour of their own claims: armour, heads, the elytra,
// saddles, harnesses and body armour. With nothing to wear it, each is
// tossed like any item.
func dispenseEquippable(item int32) bool {
	return dispenseWearSlot(item) >= 0 || item == itemSaddle || isHarness(item) ||
		nautilusArmorItems[item] || horseArmorItems[item] || carpetItems[item] || item == itemWolfArmor
}

// cubeInCell is FlintAndSteelDispenseItemBehavior.tryIgniteExplosiveEntities'
// search: the first sulfur cube in the cell that canExplode.
func (h *hub) cubeInCell(dim int, c blockPos) *mob {
	for _, m := range h.mobs {
		if m.dim != dim || m.etype != entitySulfurCube || !m.canExplode() {
			continue
		}
		b := m.box()
		if boxMeetsCell(m.x, m.y, m.z, b.w/2, b.h, c) {
			return m
		}
	}
	return nil
}
