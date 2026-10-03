package server

import (
	attachproto "github.com/tachyne/tachyne-common/attach"
)

// A mob's slots (Entity.getSlot over a living mob), what /item, /loot and
// /execute if items read and write:
//
//	98 / 99           the main and off hand          (LivingEntity: any item)
//	100–103           feet, legs, chest, head        (chest/legs/feet take only their own piece)
//	105               the body (horse and wolf armour, a llama's carpet, a happy ghast's harness)
//	106               the saddle
//	300–307           a villager's, a wandering trader's or a piglin's inventory
//	                  (AbstractVillager, Piglin: SimpleContainer(8))
//	499               a donkey's, mule's or llama's chest (AbstractChestedHorse)
//	500+              that chest's contents (AbstractHorse.inventory)
//
// An allay's liked item is its main hand. The body, saddle and armour
// slots refuse a stack that does not equip there
// (createEquipmentSlotAccess's filter: getEquipmentSlotForItem).

// mobItemTarget is the slot holder for a mob.
func (h *hub) mobItemTarget(m *mob) itemTarget {
	eq := func(get func() invStack, put func(invStack), fits func(invStack) bool) *slotAccess {
		return &slotAccess{get: get, set: func(st invStack) bool {
			if st.item == 0 || st.count <= 0 {
				st = invStack{}
			}
			if st.item != 0 && fits != nil && !fits(st) {
				return false
			}
			put(st)
			return true
		}}
	}
	return itemTarget{name: cmdEntity{m: m}.name(),
		slotAt: func(id int) *slotAccess {
			switch {
			case id == 98:
				return eq(m.heldStack, m.setHeld, nil)
			case id == 99:
				return eq(func() invStack { return m.offhand }, func(st invStack) { m.offhand = st }, nil)
			case id == 103: // the head takes anything
				return eq(func() invStack { return m.gear[0] }, func(st invStack) { m.gear[0] = st; m.refreshGearArmor() }, nil)
			case id >= 100 && id <= 102:
				slot := 103 - id
				return eq(func() invStack { return m.gear[slot] }, func(st invStack) { m.gear[slot] = st; m.refreshGearArmor() },
					func(st invStack) bool { return equipSlotOnUse(st.item) == slot })
			case id == 105:
				if m.etype == entityHappyGhast {
					return eq(func() invStack {
						if m.harness == 0 {
							return invStack{}
						}
						return invStack{item: m.harness, count: 1}
					}, func(st invStack) { m.harness = st.item }, func(st invStack) bool { return isHarness(st.item) })
				}
				return eq(func() invStack { return m.armorSt },
					func(st invStack) { m.armorSt = st; m.refreshGearArmor() },
					func(st invStack) bool {
						return bodyArmorFor(m.etype, st.item) || (m.etype == entityWolf && st.item == itemWolfArmor)
					})
			case id == 106:
				return eq(func() invStack {
					switch {
					case m.saddleSt.item != 0:
						return m.saddleSt
					case m.saddled:
						return invStack{item: itemSaddle, count: 1}
					}
					return invStack{}
				}, func(st invStack) { m.saddleSt, m.saddled = st, st.item != 0 },
					func(st invStack) bool { return st.item == itemSaddle && mobSaddleable(m) })
			case id >= 300 && id < 300+villagerInvSlots && mobHasInventory(m):
				i := id - 300
				return &slotAccess{get: func() invStack {
					if i < len(m.hoard) {
						return m.hoard[i]
					}
					return invStack{}
				}, set: func(st invStack) bool {
					if st.item == 0 || st.count <= 0 {
						st = invStack{}
					}
					for len(m.hoard) <= i {
						m.hoard = append(m.hoard, invStack{})
					}
					m.hoard[i] = st
					for len(m.hoard) > 0 && m.hoard[len(m.hoard)-1].item == 0 { // the list keeps no trailing gaps
						m.hoard = m.hoard[:len(m.hoard)-1]
					}
					return true
				}}
			case id == 499 && chestedFamily(m.etype):
				return &slotAccess{get: func() invStack {
					if m.chested {
						return invStack{item: int32(itemByName["chest"]), count: 1}
					}
					return invStack{}
				}, set: func(st invStack) bool { // AbstractChestedHorse.getSlot(499)
					switch {
					case st.item == 0 || st.count <= 0:
						if m.chested {
							m.chested, m.chest = false, nil
						}
						return true
					case st.item == int32(itemByName["chest"]):
						if !m.chested {
							m.chested = true
							if m.strength == 0 {
								m.strength = 3
							}
							m.chest = make([]invStack, horseColumns(m)*3)
						}
						return true
					}
					return false
				}}
			case id >= 500 && id-500 < len(m.chest) && m.chested:
				return stackSlot(&m.chest[id-500], false)
			}
			return nil
		},
		changed: func(players map[int32]*tracked) { h.mobEquipmentChanged(players, m) },
	}
}

// mobHasInventory: the mobs with an 8-slot inventory at 300+.
func mobHasInventory(m *mob) bool {
	switch m.etype {
	case entityVillager, entityWanderingTrader, entityPiglin:
		return true
	}
	return false
}

// mobSaddleable is whether a saddle equips on the mob (its SADDLE slot
// exists: the horse family, pigs, striders, camels, nautiluses).
func mobSaddleable(m *mob) bool {
	if horseFamily(m.etype) && m.etype != entityLlama && m.etype != entityTraderLlama {
		return true
	}
	switch m.etype {
	case entityPig, entityStrider, entityNautilus, entityID("zombie_nautilus"):
		return true
	}
	return false
}

// mobEquipmentChanged re-sends every equipment slot of the mob to its
// viewers (the per-slot sync of LivingEntity.detectEquipmentUpdates).
func (h *hub) mobEquipmentChanged(players map[int32]*tracked, m *mob) {
	m.persistent = true
	e := equipEv(m.eid, m.heldStack(), m.offhand, m.gear)
	if m.etype == entityHappyGhast {
		if m.harness != 0 {
			e.Slots[attachproto.EquipBody] = stackEv(invStack{item: m.harness, count: 1})
		}
	} else {
		e.Slots[attachproto.EquipBody] = stackEv(m.armorSt)
	}
	if mobSaddleable(m) {
		if m.saddled {
			sd := m.saddleSt
			if sd.item == 0 {
				sd = invStack{item: itemSaddle, count: 1}
			}
			e.Slots[attachproto.EquipSaddle] = stackEv(sd)
		}
		e.SendSaddle = true
	}
	h.toNearbyEv(players, m.dim, m.x, m.z, e)
}
